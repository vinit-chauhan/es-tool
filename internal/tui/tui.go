// Package tui implements the full-screen interactive Elasticsearch browser
// (indices → docs → viewer) built on tcell.
package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	appconfig "github.com/vinit-chauhan/es-tool/internal/config"
	"github.com/vinit-chauhan/es-tool/internal/esclient"
	"github.com/vinit-chauhan/es-tool/internal/util"
)

// ---------------------------------------------------------------------------
// App / status
// ---------------------------------------------------------------------------

type app struct {
	screen        tcell.Screen
	client        *esclient.Client
	store         *appconfig.Store
	config        appconfig.Config
	activeCluster string
	settingsWarn  string
	showHidden    bool
	health        clusterHealth
	status        statusMsg
}

type statusMsg struct {
	text  string
	isErr bool
}

func (s *statusMsg) set(text string)    { s.text, s.isErr = text, false }
func (s *statusMsg) setErr(text string) { s.text, s.isErr = text, true }
func (s *statusMsg) clear()             { s.text, s.isErr = "", false }

type clusterHealthState int

const (
	healthChecking clusterHealthState = iota
	healthGreen
	healthYellow
	healthRed
	healthConnected
	healthAuthError
	healthUnavailable
	healthOffline
)

type clusterHealth struct {
	state  clusterHealthState
	code   int
	detail string
}

func (h clusterHealth) label() string {
	switch h.state {
	case healthGreen:
		return "CLUSTER GREEN"
	case healthYellow:
		return "CLUSTER YELLOW"
	case healthRed:
		return "CLUSTER RED"
	case healthConnected:
		return "CLUSTER CONNECTED"
	case healthAuthError:
		return fmt.Sprintf("CLUSTER AUTH %d", h.code)
	case healthUnavailable:
		return fmt.Sprintf("HEALTH HTTP %d", h.code)
	case healthOffline:
		return "CLUSTER OFFLINE"
	default:
		return "CLUSTER CHECKING"
	}
}

func (h clusterHealth) style() tcell.Style {
	color := tcell.ColorGray
	switch h.state {
	case healthGreen:
		color = tcell.ColorGreen
	case healthYellow, healthUnavailable:
		color = tcell.ColorYellow
	case healthRed, healthAuthError, healthOffline:
		color = tcell.ColorRed
	case healthConnected:
		color = tcell.ColorBlue
	}
	return styleHeader.Foreground(color)
}

// quitSignal unwinds nested screens when the user presses 'q'.
type quitSignal struct{}

// Run is the public entrypoint used by the CLI.
func Run(client *esclient.Client, startIndex string) error {
	store, err := appconfig.DefaultStore()
	if err != nil {
		return fmt.Errorf("settings unavailable: %w", err)
	}
	cfg, loadErr := store.Load()
	settingsWarn := ""
	if loadErr != nil {
		cfg = appconfig.New()
		settingsWarn = "saved settings were ignored: " + loadErr.Error()
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("tui unavailable: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("tui init failed: %w", err)
	}
	defer screen.Fini()
	screen.HideCursor()

	a := &app{
		screen:       screen,
		client:       client,
		store:        store,
		config:       cfg,
		settingsWarn: settingsWarn,
	}
	if !esclient.EnvConfigured() && cfg.Active != "" {
		if cluster, ok := cfg.Find(cfg.Active); ok {
			a.configureClient(cluster)
			a.activeCluster = cluster.Name
		}
	}

	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(quitSignal); ok {
				return
			}
			panic(r)
		}
	}()

	if startIndex != "" {
		a.docsScreen(&docsContext{index: startIndex, size: 50})
		return nil
	}
	for {
		chosen, quit := a.indicesScreen()
		if quit {
			return nil
		}
		a.docsScreen(&docsContext{index: chosen, size: 50})
	}
}

// ---------------------------------------------------------------------------
// Styles & drawing primitives
// ---------------------------------------------------------------------------

var (
	styleDefault = tcell.StyleDefault
	styleHeader  = tcell.StyleDefault.Reverse(true).Bold(true)
	styleSelect  = tcell.StyleDefault.Reverse(true)
	styleDim     = tcell.StyleDefault.Dim(true)
	styleUnder   = tcell.StyleDefault.Underline(true)
	styleErr     = tcell.StyleDefault.Foreground(tcell.ColorRed).Bold(true)
	styleBold    = tcell.StyleDefault.Bold(true)
)

func (a *app) size() (int, int) { return a.screen.Size() }

// drawText writes s at (x,y) clipped to the screen width.
func (a *app) drawText(x, y int, style tcell.Style, s string) {
	maxX, maxY := a.screen.Size()
	if y < 0 || y >= maxY {
		return
	}
	for _, r := range s {
		if x >= maxX {
			break
		}
		if x >= 0 {
			a.screen.SetContent(x, y, r, nil, style)
		}
		x++
	}
}

// drawBar fills row y with s padded to full width using style.
func (a *app) drawBar(y int, style tcell.Style, s string) {
	maxX, _ := a.screen.Size()
	a.drawText(0, y, style, util.PadRight(util.Clip(s, maxX), maxX))
}

func (a *app) clearLine(y int) {
	maxX, _ := a.screen.Size()
	a.drawText(0, y, styleDefault, strings.Repeat(" ", maxX))
}

func (a *app) drawHeader(title, subtitle string) {
	bar := " " + title
	if subtitle != "" {
		bar += " — " + subtitle
	}
	a.drawBar(0, styleHeader, bar)
	maxX, _ := a.size()
	health := " " + a.health.label() + " "
	healthWidth := len([]rune(health))
	a.drawText(max(0, maxX-healthWidth), 0, a.health.style(), health)
}

