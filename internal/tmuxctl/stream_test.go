package tmuxctl

import "testing"

func TestNormalizeSnapshot(t *testing.T) {
	t.Parallel()

	raw := `╭─────────────────────────────────────────────╮
│ >_ OpenAI Codex (v0.115.0-alpha.27)         │
│                                             │
│ model:     gpt-5.4 xhigh   /model to change │
│ directory: /Volumes/newver/flow             │
╰─────────────────────────────────────────────╯

  Tip: New Build faster with the Codex App.

› Just answer hello

• Hello

› Write tests for @filename

  gpt-5.4 xhigh · 100% left · /Volumes/newver/flow
`

	got := NormalizeSnapshot(raw)
	want := "• Hello"
	if got != want {
		t.Fatalf("NormalizeSnapshot() = %q, want %q", got, want)
	}
}

func TestDiffText(t *testing.T) {
	t.Parallel()

	prev := "• Hel"
	curr := "• Hello"

	delta, reset := DiffText(prev, curr)
	if reset {
		t.Fatal("reset = true, want false")
	}
	if got, want := delta, "lo"; got != want {
		t.Fatalf("delta = %q, want %q", got, want)
	}
}

func TestIsBusy(t *testing.T) {
	t.Parallel()

	raw := "• Working (2s • esc to interrupt)"
	if !IsBusy(raw) {
		t.Fatal("IsBusy() = false, want true")
	}
}
