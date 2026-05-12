package tui

import (
	"errors"
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

// TestFormScreenErrorPanelPrefersOutput ensures the Error panel surfaces the
// captured stdout/stderr of the failed subcommand, falling back to the bare
// exit-status string only when the child produced no output. Without this,
// validator messages from commands like `attest` would be replaced by the
// useless `exit status 1`.
func TestFormScreenErrorPanelPrefersOutput(t *testing.T) {
	configPath := t.TempDir() + "/settings.json"

	newForm := func(t *testing.T) *formScreen {
		t.Helper()
		base := newScreen(screenAttest, configPath)
		form, ok := base.(*formScreen)
		if !ok {
			t.Fatalf("expected *formScreen, got %T", base)
		}
		form.SetSize(120, 30)
		return form
	}

	deliver := func(form *formScreen, output string, err error) string {
		updated, _ := form.Update(commandResultMsg{output: output, err: err})
		return updated.View()
	}

	t.Run("captured output wins over exit status", func(t *testing.T) {
		view := deliver(newForm(t), "identity attestation must include every word", errors.New("exit status 1"))
		if !strings.Contains(view, "identity attestation must include every word") {
			t.Fatalf("expected captured output in view, got: %s", view)
		}
		if strings.Contains(view, "exit status 1") {
			t.Fatalf("exit status string should be hidden when output is present, got: %s", view)
		}
	})

	t.Run("falls back to exit status when output empty", func(t *testing.T) {
		view := deliver(newForm(t), "   ", errors.New("exit status 1"))
		if !strings.Contains(view, "exit status 1") {
			t.Fatalf("expected fallback to exit-status string, got: %s", view)
		}
	})
}