func (a *app) drawStatus(hint string) {
	_, maxY := a.size()
	statusLine := maxY - 2
	hintLine := maxY - 1
	a.clearLine(statusLine)
	a.clearLine(hintLine)
	a.drawText(0, hintLine, styleDim, hint)
	if a.status.text != "" {
		st := styleBold
		if a.status.isErr {
			st = styleErr
		}
		a.drawText(0, statusLine, st, a.status.text)
	}
}

// ---------------------------------------------------------------------------
// Prompt / confirm
// ---------------------------------------------------------------------------

// prompt shows an inline single-line editor above the hotkey row. It returns
// the entered text and ok=false if the user pressed Esc.
func (a *app) prompt(label, initial string) (string, bool) {
	return a.promptValue(label, initial, false)
}

// promptSecret is the masked equivalent of prompt.
func (a *app) promptSecret(label, initial string) (string, bool) {
	return a.promptValue(label, initial, true)
}

func (a *app) promptValue(label, initial string, masked bool) (string, bool) {
	buf := []rune(initial)
	cursor := len(buf)

	for {
		maxX, maxY := a.size()
		line := maxY - 2
		hintLine := maxY - 1
		a.clearLine(line)
		a.clearLine(hintLine)
		a.drawText(0, hintLine, styleDim, "←/→ cursor  Home/End  Backspace/Del edit  Ctrl+U clear  Enter apply  Esc cancel")
		a.drawText(0, line, styleBold, label)

		labelWidth := len([]rune(label))
		inputX := min(labelWidth, max(0, maxX-1))
		available := max(1, maxX-inputX)
		start := 0
		if cursor >= available {
			start = cursor - available + 1
		}
		end := min(len(buf), start+available)
		visible := buf[start:end]
		value := string(visible)
		if masked {
			value = strings.Repeat("*", len(visible))
		}
		a.drawText(inputX, line, styleDefault, value)
		cursorX := min(maxX-1, inputX+cursor-start)
		a.screen.ShowCursor(cursorX, line)
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch ke.Key() {
		case tcell.KeyEscape:
			a.screen.HideCursor()
			return "", false
		case tcell.KeyEnter:
			a.screen.HideCursor()
			return string(buf), true
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if cursor > 0 {
				copy(buf[cursor-1:], buf[cursor:])
				buf = buf[:len(buf)-1]
				cursor--
			}
		case tcell.KeyDelete:
			if cursor < len(buf) {
				copy(buf[cursor:], buf[cursor+1:])
				buf = buf[:len(buf)-1]
			}
		case tcell.KeyLeft:
			cursor = max(0, cursor-1)
		case tcell.KeyRight:
			cursor = min(len(buf), cursor+1)
		case tcell.KeyHome, tcell.KeyCtrlA:
			cursor = 0
		case tcell.KeyEnd, tcell.KeyCtrlE:
			cursor = len(buf)
		case tcell.KeyCtrlU:
			buf = buf[:0]
			cursor = 0
		case tcell.KeyRune:
			buf = append(buf, 0)
			copy(buf[cursor+1:], buf[cursor:])
			buf[cursor] = ke.Rune()
			cursor++
		}
	}
}

