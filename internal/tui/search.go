package tui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/util"
)

const operationExactCount = "exact-count"

// searchRequest builds the _search call for the current query state. Keys the
// JSON body already defines are left out of the URL parameters, because
// Elasticsearch rejects a request that sets the same option twice.
func searchRequest(m Model) (string, any, map[string]string) {
	params := map[string]string{"seq_no_primary_term": "true"}
	set := func(key, value string) {
		if value == "" {
			return
		}
		if _, defined := m.searchBody[key]; defined {
			return
		}
		params[key] = value
	}
	set("from", strconv.Itoa(m.from))
	set("size", strconv.Itoa(m.pageSize))
	set("track_total_hits", "true")
	set("sort", m.sort)
	if m.idsOnly {
		set("_source", "false")
	} else {
		set("_source", m.source)
	}
	if m.searchBody != nil {
		return "POST", m.searchBody, params
	}
	set("q", m.query)
	return "GET", nil, params
}

// countRequest narrows the current query state down to the parts _count and
// _delete_by_query accept.
func countRequest(m Model) (string, any, map[string]string) {
	if query, ok := m.searchBody["query"]; ok {
		return "POST", map[string]any{"query": query}, nil
	}
	if m.query != "" {
		return "GET", nil, map[string]string{"q": m.query}
	}
	return "GET", nil, nil
}

func (m Model) hasQuery() bool {
	if _, ok := m.searchBody["query"]; ok {
		return true
	}
	return m.query != ""
}

func exactCountCmd(m Model) tea.Cmd {
	method, body, params := countRequest(m)
	return requestCmd(
		m.client,
		operationExactCount,
		method,
		"/"+url.PathEscape(m.currentIndex)+"/_count",
		body,
		params,
	)
}

func (m *Model) receiveExactCount(body any) {
	response, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected count response: %T", body), isErr: true}
		return
	}
	m.exactCount = util.AsInt(response["count"])
	m.status = notification{text: fmt.Sprintf("%s matches %d documents", m.currentIndex, m.exactCount)}
}

func (m *Model) updateSearch(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "b":
		m.popScreen()
	case "f":
		return m.openPrompt(promptSearchQuery, "Lucene query:", m.query)
	case "s":
		return m.openPrompt(promptSearchSort, "Sort (field:asc,field:desc):", m.sort)
	case "o":
		return m.openPrompt(promptSearchSource, "Source fields (comma separated):", m.source)
	case "j":
		return m.openSearchBodyEditor()
	case "i":
		m.idsOnly = !m.idsOnly
	case "x":
		m.query, m.sort, m.source = "", "", ""
		m.searchBody = nil
		m.idsOnly = false
		m.exactCount = 0
		m.status = notification{text: "Query builder reset"}
	case "c":
		if !m.hasQuery() {
			m.status = notification{text: "Counting every document in " + m.currentIndex}
		}
		m.loading = true
		return exactCountCmd(*m)
	case "enter":
		m.from = 0
		m.loading = true
		m.popScreen()
		return fetchDocumentsCmd(*m)
	}
	return nil
}

func (m Model) searchView() string {
	body := styles.dim.Render("(not set)")
	if m.searchBody != nil {
		body = util.Clip(compactJSON(m.searchBody), max(20, m.width-32))
	}
	source := valueOrEmpty(m.source)
	if m.idsOnly {
		source = styles.dim.Render("(ignored while IDs only is on)")
	}
	rows := []string{
		fieldRow("f", "Lucene query", valueOrEmpty(m.query)),
		fieldRow("s", "Sort", valueOrEmpty(m.sort)),
		fieldRow("o", "Source fields", source),
		fieldRow("j", "JSON body", body),
		fieldRow("i", "IDs only", map[bool]string{true: "yes", false: "no"}[m.idsOnly]),
	}
	if m.searchBody != nil && m.query != "" {
		rows = append(rows, "", styles.dim.Render("The JSON body wins; the Lucene query is not sent."))
	}
	actions := styles.key.Render("enter") + " Run against " + m.currentIndex +
		"   " + styles.key.Render("c") + " Exact count" +
		"   " + styles.key.Render("x") + " Reset"
	if m.exactCount > 0 {
		actions += "\n" + styles.subtitle.Render(fmt.Sprintf("Last exact count: %d", m.exactCount))
	}
	return styles.panel.Width(max(32, m.width-4)).Render(
		styles.title.Render("Query builder") + "\n\n" + strings.Join(rows, "\n") + "\n\n" + actions,
	)
}

// querySummary describes the active query state for the document list header.
func (m Model) querySummary() string {
	parts := make([]string, 0, 4)
	switch {
	case m.searchBody != nil:
		parts = append(parts, "JSON body")
	case m.query != "":
		parts = append(parts, "q="+m.query)
	}
	if m.sort != "" {
		parts = append(parts, "sort="+m.sort)
	}
	switch {
	case m.idsOnly:
		parts = append(parts, "IDs only")
	case m.source != "":
		parts = append(parts, "_source="+m.source)
	}
	if len(parts) == 0 {
		return ""
	}
	return " • " + strings.Join(parts, " • ")
}
