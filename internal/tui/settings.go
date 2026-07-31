package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

// ---------------------------------------------------------------------------
// Settings screen (saved cluster profiles)
// ---------------------------------------------------------------------------

func newSettingsTable() table.Model {
	model := table.New(
		table.WithColumns(settingsColumns(100)),
		table.WithFocused(true),
		table.WithHeight(12),
	)
	tableStyles := table.DefaultStyles()
	tableStyles.Header = tableStyles.Header.Bold(true)
	tableStyles.Selected = styles.selected
	model.SetStyles(tableStyles)
	return model
}

func settingsColumns(width int) []table.Column {
	// 2 cells of table padding per column (6 columns = 12 cells); the URL
	// column absorbs whatever is left after the fixed columns.
	name := min(24, max(12, width/5))
	url := max(20, width-name-40)
	return []table.Column{
		{Title: "", Width: 3},
		{Title: "Name", Width: name},
		{Title: "URL", Width: url},
		{Title: "Health", Width: 9},
		{Title: "Auth", Width: 8},
		{Title: "TLS", Width: 6},
	}
}

func (m *Model) setSettingsColumns() {
	m.settingsTable.SetColumns(settingsColumns(max(50, m.width)))
}

func (m *Model) refreshSettingsRows() {
	rows := make([]table.Row, 0, len(m.config.Clusters))
	for _, cluster := range m.config.Clusters {
		active := ""
		if cluster.Name == m.config.Active {
			active = "●"
		}
		rows = append(rows, table.Row{
			active,
			cluster.Name,
			cluster.URL,
			m.profileHealthLabel(cluster.Name),
			clusterAuthMode(cluster),
			map[bool]string{true: "verify", false: "skip"}[cluster.VerifyTLS],
		})
	}
	m.settingsTable.SetRows(rows)
	if m.settingsTable.Cursor() >= len(rows) {
		m.settingsTable.SetCursor(max(0, len(rows)-1))
	}
}

// profileHealthMsg reports the health of one saved profile, probed with its
// own connection so the main client is never disturbed.
type profileHealthMsg struct {
	name   string
	status int
	body   any
	err    error
}

func profileHealthCmd(cluster appconfig.Cluster) tea.Cmd {
	return func() tea.Msg {
		probe := esclient.New(esclient.Options{
			BaseURL:   cluster.URL,
			APIKey:    cluster.APIKey,
			User:      cluster.User,
			Password:  cluster.Password,
			VerifyTLS: cluster.VerifyTLS,
		})
		status, body, err := probe.Request("GET", "/_cluster/health", nil, nil)
		return profileHealthMsg{name: cluster.Name, status: status, body: body, err: err}
	}
}

// checkProfileHealth marks every saved profile as checking and probes each of
// them concurrently.
func (m *Model) checkProfileHealth() tea.Cmd {
	if len(m.config.Clusters) == 0 {
		return nil
	}
	commands := make([]tea.Cmd, 0, len(m.config.Clusters))
	for _, cluster := range m.config.Clusters {
		m.profileHealth[cluster.Name] = healthStatus{state: stateHealthChecking}
		commands = append(commands, profileHealthCmd(cluster))
	}
	m.refreshSettingsRows()
	return tea.Batch(commands...)
}

func (m *Model) receiveProfileHealth(msg profileHealthMsg) {
	m.profileHealth[msg.name] = healthFromResponse(msg.status, msg.body, msg.err)
	m.refreshSettingsRows()
}

// profileHealthLabel renders a profile's probed health as one of the four
// user-facing values: green, yellow, red, or unknown.
func (m Model) profileHealthLabel(name string) string {
	health, ok := m.profileHealth[name]
	if !ok {
		return "unknown"
	}
	switch health.state {
	case stateHealthChecking:
		return "…"
	case stateHealthGreen:
		return "green"
	case stateHealthYellow:
		return "yellow"
	case stateHealthRed:
		return "red"
	default:
		return "unknown"
	}
}

func clusterAuthMode(cluster appconfig.Cluster) string {
	switch {
	case cluster.APIKey != "":
		return "apikey"
	case cluster.User != "":
		return "basic"
	default:
		return "none"
	}
}

func (m *Model) updateSettings(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.popScreen()
	case "r":
		m.loading = true
		m.health = healthStatus{state: stateHealthChecking}
		return tea.Batch(healthCmd(m.client), m.checkProfileHealth())
	case "a":
		m.openClusterEditor("", appconfig.Cluster{URL: m.client.BaseURL, VerifyTLS: true})
	case "e":
		if cluster, ok := m.selectedCluster(); ok {
			m.openClusterEditor(cluster.Name, cluster)
		}
	case "enter":
		if cluster, ok := m.selectedCluster(); ok {
			return m.activateCluster(cluster)
		}
	case "d":
		if cluster, ok := m.selectedCluster(); ok {
			m.pendingProfile = cluster.Name
			return m.openPrompt(promptDeleteProfile, "Type "+cluster.Name+" to delete:", "")
		}
	case "c":
		return m.openPrompt(promptQuickConnectURL, "Session URL:", m.client.BaseURL)
	default:
		var cmd tea.Cmd
		m.settingsTable, cmd = m.settingsTable.Update(msg)
		return cmd
	}
	return nil
}