func (a *app) confirm(question string) bool {
	ans, ok := a.prompt(question+" [y/N]: ", "")
	if !ok {
		return false
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes"
}

// ---------------------------------------------------------------------------
// List state (shared by indices/docs screens)
// ---------------------------------------------------------------------------

type listState struct {
	length int
	cursor int
	top    int
}

func (l *listState) move(delta, viewport int) {
	if l.length == 0 {
		l.cursor, l.top = 0, 0
		return
	}
	l.cursor = max(0, min(l.length-1, l.cursor+delta))
	if l.cursor < l.top {
		l.top = l.cursor
	} else if l.cursor >= l.top+viewport {
		l.top = l.cursor - viewport + 1
	}
}

func (l *listState) home() { l.cursor, l.top = 0, 0 }

func (l *listState) end(viewport int) {
	if l.length == 0 {
		return
	}
	l.cursor = l.length - 1
	l.top = max(0, l.cursor-viewport+1)
}

func (a *app) drawList(topY, height, width int, st *listState, render func(i int) string) {
	for i := 0; i < height; i++ {
		idx := st.top + i
		rowY := topY + i
		a.clearLine(rowY)
		if idx >= st.length {
			continue
		}
		style := styleDefault
		if idx == st.cursor {
			style = styleSelect
		}
		a.drawText(0, rowY, style, util.Clip(util.PadRight(render(idx), width), width))
	}
}

// ---------------------------------------------------------------------------
// Data fetching
// ---------------------------------------------------------------------------

type docsContext struct {
	index      string
	query      string
	filterText string
	size       int
	from       int
	total      int
}

func clusterHealthFromResponse(status int, body any, requestErr error) clusterHealth {
	if requestErr != nil {
		return clusterHealth{state: healthOffline, detail: requestErr.Error()}
	}
	if status == 401 {
		return clusterHealth{state: healthAuthError, code: status}
	}
	if status == 403 {
		return clusterHealth{state: healthUnavailable, code: status}
	}
	if response, ok := body.(map[string]any); ok {
		switch strings.ToLower(util.AsStr(response["status"])) {
		case "green":
			return clusterHealth{state: healthGreen}
		case "yellow":
			return clusterHealth{state: healthYellow}
		case "red":
			return clusterHealth{state: healthRed}
		}
	}
	if status >= 300 {
		return clusterHealth{state: healthUnavailable, code: status}
	}
	return clusterHealth{state: healthConnected}
}

func (a *app) refreshHealth() bool {
	status, body, err := a.client.Request("GET", "/_cluster/health", nil,
		map[string]string{"filter_path": "status"})
	a.health = clusterHealthFromResponse(status, body, err)
	return err == nil
}

func (a *app) updateHealthAfterRequest(requestErr error) {
	if requestErr != nil && strings.HasPrefix(requestErr.Error(), "connection error:") {
		a.health = clusterHealth{state: healthOffline, detail: requestErr.Error()}
		return
	}
	a.refreshHealth()
}

func (a *app) fetchIndices(showHidden bool) ([]map[string]any, error) {
	expandWildcards := "open,closed"
	if showHidden {
		expandWildcards = "all"
	}
	status, body, err := a.client.Request("GET", "/_cat/indices",
		nil, map[string]string{
			"expand_wildcards": expandWildcards,
			"format":           "json",
			"v":                "true",
		})
	if err != nil {
		return nil, err
	}
	arr, ok := body.([]any)
	if status >= 300 || !ok {
		return nil, fmt.Errorf("_cat/indices failed: HTTP %d", status)
	}
	var indices []map[string]any
	for _, r := range arr {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		indices = append(indices, m)
	}
	sort.Slice(indices, func(i, j int) bool {
		return util.AsStr(indices[i]["index"]) < util.AsStr(indices[j]["index"])
	})
	return indices, nil
}

func (a *app) fetchDocs(ctx *docsContext) ([]map[string]any, error) {
	params := map[string]string{
		"size": strconv.Itoa(ctx.size),
		"from": strconv.Itoa(ctx.from),
	}
	if ctx.query != "" {
		params["q"] = ctx.query
	}
	status, body, err := a.client.Request("GET", "/"+ctx.index+"/_search", nil, params)
	if err != nil {
		return nil, err
	}
	m, ok := body.(map[string]any)
	if status >= 300 || !ok {
		return nil, fmt.Errorf("search failed: HTTP %d", status)
	}
	hitsBlock, _ := m["hits"].(map[string]any)
	switch t := hitsBlock["total"].(type) {
	case map[string]any:
		ctx.total = util.AsInt(t["value"])
	default:
		ctx.total = util.AsInt(hitsBlock["total"])
	}
	var hits []map[string]any
	if arr, ok := hitsBlock["hits"].([]any); ok {
		for _, h := range arr {
			if hm, ok := h.(map[string]any); ok {
				hits = append(hits, hm)
			}
		}
	}
	return hits, nil
}

func renderDocRow(hit map[string]any) string {
	docID := util.AsStr(hit["_id"])
	src, _ := hit["_source"].(map[string]any)
	previewKeys := []string{"name", "title", "status", "state", "@timestamp", "id"}
	var parts []string
	for _, k := range previewKeys {
		if v, ok := src[k]; ok {
			var vs string
			switch v.(type) {
			case map[string]any, []any:
				b, _ := json.Marshal(v)
				vs = util.Clip(string(b), 40)
			default:
				vs = util.AsStr(v)
			}
			parts = append(parts, fmt.Sprintf("%s=%s", k, vs))
		}
		if len(parts) >= 3 {
			break
		}
	}
	var suffix string
	if len(parts) > 0 {
		suffix = strings.Join(parts, "  ")
	} else {
		b, _ := json.Marshal(src)
		suffix = util.Clip(string(b), 120)
	}
	return fmt.Sprintf("%-40s  %s", docID, suffix)
}

func filterIndices(items []map[string]any, needle string, showHidden bool) []map[string]any {
	if needle == "" && showHidden {
		return items
	}
	low := strings.ToLower(needle)
	var out []map[string]any
	for _, it := range items {
		name := util.AsStr(it["index"])
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		if strings.Contains(strings.ToLower(name), low) {
			out = append(out, it)
		}
	}
	return out
}

func filterHits(items []map[string]any, needle string) []map[string]any {
	if needle == "" {
		return items
	}
	low := strings.ToLower(needle)
	var out []map[string]any
	for _, it := range items {
		if strings.Contains(strings.ToLower(renderDocRow(it)), low) {
			out = append(out, it)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Hotkey help
// ---------------------------------------------------------------------------

type hotkeyHelpEntry struct {
	keys        string
	description string
}

type hotkeyHelpSection struct {
	title   string
	entries []hotkeyHelpEntry
}

var hotkeyHelpSections = []hotkeyHelpSection{
	{
		title: "Global",
		entries: []hotkeyHelpEntry{
			{keys: "?", description: "Open or close this hotkey reference"},
			{keys: "q", description: "Quit the TUI"},
			{keys: "b / Esc", description: "Go back or cancel (Esc quits from Indices)"},
			{keys: "S", description: "Open cluster settings"},
		},
	},
	{
		title: "Navigation",
		entries: []hotkeyHelpEntry{
			{keys: "↑ / ↓, j / k", description: "Move the selection or scroll one line"},
			{keys: "PgUp / PgDn", description: "Move or scroll one page"},
			{keys: "g / G, Home / End", description: "Jump to the first/last item or top/bottom"},
			{keys: "Enter / v", description: "Open the selected item"},
		},
	},
	{
		title: "Indices",
		entries: []hotkeyHelpEntry{
			{keys: "/", description: "Filter visible indices by name"},
			{keys: "h", description: "Toggle hidden indices"},
			{keys: "r", description: "Refresh indices and cluster health"},
			{keys: "Enter / v", description: "Open documents in the selected index"},
		},
	},
	{
		title: "Documents",
		entries: []hotkeyHelpEntry{
			{keys: "Enter / v", description: "View the selected document"},
			{keys: "e", description: "Edit the selected document in $EDITOR"},
			{keys: "d", description: "Delete the selected document after confirmation"},
			{keys: "/", description: "Filter loaded documents by ID or source text"},
			{keys: "f", description: "Set the server-side Lucene query"},
			{keys: "n / p", description: "Load the next/previous page"},
			{keys: "s", description: "Change the page size"},
			{keys: "r", description: "Refresh the current page"},
		},
	},
	{
		title: "Document viewer",
		entries: []hotkeyHelpEntry{
			{keys: "e", description: "Edit the open document in $EDITOR"},
			{keys: "d", description: "Delete the open document after confirmation"},
		},
	},
	{
		title: "Cluster settings",
		entries: []hotkeyHelpEntry{
			{keys: "Enter", description: "Use the selected cluster profile"},
			{keys: "a", description: "Add and activate a cluster profile"},
			{keys: "e", description: "Edit the selected cluster profile"},
			{keys: "r", description: "Refresh cluster health"},
		},
	},
	{
		title: "Cluster profile editor",
		entries: []hotkeyHelpEntry{
			{keys: "↑ / ↓, j / k", description: "Select a settings field"},
			{keys: "Enter", description: "Edit a field or toggle the selected option"},
			{keys: "← / →, Space", description: "Change authentication or TLS verification"},
			{keys: "s", description: "Validate and save the profile"},
			{keys: "b / Esc", description: "Cancel without saving"},
		},
	},
	{
		title: "Inline value editor",
		entries: []hotkeyHelpEntry{
			{keys: "← / →", description: "Move the cursor"},
			{keys: "Home / End", description: "Move to the start/end of the value"},
			{keys: "Ctrl+A / Ctrl+E", description: "Move to the start/end of the value"},
			{keys: "Backspace / Delete", description: "Delete before/at the cursor"},
			{keys: "Ctrl+U", description: "Clear the value"},
			{keys: "Enter", description: "Apply the edited value"},
			{keys: "Esc", description: "Cancel editing"},
		},
	},
}

type hotkeyHelpRow struct {
	section     string
	keys        string
	description string
}

func hotkeyHelpRows() []hotkeyHelpRow {
	var rows []hotkeyHelpRow
	for i, section := range hotkeyHelpSections {
		if i > 0 {
			rows = append(rows, hotkeyHelpRow{})
		}
		rows = append(rows, hotkeyHelpRow{section: section.title})
		for _, entry := range section.entries {
			rows = append(rows, hotkeyHelpRow{
				keys:        entry.keys,
				description: entry.description,
			})
		}
	}
	return rows
}

func (a *app) helpScreen() {
	rows := hotkeyHelpRows()
	top := 0

	for {
		a.screen.Clear()
		maxX, maxY := a.size()
		a.drawHeader("Keyboard shortcuts", fmt.Sprintf("%d shortcuts", hotkeyHelpCount()))

		bodyTop := 2
		bodyHeight := max(1, maxY-bodyTop-2)
		maxTop := max(0, len(rows)-bodyHeight)
		top = max(0, min(top, maxTop))
		for i := 0; i < bodyHeight; i++ {
			rowIndex := top + i
			if rowIndex >= len(rows) {
				break
			}
			row := rows[rowIndex]
			y := bodyTop + i
			switch {
			case row.section != "":
				a.drawText(0, y, styleBold, row.section)
			case row.keys != "":
				a.drawText(2, y, styleBold, util.Clip(util.PadRight(row.keys, 22), 22))
				a.drawText(26, y, styleDefault, util.Clip(row.description, max(0, maxX-26)))
			}
		}
		a.drawStatus("↑/↓ scroll  PgUp/PgDn page  g/G top/bottom  ?/b/Esc close  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && (ke.Rune() == '?' || ke.Rune() == 'b'), ke.Key() == tcell.KeyEscape:
			return
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			top--
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			top++
		case ke.Key() == tcell.KeyPgUp:
			top -= bodyHeight
		case ke.Key() == tcell.KeyPgDn:
			top += bodyHeight
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			top = 0
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			top = maxTop
		}
	}
}

func hotkeyHelpCount() int {
	count := 0
	for _, section := range hotkeyHelpSections {
		count += len(section.entries)
	}
	return count
}

// ---------------------------------------------------------------------------
// Cluster settings
// ---------------------------------------------------------------------------

type clusterField int

const (
	fieldName clusterField = iota
	fieldURL
	fieldAuth
	fieldAPIKey
	fieldUser
	fieldPassword
	fieldVerifyTLS
)

func (a *app) configureClient(cluster appconfig.Cluster) {
	a.client.Configure(esclient.Options{
		BaseURL:   cluster.URL,
		APIKey:    cluster.APIKey,
		User:      cluster.User,
		Password:  cluster.Password,
		VerifyTLS: cluster.VerifyTLS,
	})
	a.health = clusterHealth{state: healthChecking}
}

func clusterAuthMode(cluster appconfig.Cluster) string {
	switch {
	case cluster.APIKey != "":
		return "apikey"
	case cluster.User != "" || cluster.Password != "":
		return "basic"
	default:
		return "none"
	}
}

func clusterAuthLabel(cluster appconfig.Cluster) string {
	switch clusterAuthMode(cluster) {
	case "apikey":
		return "API key"
	case "basic":
		return "basic"
	default:
		return "none"
	}
}

func secretSummary(value string) string {
	if value == "" {
		return "<not set>"
	}
	return "<set>"
}

func visibleClusterFields(auth string) []clusterField {
	fields := []clusterField{fieldName, fieldURL, fieldAuth}
	switch auth {
	case "apikey":
		fields = append(fields, fieldAPIKey)
	case "basic":
		fields = append(fields, fieldUser, fieldPassword)
	}
	return append(fields, fieldVerifyTLS)
}

func cycleAuthMode(current string, delta int) string {
	modes := []string{"none", "apikey", "basic"}
	index := 0
	for i, mode := range modes {
		if mode == current {
			index = i
			break
		}
	}
	index = (index + delta + len(modes)) % len(modes)
	return modes[index]
}

func validateClusterAuth(auth string, cluster appconfig.Cluster) error {
	switch auth {
	case "apikey":
		if strings.TrimSpace(cluster.APIKey) == "" {
			return fmt.Errorf("API key is required for API key authentication")
		}
	case "basic":
		if strings.TrimSpace(cluster.User) == "" {
			return fmt.Errorf("username is required for basic authentication")
		}
	}
	return nil
}

func clusterFieldText(field clusterField, cluster appconfig.Cluster, auth string) (string, string) {
	switch field {
	case fieldName:
		return "Name", cluster.Name
	case fieldURL:
		return "URL", cluster.URL
	case fieldAuth:
		switch auth {
		case "apikey":
			return "Authentication", "API key"
		case "basic":
			return "Authentication", "Username / password"
		default:
			return "Authentication", "None"
		}
	case fieldAPIKey:
		return "API key", secretSummary(cluster.APIKey)
	case fieldUser:
		return "Username", cluster.User
	case fieldPassword:
		return "Password", secretSummary(cluster.Password)
	case fieldVerifyTLS:
		if cluster.VerifyTLS {
			return "Verify TLS", "On"
		}
		return "Verify TLS", "Off (insecure)"
	default:
		return "", ""
	}
}

func (a *app) editClusterScreen(initial appconfig.Cluster, originalName string) (appconfig.Cluster, bool) {
	draft := initial
	auth := clusterAuthMode(draft)
	selected := 0
	a.status.clear()

	for {
		fields := visibleClusterFields(auth)
		selected = min(selected, len(fields)-1)

		a.screen.Clear()
		maxX, _ := a.size()
		title := "Add cluster"
		if originalName != "" {
			title = "Edit cluster"
		}
		a.drawHeader(title, "credentials are saved in the user config file")
		a.drawText(2, 2, styleDim, "Select a field and press Enter to edit it.")

		for i, field := range fields {
			label, value := clusterFieldText(field, draft, auth)
			row := fmt.Sprintf("  %-18s %s", label, value)
			style := styleDefault
			if i == selected {
				style = styleSelect
			}
			a.drawText(0, 4+i, style, util.Clip(util.PadRight(row, maxX), maxX))
		}
		a.drawStatus("? help  ↑/↓ field  Enter edit  ←/→ change option  s save  b/Esc cancel  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		field := fields[selected]
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == '?':
			a.helpScreen()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			a.status.clear()
			return appconfig.Cluster{}, false
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			selected = max(0, selected-1)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			selected = min(len(fields)-1, selected+1)
		case ke.Key() == tcell.KeyLeft:
			switch field {
			case fieldAuth:
				auth = cycleAuthMode(auth, -1)
			case fieldVerifyTLS:
				draft.VerifyTLS = !draft.VerifyTLS
			}
			a.status.clear()
		case ke.Key() == tcell.KeyRight, ke.Key() == tcell.KeyRune && ke.Rune() == ' ':
			switch field {
			case fieldAuth:
				auth = cycleAuthMode(auth, 1)
			case fieldVerifyTLS:
				draft.VerifyTLS = !draft.VerifyTLS
			}
			a.status.clear()
		case ke.Key() == tcell.KeyEnter:
			var value string
			var accepted bool
			switch field {
			case fieldName:
				value, accepted = a.prompt("cluster name: ", draft.Name)
				if accepted {
					draft.Name = value
				}
			case fieldURL:
				value, accepted = a.prompt("cluster URL: ", draft.URL)
				if accepted {
					draft.URL = value
				}
			case fieldAuth:
				auth = cycleAuthMode(auth, 1)
				accepted = true
			case fieldAPIKey:
				value, accepted = a.promptSecret("API key (Ctrl+U replaces): ", draft.APIKey)
				if accepted {
					draft.APIKey = value
				}
			case fieldUser:
				value, accepted = a.prompt("username: ", draft.User)
				if accepted {
					draft.User = value
				}
			case fieldPassword:
				value, accepted = a.promptSecret("password (Ctrl+U replaces): ", draft.Password)
				if accepted {
					draft.Password = value
				}
			case fieldVerifyTLS:
				draft.VerifyTLS = !draft.VerifyTLS
				accepted = true
			}
			if accepted {
				a.status.clear()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 's':
			switch auth {
			case "none":
				draft.APIKey, draft.User, draft.Password = "", "", ""
			case "apikey":
				draft.User, draft.Password = "", ""
			case "basic":
				draft.APIKey = ""
			}
			draft.Normalize()
			if err := validateClusterAuth(auth, draft); err != nil {
				a.status.setErr(err.Error())
				continue
			}
			candidate := a.config.Clone()
			if err := candidate.Upsert(originalName, draft); err != nil {
				a.status.setErr(err.Error())
				continue
			}
			a.status.clear()
			return draft, true
		}
	}
}

func (a *app) saveCluster(originalName string, cluster appconfig.Cluster) bool {
	next := a.config.Clone()
	if err := next.Upsert(originalName, cluster); err != nil {
		a.status.setErr("save failed: " + err.Error())
		return false
	}

	activate := originalName == "" || a.activeCluster == originalName
	if activate {
		next.Active = cluster.Name
	}
	if err := a.store.Save(next); err != nil {
		a.status.setErr("save failed: " + err.Error())
		return false
	}

	a.config = next
	a.settingsWarn = ""
	if activate {
		a.configureClient(cluster)
		a.activeCluster = cluster.Name
		a.status.set(fmt.Sprintf("saved and using cluster %q", cluster.Name))
		return true
	}
	a.status.set(fmt.Sprintf("saved cluster %q", cluster.Name))
	return false
}

func (a *app) activateSavedCluster(name string) bool {
	cluster, ok := a.config.Find(name)
	if !ok {
		a.status.setErr(fmt.Sprintf("cluster %q no longer exists", name))
		return false
	}
	next := a.config.Clone()
	next.Active = name
	if err := a.store.Save(next); err != nil {
		a.status.setErr("save failed: " + err.Error())
		return false
	}
	a.config = next
	a.settingsWarn = ""
	a.configureClient(cluster)
	a.activeCluster = name
	a.status.set(fmt.Sprintf("using cluster %q", name))
	return true
}

func (a *app) settingsScreen() (connectionChanged bool) {
	st := &listState{length: len(a.config.Clusters)}

	for {
		st.length = len(a.config.Clusters)
		st.cursor = min(st.cursor, max(0, st.length-1))
		st.top = min(st.top, st.cursor)

		a.screen.Clear()
		maxX, maxY := a.size()
		source := "environment/session"
		if a.activeCluster != "" {
			source = "profile " + a.activeCluster
		}
		a.drawHeader("Settings", fmt.Sprintf("current: %s [%s]", a.client.BaseURL, source))
		if a.settingsWarn != "" {
			a.drawText(0, 1, styleErr, util.Clip("Warning: "+a.settingsWarn, maxX))
		}
		a.drawText(0, 2, styleUnder, util.Clip(fmt.Sprintf("  %-20s  %-44s  %-9s  %s", "name", "URL", "auth", "TLS"), maxX))

		bodyTop := 3
		bodyHeight := max(1, maxY-bodyTop-2)
		if st.length == 0 {
			a.drawText(2, bodyTop+1, styleDim, "No saved clusters. Press a to add one.")
		} else {
			a.drawList(bodyTop, bodyHeight, maxX, st, func(i int) string {
				cluster := a.config.Clusters[i]
				marker := " "
				if cluster.Name == a.activeCluster {
					marker = "*"
				}
				tls := "verify"
				if !cluster.VerifyTLS {
					tls = "skip"
				}
				return fmt.Sprintf("%s %-20s  %-44s  %-9s  %s",
					marker,
					util.Clip(cluster.Name, 20),
					util.Clip(cluster.URL, 44),
					clusterAuthLabel(cluster),
					tls)
			})
		}
		a.drawStatus("? help  ↑/↓ move  Enter use  a add  e edit  r health  b/Esc back  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == '?':
			a.helpScreen()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			return connectionChanged
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			st.move(-1, bodyHeight)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			st.move(1, bodyHeight)
		case ke.Key() == tcell.KeyPgDn:
			st.move(bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyPgUp:
			st.move(-bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			st.home()
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			st.end(bodyHeight)
		case ke.Key() == tcell.KeyEnter:
			if st.length > 0 && a.activateSavedCluster(a.config.Clusters[st.cursor].Name) {
				a.refreshHealth()
				connectionChanged = true
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'a':
			initial := appconfig.Cluster{
				URL:       a.client.BaseURL,
				APIKey:    a.client.APIKey,
				User:      a.client.User,
				Password:  a.client.Password,
				VerifyTLS: a.client.VerifyTLS,
			}
			if cluster, saved := a.editClusterScreen(initial, ""); saved {
				if a.saveCluster("", cluster) {
					a.refreshHealth()
					connectionChanged = true
				}
				st.length = len(a.config.Clusters)
				st.end(bodyHeight)
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'e':
			if st.length > 0 {
				cluster := a.config.Clusters[st.cursor]
				if edited, saved := a.editClusterScreen(cluster, cluster.Name); saved {
					if a.saveCluster(cluster.Name, edited) {
						a.refreshHealth()
						connectionChanged = true
					}
				}
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'r':
			if a.refreshHealth() {
				a.status.set("cluster health refreshed")
			} else {
				a.status.setErr("cluster health check failed: " + a.health.detail)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Indices screen
// ---------------------------------------------------------------------------

func (a *app) indicesScreen() (selected string, quit bool) {
	var rawItems, items []map[string]any
	st := &listState{}
	filterText := ""

	applyFilter := func() {
		items = filterIndices(rawItems, filterText, a.showHidden)
		st.length = len(items)
		st.cursor = min(st.cursor, max(0, st.length-1))
		st.top = min(st.top, st.cursor)
	}
	reload := func() {
		v, err := a.fetchIndices(a.showHidden)
		a.updateHealthAfterRequest(err)
		if err != nil {
			a.status.setErr("failed to fetch indices: " + err.Error())
			rawItems = nil
		} else {
			a.status.clear()
			rawItems = v
		}
		applyFilter()
	}
	reload()

	for {
		a.screen.Clear()
		maxX, maxY := a.size()
		sub := fmt.Sprintf("%s   (%d shown / %d total)", a.client.BaseURL, len(items), len(rawItems))
		if filterText != "" {
			sub += fmt.Sprintf("   filter: %q", filterText)
		}
		if a.showHidden {
			sub += "   hidden: shown"
		}
		a.drawHeader("Indices", sub)

		colHdr := fmt.Sprintf("%-6s  %-40s  %10s  %10s", "health", "index", "docs", "size")
		a.drawText(0, 2, styleUnder, util.Clip(colHdr, maxX))

		bodyTop := 3
		bodyHeight := maxY - bodyTop - 2
		a.drawList(bodyTop, bodyHeight, maxX, st, func(i int) string {
			r := items[i]
			return fmt.Sprintf("%-6s  %-40s  %10s  %10s",
				util.AsStr(r["health"]),
				util.Clip(util.AsStr(r["index"]), 40),
				util.AsStr(r["docs.count"]),
				util.AsStr(r["store.size"]))
		})
		a.drawStatus("? help  ↑/↓ move  Enter open  / filter  h hidden  r refresh  S settings  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == '?':
			a.helpScreen()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q', ke.Key() == tcell.KeyEscape:
			return "", true
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			st.move(-1, bodyHeight)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			st.move(1, bodyHeight)
		case ke.Key() == tcell.KeyPgDn:
			st.move(bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyPgUp:
			st.move(-bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			st.home()
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			st.end(bodyHeight)
		case ke.Key() == tcell.KeyEnter, ke.Key() == tcell.KeyRune && ke.Rune() == 'v':
			if st.length > 0 {
				return util.AsStr(items[st.cursor]["index"]), false
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == '/':
			if v, ok := a.prompt("filter: ", filterText); ok {
				filterText = v
				applyFilter()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'h':
			a.showHidden = !a.showHidden
			st.home()
			reload()
			if a.showHidden {
				a.status.set("showing hidden indices")
			} else {
				a.status.set("hidden indices hidden")
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'S':
			if a.settingsScreen() {
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'r':
			reload()
		}
	}
}

// ---------------------------------------------------------------------------
// Docs screen
// ---------------------------------------------------------------------------

func (a *app) docsScreen(ctx *docsContext) {
	var rawHits, items []map[string]any
	st := &listState{}

	applyFilter := func() {
		items = filterHits(rawHits, ctx.filterText)
		st.length = len(items)
		st.cursor = min(st.cursor, max(0, st.length-1))
		st.top = min(st.top, st.cursor)
	}
	reload := func() {
		v, err := a.fetchDocs(ctx)
		a.updateHealthAfterRequest(err)
		if err != nil {
			a.status.setErr("search failed: " + err.Error())
			rawHits = nil
		} else {
			a.status.clear()
			rawHits = v
		}
		applyFilter()
	}
	reload()

	for {
		a.screen.Clear()
		maxX, maxY := a.size()

		queryPart := "all"
		if ctx.query != "" {
			queryPart = fmt.Sprintf("q=%q", ctx.query)
		}
		subParts := []string{
			queryPart,
			fmt.Sprintf("page from=%d size=%d", ctx.from, ctx.size),
			fmt.Sprintf("shown=%d/%d total=%d", len(items), len(rawHits), ctx.total),
		}
		if ctx.filterText != "" {
			subParts = append(subParts, fmt.Sprintf("filter=%q", ctx.filterText))
		}
		a.drawHeader(ctx.index, strings.Join(subParts, "   "))
		a.drawText(0, 2, styleUnder, util.Clip(fmt.Sprintf("%-40s  preview", "_id"), maxX))

		bodyTop := 3
		bodyHeight := maxY - bodyTop - 2
		a.drawList(bodyTop, bodyHeight, maxX, st, func(i int) string {
			return renderDocRow(items[i])
		})
		a.drawStatus("? help  Enter/v view  e edit  d delete  / filter  f query  n/p page  s size  r refresh  S settings  b back  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == '?':
			a.helpScreen()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			return
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			st.move(-1, bodyHeight)
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			st.move(1, bodyHeight)
		case ke.Key() == tcell.KeyPgDn:
			st.move(bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyPgUp:
			st.move(-bodyHeight, bodyHeight)
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			st.home()
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			st.end(bodyHeight)
		case ke.Key() == tcell.KeyEnter, ke.Key() == tcell.KeyRune && ke.Rune() == 'v':
			if st.length > 0 {
				a.viewDocScreen(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'e':
			if st.length > 0 {
				a.editDoc(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'd':
			if st.length > 0 {
				a.deleteDoc(ctx.index, items[st.cursor])
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == '/':
			if v, ok := a.prompt("filter (id/source substring): ", ctx.filterText); ok {
				ctx.filterText = v
				applyFilter()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'f':
			if v, ok := a.prompt("Lucene q (empty = all): ", ctx.query); ok {
				ctx.query = v
				ctx.from = 0
				st.home()
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'n':
			ctx.from += ctx.size
			st.home()
			reload()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'p':
			ctx.from = max(0, ctx.from-ctx.size)
			st.home()
			reload()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 's':
			if v, ok := a.prompt("page size: ", strconv.Itoa(ctx.size)); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					ctx.size = max(1, min(10000, n))
					ctx.from = 0
					st.home()
					reload()
				}
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'S':
			if a.settingsScreen() {
				ctx.from = 0
				st.home()
				reload()
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'r':
			reload()
		}
	}
}

// ---------------------------------------------------------------------------
// Viewer screen
// ---------------------------------------------------------------------------

func (a *app) viewDocScreen(index string, hit map[string]any) {
	docID := util.AsStr(hit["_id"])
	source := hit["_source"]
	text, _ := util.MarshalIndent(source)
	lines := splitLines(text)
	top := 0

	for {
		a.screen.Clear()
		maxX, maxY := a.size()
		a.drawHeader(index+" / "+docID, fmt.Sprintf("%d lines", len(lines)))
		bodyTop := 2
		bodyHeight := maxY - bodyTop - 2
		top = max(0, min(top, max(0, len(lines)-bodyHeight)))
		for i := 0; i < bodyHeight; i++ {
			li := top + i
			if li >= len(lines) {
				break
			}
			a.drawText(0, bodyTop+i, styleDefault, util.Clip(lines[li], maxX))
		}
		a.drawStatus("? help  ↑/↓ scroll  PgUp/PgDn page  g/G top/bot  e edit  d delete  S settings  b/Esc back  q quit")
		a.screen.Show()

		ev := a.screen.PollEvent()
		ke, ok := ev.(*tcell.EventKey)
		if !ok {
			continue
		}
		switch {
		case ke.Key() == tcell.KeyRune && ke.Rune() == '?':
			a.helpScreen()
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'q':
			panic(quitSignal{})
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'b', ke.Key() == tcell.KeyEscape:
			return
		case ke.Key() == tcell.KeyUp, ke.Key() == tcell.KeyRune && ke.Rune() == 'k':
			top--
		case ke.Key() == tcell.KeyDown, ke.Key() == tcell.KeyRune && ke.Rune() == 'j':
			top++
		case ke.Key() == tcell.KeyPgDn:
			top += bodyHeight
		case ke.Key() == tcell.KeyPgUp:
			top -= bodyHeight
		case ke.Key() == tcell.KeyHome, ke.Key() == tcell.KeyRune && ke.Rune() == 'g':
			top = 0
		case ke.Key() == tcell.KeyEnd, ke.Key() == tcell.KeyRune && ke.Rune() == 'G':
			top = max(0, len(lines)-bodyHeight)
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'e':
			if a.editDoc(index, hit) {
				s, body, _ := a.client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
				if s < 300 {
					if m, ok := body.(map[string]any); ok {
						source = m["_source"]
						text, _ = util.MarshalIndent(source)
						lines = splitLines(text)
						for k, v := range m {
							hit[k] = v
						}
					}
				}
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'd':
			if a.deleteDoc(index, hit) {
				return
			}
		case ke.Key() == tcell.KeyRune && ke.Rune() == 'S':
			if a.settingsScreen() {
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Mutating actions
// ---------------------------------------------------------------------------

// editDoc fetches the latest doc, opens it in $EDITOR (tcell suspended), and
// PUTs it back with optimistic concurrency control. Returns true on success.
func (a *app) editDoc(index string, hit map[string]any) bool {
	docID := util.AsStr(hit["_id"])
	s, body, err := a.client.Request("GET", "/"+index+"/_doc/"+docID, nil, nil)
	if err != nil || s >= 300 {
		a.status.setErr(fmt.Sprintf("get failed: HTTP %d", s))
		return false
	}
	m, ok := body.(map[string]any)
	if !ok {
		a.status.setErr("get failed: unexpected response")
		return false
	}
	source := m["_source"]
	originalText, _ := util.MarshalIndent(source)

	editedText, ok := a.suspendEdit(originalText)
	if !ok {
		a.status.setErr("edit cancelled")
		return false
	}
	if strings.TrimSpace(editedText) == strings.TrimSpace(originalText) {
		a.status.set("no changes")
		return false
	}
	var newSource any
	dec := json.NewDecoder(strings.NewReader(editedText))
	dec.UseNumber()
	if err := dec.Decode(&newSource); err != nil {
		a.status.setErr("invalid JSON: " + err.Error())
		return false
	}

	params := map[string]string{"refresh": "true"}
	if seq, ok1 := m["_seq_no"]; ok1 {
		if term, ok2 := m["_primary_term"]; ok2 {
			params["if_seq_no"] = util.AsStr(seq)
			params["if_primary_term"] = util.AsStr(term)
		}
	}
	s, resp, err := a.client.Request("PUT", "/"+index+"/_doc/"+docID, newSource, params)
	if err != nil || s >= 300 {
		a.status.setErr(util.Clip(fmt.Sprintf("update failed: HTTP %d: %v", s, resp), 200))
		return false
	}
	result := "ok"
	if rm, ok := resp.(map[string]any); ok {
		if r := util.AsStr(rm["result"]); r != "" {
			result = r
		}
	}
	a.status.set(fmt.Sprintf("updated %s/%s (%s)", index, docID, result))
	hit["_source"] = newSource
	return true
}

func (a *app) deleteDoc(index string, hit map[string]any) bool {
	docID := util.AsStr(hit["_id"])
	if !a.confirm(fmt.Sprintf("delete %s/%s?", index, docID)) {
		a.status.set("delete cancelled")
		return false
	}
	s, resp, err := a.client.Request("DELETE", "/"+index+"/_doc/"+docID, nil, map[string]string{"refresh": "true"})
	if err != nil || s >= 300 {
		a.status.setErr(util.Clip(fmt.Sprintf("delete failed: HTTP %d: %v", s, resp), 200))
		return false
	}
	a.status.set(fmt.Sprintf("deleted %s/%s", index, docID))
	return true
}

// suspendEdit suspends the tcell screen, runs $EDITOR, and resumes.
func (a *app) suspendEdit(initialText string) (string, bool) {
	if err := a.screen.Suspend(); err != nil {
		a.status.setErr("cannot suspend screen: " + err.Error())
		return "", false
	}
	edited, err := util.ShellEdit(initialText+"\n", ".json")
	_ = a.screen.Resume()
	if err != nil {
		return "", false
	}
	return edited, true
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	lines := strings.Split(s, "\n")
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
