package whatsapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	goclawprotocol "github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const qrSessionTimeout = 3 * time.Minute

// Structured failure reasons emitted in the whatsapp.qr.done event payload so
// the UI can render an actionable message + the correct next step instead of
// surfacing whatsmeow's raw internal error string.
const (
	// qrReasonAlreadyPairedDisconnected: device holds a paired identity
	// (Store.ID != nil) but is disconnected; QR needs a re-link (force reauth)
	// to clear the session first.
	qrReasonAlreadyPairedDisconnected = "already_paired_disconnected"
	// qrReasonSessionClearFailed: Reauth could not delete the paired device
	// store; the QR flow was aborted rather than calling GetQRChannel against
	// a populated store.
	qrReasonSessionClearFailed = "session_clear_failed"
	// qrReasonStartFailed: any other failure starting the QR flow.
	qrReasonStartFailed = "qr_start_failed"
	// qrReasonTimeout: the QR session was still active but the qrSessionTimeout
	// deadline lapsed, OR whatsmeow emitted a QRChannelTimeout (Disconnected).
	qrReasonTimeout = "timeout"
	// qrReasonClosed: the QR channel closed without a recognised terminal event.
	qrReasonClosed = "closed"
	// qrReasonUnexpectedState: whatsmeow emitted QRChannelErrUnexpectedEvent
	// (ConnectFailure / Connected / LoggedOut / TemporaryBan during pairing).
	qrReasonUnexpectedState = "unexpected_state"
	// qrReasonPairFailed: whatsmeow emitted a PairError (QRChannelEventError).
	qrReasonPairFailed = "pair_failed"
	// qrReasonClientOutdated: whatsmeow emitted QRChannelClientOutdated —
	// WhatsApp rejected the embedded client version (HTTP 405). Fix is to
	// upgrade whatsmeow (and thus GoClaw), not an operator action.
	qrReasonClientOutdated = "client_outdated"

	// qrChanEventError mirrors whatsmeow's QRChannelEventError ("error") — a
	// pair error item whose Error field carries the failure.
	qrChanEventError = "error"
	// qrChanErrClientOutdated mirrors whatsmeow's QRChannelClientOutdated.
	qrChanErrClientOutdated = "err-client-outdated"
)

// cancelEntry wraps a CancelFunc so it can be stored in sync.Map.CompareAndDelete.
type cancelEntry struct {
	cancel context.CancelFunc
}

// QRMethods handles whatsapp.qr.start — delivers QR codes to the UI wizard.
type QRMethods struct {
	instanceStore  store.ChannelInstanceStore
	manager        *channels.Manager
	activeSessions sync.Map // instanceID (string) -> *cancelEntry
}

func NewQRMethods(instanceStore store.ChannelInstanceStore, manager *channels.Manager) *QRMethods {
	return &QRMethods{instanceStore: instanceStore, manager: manager}
}

func (m *QRMethods) Register(router *gateway.MethodRouter) {
	router.Register(goclawprotocol.MethodWhatsAppQRStart, m.handleQRStart)
}

