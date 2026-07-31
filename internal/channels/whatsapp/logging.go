package whatsapp

import (
	"fmt"
	"log/slog"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// slogWhatsAppLogger adapts the gateway's *log/slog.Logger to whatsmeow's
// waLog.Logger interface.
//
// whatsmeow accepts a waLog.Logger at client construction (NewClient). Passing
// nil selects the built-in noop logger, which silently swallows ALL of
// whatsmeow's internal diagnostics — connection failures, QR-generation
// errors, pair errors, stream errors. That made QR-pairing stalls present as
// "waiting forever" with no clue (SRS 013 Mode 5). This adapter routes those
// diagnostics into the gateway log (under component=whatsmeow + the channel
// name) so failures are observable. The gateway's configured log level still
// gates verbosity (e.g. Debugf is dropped at INFO).
type slogWhatsAppLogger struct {
	l *slog.Logger
}

func (s slogWhatsAppLogger) Debugf(format string, args ...any) {
	s.l.Debug(fmt.Sprintf(format, args...))
}

func (s slogWhatsAppLogger) Infof(format string, args ...any) {
	s.l.Info(fmt.Sprintf(format, args...))
}

func (s slogWhatsAppLogger) Warnf(format string, args ...any) {
	s.l.Warn(fmt.Sprintf(format, args...))
}

func (s slogWhatsAppLogger) Errorf(format string, args ...any) {
	s.l.Error(fmt.Sprintf(format, args...))
}

// Sub returns a child logger tagged with the whatsmeow sub-module (e.g.
// "QRChannel", "keepalive"), matching whatsmeow's internal Log.Sub() usage.
func (s slogWhatsAppLogger) Sub(module string) waLog.Logger {
	return slogWhatsAppLogger{l: s.l.With("wa_sub", module)}
}

// whatsmeowLogger builds a waLog.Logger scoped to this channel. Use it for
// every whatsmeow.NewClient call so the client's internal diagnostics surface.
func (c *Channel) whatsmeowLogger() waLog.Logger {
	return slogWhatsAppLogger{l: slog.With("component", "whatsmeow", "channel", c.Name())}
}
