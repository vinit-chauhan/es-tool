// Package tui implements es-tool's interactive Bubble Tea interface.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

type screenKind int

const (
	screenIndices screenKind = iota
	screenDocuments
	screenDocument
	screenIndexDetails
	screenSettings
	screenClusterEditor
	screenSearch
	screenClusterInfo
)

type promptKind int

const (
	promptNone promptKind = iota
	promptIndexFilter
	promptDocFilter
	promptServerQuery
	promptPageSize
	promptDeleteDocument
	promptClusterName
	promptClusterURL
	promptClusterAPIKey
	promptClusterUser
	promptClusterPassword
	promptQuickConnectURL
	promptDeleteProfile
	promptCreateDocumentID
	promptDeleteByQuery
	promptSearchQuery
	promptSearchSort
	promptSearchSource
	promptDiscardEdits
)

type notification struct {
	text  string
	isErr bool
}

// Model owns global application state and delegates events to the active
// screen. A history stack makes every screen consistently navigable.
type Model struct {
	client        *esclient.Client
	store         *appconfig.Store
	config        appconfig.Config
	activeCluster string

	screen  screenKind
	history []screenKind
	width   int
	height  int

	spinner  spinner.Model
	loading  bool
	health   healthStatus
	status   notification
	showHelp bool
	helpView viewport.Model

	prompt       textinput.Model
	promptKind   promptKind
	promptLabel  string
	promptSecret bool

	allIndices  []map[string]any
	indexTable  table.Model
	indexFilter string
	showHidden  bool

	currentIndex string
	detailTab    int
	detailView   viewport.Model
	detailText   [2]string

	infoView viewport.Model
	infoText string

	docTable           table.Model
	allDocHits         []documentHit
	docHits            []documentHit
	docFilter          string
	query              string
	sort               string
	source             string
	searchBody         map[string]any
	idsOnly            bool
	pageSize           int
	from               int
	total              int
	exactCount         int
	currentDocID       string
	currentDoc         map[string]any
	docView            viewport.Model
	wrapJSON           bool
	pendingDocID       string
	pendingDeleteCount int

	settingsTable   table.Model
	profileHealth   map[string]healthStatus
	editingCluster  appconfig.Cluster
	editingOriginal string
	editingAuth     string
	editingBaseline appconfig.Cluster
	baselineAuth    string
	editorCursor    int
	pendingDiscard  string
	pendingProfile  string
}

// Run launches the full-screen TUI.
func Run(client *esclient.Client, startIndex string) error {
	model, err := newModel(client, startIndex)
	if err != nil {
		return err
	}
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func newModel(client *esclient.Client, startIndex string) (Model, error) {
	store, err := appconfig.DefaultStore()
	if err != nil {
		return Model{}, fmt.Errorf("settings unavailable: %w", err)
	}
	cfg, loadErr := store.Load()
	status := notification{}
	if loadErr != nil {
		cfg = appconfig.New()
		status = notification{text: "saved settings ignored: " + loadErr.Error(), isErr: true}
	}

	active := ""
	if !esclient.EnvConfigured() && cfg.Active != "" {
		if cluster, ok := cfg.Find(cfg.Active); ok {
			configureClient(client, cluster)
			active = cluster.Name
		}
	}

	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = styles.spinner
	input := textinput.New()
	input.CharLimit = 4096

	model := Model{
		client:        client,
		store:         store,
		config:        cfg,
		activeCluster: active,
		screen:        screenIndices,
		spinner:       spin,
		loading:       true,
		health:        healthStatus{state: stateHealthChecking},
		status:        status,
		prompt:        input,
		indexTable:    newIndexTable(),
		detailView:    viewport.New(0, 0),
		infoView:      viewport.New(0, 0),
		docTable:      newDocumentTable(),
		pageSize:      50,
		docView:       viewport.New(0, 0),
		settingsTable: newSettingsTable(),
		profileHealth: map[string]healthStatus{},
		helpView:      viewport.New(0, 0),
	}
	if startIndex != "" {
		model.screen = screenDocuments
		model.currentIndex = startIndex
		model.status = notification{text: "Document browser is being initialized"}
	}
	return model, nil
}

func configureClient(client *esclient.Client, cluster appconfig.Cluster) {
	client.Configure(esclient.Options{
		BaseURL:   cluster.URL,
		APIKey:    cluster.APIKey,
		User:      cluster.User,
		Password:  cluster.Password,
		VerifyTLS: cluster.VerifyTLS,
	})
}

func (m Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.spinner.Tick, healthCmd(m.client)}
	if m.screen == screenIndices {
		commands = append(commands, fetchIndicesCmd(m.client, m.showHidden))
	} else if m.screen == screenDocuments {
		commands = append(commands, fetchDocumentsCmd(m))
	}
	return tea.Batch(commands...)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	var commands []tea.Cmd
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		commands = append(commands, cmd)
	case healthMsg:
		m.loading = false
		m.health = healthFromResponse(msg.status, msg.body, msg.err)
	case requestMsg:
		m.loading = false
		if msg.err != nil {
			m.status = notification{text: msg.err.Error(), isErr: true}
		} else {
			m.health = healthStatus{state: stateHealthConnected}
			if cmd := m.handleResponse(msg); cmd != nil {
				commands = append(commands, cmd)
			}
		}
	case editorDoneMsg:
		m.loading = false
		if cmd := m.handleEditorDone(msg); cmd != nil {
			commands = append(commands, cmd)
		}
	case clusterInfoMsg:
		m.loading = false
		m.receiveClusterInfo(msg)
	case profileHealthMsg:
		m.receiveProfileHealth(msg)
	case tea.KeyMsg:
		if m.promptKind != promptNone {
			return m.updatePrompt(msg)
		}
		if m.showHelp {
			switch msg.String() {
			case "?", "esc", "q":
				m.showHelp = false
				return m, nil
			default:
				var cmd tea.Cmd
				m.helpView, cmd = m.helpView.Update(msg)
				return m, cmd
			}
		}
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "q" {
			if m.screen == screenClusterEditor {
				return m, m.confirmLeaveEditor("quit")
			}
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.showHelp = true
			m.helpView.SetContent(renderHelpContent())
			m.helpView.GotoTop()
			return m, nil
		}
		if msg.String() == "." && m.screen != screenSettings && m.screen != screenClusterEditor {
			m.refreshSettingsRows()
			m.pushScreen(screenSettings)
			return m, m.checkProfileHealth()
		}
		cmd := m.updateScreen(msg)
		if cmd != nil {
			commands = append(commands, cmd)
		}
	}
	return m, tea.Batch(commands...)
}

