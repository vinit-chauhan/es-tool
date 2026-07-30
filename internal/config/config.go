// Package config persists named Elasticsearch cluster profiles.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const currentVersion = 1

// Cluster contains the connection settings for one Elasticsearch cluster.
// API keys and passwords are stored in the config file, which is written with
// owner-only permissions.
type Cluster struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	APIKey    string `json:"api_key,omitempty"`
	User      string `json:"user,omitempty"`
	Password  string `json:"password,omitempty"`
	VerifyTLS bool   `json:"verify_tls"`
}

// UnmarshalJSON keeps TLS verification secure by default when loading a
// hand-written or older profile that omits verify_tls.
func (c *Cluster) UnmarshalJSON(data []byte) error {
	type plain Cluster
	*c = Cluster{VerifyTLS: true}
	return json.Unmarshal(data, (*plain)(c))
}

// Normalize trims user-entered values and removes a trailing slash from URL.
func (c *Cluster) Normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	c.User = strings.TrimSpace(c.User)
}

// Validate checks that a cluster profile can be used safely by the client.
func (c Cluster) Validate() error {
	if strings.TrimSpace(c.Name) == "" {
		return errors.New("cluster name is required")
	}
	rawURL := strings.TrimSpace(c.URL)
	if rawURL == "" {
		return errors.New("cluster URL is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return errors.New("cluster URL must include http:// or https:// and a host")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("cluster URL scheme must be http or https")
	}
	if u.User != nil {
		return errors.New("put credentials in the authentication fields, not the URL")
	}
	if c.APIKey != "" && (c.User != "" || c.Password != "") {
		return errors.New("choose either API key or basic authentication")
	}
	if c.Password != "" && c.User == "" {
		return errors.New("username is required when a password is set")
	}
	return nil
}

// Config is the on-disk settings document.
type Config struct {
	Version  int       `json:"version"`
	Active   string    `json:"active,omitempty"`
	Clusters []Cluster `json:"clusters"`
}

// New returns an empty config using the current schema version.
func New() Config {
	return Config{Version: currentVersion, Clusters: []Cluster{}}
}

// Clone returns an independent copy of the config.
func (c Config) Clone() Config {
	out := c
	out.Clusters = append([]Cluster(nil), c.Clusters...)
	return out
}

// Find returns a cluster by name.
func (c Config) Find(name string) (Cluster, bool) {
	for _, cluster := range c.Clusters {
		if cluster.Name == name {
			return cluster, true
		}
	}
	return Cluster{}, false
}

// Upsert adds a new cluster or replaces originalName. Renaming the configured
// default cluster updates Active as well.
func (c *Config) Upsert(originalName string, cluster Cluster) error {
	cluster.Normalize()
	if err := cluster.Validate(); err != nil {
		return err
	}

	originalIndex := -1
	for i, existing := range c.Clusters {
		if existing.Name == originalName {
			originalIndex = i
		}
		if existing.Name == cluster.Name && existing.Name != originalName {
			return fmt.Errorf("cluster %q already exists", cluster.Name)
		}
	}

	switch {
	case originalName == "":
		c.Clusters = append(c.Clusters, cluster)
	case originalIndex >= 0:
		c.Clusters[originalIndex] = cluster
	default:
		return fmt.Errorf("cluster %q no longer exists", originalName)
	}
	if c.Active == originalName && originalName != cluster.Name {
		c.Active = cluster.Name
	}
	return nil
}

// Validate checks the complete config, including unique names and Active.
func (c Config) Validate() error {
	seen := make(map[string]struct{}, len(c.Clusters))
	for _, cluster := range c.Clusters {
		if err := cluster.Validate(); err != nil {
			return fmt.Errorf("cluster %q: %w", cluster.Name, err)
		}
		if _, ok := seen[cluster.Name]; ok {
			return fmt.Errorf("duplicate cluster name %q", cluster.Name)
		}
		seen[cluster.Name] = struct{}{}
	}
	if c.Active != "" {
		if _, ok := seen[c.Active]; !ok {
			return fmt.Errorf("active cluster %q does not exist", c.Active)
		}
	}
	return nil
}

// Store loads and saves a config at Path.
type Store struct {
	Path string
}

// DefaultStore returns the platform-specific per-user config store.
func DefaultStore() (*Store, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user config directory: %w", err)
	}
	return &Store{Path: filepath.Join(root, "es-tool", "config.json")}, nil
}

// Load reads the config. A missing file is treated as an empty config.
func (s *Store) Load() (Config, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", s.Path, err)
	}
	if info, statErr := os.Stat(s.Path); statErr == nil && info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(s.Path, 0o600); err != nil {
			return Config{}, fmt.Errorf("secure %s: %w", s.Path, err)
		}
	}

	cfg := New()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", s.Path, err)
	}
	if cfg.Version == 0 {
		cfg.Version = currentVersion
	}
	if cfg.Version != currentVersion {
		return Config{}, fmt.Errorf("unsupported config version %d", cfg.Version)
	}
	if cfg.Clusters == nil {
		cfg.Clusters = []Cluster{}
	}
	for i := range cfg.Clusters {
		cfg.Clusters[i].Normalize()
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate %s: %w", s.Path, err)
	}
	return cfg, nil
}

// Save atomically writes the config with owner-only permissions.
func (s *Store) Save(cfg Config) error {
	cfg.Version = currentVersion
	if cfg.Clusters == nil {
		cfg.Clusters = []Cluster{}
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpName, s.Path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	if err := os.Chmod(s.Path, 0o600); err != nil {
		return fmt.Errorf("secure config: %w", err)
	}
	return nil
}
