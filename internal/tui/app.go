package tui

import tea "github.com/charmbracelet/bubbletea"

type screenKey string

const (
	screenMenu      screenKey = "menu"
	screenSurvey    screenKey = "survey"
	screenConfigure screenKey = "configure"
	screenExtract   screenKey = "extract"
	screenAttest    screenKey = "attest"
	screenPublish   screenKey = "publish"
	screenRunlog    screenKey = "runlog"
	screenTransform screenKey = "transform"
	screenValidate  screenKey = "validate"
)

type screenDescriptor struct {
	Title string
	Body  string
}

type model struct {
	screen     screenKey
	menu       menuModel
	active     tea.Model
	configPath string
}

func NewApp(configPath string) tea.Model {
	return model{
		screen:     screenMenu,
		menu:       newMenuModel(),
		configPath: configPath,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			if m.screen == screenMenu {
				return m, tea.Quit
			}
		case "esc":
			if m.screen != screenMenu {
				m.screen = screenMenu
				m.active = nil
				return m, nil
			}
		case "enter":
			if m.screen == screenMenu {
				selection := m.menu.Selected()
				if selection == "quit" {
					return m, tea.Quit
				}
				m.screen = screenKey(selection)
				m.active = newScreen(m.screen, m.configPath)
				if m.active == nil {
					m.screen = screenMenu
					return m, nil
				}
				return m, m.active.Init()
			}
		}
	}

	if m.screen == screenMenu {
		updated, cmd := m.menu.Update(msg)
		m.menu = updated.(menuModel)
		return m, cmd
	}
	if m.active != nil {
		updated, cmd := m.active.Update(msg)
		m.active = updated
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	if m.screen == screenMenu || m.active == nil {
		return m.menu.View()
	}
	return m.active.View()
}
