package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSmokeViewsDoNotPanic walks each screen once and exercises its View at a
// representative terminal size. It's a coarse guard against layout regressions
// — e.g. Width<=0 panics in lipgloss, nil pointers on unloaded command state.
func TestSmokeViewsDoNotPanic(t *testing.T) {
	configPath := t.TempDir() + "/settings.json"

	screens := []screenKey{
		screenSurvey,
		screenConfigure,
		screenExtract,
		screenAttest,
		screenPublish,
		screenRunlog,
		screenTransform,
		screenValidate,
	}
	for _, key := range screens {
		t.Run(string(key), func(t *testing.T) {
			m := newScreen(key, configPath)
			if m == nil {
				t.Fatalf("newScreen returned nil")
			}
			if sz, ok := m.(sizeAware); ok {
				sz.SetSize(100, 30)
			}
			_ = m.Init()
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
			if view := strings.TrimSpace(updated.View()); view == "" {
				t.Fatalf("empty view for %s", key)
			}
		})
	}
}

func TestSmokeAppInitialView(t *testing.T) {
	configPath := t.TempDir() + "/settings.json"
	app := NewApp(configPath)
	updated, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	view := updated.View()
	if !strings.Contains(view, "MNEMOSYNE") {
		t.Fatalf("expected brand in header, got: %s", view)
	}
	if !strings.Contains(view, "Survey") {
		t.Fatalf("expected first menu item in body, got: %s", view)
	}
}
