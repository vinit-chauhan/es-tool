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
