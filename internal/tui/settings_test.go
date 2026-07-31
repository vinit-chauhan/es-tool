package tui

import (
	"reflect"
	"testing"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
)

func TestEditorFieldsFollowAuthMode(t *testing.T) {
	tests := []struct {
		auth string
		want []editorFieldID
	}{
		{"none", []editorFieldID{fieldName, fieldURL, fieldAuth, fieldTLS, fieldSave}},
		{"apikey", []editorFieldID{fieldName, fieldURL, fieldAuth, fieldAPIKey, fieldTLS, fieldSave}},
		{"basic", []editorFieldID{fieldName, fieldURL, fieldAuth, fieldUser, fieldPassword, fieldTLS, fieldSave}},
	}
	for _, tt := range tests {
		m := Model{editingAuth: tt.auth}
		if got := m.editorFields(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("editorFields(%s) = %v, want %v", tt.auth, got, tt.want)
		}
	}
}

func TestSessionEditorHasNoNameField(t *testing.T) {
	m := Model{editingAuth: "basic", editingSession: true}
	want := []editorFieldID{fieldURL, fieldAuth, fieldUser, fieldPassword, fieldTLS, fieldSave}
	if got := m.editorFields(); !reflect.DeepEqual(got, want) {
		t.Errorf("session editorFields() = %v, want %v", got, want)
	}
}

func TestEditorDirtyDetection(t *testing.T) {
	base := appconfig.Cluster{Name: "prod", URL: "https://example.com", VerifyTLS: true}
	m := Model{
		editingCluster:  base,
		editingBaseline: base,
		editingAuth:     "none",
		baselineAuth:    "none",
	}
	if m.editorDirty() {
		t.Fatal("unchanged editor must not be dirty")
	}
	m.editingCluster.URL = "https://other.example.com"
	if !m.editorDirty() {
		t.Fatal("changed URL must mark the editor dirty")
	}
	m.editingCluster = base
	m.editingAuth = "apikey"
	if !m.editorDirty() {
		t.Fatal("changed auth mode must mark the editor dirty")
	}
}

func TestResetClusterStateDropsPreviousClusterData(t *testing.T) {
	m := testModel(t, 100, 30)
	m.receiveIndices([]any{
		map[string]any{"health": "green", "status": "open", "index": "old-index", "docs.count": "1", "store.size": "1kb"},
	})
	m.currentIndex = "old-index"
	m.query = "status:running"
	m.total = 42
	m.currentDocID = "doc-1"
	oldEpoch := m.connEpoch

	m.resetClusterState()

	if m.connEpoch == oldEpoch {
		t.Fatal("resetClusterState must advance the connection epoch")
	}
	if len(m.indexTable.Rows()) != 0 || m.allIndices != nil {
		t.Error("index list from the previous cluster must be cleared")
	}
	if m.currentIndex != "" || m.query != "" || m.total != 0 || m.currentDocID != "" {
		t.Error("document state from the previous cluster must be cleared")
	}

	// A slow response from the previous cluster must be ignored.
	updated, _ := m.Update(requestMsg{
		operation: operationIndices,
		epoch:     oldEpoch,
		status:    200,
		body: []any{
			map[string]any{"health": "green", "status": "open", "index": "stale-index", "docs.count": "9", "store.size": "9kb"},
		},
	})
	m = updated.(Model)
	if len(m.indexTable.Rows()) != 0 {
		t.Error("a response stamped with the old epoch must not repopulate the table")
	}
}

func TestProfileHealthLabel(t *testing.T) {
	m := Model{profileHealth: map[string]healthStatus{
		"green":    {state: stateHealthGreen},
		"yellow":   {state: stateHealthYellow},
		"red":      {state: stateHealthRed},
		"offline":  {state: stateHealthOffline},
		"auth":     {state: stateHealthAuthError},
		"checking": {state: stateHealthChecking},
	}}
	tests := map[string]string{
		"green":     "green",
		"yellow":    "yellow",
		"red":       "red",
		"offline":   "unknown",
		"auth":      "unknown",
		"checking":  "…",
		"never-ran": "unknown",
	}
	for name, want := range tests {
		if got := m.profileHealthLabel(name); got != want {
			t.Errorf("profileHealthLabel(%s) = %q, want %q", name, got, want)
		}
	}
}
