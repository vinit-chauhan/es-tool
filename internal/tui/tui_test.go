package tui

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

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

func TestDrawStatusKeepsHotkeysOnBottomLine(t *testing.T) {
	screen := newSimulationScreen(t)
	a := &app{screen: screen}
	a.status.set("cluster saved")

	a.drawStatus("S settings  q quit")
	screen.Show()

	if got := simulationRow(screen, 8); got != "cluster saved" {
		t.Fatalf("notification row = %q", got)
	}
	if got := simulationRow(screen, 9); got != "S settings  q quit" {
		t.Fatalf("hotkey row = %q", got)
	}
}

func TestClusterHealthFromResponse(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      any
		err       error
		wantState clusterHealthState
		wantCode  int
	}{
		{
			name:      "green",
			status:    200,
			body:      map[string]any{"status": "green"},
			wantState: healthGreen,
		},
		{
			name:      "yellow",
			status:    200,
			body:      map[string]any{"status": "yellow"},
			wantState: healthYellow,
		},
		{
			name:      "red",
			status:    200,
			body:      map[string]any{"status": "red"},
			wantState: healthRed,
		},
		{
			name:      "connected without health",
			status:    200,
			body:      map[string]any{},
			wantState: healthConnected,
		},
		{
			name:      "unauthorized",
			status:    401,
			wantState: healthAuthError,
			wantCode:  401,
		},
		{
			name:      "health forbidden",
			status:    403,
			wantState: healthUnavailable,
			wantCode:  403,
		},
		{
			name:      "health endpoint failure",
			status:    503,
			wantState: healthUnavailable,
			wantCode:  503,
		},
		{
			name:      "offline",
			err:       errors.New("connection refused"),
			wantState: healthOffline,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clusterHealthFromResponse(tt.status, tt.body, tt.err)
			if got.state != tt.wantState || got.code != tt.wantCode {
				t.Fatalf("health = %#v, want state %v code %d", got, tt.wantState, tt.wantCode)
			}
		})
	}
}

func TestDrawHeaderShowsHealthAtTopRight(t *testing.T) {
	screen := newSimulationScreen(t)
	a := &app{
		screen: screen,
		health: clusterHealth{state: healthGreen},
	}

	a.drawHeader("Indices", "http://localhost:9200")
	screen.Show()

	if got := simulationRow(screen, 0); !strings.HasSuffix(got, "CLUSTER GREEN") {
		t.Fatalf("header row = %q", got)
	}
}

func TestPromptValueSupportsCursorEditing(t *testing.T) {
	screen := newSimulationScreen(t)
	a := &app{screen: screen}
	events := []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'X', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, 'Z', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyEnd, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
	}
	posted := make(chan struct{})
	go func() {
		defer close(posted)
		for _, event := range events {
			screen.PostEventWait(event)
		}
	}()

	got, ok := a.prompt("value: ", "abcd")
	<-posted
	if !ok {
		t.Fatal("prompt() was cancelled")
	}
	if got != "ZabX" {
		t.Fatalf("prompt() = %q, want ZabX", got)
	}
}

func newSimulationScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	screen.SetSize(80, 10)
	t.Cleanup(screen.Fini)
	return screen
}

func simulationRow(screen tcell.SimulationScreen, y int) string {
	width, _ := screen.Size()
	row := make([]rune, 0, width)
	for x := 0; x < width; x++ {
		mainc, _, _, _ := screen.GetContent(x, y)
		row = append(row, mainc)
	}
	return strings.TrimSpace(string(row))
}
