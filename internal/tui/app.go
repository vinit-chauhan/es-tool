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

	prompt      textinput.Model
	promptKind  promptKind
	promptLabel string

	allIndices  []map[string]any
	indexTable  table.Model
	indexFilter string
	showHidden  bool

	currentIndex string
	detailTab    int
	detailView   viewport.Model
	detailText   [2]string
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
		m.health = healthFromResponse(msg.status, msg.body, msg.err)
	case requestMsg:
		m.loading = false
		if msg.err != nil {
			m.status = notification{text: msg.err.Error(), isErr: true}
		} else {
			m.health = healthStatus{state: stateHealthConnected}
			m.handleResponse(msg)
		}
	case editorDoneMsg:
		m.loading = false
		m.handleEditorDone(msg)
	case tea.KeyMsg:
		if m.promptKind != promptNone {
			return m.updatePrompt(msg)
		}
		if msg.String() == "ctrl+c" || (msg.String() == "q" && !m.showHelp) {
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.showHelp = !m.showHelp
			return m, nil
		}
		if m.showHelp {
			if msg.String() == "esc" {
				m.showHelp = false
			}
			return m, nil
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
}

func (m *Model) updateScreen(msg tea.KeyMsg) tea.Cmd {
	switch m.screen {
	case screenIndices:
		return m.updateIndices(msg)
	case screenIndexDetails:
		return m.updateIndexDetails(msg)
	case screenDocuments:
		if msg.String() == "esc" || msg.String() == "b" {
			m.popScreen()
		}
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
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.prompt, cmd = m.prompt.Update(msg)
	return m, cmd
}

func (m *Model) openPrompt(kind promptKind, label, value string) tea.Cmd {
	m.promptKind = kind
	m.promptLabel = label
	m.prompt.SetValue(value)
	m.prompt.CursorEnd()
	m.prompt.Focus()
	return textinput.Blink
}

func (m *Model) closePrompt() {
	m.prompt.Blur()
	m.promptKind = promptNone
	m.promptLabel = ""
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
	if m.showHelp {
		body = renderHelpOverlay(m.width, m.height)
	}
	hint := m.screenHint()
	if m.promptKind != promptNone {
		hint = "enter: apply • esc: cancel"
		body += "\n" + styles.key.Render(m.promptLabel) + " " + m.prompt.View()
	}
	if m.loading {
		body = m.spinner.View() + " " + styles.dim.Render("Loading…") + "\n" + body
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		body,
		renderFooter(m.status, hint, m.width),
	)
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
		return styles.panel.Width(max(20, m.width-4)).Render(
			styles.title.Render(m.currentIndex) + "\n\nDocument browser migration in progress.",
		)
	default:
		return styles.panel.Width(max(20, m.width-4)).Render("Screen migration in progress.")
	}
}

func (m Model) screenHint() string {
	switch m.screen {
	case screenIndices:
		return "enter: open • /: filter • h: hidden • S: details • r: refresh • ?: help • q: quit"
	case screenIndexDetails:
		return "tab/←/→: settings/mappings • r: refresh • b/esc: back • ?: help • q: quit"
	case screenDocuments:
		return "b/esc: back • ?: help • q: quit"
	default:
		return "b/esc: back • ?: help • q: quit"
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

func (m *Model) handleResponse(msg requestMsg) {
	switch msg.operation {
	case operationIndices:
		m.receiveIndices(msg.body)
	case operationIndexDetails:
		m.receiveIndexDetails(msg.body)
	}
}

type editorDoneMsg struct {
	err error
}

func (m *Model) handleEditorDone(msg editorDoneMsg) {
	if msg.err != nil {
		m.status = notification{text: msg.err.Error(), isErr: true}
	}
}
