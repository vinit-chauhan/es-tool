// Package esclient is a thin HTTP client for the Elasticsearch REST API.
//
// It speaks the ES REST API directly using net/http. Authentication is
// optional and can be provided via an API key, basic auth, or none (default
// for the local elastic-package stack).
//
// Configuration (env vars, all optional):
//
//	ES_URL         Base URL of Elasticsearch (default: http://localhost:9202)
//	ES_API_KEY     Encoded API key (sent as Authorization: ApiKey ...)
//	ES_USER        Basic-auth user (used with ES_PASSWORD)
//	ES_PASSWORD    Basic-auth password
//	ES_VERIFY_TLS  "0"/"false"/"no" to disable TLS verification (default: on)
package esclient

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultURL is used when ES_URL is not set.
const DefaultURL = "http://localhost:9202"

// Client is a thin HTTP client for the Elasticsearch REST API.
type Client struct {
	BaseURL   string
	APIKey    string
	User      string
	Password  string
	VerifyTLS bool

	http *http.Client
}

// NewFromEnv builds a Client from the ES_* environment variables.
func NewFromEnv() *Client {
	verify := strings.ToLower(getenvDefault("ES_VERIFY_TLS", "1"))
	verifyTLS := true
	switch verify {
	case "0", "false", "no":
		verifyTLS = false
	}
	c := &Client{
		BaseURL:   strings.TrimRight(getenvDefault("ES_URL", DefaultURL), "/"),
		APIKey:    os.Getenv("ES_API_KEY"),
		User:      os.Getenv("ES_USER"),
		Password:  os.Getenv("ES_PASSWORD"),
		VerifyTLS: verifyTLS,
	}
	c.http = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS}, //nolint:gosec // opt-in via ES_VERIFY_TLS
		},
	}
	return c
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// AuthMode returns "apikey", "basic", or "none" describing the auth mode.
func (c *Client) AuthMode() string {
	switch {
	case c.APIKey != "":
		return "apikey"
	case c.User != "":
		return "basic"
	default:
		return "none"
	}
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	switch {
	case c.APIKey != "":
		req.Header.Set("Authorization", "ApiKey "+c.APIKey)
	case c.User != "":
		token := base64.StdEncoding.EncodeToString([]byte(c.User + ":" + c.Password))
		req.Header.Set("Authorization", "Basic "+token)
	}
}

// Request performs an HTTP request against the cluster.
//
// body may be nil, a string (sent verbatim), or any JSON-serializable value.
// It returns the HTTP status code and the parsed JSON (as any) or the raw
// string when the response is not valid JSON.
func (c *Client) Request(method, path string, body any, params map[string]string) (int, any, error) {
	u := c.BaseURL + "/" + strings.TrimLeft(path, "/")
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			if v != "" {
				q.Set(k, v)
			}
		}
		if enc := q.Encode(); enc != "" {
			u = u + "?" + enc
		}
	}

	var reader io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			reader = strings.NewReader(b)
		case []byte:
			reader = bytes.NewReader(b)
		default:
			data, err := json.Marshal(b)
			if err != nil {
				return 0, nil, fmt.Errorf("marshal body: %w", err)
			}
			reader = bytes.NewReader(data)
		}
	}

	req, err := http.NewRequest(strings.ToUpper(method), u, reader)
	if err != nil {
		return 0, nil, err
	}
	c.setHeaders(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("connection error: %v (%s)", err, u)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	if len(raw) == 0 {
		return resp.StatusCode, nil, nil
	}
	var parsed any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // preserve integer fidelity (e.g. _seq_no, timestamps)
	if err := dec.Decode(&parsed); err != nil {
		return resp.StatusCode, string(raw), nil
	}
	return resp.StatusCode, parsed, nil
}
