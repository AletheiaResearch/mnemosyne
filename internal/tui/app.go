package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/AletheiaResearch/mnemosyne/internal/tui/common"
	"github.com/AletheiaResearch/mnemosyne/internal/version"
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

var screenTitles = map[screenKey]string{
	screenMenu:      "menu",
	screenSurvey:    "survey",
	screenConfigure: "configure",
	screenExtract:   "extract",
	screenAttest:    "attest",
	screenPublish:   "publish",
	screenRunlog:    "runlog",
	screenTransform: "transform",
	screenValidate:  "validate",
}

type sizeAware interface {
	SetSize(width, height int)
}

type footerAware interface {
	FooterHints() string
	FooterStatus() string
}

type model struct {
	screen     screenKey
	menu       *menuModel
	active     tea.Model
	configPath string
	width      int
	height     int
}

func NewApp(configPath string) tea.Model {
	menu := newMenuModel()
	return &model{
		screen:     screenMenu,
		menu:       menu,
		configPath: configPath,
	}
}

func (m *model) Init() tea.Cmd {
	return nil
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		innerW, innerH := common.InnerSize(m.width, m.height)
		innerW -= 2 // account for body horizontal padding
		if innerW < 0 {
			innerW = 0
		}
		m.menu.SetSize(innerW, innerH)
		if sz, ok := m.active.(sizeAware); ok {
			sz.SetSize(innerW, innerH)
		}
		return m, nil

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
				if sz, ok := m.active.(sizeAware); ok {
					innerW, innerH := common.InnerSize(m.width, m.height)
					innerW -= 2
					if innerW < 0 {
						innerW = 0
					}
					sz.SetSize(innerW, innerH)
				}
				return m, m.active.Init()
			}
		}
	}

	if m.screen == screenMenu {
		updated, cmd := m.menu.Update(msg)
		m.menu = updated.(*menuModel)
		return m, cmd
	}
	if m.active != nil {
		updated, cmd := m.active.Update(msg)
		m.active = updated
		return m, cmd
	}
	return m, nil
}

func (m *model) View() string {
	if m.width == 0 || m.height == 0 {
		return "initialising…"
	}

	var body string
	var active tea.Model
	if m.screen == screenMenu || m.active == nil {
		active = m.menu
		body = m.menu.View()
	} else {
		active = m.active
		body = m.active.View()
	}

	breadcrumb := "menu"
	if m.screen != screenMenu {
		if title, ok := screenTitles[m.screen]; ok {
			breadcrumb = "menu › " + title
		}
	}

	hints := "esc back · ↑↓ navigate · q quit"
	if m.screen == screenMenu {
		hints = "↑↓ navigate · enter select · q quit"
	}
	status := ""
	if fa, ok := active.(footerAware); ok {
		if h := fa.FooterHints(); h != "" {
			hints = h
		}
		status = fa.FooterStatus()
	}
	if status == "" {
		status = "v" + version.Version
	}

	header := common.Header("MNEMOSYNE", breadcrumb, "v"+version.Version, m.width)
	footer := common.Footer(hints, status, m.width)
	return common.Compose(header, body, footer, m.width, m.height)
}
