package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestRequestError(t *testing.T) {
	if err := requestError(200, nil, nil); err != nil {
		t.Errorf("2xx status should not error, got %v", err)
	}
	if err := requestError(500, nil, nil); err == nil {
		t.Error("5xx status should error")
	}
	wrapped := errors.New("boom")
	if err := requestError(0, nil, wrapped); !errors.Is(err, wrapped) {
		t.Errorf("transport error should pass through unchanged, got %v", err)
	}
	err := requestError(404, map[string]any{"error": "missing"}, nil)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error body should surface status and body, got %v", err)
	}
}

func TestHealthFromResponse(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   any
		err    error
		want   healthStatusState
	}{
		{"transport error", 0, nil, errors.New("dial tcp: refused"), stateHealthOffline},
		{"unauthorized", 401, nil, nil, stateHealthAuthError},
		{"server error", 503, nil, nil, stateHealthUnavailable},
		{"green", 200, map[string]any{"status": "green"}, nil, stateHealthGreen},
		{"yellow", 200, map[string]any{"status": "yellow"}, nil, stateHealthYellow},
		{"red", 200, map[string]any{"status": "red"}, nil, stateHealthRed},
		{"connected without health body", 200, map[string]any{}, nil, stateHealthConnected},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := healthFromResponse(tc.status, tc.body, tc.err)
			if got.state != tc.want {
				t.Errorf("healthFromResponse(%d, %v, %v).state = %v, want %v", tc.status, tc.body, tc.err, got.state, tc.want)
			}
		})
	}
}
