package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type theme struct {
	title       lipgloss.Style
	subtitle    lipgloss.Style
	panel       lipgloss.Style
	selected    lipgloss.Style
	dim         lipgloss.Style
	success     lipgloss.Style
	warning     lipgloss.Style
	danger      lipgloss.Style
	spinner     lipgloss.Style
	status      lipgloss.Style
	statusError lipgloss.Style
	key         lipgloss.Style
	jsonString  lipgloss.Style
	jsonNumber  lipgloss.Style
	jsonBool    lipgloss.Style
	match       lipgloss.Style
}

var styles = theme{
	title: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#3A3A9A", Dark: "#B6B7FF"}),
	subtitle: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#A0A0A0"}),
	panel: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.AdaptiveColor{Light: "#B0B0B0", Dark: "#4A4A4A"}).
		Padding(1, 2),
	selected: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#101014"}).
		Background(lipgloss.AdaptiveColor{Light: "#5A5AC8", Dark: "#B6B7FF"}),
	dim: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#777777", Dark: "#777777"}),
	success: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#19733B", Dark: "#5EE391"}),
	warning: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#8A6300", Dark: "#FFD166"}),
	danger: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#A82032", Dark: "#FF6B7A"}),
	spinner: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#5A5AC8", Dark: "#B6B7FF"}),
	status: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#202020", Dark: "#E7E7E7"}),
	statusError: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#A82032", Dark: "#FF6B7A"}),
	key: lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#3A3A9A", Dark: "#B6B7FF"}),
	jsonString: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#19733B", Dark: "#5EE391"}),
	jsonNumber: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#0B5FA5", Dark: "#7FC4FF"}),
	jsonBool: lipgloss.NewStyle().
		Foreground(lipgloss.AdaptiveColor{Light: "#8A6300", Dark: "#FFD166"}),
	match: lipgloss.NewStyle().
		Bold(true).
		Underline(true).
		Foreground(lipgloss.AdaptiveColor{Light: "#A82032", Dark: "#FF6B7A"}),
}

// highlightMatch underlines the first case-insensitive occurrence of filter
// within text, for filtered index/document lists. Empty filter or no match
// returns text unchanged.
func highlightMatch(text, filter string) string {
	if filter == "" {
		return text
	}
	idx := strings.Index(strings.ToLower(text), strings.ToLower(filter))
	if idx < 0 {
		return text
	}
	return text[:idx] + styles.match.Render(text[idx:idx+len(filter)]) + text[idx+len(filter):]
}

var (
	jsonKeyLineRe  = regexp.MustCompile(`^"((?:[^"\\]|\\.)*)":\s*(.*)$`)
	jsonStringRe   = regexp.MustCompile(`^"((?:[^"\\]|\\.)*)"(,?)$`)
	jsonBoolNullRe = regexp.MustCompile(`^(true|false|null)(,?)$`)
	jsonNumberRe   = regexp.MustCompile(`^(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)(,?)$`)
)

// highlightJSON colors keys, strings, numbers, and booleans/null in
// pretty-printed JSON produced by util.Dump, one line at a time so ANSI
// escapes from an earlier pass are never re-matched by a later regex.
func highlightJSON(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = highlightJSONLine(line)
	}
	return strings.Join(lines, "\n")
}

func highlightJSONLine(line string) string {
	indentLen := len(line) - len(strings.TrimLeft(line, " "))
	indent := line[:indentLen]
	rest := line[indentLen:]
	if rest == "" {
		return line
	}
	if m := jsonKeyLineRe.FindStringSubmatch(rest); m != nil {
		key := styles.key.Render(`"`+m[1]+`"`) + ":"
		return indent + key + " " + highlightJSONValue(m[2])
	}
	return indent + highlightJSONValue(rest)
}

func highlightJSONValue(value string) string {
	switch {
	case jsonStringRe.MatchString(value):
		m := jsonStringRe.FindStringSubmatch(value)
		return styles.jsonString.Render(`"`+m[1]+`"`) + m[2]
	case jsonBoolNullRe.MatchString(value):
		m := jsonBoolNullRe.FindStringSubmatch(value)
		return styles.jsonBool.Render(m[1]) + m[2]
	case jsonNumberRe.MatchString(value):
		m := jsonNumberRe.FindStringSubmatch(value)
		return styles.jsonNumber.Render(m[1]) + m[2]
	default:
		return value
	}
}

