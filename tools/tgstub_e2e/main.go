package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magnaflowlabs/imcodex/internal/gateway"
	"github.com/magnaflowlabs/imcodex/internal/telegram"
	"github.com/magnaflowlabs/imcodex/internal/tmuxctl"
)

type config struct {
	groupID         string
	cwd             string
	session         string
	prompt          string
	promptFile      string
	timeout         time.Duration
	readyTimeout    time.Duration
	stableWindow    time.Duration
	history         int
	requireExisting bool
	token           string
	send429Count    int
	edit429Count    int
	retryAfterSec   int
}

func main() {
	cfg, err := parseFlags()
	if err != nil {
		log.Fatal(err)
	}

	if cfg.promptFile != "" {
		data, err := os.ReadFile(cfg.promptFile)
		if err != nil {
			log.Fatalf("read prompt file failed: %v", err)
		}
		cfg.prompt = strings.TrimSpace(string(data))
	}
	if strings.TrimSpace(cfg.prompt) == "" {
		cfg.prompt = "请先思考至少90秒且不要输出任何正文；思考完成后仅输出一行：E2E-STUB-CHECK-DONE"
	}
	if strings.TrimSpace(cfg.session) == "" {
		cfg.session = gateway.DefaultSessionNameForGroup(cfg.groupID, cfg.cwd)
	}

	chatID, err := strconv.ParseInt(strings.TrimSpace(cfg.groupID), 10, 64)
	if err != nil {
		log.Fatalf("invalid group id %q: %v", cfg.groupID, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	if cfg.requireExisting {
		ok, err := hasTmuxSession(ctx, cfg.session)
		if err != nil {
			log.Fatalf("check tmux session failed: %v", err)
		}
		if !ok {
			log.Fatalf("tmux session %q not found (start codex in tmux first, or use -require-existing=false)", cfg.session)
		}
	}

	console := tmuxctl.New()
	if _, err := console.EnsureSession(ctx, tmuxctl.SessionSpec{
		SessionName:                 cfg.session,
		CWD:                         cfg.cwd,
		StartupWait:                 2 * time.Second,
		AutoPressEnterOnTrustPrompt: true,
	}); err != nil {
		log.Fatalf("ensure tmux session failed: %v", err)
	}

	if err := waitForReadyPane(ctx, console, cfg.session, cfg.history, cfg.readyTimeout); err != nil {
		log.Fatalf("wait tmux/codex ready failed: %v", err)
	}

	baseRaw, err := console.Capture(ctx, cfg.session, cfg.history)
	if err != nil {
		log.Fatalf("capture tmux baseline failed: %v", err)
	}
	baseNorm := tmuxctl.NormalizeSnapshot(baseRaw)

	stub := newTelegramStub(cfg.token, cfg.send429Count, cfg.edit429Count, cfg.retryAfterSec)
	defer stub.Close()

	tgClient := telegram.NewClient(cfg.token, stub.BaseURL())
	router, err := gateway.NewRouter(
		ctx,
		[]gateway.Options{{
			GroupID:     cfg.groupID,
			CWD:         cfg.cwd,
			SessionName: cfg.session,
		}},
		tgClient,
		console,
		tgClient,
		nil,
	)
	if err != nil {
		log.Fatalf("create gateway router failed: %v", err)
	}
	receiver := telegram.NewReceiver(tgClient, router, nil)
	receiverErr := make(chan error, 1)
	go func() {
		receiverErr <- receiver.Start(ctx)
	}()

	stub.InjectInboundMessage(chatID, cfg.prompt)
	log.Printf("injected prompt into telegram stub: group_id=%s session=%s", cfg.groupID, cfg.session)

	finalRaw, busySeen, err := waitForRunCompletion(ctx, console, cfg.session, stub, cfg.history, cfg.stableWindow)
	if err != nil {
		log.Fatalf("wait for run completion failed: %v", err)
	}
	finalNorm := tmuxctl.NormalizeSnapshot(finalRaw)

	expected := strings.Trim(tmuxctl.SliceAfter(baseNorm, finalNorm), "\n")
	actual := strings.Trim(stub.AggregatedBody(), "\n")

	log.Printf("busy_seen=%v expected_runes=%d actual_runes=%d", busySeen, runeCount(expected), runeCount(actual))

	if expected != actual {
		reportMismatch(expected, actual, stub.Events(), stub.Revisions())
		os.Exit(1)
	}

	cancel()
	select {
	case err := <-receiverErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("receiver exited with error: %v", err)
		}
	case <-time.After(2 * time.Second):
	}
	log.Printf("PASS: forwarded body matches tmux delta exactly")
}

func parseFlags() (config, error) {
	cfg := config{}
	flag.StringVar(&cfg.groupID, "group-id", "", "Telegram group/supergroup id, e.g. -5125916641")
	flag.StringVar(&cfg.cwd, "cwd", "", "working directory for the target Codex session")
	flag.StringVar(&cfg.session, "session", "", "tmux session name (default: computed from group/cwd)")
	flag.StringVar(&cfg.prompt, "prompt", "", "prompt text to inject into telegram getUpdates")
	flag.StringVar(&cfg.promptFile, "prompt-file", "", "read prompt text from file")
	flag.DurationVar(&cfg.timeout, "timeout", 20*time.Minute, "overall e2e timeout")
	flag.DurationVar(&cfg.readyTimeout, "ready-timeout", 2*time.Minute, "max wait until tmux/codex session looks ready before baseline capture")
	flag.DurationVar(&cfg.stableWindow, "stable-window", 12*time.Second, "idle+forwarding stable window before final capture")
	flag.IntVar(&cfg.history, "history", 4000, "tmux capture history lines")
	flag.BoolVar(&cfg.requireExisting, "require-existing", true, "require tmux session to already exist")
	flag.StringVar(&cfg.token, "token", "e2e-token", "telegram bot token used by local stub URL")
	flag.IntVar(&cfg.send429Count, "send-429", 0, "inject HTTP 429 for first N sendMessage calls")
	flag.IntVar(&cfg.edit429Count, "edit-429", 0, "inject HTTP 429 for first N editMessageText calls")
	flag.IntVar(&cfg.retryAfterSec, "retry-after", 2, "retry_after seconds used in injected 429 responses")
	flag.Parse()

	if strings.TrimSpace(cfg.groupID) == "" {
		return cfg, fmt.Errorf("-group-id is required")
	}
	if strings.TrimSpace(cfg.cwd) == "" {
		return cfg, fmt.Errorf("-cwd is required")
	}
	if cfg.timeout <= 0 {
		return cfg, fmt.Errorf("-timeout must be > 0")
	}
	if cfg.readyTimeout <= 0 {
		return cfg, fmt.Errorf("-ready-timeout must be > 0")
	}
	if cfg.stableWindow <= 0 {
		return cfg, fmt.Errorf("-stable-window must be > 0")
	}
	if cfg.send429Count < 0 {
		return cfg, fmt.Errorf("-send-429 must be >= 0")
	}
	if cfg.edit429Count < 0 {
		return cfg, fmt.Errorf("-edit-429 must be >= 0")
	}
	if cfg.retryAfterSec <= 0 {
		return cfg, fmt.Errorf("-retry-after must be > 0")
	}
	return cfg, nil
}

func waitForReadyPane(ctx context.Context, console *tmuxctl.Client, session string, history int, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var firstReadyAt time.Time
	for {
		select {
		case <-readyCtx.Done():
			return readyCtx.Err()
		case <-ticker.C:
			snapshot, err := console.Capture(readyCtx, session, history)
			if err != nil {
				firstReadyAt = time.Time{}
				continue
			}
			if tmuxctl.IsBusy(snapshot) {
				firstReadyAt = time.Time{}
				continue
			}
			_, hasPrompt := tmuxctl.InputStatusSlot(snapshot)
			if !hasPrompt {
				firstReadyAt = time.Time{}
				continue
			}
			if firstReadyAt.IsZero() {
				firstReadyAt = time.Now()
				continue
			}
			// Require a short stable window to avoid startup transitional noise.
			if time.Since(firstReadyAt) >= 2*time.Second {
				return nil
			}
		}
	}
}

func hasTmuxSession(ctx context.Context, session string) (bool, error) {
	cmd := exec.CommandContext(ctx, "tmux", "has-session", "-t", session)
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func waitForRunCompletion(ctx context.Context, console *tmuxctl.Client, session string, stub *telegramStub, history int, stable time.Duration) (string, bool, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var (
		busySeen       bool
		idleSince      time.Time
		lastBody       = stub.AggregatedBody()
		lastBodyChange = time.Now()
		lastSnapshot   string
	)

	for {
		select {
		case <-ctx.Done():
			return "", busySeen, ctx.Err()
		case <-ticker.C:
			snapshot, err := console.Capture(ctx, session, history)
			if err != nil {
				continue
			}
			lastSnapshot = snapshot
			busy := tmuxctl.IsBusy(snapshot)
			if busy {
				busySeen = true
				idleSince = time.Time{}
			} else if busySeen && idleSince.IsZero() {
				idleSince = time.Now()
			}

			body := stub.AggregatedBody()
			if body != lastBody {
				lastBody = body
				lastBodyChange = time.Now()
			}

			if busySeen && !idleSince.IsZero() && time.Since(idleSince) >= stable && time.Since(lastBodyChange) >= stable {
				return lastSnapshot, busySeen, nil
			}
			if !busySeen && strings.TrimSpace(body) != "" && time.Since(lastBodyChange) >= 2*stable {
				return lastSnapshot, busySeen, nil
			}
		}
	}
}

func reportMismatch(expected string, actual string, events []string, revisions []string) {
	eRunes := []rune(expected)
	aRunes := []rune(actual)
	minLen := len(eRunes)
	if len(aRunes) < minLen {
		minLen = len(aRunes)
	}
	idx := minLen
	for i := 0; i < minLen; i++ {
		if eRunes[i] != aRunes[i] {
			idx = i
			break
		}
	}

	log.Printf("FAIL: forwarded body mismatch")
	log.Printf("first_diff_rune=%d expected_len=%d actual_len=%d", idx, len(eRunes), len(aRunes))
	log.Printf("expected_head=%q", headRunes(expected, 320))
	log.Printf("actual_head=%q", headRunes(actual, 320))
	log.Printf("expected_tail=%q", tailRunes(expected, 320))
	log.Printf("actual_tail=%q", tailRunes(actual, 320))
	if idx < len(eRunes) && idx < len(aRunes) {
		expCtx := contextRunes(eRunes, idx, 100)
		actCtx := contextRunes(aRunes, idx, 100)
		log.Printf("expected_ctx@diff=%q", expCtx)
		log.Printf("actual_ctx@diff=%q", actCtx)
	}
	if strings.Contains(actual, expected) {
		log.Printf("containment: actual contains expected at rune index %d", strings.Index(actual, expected))
	}
	if strings.Contains(expected, actual) {
		log.Printf("containment: expected contains actual at rune index %d", strings.Index(expected, actual))
	}

	if len(events) > 0 {
		start := len(events) - 20
		if start < 0 {
			start = 0
		}
		log.Printf("last_events(%d):", len(events)-start)
		for _, item := range events[start:] {
			log.Printf("  %s", item)
		}
	}
	if len(revisions) > 0 {
		start := len(revisions) - 10
		if start < 0 {
			start = 0
		}
		log.Printf("last_revisions(%d):", len(revisions)-start)
		for i := start; i < len(revisions); i++ {
			log.Printf("  #%d len=%d tail=%q", i+1, runeCount(revisions[i]), tailRunes(revisions[i], 140))
		}
	}
}

func headRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

func tailRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[len(rs)-n:])
}

