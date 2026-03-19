package gateway

import (
	"context"
	"log/slog"
	"testing"
)

func TestRouterRoutesKnownGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messenger := &fakeMessenger{}
	console := &fakeConsole{}
	router, err := NewRouter(ctx, []Options{
		{GroupID: "oc_1", CWD: "/srv/a"},
		{GroupID: "oc_2", CWD: "/srv/b"},
	}, messenger, console, slog.Default())
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	if err := router.HandleMessage(context.Background(), IncomingMessage{
		GroupID: "oc_2",
		Text:    "hello",
	}); err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	if router.GroupCount() != 2 {
		t.Fatalf("GroupCount() = %d, want 2", router.GroupCount())
	}
}

func TestRouterRejectsDuplicateGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := NewRouter(ctx, []Options{
		{GroupID: "oc_1", CWD: "/srv/a"},
		{GroupID: "oc_1", CWD: "/srv/b"},
	}, &fakeMessenger{}, &fakeConsole{}, slog.Default())
	if err == nil {
		t.Fatal("NewRouter() error = nil, want duplicate group error")
	}
}
