package util

import "testing"

func TestAsStr(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"true", true, "true"},
		{"false", false, "false"},
		{"number", 42, "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AsStr(tc.in); got != tc.want {
				t.Errorf("AsStr(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAsInt(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"float64", float64(7), 7},
		{"int", 3, 3},
		{"numeric string", "12", 12},
		{"non-numeric string", "nope", 0},
		{"nil", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AsInt(tc.in); got != tc.want {
				t.Errorf("AsInt(%#v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestDump(t *testing.T) {
	if got := Dump("already a string"); got != "already a string" {
		t.Errorf("Dump(string) = %q, want passthrough", got)
	}
	got := Dump(map[string]any{"b": 1, "a": 2})
	want := "{\n  \"a\": 2,\n  \"b\": 1\n}"
	if got != want {
		t.Errorf("Dump(map) = %q, want %q", got, want)
	}
}

func TestJSONEqual(t *testing.T) {
	a := map[string]any{"x": 1, "y": "z"}
	b := map[string]any{"y": "z", "x": 1}
	if !JSONEqual(a, b) {
		t.Error("JSONEqual should treat differently-ordered maps as equal")
	}
	c := map[string]any{"x": 2}
	if JSONEqual(a, c) {
		t.Error("JSONEqual should not treat differing values as equal")
	}
}

func TestCoerceScalar(t *testing.T) {
	if v := CoerceScalar("true"); v != true {
		t.Errorf("CoerceScalar(true) = %#v, want true", v)
	}
	if v := CoerceScalar("hello world"); v != "hello world" {
		t.Errorf("CoerceScalar(raw) = %#v, want raw string", v)
	}
}

func TestPadRightAndClip(t *testing.T) {
	if got := PadRight("ab", 5); got != "ab   " {
		t.Errorf("PadRight = %q", got)
	}
	if got := PadRight("abcdef", 3); got != "abcdef" {
		t.Errorf("PadRight should not truncate, got %q", got)
	}
	if got := Clip("abcdef", 3); got != "abc" {
		t.Errorf("Clip = %q, want abc", got)
	}
	if got := Clip("ab", 0); got != "" {
		t.Errorf("Clip with width 0 should be empty, got %q", got)
	}
}