func runeCount(s string) int {
	return len([]rune(s))
}

func contextRunes(rs []rune, center int, radius int) string {
	if len(rs) == 0 {
		return ""
	}
	if center < 0 {
		center = 0
	}
	if center > len(rs) {
		center = len(rs)
	}
	if radius <= 0 {
		radius = 32
	}
	start := center - radius
	if start < 0 {
		start = 0
	}
	end := center + radius
	if end > len(rs) {
		end = len(rs)
	}
	return string(rs[start:end])
}

type telegramStub struct {
	token string
	srv   *httptest.Server

	mu                    sync.Mutex
	updates               []telegram.Update
	nextUpdateID          int64
	nextInboundMessageID  int64
	nextOutboundMessageID int64
	outboundOrder         []int64
	outboundText          map[int64]string
	events                []string
	revisions             []string
	send429Remain         int
	edit429Remain         int
	retryAfterSec         int
}

func newTelegramStub(token string, send429Count int, edit429Count int, retryAfterSec int) *telegramStub {
	s := &telegramStub{
		token:                 strings.TrimSpace(token),
		nextUpdateID:          1,
		nextInboundMessageID:  100,
		nextOutboundMessageID: 1000,
		outboundText:          make(map[int64]string),
		send429Remain:         send429Count,
		edit429Remain:         edit429Count,
		retryAfterSec:         retryAfterSec,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.srv = httptest.NewServer(mux)
	return s
}

func (s *telegramStub) BaseURL() string {
	return s.srv.URL
}

func (s *telegramStub) Close() {
	if s == nil || s.srv == nil {
		return
	}
	s.srv.Close()
}

func (s *telegramStub) InjectInboundMessage(chatID int64, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgID := s.nextInboundMessageID
	s.nextInboundMessageID++

	update := telegram.Update{
		UpdateID: s.nextUpdateID,
		Message: &telegram.Message{
			MessageID: msgID,
			Chat: telegram.Chat{
				ID:   chatID,
				Type: "supergroup",
			},
			From: &telegram.User{IsBot: false},
			Text: text,
		},
	}
	s.nextUpdateID++
	s.updates = append(s.updates, update)
}

func (s *telegramStub) AggregatedBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.aggregatedBodyLocked()
}

