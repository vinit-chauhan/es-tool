package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
	"github.com/vinit-chauhan/es-tool/internal/esclient"
)

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
	return []table.Column{
		{Title: "", Width: 3},
		{Title: "Name", Width: min(24, max(12, width/4))},
		{Title: "URL", Width: max(20, width/2)},
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
			clusterAuthMode(cluster),
			map[bool]string{true: "verify", false: "skip"}[cluster.VerifyTLS],
		})
	}
	m.settingsTable.SetRows(rows)
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
	case "esc", "b":
		m.popScreen()
	case "r":
		m.loading = true
		m.health = healthStatus{state: stateHealthChecking}
		return healthCmd(m.client)
	case "a":
		m.editingOriginal = ""
		m.editingCluster = appconfig.Cluster{URL: m.client.BaseURL, VerifyTLS: true}
		m.editingAuth = "none"
		m.pushScreen(screenClusterEditor)
	case "e":
		if cluster, ok := m.selectedCluster(); ok {
			m.editingOriginal = cluster.Name
			m.editingCluster = cluster
			m.editingAuth = clusterAuthMode(cluster)
			m.pushScreen(screenClusterEditor)
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

func (m *Model) updateClusterEditor(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "b":
		m.popScreen()
	case "n":
		return m.openPrompt(promptClusterName, "Profile name:", m.editingCluster.Name)
	case "u":
		return m.openPrompt(promptClusterURL, "Elasticsearch URL:", m.editingCluster.URL)
	case "a":
		switch m.editingAuth {
		case "none":
			m.editingAuth = "apikey"
		case "apikey":
			m.editingAuth = "basic"
		default:
			m.editingAuth = "none"
		}
	case "k":
		m.editingAuth = "apikey"
		return m.openSecretPrompt(promptClusterAPIKey, "API key:", m.editingCluster.APIKey)
	case "x":
		m.editingAuth = "basic"
		return m.openPrompt(promptClusterUser, "Username:", m.editingCluster.User)
	case "p":
		m.editingAuth = "basic"
		return m.openSecretPrompt(promptClusterPassword, "Password:", m.editingCluster.Password)
	case "t":
		m.editingCluster.VerifyTLS = !m.editingCluster.VerifyTLS
	case "s":
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
	rows := []string{
		fieldRow("n", "Name", valueOrEmpty(m.editingCluster.Name)),
		fieldRow("u", "URL", valueOrEmpty(m.editingCluster.URL)),
		fieldRow("a", "Authentication", m.editingAuth),
		fieldRow("k", "API key", secret(m.editingCluster.APIKey)),
		fieldRow("x", "Username", valueOrEmpty(m.editingCluster.User)),
		fieldRow("p", "Password", secret(m.editingCluster.Password)),
		fieldRow("t", "TLS", tls),
	}
	return styles.panel.Width(max(32, m.width-4)).Render(
		styles.title.Render("Cluster profile") + "\n\n" + strings.Join(rows, "\n") +
			"\n\n" + styles.key.Render("s") + " Save and activate",
	)
}

func fieldRow(key, label, value string) string {
	return fmt.Sprintf("%s  %-16s %s", styles.key.Render(key), label, value)
}

func valueOrEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return styles.dim.Render("(not set)")
	}
	return value
}

func (m Model) selectedCluster() (appconfig.Cluster, bool) {
	row := m.settingsTable.SelectedRow()
	if len(row) < 2 {
		return appconfig.Cluster{}, false
	}
	return m.config.Find(row[1])
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
	m.refreshSettingsRows()
	m.popScreen()
	m.loading = true
	m.health = healthStatus{state: stateHealthChecking}
	m.status = notification{text: "Saved and activated " + cluster.Name}
	return healthCmd(m.client)
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
	m.refreshSettingsRows()
	m.loading = true
	m.health = healthStatus{state: stateHealthChecking}
	m.status = notification{text: "Activated " + cluster.Name}
	return healthCmd(m.client)
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
	m.loading = true
	m.health = healthStatus{state: stateHealthChecking}
	m.status = notification{text: "Connected for this session only"}
	return healthCmd(m.client)
}