func (m Model) selectedCluster() (appconfig.Cluster, bool) {
	row := m.settingsTable.SelectedRow()
	if len(row) < 2 {
		return appconfig.Cluster{}, false
	}
	return m.config.Find(row[1])
}

// gotoIndices switches the session to the freshly configured connection and
// lands the user on the indices screen.
func (m *Model) gotoIndices() tea.Cmd {
	m.history = nil
	m.screen = screenIndices
	m.loading = true
	m.health = healthStatus{state: stateHealthChecking}
	return tea.Batch(healthCmd(m.client), fetchIndicesCmd(m.client, m.showHidden))
}

func (m *Model) activateCluster(cluster appconfig.Cluster) tea.Cmd {
	next := m.config.Clone()
	next.Active = cluster.Name
	if err := m.store.Save(next); err != nil {
		m.status = notification{text: "activate profile: " + err.Error(), isErr: true}
		return nil
	}
	m.config = next
	configureClient(m.client, cluster)
	m.activeCluster = cluster.Name
	m.status = notification{text: "Connected to " + cluster.Name}
	return m.gotoIndices()
}

func (m *Model) deleteProfile(name string) {
	next := m.config.Clone()
	if err := next.Delete(name); err != nil {
		m.status = notification{text: err.Error(), isErr: true}
		return
	}
	if err := m.store.Save(next); err != nil {
		m.status = notification{text: "delete profile: " + err.Error(), isErr: true}
		return
	}
	m.config = next
	if m.activeCluster == name {
		m.activeCluster = ""
	}
	delete(m.profileHealth, name)
	m.refreshSettingsRows()
	m.status = notification{text: "Deleted profile " + name}
}

func (m *Model) quickConnect(rawURL string) tea.Cmd {
	cluster := appconfig.Cluster{
		Name:      "session",
		URL:       rawURL,
		VerifyTLS: true,
	}
	cluster.Normalize()
	if err := cluster.Validate(); err != nil {
		m.status = notification{text: err.Error(), isErr: true}
		return nil
	}
	m.client.Configure(esclient.Options{BaseURL: cluster.URL, VerifyTLS: true})
	m.activeCluster = ""
	m.status = notification{text: "Connected for this session only"}
	return m.gotoIndices()
}

// ---------------------------------------------------------------------------
// Cluster profile editor
// ---------------------------------------------------------------------------

type editorFieldID int

const (
	fieldName editorFieldID = iota
	fieldURL
	fieldAuth
	fieldAPIKey
	fieldUser
	fieldPassword
	fieldTLS
	fieldSave
)

// editorFields lists the navigable rows for the current auth mode.
func (m Model) editorFields() []editorFieldID {
	fields := []editorFieldID{fieldName, fieldURL, fieldAuth}
	switch m.editingAuth {
	case "apikey":
		fields = append(fields, fieldAPIKey)
	case "basic":
		fields = append(fields, fieldUser, fieldPassword)
	}
	return append(fields, fieldTLS, fieldSave)
}

func (m *Model) openClusterEditor(originalName string, cluster appconfig.Cluster) {
	m.editingOriginal = originalName
	m.editingCluster = cluster
	m.editingAuth = clusterAuthMode(cluster)
	m.editingBaseline = cluster
	m.baselineAuth = m.editingAuth
	m.editorCursor = 0
	m.pushScreen(screenClusterEditor)
}

// editorDirty reports whether the profile being edited differs from the state
// it had when the editor was opened.
func (m Model) editorDirty() bool {
	return m.editingCluster != m.editingBaseline || m.editingAuth != m.baselineAuth
}

// confirmLeaveEditor pops the editor, or asks for confirmation first when
// there are unsaved changes. action is "back" or "quit".
func (m *Model) confirmLeaveEditor(action string) tea.Cmd {
	if !m.editorDirty() {
		if action == "quit" {
			return tea.Quit
		}
		m.popScreen()
		return nil
	}
	m.pendingDiscard = action
	return m.openPrompt(promptDiscardEdits, "Unsaved changes — type y to discard:", "")
}

func (m *Model) updateClusterEditor(msg tea.KeyMsg) tea.Cmd {
	fields := m.editorFields()
	m.editorCursor = min(m.editorCursor, len(fields)-1)

	switch msg.String() {
	case "esc":
		return m.confirmLeaveEditor("back")
	case "up", "k":
		m.editorCursor = max(0, m.editorCursor-1)
	case "down", "j":
		m.editorCursor = min(len(fields)-1, m.editorCursor+1)
	case "left", "right", " ":
		m.toggleEditorField(fields[m.editorCursor], msg.String() == "left")
	case "enter":
		return m.activateEditorField(fields[m.editorCursor])
	case "ctrl+s":
		return m.saveEditingCluster()
	}
	return nil
}

