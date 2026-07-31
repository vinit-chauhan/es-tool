package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

const (
	operationDocuments          = "documents"
	operationGetDocument        = "get-document"
	operationGetDocumentForEdit = "get-document-for-edit"
	operationEditDocument       = "edit-document"
	operationDeleteDocument     = "delete-document"
)

type documentHit struct {
	id     string
	source any
	raw    map[string]any
}

func newDocumentTable() table.Model {
	model := table.New(
		table.WithColumns(documentColumns(100)),
		table.WithFocused(true),
		table.WithHeight(12),
	)
	tableStyles := table.DefaultStyles()
	tableStyles.Header = tableStyles.Header.Bold(true)
	tableStyles.Selected = styles.selected
	model.SetStyles(tableStyles)
	return model
}

func documentColumns(width int) []table.Column {
	idWidth := min(40, max(16, width/3))
	return []table.Column{
		{Title: "Document ID", Width: idWidth},
		{Title: "Preview", Width: max(20, width-idWidth-5)},
	}
}

func (m *Model) setDocumentColumns() {
	m.docTable.SetColumns(documentColumns(max(40, m.width)))
}

func fetchDocumentsCmd(m Model) tea.Cmd {
	method, body, params := searchRequest(m)
	return requestCmd(
		m.client,
		operationDocuments,
		method,
		"/"+url.PathEscape(m.currentIndex)+"/_search",
		body,
		params,
	)
}

func getDocumentCmd(client *esclient.Client, index, id, operation string) tea.Cmd {
	return requestCmd(
		client,
		operation,
		"GET",
		"/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id),
		nil,
		nil,
	)
}

func deleteDocumentCmd(client *esclient.Client, index, id string) tea.Cmd {
	return requestCmd(
		client,
		operationDeleteDocument,
		"DELETE",
		"/"+url.PathEscape(index)+"/_doc/"+url.PathEscape(id),
		nil,
		map[string]string{"refresh": "true"},
	)
}

func (m *Model) updateDocuments(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.popScreen()
	case "r":
		m.loading = true
		return fetchDocumentsCmd(*m)
	case "/":
		return m.openPrompt(promptDocFilter, "Filter visible documents:", m.docFilter)
	case "f":
		return m.openPrompt(promptServerQuery, "Lucene query:", m.query)
	case "F":
		m.pushScreen(screenSearch)
	case "s":
		return m.openPrompt(promptPageSize, "Page size (1–10000):", strconv.Itoa(m.pageSize))
	case "n":
		if m.from+m.pageSize < m.total {
			m.from += m.pageSize
			m.loading = true
			return fetchDocumentsCmd(*m)
		}
		m.status = notification{text: "Already on the last page"}
	case "p":
		if m.from > 0 {
			m.from = max(0, m.from-m.pageSize)
			m.loading = true
			return fetchDocumentsCmd(*m)
		}
		m.status = notification{text: "Already on the first page"}
	case "enter", "v":
		if hit, ok := m.selectedDocument(); ok {
			m.currentDocID = hit.id
			m.loading = true
			m.pushScreen(screenDocument)
			return getDocumentCmd(m.client, m.currentIndex, hit.id, operationGetDocument)
		}
	case "e":
		if hit, ok := m.selectedDocument(); ok {
			m.currentDocID = hit.id
			m.loading = true
			return getDocumentCmd(m.client, m.currentIndex, hit.id, operationGetDocumentForEdit)
		}
	case "a":
		return m.openPrompt(promptCreateDocumentID, "Document ID (blank for auto-generated):", "")
	case "u", "U":
		if hit, ok := m.selectedDocument(); ok {
			m.currentDocID = hit.id
			return m.openPartialUpdateEditor(hit.id, msg.String() == "U")
		}
	case "d":
		if hit, ok := m.selectedDocument(); ok {
			m.currentDocID = hit.id
			m.pendingDocID = hit.id
			return m.openPrompt(promptDeleteDocument, "Type "+hit.id+" to delete:", "")
		}
	case "D":
		if !m.hasQuery() {
			m.status = notification{text: "Set a server query before using delete-by-query", isErr: true}
			return nil
		}
		m.loading = true
		m.status = notification{text: "Counting matching documents"}
		return countForDeleteCmd(*m)
	case "i":
		m.detailTab = 0
		m.loading = true
		m.pushScreen(screenIndexDetails)
		return fetchIndexDetailsCmd(m.client, m.currentIndex)
	default:
		var cmd tea.Cmd
		m.docTable, cmd = m.docTable.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) updateDocumentView(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.popScreen()
	case "r":
		m.loading = true
		return getDocumentCmd(m.client, m.currentIndex, m.currentDocID, operationGetDocument)
	case "e":
		m.loading = true
		return getDocumentCmd(m.client, m.currentIndex, m.currentDocID, operationGetDocumentForEdit)
	case "u", "U":
		return m.openPartialUpdateEditor(m.currentDocID, msg.String() == "U")
	case "d":
		m.pendingDocID = m.currentDocID
		return m.openPrompt(promptDeleteDocument, "Type "+m.currentDocID+" to delete:", "")
	case "w":
		m.wrapJSON = !m.wrapJSON
		m.refreshDocumentViewport()
	case "i":
		m.detailTab = 0
		m.loading = true
		m.pushScreen(screenIndexDetails)
		return fetchIndexDetailsCmd(m.client, m.currentIndex)
	default:
		var cmd tea.Cmd
		m.docView, cmd = m.docView.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) receiveDocuments(body any) {
	root, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected search response: %T", body), isErr: true}
		return
	}
	hitsRoot, ok := root["hits"].(map[string]any)
	if !ok {
		m.status = notification{text: "search response did not contain hits", isErr: true}
		return
	}
	m.total = totalHits(hitsRoot["total"])
	items, _ := hitsRoot["hits"].([]any)
	m.allDocHits = make([]documentHit, 0, len(items))
	for _, item := range items {
		raw, ok := item.(map[string]any)
		if !ok {
			continue
		}
		m.allDocHits = append(m.allDocHits, documentHit{
			id:     util.AsStr(raw["_id"]),
			source: raw["_source"],
			raw:    raw,
		})
	}
	m.applyDocumentFilter()
	m.status = notification{text: fmt.Sprintf("Loaded %d documents", len(m.docHits))}
}

