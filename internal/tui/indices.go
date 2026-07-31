package tui

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/util"
)

const (
	operationIndices      = "indices"
	operationIndexDetails = "index-details"
)

func newIndexTable() table.Model {
	model := table.New(
		table.WithColumns(indexColumns(100)),
		table.WithFocused(true),
		table.WithHeight(12),
	)
	tableStyles := table.DefaultStyles()
	tableStyles.Header = tableStyles.Header.
		BorderStyle(tableStyles.Header.GetBorderStyle()).
		BorderForeground(styles.dim.GetForeground()).
		Bold(true)
	tableStyles.Selected = styles.selected
	model.SetStyles(tableStyles)
	return model
}

func indexColumns(width int) []table.Column {
	// The table adds 2 cells of padding per column (5 columns = 10 cells),
	// so the content widths must sum to at most width-10 or rows overflow
	// the terminal, wrap, and push the footer off-screen.
	remaining := max(16, width-49)
	return []table.Column{
		{Title: "Health", Width: 9},
		{Title: "Status", Width: 8},
		{Title: "Index", Width: remaining},
		{Title: "Docs", Width: 10},
		{Title: "Store", Width: 10},
	}
}

func (m *Model) setIndexColumns() {
	m.indexTable.SetColumns(indexColumns(max(30, m.width)))
}

func fetchIndicesCmd(m Model) tea.Cmd {
	expand := "open,closed"
	if m.showHidden {
		expand = "all"
	}
	return requestCmd(m.client, m.connEpoch, operationIndices, "GET", "/_cat/indices", nil, map[string]string{
		"format":           "json",
		"v":                "true",
		"expand_wildcards": expand,
	})
}

func fetchIndexDetailsCmd(m Model, index string) tea.Cmd {
	return requestCmd(
		m.client,
		m.connEpoch,
		operationIndexDetails,
		"GET",
		"/"+url.PathEscape(index),
		nil,
		map[string]string{"features": "settings,mappings"},
	)
}

func (m *Model) updateIndices(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "r":
		m.loading = true
		m.status = notification{text: "Refreshing indices"}
		return fetchIndicesCmd(*m)
	case "h":
		m.showHidden = !m.showHidden
		m.loading = true
		return fetchIndicesCmd(*m)
	case "/":
		return m.openPrompt(promptIndexFilter, "Filter indices:", m.indexFilter)
	case "left":
		if m.indexPage > 0 {
			m.indexPage--
			m.renderIndexPage()
		}
	case "right":
		if (m.indexPage+1)*m.indexPageSize() < len(m.filteredIndices) {
			m.indexPage++
			m.renderIndexPage()
		}
	case "enter":
		if index := m.selectedIndex(); index != "" {
			m.currentIndex = index
			m.from = 0
			m.loading = true
			m.pushScreen(screenDocuments)
			m.status = notification{text: "Opening " + index}
			return fetchDocumentsCmd(*m)
		}
	case "i":
		if index := m.selectedIndex(); index != "" {
			m.currentIndex = index
			m.detailTab = 0
			m.loading = true
			m.pushScreen(screenIndexDetails)
			return fetchIndexDetailsCmd(*m, index)
		}
	case "c":
		m.pushScreen(screenClusterInfo)
		m.loading = true
		return fetchClusterInfoCmd(*m)
	default:
		var cmd tea.Cmd
		m.indexTable, cmd = m.indexTable.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) updateIndexDetails(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.popScreen()
	case "tab", "right", "left":
		m.detailTab = 1 - m.detailTab
		m.detailView.SetContent(m.detailText[m.detailTab])
		m.detailView.GotoTop()
	case "r":
		m.loading = true
		return fetchIndexDetailsCmd(*m, m.currentIndex)
	default:
		var cmd tea.Cmd
		m.detailView, cmd = m.detailView.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) receiveIndices(body any) {
	rows, ok := body.([]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected indices response: %T", body), isErr: true}
		return
	}
	m.allIndices = make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		if row, ok := item.(map[string]any); ok {
			m.allIndices = append(m.allIndices, row)
		}
	}
	sort.Slice(m.allIndices, func(i, j int) bool {
		return util.AsStr(m.allIndices[i]["index"]) < util.AsStr(m.allIndices[j]["index"])
	})
	m.indexPage = 0
	m.applyIndexFilter()
	m.status = notification{text: fmt.Sprintf("Loaded %d indices", len(m.filteredIndices))}
}

// indexPageSize returns how many index rows fit on one page, based on the
// table's visible height.
func (m *Model) indexPageSize() int {
	return max(1, m.indexTable.Height())
}

func (m *Model) applyIndexFilter() {
	filter := strings.ToLower(strings.TrimSpace(m.indexFilter))
	m.filteredIndices = make([]map[string]any, 0, len(m.allIndices))
	for _, item := range m.allIndices {
		name := util.AsStr(item["index"])
		if !m.showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		m.filteredIndices = append(m.filteredIndices, item)
	}
	m.indexPage = 0
	m.renderIndexPage()
}

// renderIndexPage slices the filtered indices to the current page and loads
// only those rows into the table, so up/down movement and left/right paging
// operate on discrete pages instead of one continuously scrolling list.
func (m *Model) renderIndexPage() {
	pageSize := m.indexPageSize()
	totalPages := max(1, (len(m.filteredIndices)+pageSize-1)/pageSize)
	if m.indexPage >= totalPages {
		m.indexPage = totalPages - 1
	}
	if m.indexPage < 0 {
		m.indexPage = 0
	}
	start := m.indexPage * pageSize
	end := min(start+pageSize, len(m.filteredIndices))

	rows := make([]table.Row, 0, end-start)
	for _, item := range m.filteredIndices[start:end] {
		rows = append(rows, table.Row{
			util.AsStr(item["health"]),
			util.AsStr(item["status"]),
			util.AsStr(item["index"]),
			util.AsStr(item["docs.count"]),
			util.AsStr(item["store.size"]),
		})
	}
	m.indexTable.SetRows(rows)
	m.indexTable.SetCursor(0)
}

func (m Model) selectedIndex() string {
	row := m.indexTable.SelectedRow()
	if len(row) < 3 {
		return ""
	}
	return row[2]
}

func (m *Model) receiveIndexDetails(body any) {
	root, ok := body.(map[string]any)
	if !ok {
		m.status = notification{text: fmt.Sprintf("unexpected index response: %T", body), isErr: true}
		return
	}
	entry, ok := root[m.currentIndex].(map[string]any)
	if !ok {
		m.status = notification{text: "index details missing for " + m.currentIndex, isErr: true}
		return
	}
	m.detailText[0] = highlightJSON(util.Dump(entry["settings"]))
	m.detailText[1] = highlightJSON(util.Dump(entry["mappings"]))
	m.detailView.SetContent(m.detailText[m.detailTab])
	m.detailView.GotoTop()
	m.status = notification{text: "Index metadata loaded"}
}
