package tui

import (
	"fmt"
	"net/url"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/util"
)

const (
	operationCountForDelete = "count-for-delete"
	operationDeleteByQuery  = "delete-by-query"
)

func countForDeleteCmd(m Model) tea.Cmd {
	return requestCmd(
		m.client,
		operationCountForDelete,
		"GET",
		"/"+url.PathEscape(m.currentIndex)+"/_count",
		nil,
		map[string]string{"q": m.query},
	)
}

func deleteByQueryCmd(m Model) tea.Cmd {
	return requestCmd(
		m.client,
		operationDeleteByQuery,
		"POST",
		"/"+url.PathEscape(m.currentIndex)+"/_delete_by_query",
		nil,
		map[string]string{"q": m.query, "refresh": "true"},
	)
}

func (m *Model) receiveDeleteCount(body any) tea.Cmd {
	response, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected count response: %T", body), isErr: true}
		return nil
	}
	count := util.AsInt(response["count"])
	if count == 0 {
		m.status = notification{text: "No matching documents to delete"}
		return nil
	}
	m.pendingDeleteCount = count
	phrase := fmt.Sprintf("delete %d", count)
	m.status = notification{text: fmt.Sprintf("%d documents match; this cannot be undone", count), isErr: true}
	return m.openPrompt(promptDeleteByQuery, "Type "+phrase+" to confirm:", "")
}

func deleteByQueryResult(body any) string {
	if response, ok := body.(map[string]any); ok {
		return fmt.Sprintf("Deleted %d documents", util.AsInt(response["deleted"]))
	}
	return "Delete-by-query completed"
}
