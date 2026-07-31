package tui

import "testing"

func TestParsePageSize(t *testing.T) {
	if size, err := parsePageSize("50"); err != nil || size != 50 {
		t.Errorf("parsePageSize(50) = (%d, %v), want (50, nil)", size, err)
	}
	if _, err := parsePageSize("0"); err == nil {
		t.Error("parsePageSize(0) should error")
	}
	if _, err := parsePageSize("10001"); err == nil {
		t.Error("parsePageSize(10001) should error, over the 10000 cap")
	}
	if _, err := parsePageSize("nope"); err == nil {
		t.Error("parsePageSize(non-numeric) should error")
	}
}

func TestWrapLines(t *testing.T) {
	if got := wrapLines("hello", 0); got != "hello" {
		t.Errorf("width<=0 should pass text through unchanged, got %q", got)
	}
	got := wrapLines("abcdefgh", 3)
	want := "abc\ndef\ngh"
	if got != want {
		t.Errorf("wrapLines(abcdefgh, 3) = %q, want %q", got, want)
	}
}

func TestDocumentPreview(t *testing.T) {
	if got := documentPreview(nil); got != "" {
		t.Errorf("documentPreview(nil) = %q, want empty", got)
	}
	got := documentPreview(map[string]any{"status": "running", "name": "job-1", "extra": "ignored"})
	want := "name=job-1 • status=running"
	if got != want {
		t.Errorf("documentPreview = %q, want %q", got, want)
	}
	if got := documentPreview(map[string]any{"other": "field"}); got == "" {
		t.Error("documentPreview should fall back to compact JSON when no known fields match")
	}
}