func totalHits(value any) int {
	if object, ok := value.(map[string]any); ok {
		return util.AsInt(object["value"])
	}
	return util.AsInt(value)
}

func (m *Model) applyDocumentFilter() {
	filter := strings.ToLower(strings.TrimSpace(m.docFilter))
	rows := make([]table.Row, 0, len(m.allDocHits))
	filtered := make([]documentHit, 0, len(m.allDocHits))
	for _, hit := range m.allDocHits {
		searchable := hit.id + " " + util.Dump(hit.source)
		if filter != "" && !strings.Contains(strings.ToLower(searchable), filter) {
			continue
		}
		filtered = append(filtered, hit)
		rows = append(rows, table.Row{highlightMatch(hit.id, filter), highlightMatch(documentPreview(hit.source), filter)})
	}
	m.docHits = filtered
	m.docTable.SetRows(rows)
	if m.docTable.Cursor() >= len(rows) {
		m.docTable.SetCursor(max(0, len(rows)-1))
	}
}

func documentPreview(source any) string {
	if source == nil {
		return ""
	}
	object, ok := source.(map[string]any)
	if !ok {
		return compactJSON(source)
	}
	parts := make([]string, 0, 3)
	for _, key := range []string{"name", "title", "status", "state", "@timestamp", "id"} {
		if value := util.AsStr(object[key]); value != "" {
			parts = append(parts, key+"="+value)
		}
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return compactJSON(source)
	}
	return strings.Join(parts, " • ")
}

func compactJSON(value any) string {
	return strings.Join(strings.Fields(util.Dump(value)), " ")
}

func (m Model) selectedDocument() (documentHit, bool) {
	cursor := m.docTable.Cursor()
	if cursor < 0 || cursor >= len(m.docHits) {
		return documentHit{}, false
	}
	return m.docHits[cursor], true
}

func (m *Model) receiveDocument(body any) {
	document, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected document response: %T", body), isErr: true}
		return
	}
	m.currentDoc = document
	m.currentDocID = util.AsStr(document["_id"])
	m.refreshDocumentViewport()
	m.status = notification{text: "Document loaded"}
}

func (m *Model) refreshDocumentViewport() {
	if m.currentDoc == nil {
		return
	}
	text := util.Dump(m.currentDoc)
	if m.wrapJSON {
		text = wrapLines(text, max(10, m.docView.Width))
	}
	m.docView.SetContent(highlightJSON(text))
}

func wrapLines(text string, width int) string {
	if width <= 0 {
		return text
	}
	var output []string
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		for len(runes) > width {
			output = append(output, string(runes[:width]))
			runes = runes[width:]
		}
		output = append(output, string(runes))
	}
	return strings.Join(output, "\n")
}

func parsePageSize(value string) (int, error) {
	size, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || size < 1 || size > 10000 {
		return 0, fmt.Errorf("page size must be an integer from 1 to 10000")
	}
	return size, nil
}
