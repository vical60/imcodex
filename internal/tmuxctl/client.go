package tmuxctl

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type SessionSpec struct {
	SessionName                 string
	CWD                         string
	StartupWait                 time.Duration
	AutoPressEnterOnTrustPrompt bool
}

type Client struct {
	bin       string
	enterWait time.Duration
}

func New() *Client {
	return &Client{
		bin:       "tmux",
		enterWait: 250 * time.Millisecond,
	}
}

func (c *Client) EnsureSession(ctx context.Context, spec SessionSpec) (bool, error) {
	ok, err := c.hasSession(ctx, spec.SessionName)
	if err != nil {
		return false, err
	}
	if ok {
		return false, nil
	}

	command := "exec " + shellJoin("codex", "--no-alt-screen", "-C", spec.CWD)
	if err := c.run(ctx, "new-session", "-d", "-s", spec.SessionName, command); err != nil {
		return false, fmt.Errorf("create tmux session: %w", err)
	}

	wait := spec.StartupWait
	if wait <= 0 {
		wait = 4 * time.Second
	}
	time.Sleep(wait)

	if spec.AutoPressEnterOnTrustPrompt {
		snapshot, err := c.Capture(ctx, spec.SessionName, 200)
		if err == nil && IsTrustPrompt(snapshot) {
			if err := c.sendKey(ctx, spec.SessionName, "Enter"); err != nil {
				return false, err
			}
			time.Sleep(wait / 2)
		}
	}

	return true, nil
}

func (c *Client) SendText(ctx context.Context, session string, text string) error {
	bufferName := "imcodex-" + sanitizeToken(session)
	if err := c.run(ctx, "set-buffer", "-b", bufferName, "--", text); err != nil {
		return fmt.Errorf("set tmux buffer: %w", err)
	}
	if err := c.run(ctx, "paste-buffer", "-d", "-b", bufferName, "-t", paneTarget(session)); err != nil {
		return fmt.Errorf("paste tmux buffer: %w", err)
	}
	if c.enterWait > 0 {
		time.Sleep(c.enterWait)
	}
	if err := c.sendKey(ctx, session, "Enter"); err != nil {
		return err
	}
	return nil
}

func (c *Client) Capture(ctx context.Context, session string, history int) (string, error) {
	if history <= 0 {
		history = 2000
	}
	out, err := c.output(ctx, "capture-pane", "-pJ", "-S", fmt.Sprintf("-%d", history), "-t", paneTarget(session))
	if err != nil {
		return "", fmt.Errorf("capture tmux pane: %w", err)
	}
	return out, nil
}

func (c *Client) hasSession(ctx context.Context, session string) (bool, error) {
	cmd := exec.CommandContext(ctx, c.bin, "has-session", "-t", session)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (c *Client) sendKey(ctx context.Context, session string, key string) error {
	if err := c.run(ctx, "send-keys", "-t", paneTarget(session), key); err != nil {
		return fmt.Errorf("send tmux key %s: %w", key, err)
	}
	return nil
}

func (c *Client) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *Client) output(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func paneTarget(session string) string {
	return session + ":0.0"
}

func shellJoin(args ...string) string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, shellQuote(arg))
	}
	return strings.Join(out, " ")
}

func shellQuote(in string) string {
	if in == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(in, "'", `'\''`) + "'"
}

func sanitizeToken(in string) string {
	var b strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "session"
	}
	return out
}
