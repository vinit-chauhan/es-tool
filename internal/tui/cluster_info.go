package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

// clusterInfoMsg carries the three calls behind the cluster info screen. The
// authentication lookup is reported separately because clusters without the
// security feature answer it with an error that is not worth surfacing as a
// failure.
type clusterInfoMsg struct {
	epoch   int
	root    any
	health  any
	user    any
	userErr error
	err     error
}

func fetchClusterInfoCmd(m Model) tea.Cmd {
	client, epoch := m.client, m.connEpoch
	return func() tea.Msg {
		msg := clusterInfoMsg{epoch: epoch}
		root, err := get(client, "/")
		if err != nil {
			msg.err = err
			return msg
		}
		msg.root = root
		health, err := get(client, "/_cluster/health")
		if err != nil {
			msg.err = err
			return msg
		}
		msg.health = health
		msg.user, msg.userErr = get(client, "/_security/_authenticate")
		return msg
	}
}

func get(client *esclient.Client, path string) (any, error) {
	status, body, err := client.Request("GET", path, nil, nil)
	if err := requestError(status, body, err); err != nil {
		return nil, err
	}
	return body, nil
}

func (m *Model) updateClusterInfo(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.popScreen()
	case "r":
		m.loading = true
		return fetchClusterInfoCmd(*m)
	default:
		var cmd tea.Cmd
		m.infoView, cmd = m.infoView.Update(msg)
		return cmd
	}
	return nil
}

func (m *Model) receiveClusterInfo(msg clusterInfoMsg) {
	if msg.err != nil {
		m.status = notification{text: msg.err.Error(), isErr: true}
		return
	}
	m.health = healthFromResponse(200, msg.health, nil)
	m.infoText = m.renderClusterInfo(msg)
	m.infoView.SetContent(m.infoText)
	m.infoView.GotoTop()
	m.status = notification{text: "Cluster info loaded"}
}

func (m Model) renderClusterInfo(msg clusterInfoMsg) string {
	root, _ := msg.root.(map[string]any)
	health, _ := msg.health.(map[string]any)
	version, _ := root["version"].(map[string]any)

	sections := []string{
		section("Cluster", [][2]string{
			{"Name", util.AsStr(root["cluster_name"])},
			{"UUID", util.AsStr(root["cluster_uuid"])},
			{"Version", util.AsStr(version["number"])},
			{"Distribution", util.AsStr(version["build_flavor"])},
			{"Lucene", util.AsStr(version["lucene_version"])},
			{"Tagline", util.AsStr(root["tagline"])},
		}),
		section("Health", [][2]string{
			{"Status", strings.ToUpper(util.AsStr(health["status"]))},
			{"Nodes", util.AsStr(health["number_of_nodes"])},
			{"Data nodes", util.AsStr(health["number_of_data_nodes"])},
			{"Active shards", util.AsStr(health["active_shards"])},
			{"Relocating", util.AsStr(health["relocating_shards"])},
			{"Initializing", util.AsStr(health["initializing_shards"])},
			{"Unassigned", util.AsStr(health["unassigned_shards"])},
		}),
		section("Connection", [][2]string{
			{"URL", m.client.BaseURL},
			{"Profile", valueOrEmpty(m.activeCluster)},
			{"Authentication", m.client.AuthMode()},
			{"TLS", map[bool]string{true: "verify certificates", false: "skip verification"}[m.client.VerifyTLS]},
		}),
		m.renderUserSection(msg),
	}
	return strings.Join(sections, "\n\n")
}

func (m Model) renderUserSection(msg clusterInfoMsg) string {
	if msg.userErr != nil {
		return styles.title.Render("Authenticated user") + "\n" +
			styles.dim.Render("unavailable: "+msg.userErr.Error())
	}
	user, _ := msg.user.(map[string]any)
	roles := make([]string, 0, 4)
	if items, ok := user["roles"].([]any); ok {
		for _, item := range items {
			roles = append(roles, util.AsStr(item))
		}
	}
	return section("Authenticated user", [][2]string{
		{"Username", util.AsStr(user["username"])},
		{"Full name", util.AsStr(user["full_name"])},
		{"Email", util.AsStr(user["email"])},
		{"Roles", strings.Join(roles, ", ")},
		{"Enabled", util.AsStr(user["enabled"])},
	})
}

func section(title string, rows [][2]string) string {
	lines := make([]string, 0, len(rows)+1)
	lines = append(lines, styles.title.Render(title))
	for _, row := range rows {
		if strings.TrimSpace(row[1]) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-16s %s", row[0], row[1]))
	}
	return strings.Join(lines, "\n")
}