func (m *Model) resize() {
	contentHeight := max(3, m.height-6)
	m.indexTable.SetHeight(contentHeight)
	m.indexTable.SetWidth(max(20, m.width-2))
	m.setIndexColumns()
	m.detailView.Width = max(10, m.width-4)
	m.detailView.Height = contentHeight
	m.infoView.Width = max(10, m.width-4)
	m.infoView.Height = contentHeight
	m.docTable.SetHeight(contentHeight)
	m.docTable.SetWidth(max(20, m.width-2))
	m.setDocumentColumns()
	m.docView.Width = max(10, m.width-4)
	m.docView.Height = contentHeight
	m.settingsTable.SetHeight(contentHeight)
	m.settingsTable.SetWidth(max(20, m.width-2))
	m.setSettingsColumns()
	m.helpView.Width = max(10, m.width-4)
	m.helpView.Height = contentHeight
	if m.currentDoc != nil {
		m.refreshDocumentViewport()
	}
}

func (m *Model) updateScreen(msg tea.KeyMsg) tea.Cmd {
	switch m.screen {
	case screenIndices:
		return m.updateIndices(msg)
	case screenIndexDetails:
		return m.updateIndexDetails(msg)
	case screenDocuments:
		return m.updateDocuments(msg)
	case screenDocument:
		return m.updateDocumentView(msg)
	case screenSettings:
		return m.updateSettings(msg)
	case screenClusterEditor:
		return m.updateClusterEditor(msg)
	case screenSearch:
		return m.updateSearch(msg)
	case screenClusterInfo:
		return m.updateClusterInfo(msg)
	}
	return nil
}

