package tui

import (
	"fmt"
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

func renderFooter(status notification, hint string, width int) string {
	left := ""
	if status.text != "" {
		style := styles.status
		if status.isErr {
			style = styles.statusError
		}
		left = style.Render(status.text)
	}
	right := styles.dim.Render(hint)
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", gap) + right
}

func renderHelpOverlay(width, height int) string {
	help := styles.title.Render("Keyboard shortcuts") + "\n\n" +
		fmt.Sprintf("%s  open or close help\n%s  quit immediately\n%s  go back",
			styles.key.Render("?"),
			styles.key.Render("ctrl+c"),
			styles.key.Render("esc / b"),
		)
	return styles.panel.
		Width(max(20, min(64, width-4))).
		Height(max(6, min(18, height-4))).
		Render(help)
}
