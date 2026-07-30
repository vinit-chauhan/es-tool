package tui

import (
	"path/filepath"
	"strings"
	"testing"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

func TestValidateClusterAuth(t *testing.T) {
	tests := []struct {
		name    string
		auth    string
		cluster appconfig.Cluster
		wantErr string
	}{
		{name: "none", auth: "none"},
		{name: "API key", auth: "apikey", cluster: appconfig.Cluster{APIKey: "secret"}},
		{name: "missing API key", auth: "apikey", wantErr: "API key is required"},
		{name: "basic", auth: "basic", cluster: appconfig.Cluster{User: "elastic"}},
		{name: "missing username", auth: "basic", wantErr: "username is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClusterAuth(tt.auth, tt.cluster)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateClusterAuth() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateClusterAuth() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSaveClusterAddsAndActivatesProfile(t *testing.T) {
	store := &appconfig.Store{Path: filepath.Join(t.TempDir(), "config.json")}
	client := esclient.New(esclient.Options{
		BaseURL:   esclient.DefaultURL,
		VerifyTLS: true,
	})
	a := &app{
		client:       client,
		store:        store,
		config:       appconfig.New(),
		settingsWarn: "old warning",
	}
	cluster := appconfig.Cluster{
		Name:      "production",
		URL:       "https://example.com",
		APIKey:    "secret",
		VerifyTLS: true,
	}

	if changed := a.saveCluster("", cluster); !changed {
		t.Fatal("saveCluster() did not activate a newly added profile")
	}
	if a.activeCluster != "production" || a.config.Active != "production" {
		t.Fatalf("active profile = %q / %q", a.activeCluster, a.config.Active)
	}
	if client.BaseURL != cluster.URL || client.APIKey != cluster.APIKey {
		t.Fatalf("client = %#v, want cluster connection", client)
	}
	if a.settingsWarn != "" {
		t.Fatalf("settings warning was not cleared: %q", a.settingsWarn)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Active != "production" || len(loaded.Clusters) != 1 {
		t.Fatalf("saved config = %#v", loaded)
	}
}
