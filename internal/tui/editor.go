package tui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/util"
)

type editorMode int

const (
	editorFullDocument editorMode = iota
)

type editorDoneMsg struct {
	mode        editorMode
	index       string
	id          string
	original    any
	seqNo       string
	primaryTerm string
	raw         string
	err         error
}

func (m *Model) openDocumentEditor(body any) tea.Cmd {
	document, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected document response: %T", body), isErr: true}
		return nil
	}
	source, ok := document["_source"]
	if !ok {
		m.status = notification{text: "document has no _source", isErr: true}
		return nil
	}
	m.currentDoc = document
	command, err := jsonEditorCmd(editorDoneMsg{
		mode:        editorFullDocument,
		index:       m.currentIndex,
		id:          util.AsStr(document["_id"]),
		original:    source,
		seqNo:       util.AsStr(document["_seq_no"]),
		primaryTerm: util.AsStr(document["_primary_term"]),
	}, source)
	if err != nil {
		m.status = notification{text: "open editor: " + err.Error(), isErr: true}
		return nil
	}
	m.status = notification{text: "Waiting for editor"}
	return command
}

func jsonEditorCmd(result editorDoneMsg, initial any) (tea.Cmd, error) {
	text, err := util.MarshalIndent(initial)
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	file, err := os.CreateTemp("", "es-tool-*.json")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return nil, err
	}
	if _, err := file.WriteString(text + "\n"); err != nil {
		cleanup()
		return nil, err
	}
	if err := file.Close(); err != nil {
		cleanup()
		return nil, err
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	parts, err := util.ShellSplitErr(editor)
	if err != nil || len(parts) == 0 {
		cleanup()
		if err == nil {
			err = fmt.Errorf("editor command is empty")
		}
		return nil, err
	}
	command := exec.Command(parts[0], append(parts[1:], path)...)
	return tea.ExecProcess(command, func(processErr error) tea.Msg {
		defer os.Remove(path)
		result.err = processErr
		if processErr == nil {
			data, readErr := os.ReadFile(path)
			result.raw = string(data)
			result.err = readErr
		}
		return result
	}), nil
}

func (m *Model) handleEditorDone(msg editorDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.status = notification{text: "editor failed: " + msg.err.Error(), isErr: true}
		return nil
	}
	var edited any
	decoder := json.NewDecoder(strings.NewReader(msg.raw))
	decoder.UseNumber()
	if err := decoder.Decode(&edited); err != nil {
		m.status = notification{text: "edited file is not valid JSON: " + err.Error(), isErr: true}
		return nil
	}
	if _, ok := edited.(map[string]any); !ok {
		m.status = notification{text: "document _source must be a JSON object", isErr: true}
		return nil
	}
	if util.JSONEqual(edited, msg.original) {
		m.status = notification{text: "No changes to save"}
		return nil
	}

	params := map[string]string{"refresh": "true"}
	if msg.seqNo != "" && msg.primaryTerm != "" {
		params["if_seq_no"] = msg.seqNo
		params["if_primary_term"] = msg.primaryTerm
	}
	m.loading = true
	return requestCmd(
		m.client,
		operationEditDocument,
		"PUT",
		"/"+url.PathEscape(msg.index)+"/_doc/"+url.PathEscape(msg.id),
		edited,
		params,
	)
}
