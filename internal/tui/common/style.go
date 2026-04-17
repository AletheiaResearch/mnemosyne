package common

import "github.com/charmbracelet/lipgloss"

var (
	ColorBrand    = lipgloss.Color("#7d56f4")
	ColorBrandDim = lipgloss.Color("#5b3fd6")
	ColorAccent   = lipgloss.Color("#22d3ee")
	ColorSuccess  = lipgloss.Color("#34d399")
	ColorWarn     = lipgloss.Color("#fbbf24")
	ColorError    = lipgloss.Color("#f87171")
	ColorOnBrand  = lipgloss.Color("#ffffff")
	ColorOnAccent = lipgloss.Color("#0c4a6e")

	ColorText     = lipgloss.AdaptiveColor{Light: "#111827", Dark: "#e5e7eb"}
	ColorEmphasis = lipgloss.AdaptiveColor{Light: "#030712", Dark: "#f9fafb"}
	ColorMuted    = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	ColorSubtle   = lipgloss.AdaptiveColor{Light: "#9ca3af", Dark: "#6b7280"}
	ColorBorder   = lipgloss.AdaptiveColor{Light: "#d1d5db", Dark: "#3f3f46"}
	ColorBorderHi = lipgloss.AdaptiveColor{Light: "#7d56f4", Dark: "#a78bfa"}
)

var (
	TitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(ColorEmphasis)
	SubtitleStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	MutedStyle    = lipgloss.NewStyle().Foreground(ColorMuted)
	SubtleStyle   = lipgloss.NewStyle().Foreground(ColorSubtle)
	LabelStyle    = lipgloss.NewStyle().Foreground(ColorMuted)
	ValueStyle    = lipgloss.NewStyle().Foreground(ColorText)
	FocusStyle    = lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)
	AccentStyle   = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	ErrorStyle    = lipgloss.NewStyle().Foreground(ColorError).Bold(true)
	SuccessStyle  = lipgloss.NewStyle().Foreground(ColorSuccess).Bold(true)
	WarnStyle     = lipgloss.NewStyle().Foreground(ColorWarn).Bold(true)

	PanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(0, 1)

	PanelTitleStyle = lipgloss.NewStyle().
			Foreground(ColorBrand).
			Bold(true).
			PaddingRight(1)

	BrandPillStyle = lipgloss.NewStyle().
			Background(ColorBrand).
			Foreground(ColorOnBrand).
			Bold(true).
			Padding(0, 1)

	AccentPillStyle = lipgloss.NewStyle().
			Background(ColorAccent).
			Foreground(ColorOnAccent).
			Bold(true).
			Padding(0, 1)

	SuccessPillStyle = lipgloss.NewStyle().
				Background(ColorSuccess).
				Foreground(lipgloss.Color("#064e3b")).
				Bold(true).
				Padding(0, 1)

	WarnPillStyle = lipgloss.NewStyle().
			Background(ColorWarn).
			Foreground(lipgloss.Color("#78350f")).
			Bold(true).
			Padding(0, 1)

	ErrorPillStyle = lipgloss.NewStyle().
			Background(ColorError).
			Foreground(lipgloss.Color("#450a0a")).
			Bold(true).
			Padding(0, 1)

	MutedPillStyle = lipgloss.NewStyle().
			Background(ColorSubtle).
			Foreground(lipgloss.Color("#111827")).
			Bold(true).
			Padding(0, 1)
)

func Pill(style lipgloss.Style, text string) string {
	return style.Render(text)
}
