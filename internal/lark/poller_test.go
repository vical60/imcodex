package lark

import (
	"context"
	"testing"

	"github.com/magnaflowlabs/imcodex/internal/gateway"
)

func TestPollerPollGroupForwardsOnlyNewMessages(t *testing.T) {
	t.Parallel()

	handler := &recordingHandler{}
	lister := &fakeMessageLister{
		messages: []ListedMessage{
			{
				Message:         gateway.IncomingMessage{GroupID: "oc_1", MessageID: "om_new"},
				CreatedAtMillis: 2_000,
			},
		},
	}

	poller := NewPoller(lister, []string{"oc_1"}, handler, nil)
	poller.cursors["oc_1"] = pollCursor{
		createdAtMillis: 1_000,
		messageIDs:      map[string]struct{}{"om_old": {}},
	}

	if err := poller.pollGroup(context.Background(), "oc_1"); err != nil {
		t.Fatalf("pollGroup() error = %v", err)
	}
	if len(handler.messages) != 1 {
		t.Fatalf("handler.messages = %d, want 1", len(handler.messages))
	}
	if got := handler.messages[0].MessageID; got != "om_new" {
		t.Fatalf("handler.messages[0].MessageID = %q, want om_new", got)
	}
	if got := poller.cursors["oc_1"].createdAtMillis; got != 2_000 {
		t.Fatalf("cursor.createdAtMillis = %d, want 2000", got)
	}
}

type fakeMessageLister struct {
	messages []ListedMessage
}

func (f *fakeMessageLister) ListChatMessagesSince(context.Context, string, int64) ([]ListedMessage, error) {
	return f.messages, nil
}

type recordingHandler struct {
	messages []gateway.IncomingMessage
}

func (h *recordingHandler) HandleMessage(_ context.Context, msg gateway.IncomingMessage) error {
	h.messages = append(h.messages, msg)
	return nil
}
