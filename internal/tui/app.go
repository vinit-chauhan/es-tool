package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

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

type notification struct {
	text  string
	isErr bool
}

// Model is the root Bubble Tea model. Screen-specific state is attached to
// this model as each screen is migrated, while navigation remains centralized.
type Model struct {
	client        *esclient.Client
	store         *appconfig.Store
	config        appconfig.Config
	activeCluster string
	startIndex    string

	screen  screenKind
	history []screenKind
	width   int
	height  int

	spinner  spinner.Model
	loading  bool
	health   healthStatus
	status   notification
	showHelp bool
}

// newModel loads saved profiles and applies the active profile unless the
// connection was explicitly configured through ES_* environment variables.
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
	initial := screenIndices
	if startIndex != "" {
		initial = screenDocuments
	}
	return Model{
		client:        client,
		store:         store,
		config:        cfg,
		activeCluster: active,
		startIndex:    startIndex,
		screen:        initial,
		spinner:       spin,
		health:        healthStatus{state: stateHealthChecking},
		status:        status,
	}, nil
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
	return tea.Batch(m.spinner.Tick, healthCmd(m.client))
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if msg.String() == "?" {
			m.showHelp = !m.showHelp
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case healthMsg:
		m.loading = false
		m.health = healthFromResponse(msg.status, msg.body, msg.err)
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}
	title := renderHeader("es-tool", m.client.BaseURL, m.health, m.width)
	body := styles.panel.Width(max(0, m.width-4)).Render(
		styles.title.Render("Bubble Tea migration") + "\n\n" +
			"The interactive screens are loading.",
	)
	if m.showHelp {
		body = renderHelpOverlay(m.width, m.height)
	}
	return title + "\n" + body + "\n" + renderFooter(m.status, "?: help • ctrl+c: quit", m.width)
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
