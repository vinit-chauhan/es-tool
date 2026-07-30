package esclient

import (
	"net/http"
	"os"
	"testing"
)

func TestNewAndConfigure(t *testing.T) {
	client := New(Options{
		BaseURL:   "https://example.com/",
		APIKey:    "first",
		VerifyTLS: false,
	})
	if client.BaseURL != "https://example.com" {
		t.Fatalf("BaseURL = %q", client.BaseURL)
	}
	if client.AuthMode() != "apikey" {
		t.Fatalf("AuthMode() = %q", client.AuthMode())
	}
	firstTransport := client.http.Transport
	assertTLSVerification(t, firstTransport, false)

	client.Configure(Options{
		BaseURL:   "http://localhost:9200",
		User:      "elastic",
		Password:  "password",
		VerifyTLS: true,
	})
	if client.AuthMode() != "basic" {
		t.Fatalf("AuthMode() = %q", client.AuthMode())
	}
	if client.http.Transport == firstTransport {
		t.Fatal("Configure() did not rebuild the HTTP transport")
	}
	assertTLSVerification(t, client.http.Transport, true)
}

func TestSetHeaders(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "none",
			opts: Options{BaseURL: DefaultURL, VerifyTLS: true},
		},
		{
			name: "API key",
			opts: Options{BaseURL: DefaultURL, APIKey: "encoded", VerifyTLS: true},
			want: "ApiKey encoded",
		},
		{
			name: "basic",
			opts: Options{BaseURL: DefaultURL, User: "elastic", Password: "password", VerifyTLS: true},
			want: "Basic ZWxhc3RpYzpwYXNzd29yZA==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, DefaultURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			New(tt.opts).setHeaders(req)
			if got := req.Header.Get("Authorization"); got != tt.want {
				t.Fatalf("Authorization = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnvConfiguredIgnoresEmptyValues(t *testing.T) {
	keys := []string{"ES_URL", "ES_API_KEY", "ES_USER", "ES_PASSWORD", "ES_VERIFY_TLS"}
	type previousValue struct {
		value string
		set   bool
	}
	previous := make(map[string]previousValue, len(keys))
	for _, key := range keys {
		value, set := os.LookupEnv(key)
		previous[key] = previousValue{value: value, set: set}
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, key := range keys {
			old := previous[key]
			if old.set {
				_ = os.Setenv(key, old.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})

	if EnvConfigured() {
		t.Fatal("EnvConfigured() = true with no ES_* variables")
	}
	if err := os.Setenv("ES_URL", ""); err != nil {
		t.Fatal(err)
	}
	if EnvConfigured() {
		t.Fatal("EnvConfigured() = true with an empty ES_URL")
	}
	if err := os.Setenv("ES_URL", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if !EnvConfigured() {
		t.Fatal("EnvConfigured() = false with a non-empty ES_URL")
	}
}

func assertTLSVerification(t *testing.T, transport http.RoundTripper, want bool) {
	t.Helper()
	httpTransport, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", transport)
	}
	tlsConfig := httpTransport.TLSClientConfig
	if tlsConfig == nil {
		t.Fatal("TLS config is nil")
	}
	if got := !tlsConfig.InsecureSkipVerify; got != want {
		t.Fatalf("TLS verification = %t, want %t", got, want)
	}
}