func (m *QRMethods) handleQRStart(ctx context.Context, client *gateway.Client, req *goclawprotocol.RequestFrame) {
	var params struct {
		InstanceID  string `json:"instance_id"`
		ForceReauth bool   `json:"force_reauth"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInvalidRequest, "invalid instance_id"))
		return
	}

	inst, err := m.instanceStore.Get(ctx, instID)
	if err != nil || inst.ChannelType != channels.TypeWhatsApp {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "whatsapp instance not found"))
		return
	}

	qrCtx, cancel := context.WithTimeout(ctx, qrSessionTimeout)
	entry := &cancelEntry{cancel: cancel}

	// Cancel any previous QR session for this instance.
	if prev, loaded := m.activeSessions.Swap(params.InstanceID, entry); loaded {
		if prevEntry, ok := prev.(*cancelEntry); ok {
			prevEntry.cancel()
		}
	}

	// ACK immediately — QR/done events arrive asynchronously.
	client.SendResponse(goclawprotocol.NewOKResponse(req.ID, map[string]any{"status": "started"}))

	go m.runQRSession(qrCtx, entry, client, params.InstanceID, inst.Name, params.ForceReauth)
}

func (m *QRMethods) runQRSession(ctx context.Context, entry *cancelEntry,
	client *gateway.Client, instanceIDStr, channelName string, forceReauth bool) {

	defer entry.cancel()
	defer m.activeSessions.CompareAndDelete(instanceIDStr, entry)

	// Wait for channel to appear in manager — instance creation triggers an async
	// reload, so the channel may not be registered yet when the wizard fires QR start.
	var wa *Channel
	for range 10 {
		if ch, ok := m.manager.GetChannel(channelName); ok {
			if w, ok := ch.(*Channel); ok {
				wa = w
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	if wa == nil {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"error":       "channel not found",
			},
		})
		return
	}

	// Already authenticated and no force-reauth → signal connected.
	if wa.IsAuthenticated() && !forceReauth {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id":       instanceIDStr,
				"success":           true,
				"already_connected": true,
			},
		})
		return
	}

	// Force reauth: clear session and prepare for fresh QR.
	if forceReauth {
		if err := wa.Reauth(); err != nil {
			slog.Warn("whatsapp QR: reauth failed", "error", err)
			client.SendEvent(goclawprotocol.EventFrame{
				Type:  goclawprotocol.FrameTypeEvent,
				Event: goclawprotocol.EventWhatsAppQRDone,
				Payload: map[string]any{
					"instance_id": instanceIDStr,
					"success":     false,
					"reason":      qrReasonSessionClearFailed,
					"error":       err.Error(),
				},
			})
			return
		}
	}

	// Deliver cached QR if available.
	if cached := wa.GetLastQRB64(); cached != "" {
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRCode,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"png_b64":     cached,
			},
		})
	}

	// Start QR flow — get QR channel from whatsmeow.
	qrChan, err := wa.StartQRFlow(ctx)
	if err != nil {
		// Map known failures to a structured reason the UI can act on; never
		// leak whatsmeow's raw error (e.g. ErrQRStoreContainsID) to the client.
		reason := qrReasonStartFailed
		if errors.Is(err, ErrAlreadyPairedDisconnected) {
			reason = qrReasonAlreadyPairedDisconnected
		}
		slog.Warn("whatsapp QR: start flow failed", "error", err, "reason", reason)
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id": instanceIDStr,
				"success":     false,
				"reason":      reason,
				"error":       err.Error(),
			},
		})
		return
	}

	if qrChan == nil {
		// Already authenticated (StartQRFlow returned nil).
		client.SendEvent(goclawprotocol.EventFrame{
			Type:  goclawprotocol.FrameTypeEvent,
			Event: goclawprotocol.EventWhatsAppQRDone,
			Payload: map[string]any{
				"instance_id":       instanceIDStr,
				"success":           true,
				"already_connected": true,
			},
		})
		return
	}

	// Process QR events from whatsmeow.
	for {
		select {
		case <-ctx.Done():
			// ctx is cancelled for two reasons: (a) a newer whatsapp.qr.start
			// took over (Swap replaced our entry and called our cancel), or
			// (b) the qrSessionTimeout deadline lapsed. (a) is a supersede —
			// the newer session owns the UI now, so exit silently. Only (b) is
			// a real timeout worth surfacing. Without this check, every rapid
			// retry emitted a false "QR session timed out" from the session it
			// superseded, so no QR ever rendered.
			if v, ok := m.activeSessions.Load(instanceIDStr); ok && v != entry {
				return // superseded by a newer QR session — silent exit
			}
			client.SendEvent(goclawprotocol.EventFrame{
				Type:  goclawprotocol.FrameTypeEvent,
				Event: goclawprotocol.EventWhatsAppQRDone,
				Payload: map[string]any{
					"instance_id": instanceIDStr,
					"success":     false,
					"reason":      qrReasonTimeout,
					"error":       "QR session timed out — restart to try again",
				},
			})
			return

		case evt, ok := <-qrChan:
			if !ok {
				// Channel closed without a terminal event we recognised. Surface
				// a failure so the UI doesn't wait forever.
				slog.Warn("whatsapp QR: channel closed without terminal event", "instance", instanceIDStr)
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     false,
						"reason":      qrReasonClosed,
						"error":       "QR session ended unexpectedly — restart to try again",
					},
				})
				return
			}

			switch evt.Event {
			case "code":
				png, qrErr := qrcode.Encode(evt.Code, qrcode.Medium, 256)
				if qrErr != nil {
					slog.Warn("whatsapp: QR PNG encode failed", "error", qrErr)
					continue
				}
				pngB64 := base64.StdEncoding.EncodeToString(png)

				wa.cacheQR(pngB64)

				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRCode,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"png_b64":     pngB64,
					},
				})

			case "success":
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     true,
					},
				})
				slog.Info("whatsapp QR session completed", "instance", instanceIDStr)
				return

			case "timeout":
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     false,
						"reason":      qrReasonTimeout,
						"error":       "QR code expired — restart to try again",
					},
				})
				return

			default:
				// err-unexpected-state (ConnectFailure/Connected/LoggedOut/TemporaryBan),
				// err-client-outdated, err-scanned-without-multidevice, or an
				// "error" item (PairError). whatsmeow closes the channel right
				// after emitting these; without this branch the UI waited forever.
				reason := qrReasonUnexpectedState
				msg := "WhatsApp ended the QR session unexpectedly — restart to try again"
				switch evt.Event {
				case qrChanErrClientOutdated:
					// WhatsApp rejected the embedded client version (HTTP 405).
					// The fix is upgrading whatsmeow/GoClaw, not retrying.
					reason = qrReasonClientOutdated
					msg = "WhatsApp rejected the gateway client version (outdated). Update GoClaw to the latest release and restart, then try again."
				case qrChanEventError:
					if evt.Error != nil {
						reason = qrReasonPairFailed
						msg = evt.Error.Error()
					}
				}
				slog.Warn("whatsapp QR: channel ended", "event", evt.Event,
					"error", evt.Error, "instance", instanceIDStr)
				client.SendEvent(goclawprotocol.EventFrame{
					Type:  goclawprotocol.FrameTypeEvent,
					Event: goclawprotocol.EventWhatsAppQRDone,
					Payload: map[string]any{
						"instance_id": instanceIDStr,
						"success":     false,
						"reason":      reason,
						"error":       msg,
					},
				})
				return
			}
		}
	}
}
