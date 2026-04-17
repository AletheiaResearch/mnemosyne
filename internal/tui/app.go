package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	screen screenKey
	menu   menuModel
}

func NewApp() tea.Model {
	return model{
		screen: screenMenu,
		menu:   newMenuModel(),
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
			if m.screen == screenMenu {
				return m, tea.Quit
			}
		case "esc", "backspace":
			if m.screen != screenMenu {
				m.screen = screenMenu
				return m, nil
			}
		case "enter":
			if m.screen == screenMenu {
				selection := m.menu.Selected()
				if selection == "quit" {
					return m, tea.Quit
				}
				m.screen = screenKey(selection)
				return m, nil
			}
		}
	}

	if m.screen == screenMenu {
		updated, cmd := m.menu.Update(msg)
		m.menu = updated.(menuModel)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	if m.screen == screenMenu {
		return m.menu.View()
	}

	screen := m.currentScreen()
	title := lipgloss.NewStyle().Bold(true).Render(screen.Title)
	body := lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(screen.Body)
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Press esc to return to the menu.")
	return strings.Join([]string{title, "", body, "", footer}, "\n")
}

func (m model) currentScreen() screenDescriptor {
	switch m.screen {
	case screenSurvey:
		return surveyScreen()
	case screenConfigure:
		return configureScreen()
	case screenExtract:
		return extractScreen()
	case screenAttest:
		return reviewScreen()
	case screenPublish:
		return publishScreen()
	case screenRunlog:
		return runlogScreen()
	case screenTransform:
		return transformScreen()
	case screenValidate:
		return validateScreen()
	default:
		return screenDescriptor{Title: "Mnemosyne", Body: "Select a workflow step from the menu."}
	}
}
