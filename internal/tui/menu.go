package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AletheiaResearch/mnemosyne/internal/tui/common"
)

type menuItem struct {
	key         string
	label       string
	description string
}

type menuModel struct {
	items  []menuItem
	index  int
	width  int
	height int
}

func newMenuModel() *menuModel {
	return &menuModel{
		items: []menuItem{
			{"survey", "Survey", "Inspect saved state and detect available groupings."},
			{"configure", "Configure", "Persist scope, redactions, handles and destination repo."},
			{"extract", "Extract", "Run the extraction pipeline and emit canonical JSONL."},
			{"attest", "Attest", "Record reviewer statements against an extracted file."},
			{"publish", "Publish", "Publish the reviewed export to your dataset repo."},
			{"runlog", "Run log", "Show the persisted workflow state from recent steps."},
			{"transform", "Transform", "Convert canonical JSONL into another serializer format."},
			{"validate", "Validate", "Validate canonical JSONL before publication."},
			{"quit", "Quit", "Exit the TUI."},
		},
	}
}

func (m *menuModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *menuModel) Selected() string {
	if m.index < 0 || m.index >= len(m.items) {
		return ""
	}
	return m.items[m.index].key
}

func (m *menuModel) Init() tea.Cmd { return nil }

func (m *menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.index > 0 {
				m.index--
			}
		case "down", "j":
			if m.index < len(m.items)-1 {
				m.index++
			}
		case "home", "g":
			m.index = 0
		case "end", "G":
			m.index = len(m.items) - 1
		case "enter":
			if m.items[m.index].key == "quit" {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *menuModel) View() string {
	panelWidth := m.width - 2
	if panelWidth < 36 {
		panelWidth = 36
	}
	if panelWidth > 72 {
		panelWidth = 72
	}

	header := lipgloss.JoinVertical(lipgloss.Left,
		common.TitleStyle.Render("Mnemosyne"),
		common.SubtitleStyle.Render("Export coding-assistant histories into anonymized archives."),
	)

	var rows []string
	rowStyle := lipgloss.NewStyle().Width(panelWidth - 4)
	focusStyle := rowStyle.
		Foreground(common.ColorOnBrand).
		Background(common.ColorBrand).
		Bold(true)

	for idx, item := range m.items {
		marker := "  "
		label := item.label
		desc := item.description
		line := marker + label
		if idx == m.index {
			marker = "▸ "
			line = marker + label
			line = focusStyle.Render(padRight(line, panelWidth-4))
		} else {
			line = rowStyle.Render(line)
		}
		rows = append(rows, line)
		if idx == m.index {
			rows = append(rows, lipgloss.NewStyle().
				PaddingLeft(2).
				Foreground(common.ColorMuted).
				Render(desc))
		}
	}

	panel := common.PanelStyle.
		Width(panelWidth).
		Render(strings.Join(rows, "\n"))

	content := lipgloss.JoinVertical(lipgloss.Left, header, "", panel)
	// Centre horizontally if there's room.
	if m.width > panelWidth+4 {
		return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
	}
	return content
}

func (m *menuModel) FooterHints() string {
	return common.HintLine("↑↓ navigate", "enter select", "q quit")
}

func (m *menuModel) FooterStatus() string { return "" }

func padRight(s string, width int) string {
	pad := width - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}