func (m Model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePrompt()
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.prompt.Value())
		kind := m.promptKind
		m.closePrompt()
		switch kind {
		case promptIndexFilter:
			m.indexFilter = value
			m.applyIndexFilter()
		case promptDocFilter:
			m.docFilter = value
			m.applyDocumentFilter()
		case promptServerQuery:
			m.query = value
			m.from = 0
			m.loading = true
			return m, fetchDocumentsCmd(m)
		case promptPageSize:
			size, err := parsePageSize(value)
			if err != nil {
				m.status = notification{text: err.Error(), isErr: true}
			} else {
				m.pageSize = size
				m.from = 0
				m.loading = true
				return m, fetchDocumentsCmd(m)
			}
		case promptDeleteDocument:
			if value != m.pendingDocID {
				m.status = notification{text: "Delete cancelled: document id did not match", isErr: true}
			} else {
				m.loading = true
				return m, deleteDocumentCmd(m.client, m.currentIndex, m.pendingDocID)
			}
		case promptClusterName:
			m.editingCluster.Name = value
		case promptClusterURL:
			m.editingCluster.URL = value
		case promptClusterAPIKey:
			m.editingCluster.APIKey = value
		case promptClusterUser:
			m.editingCluster.User = value
		case promptClusterPassword:
			m.editingCluster.Password = value
		case promptQuickConnectURL:
			if cmd := m.quickConnect(value); cmd != nil {
				return m, cmd
			}
		case promptDeleteProfile:
			if value != m.pendingProfile {
				m.status = notification{text: "Delete cancelled: profile name did not match", isErr: true}
			} else {
				m.deleteProfile(m.pendingProfile)
			}
		case promptCreateDocumentID:
			if cmd := m.openCreateDocumentEditor(value); cmd != nil {
				return m, cmd
			}
		case promptDeleteByQuery:
			expected := fmt.Sprintf("delete %d", m.pendingDeleteCount)
			if value != expected {
				m.status = notification{text: "Bulk delete cancelled: confirmation did not match", isErr: true}
			} else {
				m.loading = true
				return m, deleteByQueryCmd(m)
			}
		case promptSearchQuery:
			m.query = value
		case promptSearchSort:
			m.sort = value
		case promptSearchSource:
			m.source = value
		case promptDiscardEdits:
			answer := strings.ToLower(value)
			if answer != "y" && answer != "yes" {
				m.status = notification{text: "Kept unsaved changes"}
				return m, nil
			}
			if m.pendingDiscard == "quit" {
				return m, tea.Quit
			}
			m.popScreen()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m *Model) openPrompt(kind promptKind, label, value string) tea.Cmd {
	return m.openPromptValue(kind, label, value, false)
}

func (m *Model) openSecretPrompt(kind promptKind, label, value string) tea.Cmd {
	return m.openPromptValue(kind, label, value, true)
}

func (m *Model) openPromptValue(kind promptKind, label, value string, secret bool) tea.Cmd {
	m.promptKind = kind
	m.promptLabel = label
	m.promptSecret = secret
	if secret {
		m.prompt.EchoMode = textinput.EchoPassword
		m.prompt.EchoCharacter = '•'
	} else {
		m.prompt.EchoMode = textinput.EchoNormal
	}
	m.prompt.SetValue(value)
	m.prompt.CursorEnd()
	m.prompt.Focus()
	return textinput.Blink
}

