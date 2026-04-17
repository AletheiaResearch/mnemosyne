package tui

import tea "github.com/charmbracelet/bubbletea"

type model struct {
	menu menuModel
}

func NewApp() tea.Model {
	return model{
		menu: newMenuModel(),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}
	}
	updated, cmd := m.menu.Update(msg)
	m.menu = updated.(menuModel)
	return m, cmd
}

func (m model) View() string {
	return m.menu.View()
}
