//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteListenRawMessageStore_ListTextFilters verifies the substring text
// filters (Chat/Sender/Body) are applied server-side to BOTH the data query and
// the COUNT — so `total` matches the filtered rows — and that chat matches
// chat_name OR chat_id, sender matches sender OR sender_id, and matching is
// case-insensitive. Empty values are no-ops. See SRS 008 FR-00/FR-04.
func TestSQLiteListenRawMessageStore_ListTextFilters(t *testing.T) {
	s, ctx, _ := newListenRawTestStore(t)

	mk := func(chatName, chatID, sender, senderID, body string) store.ListenRawMessage {
		return store.ListenRawMessage{
			ID:           uuid.Must(uuid.NewV7()),
			ChannelName:  "test-channel",
			ChatID:       chatID,
			ChatName:     chatName,
			GraphID:      "graph-1",
			Sender:       sender,
			SenderID:     senderID,
			Body:         body,
			MsgTimestamp: time.Now(),
			AgentID:      "agent-1",
		}
	}

	rows := []store.ListenRawMessage{
		mk("Alpha Team", "1203-alpha@g.us", "Alice", "alice@s.whatsapp.net", "deploy the service"),
		mk("Beta Squad", "1203-beta@g.us", "Bob", "bob@s.whatsapp.net", "review the PR"),
		mk("Gamma Cell", "1203-gamma@g.us", "Carol", "carol@s.whatsapp.net", "DEPLOY failed"),
	}
	if err := s.AppendBatch(ctx, rows); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	ids := func(msgs []store.ListenRawMessage) map[string]struct{} {
		out := make(map[string]struct{}, len(msgs))
		for _, m := range msgs {
			out[m.ID.String()] = struct{}{}
		}
		return out
	}
	wantSet := func(which ...int) map[string]struct{} {
		out := make(map[string]struct{}, len(which))
		for _, i := range which {
			out[rows[i].ID.String()] = struct{}{}
		}
		return out
	}

	cases := []struct {
		name   string
		opts   store.ListenRawMessageListOpts
		want   map[string]struct{}
		wantN  int
	}{
		{"no filter returns all", store.ListenRawMessageListOpts{}, wantSet(0, 1, 2), 3},
		{"chat by name (ci)", store.ListenRawMessageListOpts{Chat: "alpha"}, wantSet(0), 1},
		{"chat by id prefix", store.ListenRawMessageListOpts{Chat: "1203"}, wantSet(0, 1, 2), 3},
		{"chat by name upper", store.ListenRawMessageListOpts{Chat: "BETA"}, wantSet(1), 1},
		{"sender by name", store.ListenRawMessageListOpts{Sender: "ali"}, wantSet(0), 1},
		{"sender by id suffix", store.ListenRawMessageListOpts{Sender: "s.whatsapp.net"}, wantSet(0, 1, 2), 3},
		{"body ci matches two", store.ListenRawMessageListOpts{Body: "deploy"}, wantSet(0, 2), 2},
		{"body no match", store.ListenRawMessageListOpts{Body: "zzznomatch"}, map[string]struct{}{}, 0},
		{"chat+body combined", store.ListenRawMessageListOpts{Chat: "alpha", Body: "deploy"}, wantSet(0), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, total, err := s.List(ctx, tc.opts)
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if total != tc.wantN {
				t.Errorf("total: got %d want %d (filter must drive the COUNT, not just the page)", total, tc.wantN)
			}
			if len(got) != tc.wantN {
				t.Fatalf("rows: got %d want %d", len(got), tc.wantN)
			}
			gotIDs := ids(got)
			for id := range tc.want {
				if _, ok := gotIDs[id]; !ok {
					t.Errorf("expected id %s in results, not found", id)
				}
			}
			if len(gotIDs) != len(tc.want) {
				t.Errorf("result id set size: got %d want %d (extra/missing rows)", len(gotIDs), len(tc.want))
			}
		})
	}
}

// TestSQLiteListenRawMessageStore_ListTextFilters_TenantIsolation verifies the
// text predicates are AND-ed inside the existing tenant-scoped WHERE, so a text
// filter cannot surface another tenant's rows. See SRS 008 FR-04/FR-08.
func TestSQLiteListenRawMessageStore_ListTextFilters_TenantIsolation(t *testing.T) {
	s, ctxMaster, _ := newListenRawTestStore(t)
	ctxOther := store.WithTenantID(context.Background(), uuid.New()) // never inserted

	m := store.ListenRawMessage{
		ID:           uuid.Must(uuid.NewV7()),
		ChannelName:  "test-channel",
		ChatID:       "1203-alpha@g.us",
		ChatName:     "Alpha Team",
		GraphID:      "graph-1",
		Sender:       "Alice",
		SenderID:     "alice@s.whatsapp.net",
		Body:         "deploy the service",
		MsgTimestamp: time.Now(),
		AgentID:      "agent-1",
	}
	if err := s.AppendBatch(ctxMaster, []store.ListenRawMessage{m}); err != nil {
		t.Fatalf("AppendBatch: %v", err)
	}

	got, total, err := s.List(ctxOther, store.ListenRawMessageListOpts{Chat: "alpha"})
	if err != nil {
		t.Fatalf("List cross-tenant: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Fatalf("cross-tenant text filter must return 0 rows, got total=%d rows=%d", total, len(got))
	}
}
