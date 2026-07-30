package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "nested", "config.json")}
	cfg := New()
	cluster := Cluster{
		Name:      "production",
		URL:       "https://example.es.io/",
		APIKey:    "secret",
		VerifyTLS: true,
	}
	if err := cfg.Upsert("", cluster); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	cfg.Active = "production"

	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.Clusters[0].Normalize()
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("Load() = %#v, want %#v", got, cfg)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.Path)
		if err != nil {
			t.Fatalf("Stat() error = %v", err)
		}
		if gotPerm := info.Mode().Perm(); gotPerm != 0o600 {
			t.Fatalf("config permissions = %o, want 600", gotPerm)
		}
	}
}

func TestLoadMissingReturnsEmptyConfig(t *testing.T) {
	store := &Store{Path: filepath.Join(t.TempDir(), "missing.json")}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := New()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestLoadDefaultsToTLSVerificationAndSecuresFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{
  "version": 1,
  "active": "legacy",
  "clusters": [{"name": "legacy", "url": "https://example.com"}]
}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := (&Store{Path: path}).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Clusters[0].VerifyTLS {
		t.Fatal("Load() disabled TLS verification when verify_tls was omitted")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("config permissions = %o, want 600", got)
		}
	}
}

func TestUpsertRejectsDuplicateAndRenamesActive(t *testing.T) {
	cfg := New()
	first := Cluster{Name: "one", URL: "http://localhost:9200", VerifyTLS: true}
	second := Cluster{Name: "two", URL: "http://localhost:9201", VerifyTLS: true}
	if err := cfg.Upsert("", first); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Upsert("", second); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Upsert("two", first); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate Upsert() error = %v", err)
	}

	cfg.Active = "one"
	first.Name = "renamed"
	if err := cfg.Upsert("one", first); err != nil {
		t.Fatalf("rename Upsert() error = %v", err)
	}
	if cfg.Active != "renamed" {
		t.Fatalf("Active = %q, want renamed", cfg.Active)
	}
}

func TestClusterValidate(t *testing.T) {
	tests := []struct {
		name    string
		cluster Cluster
		wantErr string
	}{
		{
			name:    "missing name",
			cluster: Cluster{URL: "https://example.com"},
			wantErr: "name is required",
		},
		{
			name:    "missing scheme",
			cluster: Cluster{Name: "test", URL: "example.com"},
			wantErr: "must include",
		},
		{
			name:    "credentials in URL",
			cluster: Cluster{Name: "test", URL: "https://user:pass@example.com"},
			wantErr: "authentication fields",
		},
		{
			name: "mixed authentication",
			cluster: Cluster{
				Name:   "test",
				URL:    "https://example.com",
				APIKey: "key",
				User:   "elastic",
			},
			wantErr: "either API key or basic",
		},
		{
			name:    "valid",
			cluster: Cluster{Name: "test", URL: "https://example.com", User: "elastic"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cluster.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
