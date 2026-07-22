// Package util holds small helpers shared by the cli and tui packages:
// JSON rendering/parsing, scalar coercion, and shell/editor utilities.
package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MarshalIndent mirrors Python's json.dumps(indent=2, sort_keys=True,
// ensure_ascii=False): keys are sorted (Go sorts map keys by default) and
// non-ASCII / HTML characters are left unescaped.
func MarshalIndent(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

// Dump renders data for display: strings pass through, everything else is
// pretty-printed JSON.
func Dump(data any) string {
	if s, ok := data.(string); ok {
		return s
	}
	out, err := MarshalIndent(data)
	if err != nil {
		return fmt.Sprintf("%v", data)
	}
	return out
}

// CoerceScalar coerces a CLI --set value: JSON literal first (true, 42,
// "text", [...], {...}), otherwise the raw string.
func CoerceScalar(value string) any {
	var out any
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return value
	}
	// Ensure there is no trailing garbage (e.g. "1 2" is not a JSON scalar).
	if dec.More() {
		return value
	}
	return out
}

// LoadJSONArg accepts either an inline JSON string or @path/to/file.json.
func LoadJSONArg(value string) (any, error) {
	var raw []byte
	if strings.HasPrefix(value, "@") {
		data, err := os.ReadFile(value[1:])
		if err != nil {
			return nil, err
		}
		raw = data
	} else {
		raw = []byte(value)
	}
	var out any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return out, nil
}

// JSONEqual reports whether two decoded JSON values are equal by comparing
// their canonical marshaled form.
func JSONEqual(a, b any) bool {
	as, err1 := MarshalIndent(a)
	bs, err2 := MarshalIndent(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return as == bs
}

// AsStr renders a decoded-JSON scalar the way Python's str() would for table
// cells and query params (json.Number keeps integer fidelity).
func AsStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "True"
		}
		return "False"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// AsInt extracts an int from a decoded-JSON scalar.
func AsInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return int(n)
		}
		if f, err := t.Float64(); err == nil {
			return int(f)
		}
	case float64:
		return int(t)
	case int:
		return t
	case string:
		if n, err := strconv.Atoi(t); err == nil {
			return n
		}
	}
	return 0
}

// PadRight pads s with spaces to width w.
func PadRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}

// Clip truncates s (by rune) to at most w runes.
func Clip(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > w {
		return string(r[:w])
	}
	return s
}
