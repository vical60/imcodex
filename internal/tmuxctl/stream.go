package tmuxctl

import (
	"regexp"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func NormalizeSnapshot(snapshot string) string {
	snapshot = stripANSI(snapshot)
	lines := strings.Split(snapshot, "\n")

	out := make([]string, 0, len(lines))
	prevBlank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(line, "  ") {
			line = strings.TrimPrefix(line, "  ")
		}
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			if len(out) == 0 || prevBlank {
				continue
			}
			out = append(out, "")
			prevBlank = true
			continue
		}
		if shouldIgnoreLine(trimmed) {
			continue
		}

		out = append(out, line)
		prevBlank = false
	}

	return strings.TrimSpace(strings.Join(out, "\n"))
}

func DiffText(prev string, curr string) (string, bool) {
	if curr == prev {
		return "", false
	}
	if prev == "" {
		return curr, false
	}
	if strings.HasPrefix(curr, prev) {
		return curr[len(prev):], false
	}
	return "", true
}

func IsBusy(snapshot string) bool {
	return strings.Contains(snapshot, "esc to interrupt")
}

func IsTrustPrompt(snapshot string) bool {
	return strings.Contains(snapshot, "Do you trust the contents of this directory?") ||
		strings.Contains(snapshot, "Press enter to continue")
}

func stripANSI(in string) string {
	return ansiPattern.ReplaceAllString(in, "")
}

func shouldIgnoreLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "╭"),
		strings.HasPrefix(line, "│"),
		strings.HasPrefix(line, "╰"),
		strings.HasPrefix(line, "Tip:"),
		strings.HasPrefix(line, "model:"),
		strings.HasPrefix(line, "directory:"),
		strings.HasPrefix(line, "Do you trust the contents of this directory?"),
		strings.HasPrefix(line, "comes with higher risk of prompt injection."),
		strings.HasPrefix(line, "1. Yes, continue"),
		strings.HasPrefix(line, "2. No, quit"),
		strings.HasPrefix(line, "Press enter to continue"),
		strings.HasPrefix(line, "›"):
		return true
	case strings.Contains(line, "chatgpt.com/codex"),
		strings.Contains(line, "community.openai.com"),
		strings.Contains(line, "% left ·"),
		strings.Contains(line, "esc to interrupt"):
		return true
	default:
		return false
	}
}