func (m *Model) toggleEditorField(field editorFieldID, backwards bool) {
	switch field {
	case fieldAuth:
		modes := []string{"none", "apikey", "basic"}
		current := 0
		for i, mode := range modes {
			if mode == m.editingAuth {
				current = i
			}
		}
		step := 1
		if backwards {
			step = len(modes) - 1
		}
		m.editingAuth = modes[(current+step)%len(modes)]
	case fieldTLS:
		m.editingCluster.VerifyTLS = !m.editingCluster.VerifyTLS
	}
}

func (m *Model) activateEditorField(field editorFieldID) tea.Cmd {
	switch field {
	case fieldName:
		return m.openPrompt(promptClusterName, "Profile name:", m.editingCluster.Name)
	case fieldURL:
		return m.openPrompt(promptClusterURL, "Elasticsearch URL:", m.editingCluster.URL)
	case fieldAuth:
		m.toggleEditorField(fieldAuth, false)
	case fieldAPIKey:
		return m.openSecretPrompt(promptClusterAPIKey, "API key:", m.editingCluster.APIKey)
	case fieldUser:
		return m.openPrompt(promptClusterUser, "Username:", m.editingCluster.User)
	case fieldPassword:
		return m.openSecretPrompt(promptClusterPassword, "Password:", m.editingCluster.Password)
	case fieldTLS:
		m.toggleEditorField(fieldTLS, false)
	case fieldSave:
		return m.saveEditingCluster()
	}
	return nil
}

func (m Model) clusterEditorView() string {
	secret := func(value string) string {
		if value == "" {
			return styles.dim.Render("(not set)")
		}
		return styles.dim.Render(strings.Repeat("•", min(12, len([]rune(value)))))
	}
	tls := "verify certificates"
	if !m.editingCluster.VerifyTLS {
		tls = "skip verification"
	}

	label := map[editorFieldID]string{
		fieldName:     "Name",
		fieldURL:      "URL",
		fieldAuth:     "Authentication",
		fieldAPIKey:   "API key",
		fieldUser:     "Username",
		fieldPassword: "Password",
		fieldTLS:      "TLS",
		fieldSave:     "",
	}
	value := map[editorFieldID]string{
		fieldName:     valueOrEmpty(m.editingCluster.Name),
		fieldURL:      valueOrEmpty(m.editingCluster.URL),
		fieldAuth:     m.editingAuth + styles.dim.Render("  (←/→ to change)"),
		fieldAPIKey:   secret(m.editingCluster.APIKey),
		fieldUser:     valueOrEmpty(m.editingCluster.User),
		fieldPassword: secret(m.editingCluster.Password),
		fieldTLS:      tls + styles.dim.Render("  (←/→ to change)"),
		fieldSave:     styles.key.Render("Save and connect"),
	}

	fields := m.editorFields()
	cursor := min(m.editorCursor, len(fields)-1)
	rows := make([]string, 0, len(fields)+2)
	for i, field := range fields {
		marker := "  "
		if i == cursor {
			marker = styles.key.Render("▸ ")
		}
		row := marker
		if field == fieldSave {
			row += value[field]
		} else {
			row += fmt.Sprintf("%-16s %s", label[field], value[field])
		}
		if i == cursor && field != fieldSave {
			row = marker + styles.title.Render(fmt.Sprintf("%-16s", label[field])) + " " + value[field]
		}
		rows = append(rows, row)
	}

	title := "Cluster profile"
	if m.editorDirty() {
		title += styles.warning.Render("  (unsaved changes)")
	}
	return styles.panel.Width(max(40, m.width-4)).Render(
		styles.title.Render(title) + "\n\n" + strings.Join(rows, "\n"),
	)
}

func valueOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return styles.dim.Render("(not set)")
	}
	return value
}

func (m *Model) saveEditingCluster() tea.Cmd {
	cluster := m.editingCluster
	switch m.editingAuth {
	case "none":
		cluster.APIKey, cluster.User, cluster.Password = "", "", ""
	case "apikey":
		cluster.User, cluster.Password = "", ""
		if cluster.APIKey == "" {
			m.status = notification{text: "API key is required", isErr: true}
			return nil
		}
	case "basic":
		cluster.APIKey = ""
		if cluster.User == "" {
			m.status = notification{text: "username is required", isErr: true}
			return nil
		}
	}
	next := m.config.Clone()
	if err := next.Upsert(m.editingOriginal, cluster); err != nil {
		m.status = notification{text: err.Error(), isErr: true}
		return nil
	}
	next.Active = cluster.Name
	if err := m.store.Save(next); err != nil {
		m.status = notification{text: "save profile: " + err.Error(), isErr: true}
		return nil
	}
	m.config = next
	configureClient(m.client, cluster)
	m.activeCluster = cluster.Name
	m.editingBaseline = m.editingCluster
	m.baselineAuth = m.editingAuth
	m.status = notification{text: "Saved and connected to " + cluster.Name}
	return m.gotoIndices()
}