func renderHeader(title, subtitle string, health healthStatus, width int) string {
	left := styles.title.Render(" " + title)
	if subtitle != "" {
		left += styles.subtitle.Render("  " + subtitle)
	}
	badge := healthBadge(health)
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(badge))
	return left + strings.Repeat(" ", gap) + badge
}

func healthBadge(health healthStatus) string {
	label := health.label()
	switch health.state {
	case stateHealthGreen, stateHealthConnected:
		return styles.success.Render(label)
	case stateHealthYellow, stateHealthUnavailable, stateHealthChecking:
		return styles.warning.Render(label)
	default:
		return styles.danger.Render(label)
	}
}

// renderFooter always keeps the hotkey hints visible on their own line;
// status messages (which may contain multi-line JSON from error responses)
// are flattened and clipped to a single line above them.
func renderFooter(status notification, hint string, width int) string {
	statusLine := ""
	if status.text != "" {
		style := styles.status
		if status.isErr {
			style = styles.statusError
		}
		statusLine = style.Render(singleLine(status.text, max(10, width-1)))
	}
	hintLine := styles.dim.Render(singleLine(hint, max(10, width-1)))
	return statusLine + "\n" + hintLine
}

// singleLine collapses all whitespace (including newlines) into single spaces
// and truncates the result to at most width runes.
func singleLine(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:max(1, width-1)]) + "…"
	}
	return s
}

// fieldRow renders a hotkey-labeled form row, shared by the search builder.
func fieldRow(key, label, value string) string {
	return fmt.Sprintf("%s  %-16s %s", styles.key.Render(key), label, value)
}

type helpSection struct {
	title string
	rows  [][2]string
}

var helpSections = []helpSection{
	{title: "Global", rows: [][2]string{
		{"?", "toggle this help"},
		{"ctrl+c / q", "quit"},
		{".", "cluster settings"},
		{"esc", "back"},
	}},
	{title: "Lists and viewers", rows: [][2]string{
		{"↑/↓ or j/k", "move / scroll"},
		{"g / G", "jump to top / bottom"},
		{"pgup / pgdn", "page up / down"},
		{"u / d", "half page up / down (viewers)"},
	}},
	{title: "Indices", rows: [][2]string{
		{"enter", "open index"},
		{"/", "filter"},
		{"h", "toggle hidden indices"},
		{"i", "index details (settings + mappings)"},
		{"c", "cluster info"},
		{"r", "refresh"},
	}},
	{title: "Index details", rows: [][2]string{
		{"tab", "switch settings ↔ mappings"},
		{"r", "refresh"},
	}},
	{title: "Documents", rows: [][2]string{
		{"enter", "view document"},
		{"a", "create document"},
		{"e", "edit (replace) document"},
		{"u / U", "partial update / upsert"},
		{"d / D", "delete document / delete by query"},
		{"/", "client-side filter"},
		{"f", "server query (Lucene)"},
		{"F", "advanced search builder"},
		{"n / p", "next / previous page"},
		{"s", "page size"},
		{"i", "index details"},
		{"r", "refresh"},
	}},
	{title: "Document viewer", rows: [][2]string{
		{"e", "edit (replace)"},
		{"u / U", "partial update / upsert"},
		{"d", "delete"},
		{"w", "toggle wrap"},
		{"r", "refresh"},
	}},
	{title: "Advanced search", rows: [][2]string{
		{"f", "Lucene query"},
		{"s", "sort"},
		{"o", "_source filter"},
		{"j", "raw JSON body"},
		{"i", "IDs only"},
		{"c", "exact count"},
		{"x", "reset"},
		{"enter", "run search"},
	}},
	{title: "Cluster settings", rows: [][2]string{
		{"enter", "connect and open indices"},
		{"a", "add profile"},
		{"e", "edit profile"},
		{"d", "delete profile"},
		{"c", "quick connect (session only)"},
		{"r", "check health of every profile"},
	}},
	{title: "Cluster profile editor", rows: [][2]string{
		{"↑/↓", "select field"},
		{"enter", "edit field / toggle value"},
		{"←/→", "change auth mode or TLS"},
		{"ctrl+s", "save and connect"},
		{"esc", "back (warns on unsaved changes)"},
	}},
}

// renderHelpContent builds the full hotkey reference shown inside the
// scrollable help viewport.
func renderHelpContent() string {
	var body strings.Builder
	for _, section := range helpSections {
		body.WriteString("\n")
		body.WriteString(styles.subtitle.Render(section.title))
		body.WriteString("\n")
		for _, row := range section.rows {
			fmt.Fprintf(&body, "  %-12s %s\n", styles.key.Render(row[0]), row[1])
		}
	}
	return body.String()
}
