package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type menuModel struct {
	items []string
	index int
}

func newMenuModel() menuModel {
	return menuModel{
		items: []string{
			"survey",
			"configure",
			"extract",
			"attest",
			"publish",
			"transform",
			"validate",
			"quit",
		},
	}
}

func (m menuModel) Selected() string {
	if m.index < 0 || m.index >= len(m.items) {
		return ""
	}
	return m.items[m.index]
}

func (m menuModel) Init() tea.Cmd {
	return nil
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		case "enter":
			if m.items[m.index] == "quit" {
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	var lines []string
	title := lipgloss.NewStyle().Bold(true).Render("Mnemosyne")
	lines = append(lines, title, "", "Interactive workflow", "")
	for idx, item := range m.items {
		prefix := "  "
		if idx == m.index {
			prefix = "> "
		}
		lines = append(lines, prefix+item)
	}
	lines = append(lines, "", "Use j/k or arrows to move, q to quit.")
	return strings.Join(lines, "\n")
}
