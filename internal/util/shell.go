package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EditInEditor writes initial as pretty JSON to a temp file, opens $EDITOR,
// and parses the edited result back.
func EditInEditor(initial any) (any, error) {
	text, err := MarshalIndent(initial)
	if err != nil {
		return nil, err
	}
	edited, err := ShellEdit(text+"\n", ".json")
	if err != nil {
		return nil, err
	}
	var out any
	dec := json.NewDecoder(strings.NewReader(edited))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("edited file is not valid JSON: %w", err)
	}
	return out, nil
}

// ShellEdit runs $EDITOR (or VISUAL, or vi) on a temp file pre-populated with
// initialText and returns the edited contents.
func ShellEdit(initialText, suffix string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	tmp, err := os.CreateTemp("", "es-tool-*"+suffix)
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	defer os.Remove(path)

	if !strings.HasSuffix(initialText, "\n") {
		initialText += "\n"
	}
	if _, err := tmp.WriteString(initialText); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()

	// ShellSplit lets users export EDITOR="code --wait".
	parts := ShellSplit(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	args := append(parts[1:], path)
	cmd := exec.Command(parts[0], args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ShellSplit splits a command line into tokens, honoring single and double
// quotes and backslash escapes, similar to Python's shlex.split. On a parse
// error (e.g. unclosed quote) it returns the tokens parsed so far.
func ShellSplit(s string) []string {
	tokens, _ := ShellSplitErr(s)
	return tokens
}

// ShellSplitErr is like ShellSplit but reports quoting errors.
func ShellSplitErr(s string) ([]string, error) {
	var tokens []string
	var cur strings.Builder
	inToken := false

	const (
		none = iota
		single
		double
	)
	quote := none

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch quote {
		case single:
			if c == '\'' {
				quote = none
			} else {
				cur.WriteRune(c)
			}
		case double:
			if c == '\\' && i+1 < len(runes) {
				next := runes[i+1]
				if next == '"' || next == '\\' || next == '$' || next == '`' {
					cur.WriteRune(next)
					i++
					continue
				}
				cur.WriteRune(c)
			} else if c == '"' {
				quote = none
			} else {
				cur.WriteRune(c)
			}
		default: // none
			switch {
			case c == '\'':
				quote = single
				inToken = true
			case c == '"':
				quote = double
				inToken = true
			case c == '\\' && i+1 < len(runes):
				cur.WriteRune(runes[i+1])
				i++
				inToken = true
			case c == ' ' || c == '\t' || c == '\n' || c == '\r':
				if inToken {
					tokens = append(tokens, cur.String())
					cur.Reset()
					inToken = false
				}
			default:
				cur.WriteRune(c)
				inToken = true
			}
		}
	}

	if quote != none {
		if inToken {
			tokens = append(tokens, cur.String())
		}
		return tokens, errors.New("no closing quotation")
	}
	if inToken {
		tokens = append(tokens, cur.String())
	}
	return tokens, nil
}
