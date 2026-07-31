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
