package gateway

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magnaflowlabs/imcodex/internal/tmuxctl"
)

type fakeMessenger struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeMessenger) SendTextToChat(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.texts = append(f.texts, text)
	return nil
}

func (f *fakeMessenger) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.texts))
	copy(out, f.texts)
	return out
}

type fakeConsole struct {
	mu        sync.Mutex
	captures  []string
	sendTexts []string
}

func (f *fakeConsole) EnsureSession(context.Context, tmuxctl.SessionSpec) (bool, error) {
	return true, nil
}

func (f *fakeConsole) SendText(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendTexts = append(f.sendTexts, text)
	return nil
}

func (f *fakeConsole) Capture(context.Context, string, int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.captures) == 0 {
		return "", nil
	}
	out := f.captures[0]
	if len(f.captures) > 1 {
		f.captures = f.captures[1:]
	}
	return out, nil
}

func TestServiceBridgesMessageToConsole(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	console := &fakeConsole{
		captures: []string{
			"",
			"• Working (1s • esc to interrupt)",
			"• Hel",
			"• Hello\n\n  gpt-5.4 xhigh · 100% left · /srv/demo",
			"• Hello\n\n  gpt-5.4 xhigh · 100% left · /srv/demo",
		},
	}
	messenger := &fakeMessenger{}

	svc := NewService(ctx, Options{GroupID: "oc_1", CWD: "/srv/demo", SessionName: "imcodex-demo"}, messenger, console, slog.Default())
	svc.pollEvery = 5 * time.Millisecond
	svc.history = 2000
	svc.startWait = 0
	if err := svc.HandleMessage(context.Background(), IncomingMessage{
		GroupID: "oc_1",
		Text:    "Just answer hello",
	}); err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		texts := messenger.all()
		joined := strings.Join(texts, "\n")
		if strings.Contains(joined, "Codex is processing") && strings.Contains(joined, "Hello") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("messages = %#v, want processing + output", messenger.all())
}

func TestServiceRejectsUnknownGroup(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	messenger := &fakeMessenger{}
	console := &fakeConsole{}
	svc := NewService(ctx, Options{GroupID: "oc_1", CWD: "/srv/demo", SessionName: "imcodex-demo"}, messenger, console, slog.Default())

	if err := svc.HandleMessage(context.Background(), IncomingMessage{
		GroupID: "oc_other",
		Text:    "hello",
	}); err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}

	texts := messenger.all()
	if len(texts) != 0 {
		t.Fatalf("messages = %#v, want ignored unknown-group message", texts)
	}
}

func TestServiceDoesNotDispatchSecondMessageImmediately(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	console := &fakeConsole{captures: []string{""}}
	messenger := &fakeMessenger{}

	svc := NewService(ctx, Options{GroupID: "oc_1", CWD: "/srv/demo", SessionName: "imcodex-demo"}, messenger, console, slog.Default())
	svc.pollEvery = 50 * time.Millisecond
	svc.history = 2000
	svc.startWait = 0

	if err := svc.HandleMessage(context.Background(), IncomingMessage{
		GroupID: "oc_1",
		Text:    "first",
	}); err != nil {
		t.Fatalf("HandleMessage(first) error = %v", err)
	}
	if err := svc.HandleMessage(context.Background(), IncomingMessage{
		GroupID: "oc_1",
		Text:    "second",
	}); err != nil {
		t.Fatalf("HandleMessage(second) error = %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	console.mu.Lock()
	gotNow := append([]string(nil), console.sendTexts...)
	console.mu.Unlock()
	if len(gotNow) != 1 || gotNow[0] != "first" {
		t.Fatalf("immediate sendTexts = %#v, want only first message dispatched", gotNow)
	}

	time.Sleep(80 * time.Millisecond)

	console.mu.Lock()
	gotLater := append([]string(nil), console.sendTexts...)
	console.mu.Unlock()
	if len(gotLater) < 2 || gotLater[1] != "second" {
		t.Fatalf("later sendTexts = %#v, want second message dispatched after poll", gotLater)
	}
}
