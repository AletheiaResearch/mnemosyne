package common

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ChromeRows is the number of vertical rows consumed by Header + Footer
// (title row + separator + separator + footer row).
const ChromeRows = 4

// InnerSize returns the body width and height available inside the chrome.
func InnerSize(width, height int) (int, int) {
	h := height - ChromeRows
	if h < 0 {
		h = 0
	}
	w := width
	if w < 0 {
		w = 0
	}
	return w, h
}

// Header renders the top chrome: brand pill on the left, breadcrumb in the
// middle, and optional trailing text on the right (e.g. version).
func Header(brand, breadcrumb, trailing string, width int) string {
	if width <= 0 {
		return ""
	}

	pill := BrandPillStyle.Render(brand)
	crumb := SubtitleStyle.Render(breadcrumb)
	right := SubtleStyle.Render(trailing)

	left := pill
	if breadcrumb != "" {
		left = lipgloss.JoinHorizontal(lipgloss.Center, pill, "  ", crumb)
	}

	spacer := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if spacer < 1 {
		spacer = 1
	}
	title := lipgloss.JoinHorizontal(lipgloss.Center, " ", left, strings.Repeat(" ", spacer), right, " ")

	rule := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", width))
	return lipgloss.JoinVertical(lipgloss.Left, title, rule)
}

// Footer renders the bottom chrome: keybinding hints on the left, status on
// the right.
func Footer(hints, status string, width int) string {
	if width <= 0 {
		return ""
	}

	left := SubtleStyle.Render(hints)
	right := MutedStyle.Render(status)

	spacer := width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if spacer < 1 {
		spacer = 1
	}
	line := lipgloss.JoinHorizontal(lipgloss.Center, " ", left, strings.Repeat(" ", spacer), right, " ")
	rule := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", width))
	return lipgloss.JoinVertical(lipgloss.Left, rule, line)
}

// Compose stacks header, body, and footer into a full-screen frame. It
// ensures the body is padded (or clipped) to the available height so chrome
// stays anchored at the edges.
func Compose(header, body, footer string, width, height int) string {
	inner := height - ChromeRows
	if inner < 0 {
		inner = 0
	}

	bodyStyled := lipgloss.NewStyle().
		Width(width).
		Height(inner).
		MaxHeight(inner).
		Padding(0, 1).
		Render(body)

	return lipgloss.JoinVertical(lipgloss.Left, header, bodyStyled, footer)
}

// HintLine joins keybinding hints with a middle-dot separator.
func HintLine(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, " · ")
}

// Panel renders a titled rounded panel containing the given body. The title
// sits above the border. Width is the outer width of the panel.
func Panel(title, body string, width int) string {
	if width < 6 {
		width = 6
	}
	inner := width - 2 // border columns
	box := PanelStyle.Copy().Width(inner).Render(body)
	if title == "" {
		return box
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		PanelTitleStyle.Render(title),
		box,
	)
}

// KV renders a key/value row with the key padded to keyWidth and the value
// rendered as-is (already styled by caller if needed).
func KV(key, value string, keyWidth int) string {
	k := LabelStyle.Copy().Width(keyWidth).Render(key)
	return lipgloss.JoinHorizontal(lipgloss.Top, k, " ", value)
}