func (s *telegramStub) Events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.events))
	copy(out, s.events)
	return out
}

func (s *telegramStub) Revisions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.revisions))
	copy(out, s.revisions)
	return out
}

func (s *telegramStub) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	prefix := "/bot" + s.token + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, prefix)
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeTelegramError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	switch method {
	case "getUpdates":
		offset := asInt64(payload["offset"])
		timeout := asInt64(payload["timeout"])
		updates := s.getUpdates(offset, timeout)
		writeTelegramResult(w, updates)
	case "sendMessage":
		text := asString(payload["text"])
		msgID, retryAfter, limited := s.sendMessage(text)
		if limited {
			writeTelegramRateLimited(w, retryAfter)
			return
		}
		writeTelegramResult(w, map[string]any{"message_id": msgID})
	case "editMessageText":
		msgID := asInt64(payload["message_id"])
		text := asString(payload["text"])
		retryAfter, limited, err := s.editMessage(msgID, text)
		if limited {
			writeTelegramRateLimited(w, retryAfter)
			return
		}
		if err != nil {
			writeTelegramError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTelegramResult(w, true)
	case "deleteMessage":
		msgID := asInt64(payload["message_id"])
		if err := s.deleteMessage(msgID); err != nil {
			writeTelegramError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeTelegramResult(w, true)
	case "sendChatAction":
		writeTelegramResult(w, true)
	default:
		writeTelegramError(w, http.StatusNotFound, "unknown method "+method)
	}
}

func (s *telegramStub) getUpdates(offset int64, timeoutSeconds int64) []telegram.Update {
	if timeoutSeconds < 0 {
		timeoutSeconds = 0
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	for {
		s.mu.Lock()
		out := make([]telegram.Update, 0, len(s.updates))
		for _, update := range s.updates {
			if update.UpdateID >= offset {
				out = append(out, update)
			}
		}
		s.mu.Unlock()
		if len(out) > 0 || timeoutSeconds == 0 || time.Now().After(deadline) {
			return out
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (s *telegramStub) sendMessage(text string) (int64, int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.send429Remain > 0 {
		s.send429Remain--
		s.events = append(s.events, "send:429")
		return 0, s.retryAfterSec, true
	}

	msgID := s.nextOutboundMessageID
	s.nextOutboundMessageID++
	s.outboundOrder = append(s.outboundOrder, msgID)
	s.outboundText[msgID] = text
	s.events = append(s.events, fmt.Sprintf("send:%d:%s", msgID, text))
	s.revisions = append(s.revisions, s.aggregatedBodyLocked())
	return msgID, 0, false
}

func (s *telegramStub) editMessage(msgID int64, text string) (int, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.edit429Remain > 0 {
		s.edit429Remain--
		s.events = append(s.events, "edit:429")
		return s.retryAfterSec, true, nil
	}
	if _, ok := s.outboundText[msgID]; !ok {
		return 0, false, fmt.Errorf("message to edit not found")
	}
	s.outboundText[msgID] = text
	s.events = append(s.events, fmt.Sprintf("edit:%d:%s", msgID, text))
	s.revisions = append(s.revisions, s.aggregatedBodyLocked())
	return 0, false, nil
}

func (s *telegramStub) deleteMessage(msgID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.outboundText[msgID]; !ok {
		return fmt.Errorf("message to delete not found")
	}
	delete(s.outboundText, msgID)
	for i, id := range s.outboundOrder {
		if id == msgID {
			s.outboundOrder = append(s.outboundOrder[:i], s.outboundOrder[i+1:]...)
			break
		}
	}
	s.events = append(s.events, fmt.Sprintf("delete:%d", msgID))
	s.revisions = append(s.revisions, s.aggregatedBodyLocked())
	return nil
}

func (s *telegramStub) aggregatedBodyLocked() string {
	if len(s.outboundOrder) == 0 {
		return ""
	}
	ids := make([]int64, len(s.outboundOrder))
	copy(ids, s.outboundOrder)
	sort.Slice(ids, func(i int, j int) bool { return ids[i] < ids[j] })

	var b strings.Builder
	for _, id := range ids {
		text := s.outboundText[id]
		if strings.HasPrefix(strings.TrimSpace(text), "[working]") {
			continue
		}
		b.WriteString(text)
	}
	return b.String()
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	default:
		return 0
	}
}

func writeTelegramResult(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":     true,
		"result": result,
	})
}

func writeTelegramError(w http.ResponseWriter, status int, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          false,
		"description": desc,
		"error_code":  status,
	})
}

func writeTelegramRateLimited(w http.ResponseWriter, retryAfter int) {
	if retryAfter <= 0 {
		retryAfter = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          false,
		"description": fmt.Sprintf("Too Many Requests: retry after %d", retryAfter),
		"error_code":  http.StatusTooManyRequests,
		"parameters": map[string]any{
			"retry_after": retryAfter,
		},
	})
}
