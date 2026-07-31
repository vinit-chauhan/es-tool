package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

// testModel builds a minimal but renderable model, sized like a real terminal.
func testModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := Model{
		client:        esclient.New(esclient.Options{BaseURL: esclient.DefaultURL, VerifyTLS: true}),
		screen:        screenIndices,
		indexTable:    newIndexTable(),
		docTable:      newDocumentTable(),
		settingsTable: newSettingsTable(),
		profileHealth: map[string]healthStatus{},
		pageSize:      50,
	}
	m.width, m.height = width, height
	m.resize()
	return m
}

func TestViewKeepsHotkeyHintOnBottomLine(t *testing.T) {
	for _, screen := range []screenKind{
		screenIndices, screenDocuments, screenDocument, screenIndexDetails,
		screenSettings, screenClusterEditor, screenSearch, screenClusterInfo,
	} {
		m := testModel(t, 120, 30)
		m.screen = screen
		m.status = notification{text: "some status message"}

		lines := strings.Split(m.View(), "\n")
		if len(lines) > 30 {
			t.Errorf("screen %d: view has %d lines, terminal only shows 30 — footer would be pushed off", screen, len(lines))
		}
		bottom := stripANSI(lines[len(lines)-1])
		if !strings.Contains(bottom, "esc") && !strings.Contains(bottom, "quit") {
			t.Errorf("screen %d: bottom line has no hotkey hint: %q", screen, bottom)
		}
	}
}

func TestViewLinesFitTerminalWidth(t *testing.T) {
	m := testModel(t, 100, 30)
	m.receiveIndices([]any{
		map[string]any{"health": "green", "status": "open", "index": strings.Repeat("very-long-index-name-", 8), "docs.count": "12345", "store.size": "1.2gb"},
	})
	for i, line := range strings.Split(m.View(), "\n") {
		if w := lipgloss.Width(line); w > 100 {
			t.Errorf("line %d is %d cells wide (terminal is 100); it would wrap and push the footer off:\n%q", i, w, stripANSI(line))
		}
	}
}
