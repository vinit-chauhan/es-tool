package tui

import (
	"strings"
	"testing"
)

func TestHighlightJSONPreservesStructure(t *testing.T) {
	// lipgloss disables color rendering outside a TTY, so this only checks
	// that the visible text survives the pass; ANSI styling itself is
	// exercised interactively, not under `go test`.
	input := "{\n  \"name\": \"job-1\",\n  \"count\": 3,\n  \"active\": true,\n  \"tag\": null\n}"
	got := highlightJSON(input)
	if stripANSI(got) != input {
		t.Errorf("highlighting must not change the visible text.\ngot:  %q\nwant: %q", stripANSI(got), input)
	}
}

func TestHighlightMatch(t *testing.T) {
	if got := highlightMatch("job-42", ""); got != "job-42" {
		t.Errorf("empty filter should leave text unchanged, got %q", got)
	}
	if got := highlightMatch("job-42", "zzz"); got != "job-42" {
		t.Errorf("no match should leave text unchanged, got %q", got)
	}
	got := highlightMatch("job-42", "JOB")
	if stripANSI(got) != "job-42" {
		t.Errorf("highlighting must not change the visible text, got %q", stripANSI(got))
	}
}

func TestRenderFooterKeepsHintVisibleOnMultilineErrors(t *testing.T) {
	status := notification{
		text:  "HTTP 403: {\n  \"message\": \"Forbidden due to traffic filtering.\",\n  \"ok\": false\n}",
		isErr: true,
	}
	got := renderFooter(status, "enter: open • q: quit", 80)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("footer must be exactly 2 lines (status + hint), got %d:\n%s", len(lines), got)
	}
	if !strings.Contains(stripANSI(lines[0]), "HTTP 403") {
		t.Errorf("status line lost the error text: %q", stripANSI(lines[0]))
	}
	if !strings.Contains(stripANSI(lines[1]), "q: quit") {
		t.Errorf("hint line lost the hotkeys: %q", stripANSI(lines[1]))
	}
}

func TestSingleLineFlattensAndClips(t *testing.T) {
	if got := singleLine("a\n  b\tc", 80); got != "a b c" {
		t.Errorf("singleLine() = %q, want %q", got, "a b c")
	}
	got := singleLine(strings.Repeat("x", 100), 10)
	if runes := []rune(got); len(runes) > 10 {
		t.Errorf("singleLine() length = %d runes, want <= 10 (%q)", len(runes), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated text should end with an ellipsis, got %q", got)
	}
}

// stripANSI removes CSI escape sequences so highlighted output can be
// compared against the plain text it was derived from.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			j := i + 1
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