func (m *Model) closePrompt() {
	m.prompt.Blur()
	m.promptKind = promptNone
	m.promptLabel = ""
	m.promptSecret = false
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	subtitle := m.client.BaseURL
	if m.activeCluster != "" {
		subtitle = m.activeCluster + " • " + subtitle
	}
	header := renderHeader(m.screenTitle(), subtitle, m.health, m.width)
	body := m.screenView()
	hint := m.screenHint()
	if m.showHelp {
		body = styles.title.Render("Keyboard shortcuts") + "\n" + m.helpView.View()
		hint = "↑/↓ j/k scroll • pgup/pgdn page • ?/esc/q close"
	}
	footer := renderFooter(m.status, hint, m.width)
	if m.promptKind != promptNone {
		promptLine := styles.key.Render(m.promptLabel) + " " + m.prompt.View()
		footer = promptLine + "\n" + renderFooter(m.status, "enter: apply • esc: cancel", m.width)
	}
	if m.loading {
		body = m.spinner.View() + " " + styles.dim.Render("Loading…") + "\n" + body
	}

	// Stretch or clamp the body to exactly fill the terminal so the footer
	// (and any active prompt above it) is always on the bottom rows, no
	// matter how short or tall the current screen's content is.
	if m.height > 0 {
		bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
		lines := strings.Split(body, "\n")
		if len(lines) > bodyHeight {
			lines = lines[:max(0, bodyHeight)]
		}
		for len(lines) < bodyHeight {
			lines = append(lines, "")
		}
		body = strings.Join(lines, "\n")
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m Model) screenTitle() string {
	switch m.screen {
	case screenDocuments:
		return "Documents • " + m.currentIndex
	case screenDocument:
		return "Document"
	case screenIndexDetails:
		return "Index details • " + m.currentIndex
	case screenSettings:
		return "Cluster settings"
	case screenClusterEditor:
		return "Edit cluster"
	case screenSearch:
		return "Advanced search"
	case screenClusterInfo:
		return "Cluster info"
	default:
		return "Indices"
	}
}

func (m Model) screenView() string {
	switch m.screen {
	case screenIndices:
		if len(m.indexTable.Rows()) == 0 && !m.loading {
			return styles.panel.Width(max(20, m.width-4)).Render("No indices match the current filter.")
		}
		return m.indexTable.View()
	case screenIndexDetails:
		tab := styles.key.Render("[" + []string{"Settings", "Mappings"}[m.detailTab] + "]")
		return tab + "\n" + m.detailView.View()
	case screenDocuments:
		summary := fmt.Sprintf("Showing %d–%d of %d", min(m.from+1, m.total), min(m.from+len(m.docHits), m.total), m.total)
		return styles.subtitle.Render(summary+m.querySummary()) + "\n" + m.docTable.View()
	case screenDocument:
		return m.docView.View()
	case screenSettings:
		if len(m.settingsTable.Rows()) == 0 {
			return styles.panel.Width(max(20, m.width-4)).Render("No saved profiles. Press a to add one or c for a session-only connection.")
		}
		return m.settingsTable.View()
	case screenClusterEditor:
		return m.clusterEditorView()
	case screenSearch:
		return m.searchView()
	case screenClusterInfo:
		return m.infoView.View()
	default:
		return styles.panel.Width(max(20, m.width-4)).Render("Screen migration in progress.")
	}
}

func (m Model) screenHint() string {
	switch m.screen {
	case screenIndices:
		return "enter: open • /: filter • h: hidden • i: details • c: cluster • .: settings • ?: help • q: quit"
	case screenIndexDetails:
		return "tab: settings/mappings • r: refresh • esc: back • ?: help • q: quit"
	case screenDocuments:
		return "enter: view • a: add • e: edit • d: delete • /: filter • f: query • n/p: page • ?: help • esc: back"
	case screenDocument:
		return "e: edit • u/U: update/upsert • d: delete • w: wrap • ?: help • esc: back"
	case screenSettings:
		return "enter: connect • a: add • e: edit • d: delete • c: quick connect • r: health • ?: help • esc: back"
	case screenClusterEditor:
		return "↑/↓: field • enter: edit/toggle • ←/→: change • ctrl+s: save • esc: back"
	case screenSearch:
		return "f: query • s: sort • o: source • j: body • i: ids • c: count • x: reset • enter: run • esc: back"
	case screenClusterInfo:
		return "r: refresh • ?: help • esc: back • q: quit"
	default:
		return "esc: back • ?: help • q: quit"
	}
}

func (m *Model) pushScreen(next screenKind) {
	if m.screen != next {
		m.history = append(m.history, m.screen)
		m.screen = next
	}
}

func (m *Model) popScreen() {
	if len(m.history) == 0 {
		m.screen = screenIndices
		return
	}
	last := len(m.history) - 1
	m.screen = m.history[last]
	m.history = m.history[:last]
}

func (m *Model) handleResponse(msg requestMsg) tea.Cmd {
	switch msg.operation {
	case operationIndices:
		m.receiveIndices(msg.body)
	case operationIndexDetails:
		m.receiveIndexDetails(msg.body)
	case operationDocuments:
		m.receiveDocuments(msg.body)
	case operationGetDocument:
		m.receiveDocument(msg.body)
	case operationGetDocumentForEdit:
		return m.openDocumentEditor(msg.body)
	case operationEditDocument:
		m.status = notification{text: "Document saved"}
		if m.screen == screenDocument {
			return tea.Batch(
				fetchDocumentsCmd(*m),
				getDocumentCmd(m.client, m.currentIndex, m.currentDocID, operationGetDocument),
			)
		}
		return fetchDocumentsCmd(*m)
	case operationDeleteDocument:
		m.status = notification{text: "Document deleted"}
		if m.screen == screenDocument {
			m.popScreen()
		}
		return fetchDocumentsCmd(*m)
	case operationCreateDocument:
		m.status = notification{text: "Document created"}
		return fetchDocumentsCmd(*m)
	case operationUpdateDocument:
		m.status = notification{text: "Document updated"}
		if m.screen == screenDocument {
			return tea.Batch(
				fetchDocumentsCmd(*m),
				getDocumentCmd(m.client, m.currentIndex, m.currentDocID, operationGetDocument),
			)
		}
		return fetchDocumentsCmd(*m)
	case operationCountForDelete:
		return m.receiveDeleteCount(msg.body)
	case operationDeleteByQuery:
		m.from = 0
		m.status = notification{text: deleteByQueryResult(msg.body)}
		return fetchDocumentsCmd(*m)
	case operationExactCount:
		m.receiveExactCount(msg.body)
	}
	return nil
}
