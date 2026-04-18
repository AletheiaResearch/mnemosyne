package tui

import (
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/tui/common"
)

type commandResultMsg struct {
	args   []string
	output string
	err    error
}

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldBool
)

type formField struct {
	label       string
	help        string
	kind        fieldKind
	placeholder string
	text        textinput.Model
	enabled     bool
}

func textField(label, value, placeholder string) formField {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 0
	ti.Width = 40
	ti.SetValue(value)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(common.ColorSubtle)
	ti.TextStyle = lipgloss.NewStyle().Foreground(common.ColorText)
	ti.CursorStyle = lipgloss.NewStyle().Foreground(common.ColorBrand)
	return formField{
		label:       label,
		kind:        fieldText,
		placeholder: placeholder,
		text:        ti,
	}
}

func boolField(label string, value bool) formField {
	return formField{
		label:   label,
		kind:    fieldBool,
		enabled: value,
	}
}

// Value returns the field's current value as a string ("true"/"false" for bool).
func (f *formField) Value() string {
	if f.kind == fieldBool {
		if f.enabled {
			return "true"
		}
		return "false"
	}
	return f.text.Value()
}

type formScreen struct {
	title       string
	intro       string
	configPath  string
	fields      []formField
	focus       int
	running     bool
	lastArgs    []string
	output      string
	err         string
	outputVP    viewport.Model
	outputReady bool
	width       int
	height      int
	buildArgs   func(*formScreen) []string
}

func (s *formScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.resizeFields()
	s.resizeOutput()
}

func newScreen(key screenKey, configPath string) tea.Model {
	cfg, _ := config.Load(configPath)
	switch key {
	case screenSurvey:
		return newSurveyScreen(configPath)
	case screenRunlog:
		return newRunlogScreen(configPath)
	case screenConfigure:
		tmpl := cfg.ChatTemplateValue()
		return &formScreen{
			title:      "Configure",
			intro:      "Edit persisted scope, dataset target, exclusions, redactions, handles, and transform chat-template defaults. Use comma-separated values for list fields.",
			configPath: configPath,
			fields: []formField{
				textField("Scope", cfg.OriginScope, "all"),
				textField("Destination Repo", cfg.DestinationRepo, "username/mnemosyne-traces"),
				textField("Exclude Groupings", strings.Join(cfg.ExcludedGroupings, ", "), "orchestrator:demo"),
				textField("Literal Redactions", strings.Join(cfg.CustomRedactions, ", "), "secret-token"),
				textField("Handles", strings.Join(cfg.CustomHandles, ", "), "octocat"),
				boolField("Confirm Scope", cfg.ScopeConfirmed),
				textField("Chat Template", tmpl.Name, "chatml, zephyr, vicuna"),
				textField("Chat Template File", tmpl.File, "path/to/custom.tmpl"),
				textField("BOS Token", tmpl.BOSToken, "<s>"),
				textField("EOS Token", tmpl.EOSToken, "</s>"),
				boolField("Add Generation Prompt", tmpl.AddGenerationPrompt),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"configure"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--scope", value)
				}
				if value := strings.TrimSpace(s.fields[1].Value()); value != "" {
					args = append(args, "--destination-repo", value)
				}
				args = appendListFlags(args, "--exclude", splitCSV(s.fields[2].Value()))
				args = appendListFlags(args, "--redact", splitCSV(s.fields[3].Value()))
				args = appendListFlags(args, "--handle", splitCSV(s.fields[4].Value()))
				if s.fields[5].enabled {
					args = append(args, "--confirm-scope")
				}
				args = append(args, "--chat-template-name", strings.TrimSpace(s.fields[6].Value()))
				args = append(args, "--chat-template-file", strings.TrimSpace(s.fields[7].Value()))
				args = append(args, "--chat-template-bos-token", s.fields[8].Value())
				args = append(args, "--chat-template-eos-token", s.fields[9].Value())
				if s.fields[10].enabled {
					args = append(args, "--chat-template-add-generation-prompt=true")
				} else {
					args = append(args, "--chat-template-add-generation-prompt=false")
				}
				return args
			},
		}
	case screenExtract:
		defaultOutput := ""
		if cfg.LastExtract != nil {
			defaultOutput = cfg.LastExtract.OutputPath
		}
		return &formScreen{
			title:      "Extract",
			intro:      "Run extraction with the configured redaction pipeline and write canonical JSONL. Duplicates prefer orchestrator-backed records when scope is all.",
			configPath: configPath,
			fields: []formField{
				textField("Scope", cfg.OriginScope, "all"),
				textField("Output Path", defaultOutput, "exports/mnemosyne-YYYYMMDDTHHMMSSZ.jsonl"),
				boolField("Include All Groupings", false),
				boolField("Omit Reasoning", false),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"extract"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--scope", value)
				}
				if value := strings.TrimSpace(s.fields[1].Value()); value != "" {
					args = append(args, "--output", value)
				}
				if s.fields[2].enabled {
					args = append(args, "--include-all")
				}
				if s.fields[3].enabled {
					args = append(args, "--no-reasoning")
				}
				return args
			},
		}
	case screenAttest:
		defaultFile := ""
		if cfg.LastExtract != nil {
			defaultFile = cfg.LastExtract.OutputPath
		}
		return &formScreen{
			title:      "Attest",
			intro:      "Record review statements against an extracted file. Free-form fields are single-line in the TUI and mapped directly to CLI flags.",
			configPath: configPath,
			fields: []formField{
				textField("Export File", defaultFile, "exports/mnemosyne.jsonl"),
				textField("Full Name", "", "Jane Doe"),
				boolField("Skip Name Scan", false),
				textField("Identity Scan", "", "Reviewed direct identifiers and residual mentions."),
				textField("Entity Scan", "", "Reviewed entities and sensitive references."),
				textField("Manual Review", "", "Reviewed sampled records after redaction."),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"attest"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--file", value)
				}
				if value := strings.TrimSpace(s.fields[1].Value()); value != "" {
					args = append(args, "--full-name", value)
				}
				if s.fields[2].enabled {
					args = append(args, "--skip-name-scan")
				}
				if value := strings.TrimSpace(s.fields[3].Value()); value != "" {
					args = append(args, "--identity-scan", value)
				}
				if value := strings.TrimSpace(s.fields[4].Value()); value != "" {
					args = append(args, "--entity-scan", value)
				}
				if value := strings.TrimSpace(s.fields[5].Value()); value != "" {
					args = append(args, "--manual-review", value)
				}
				return args
			},
		}
	case screenPublish:
		return &formScreen{
			title:      "Publish",
			intro:      "Re-run the existing publish command with persisted attestation state and the form inputs below.",
			configPath: configPath,
			fields: []formField{
				textField("Dataset Repo", cfg.DestinationRepo, "username/mnemosyne-traces"),
				textField("Publication Attestation", "", "I approve publication of this reviewed export."),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"publish"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--repo", value)
				}
				if value := strings.TrimSpace(s.fields[1].Value()); value != "" {
					args = append(args, "--publish-attestation", value)
				}
				return args
			},
		}
	case screenTransform:
		tmpl := cfg.ChatTemplateValue()
		return &formScreen{
			title:      "Transform",
			intro:      "Transform canonical JSONL into another serializer format. Chat-template fields default to persisted configure values and override --format when set.",
			configPath: configPath,
			fields: []formField{
				textField("Input Path", "", "exports/mnemosyne.jsonl"),
				textField("Output Path", "", "exports/mnemosyne-flat.jsonl"),
				textField("Format", "canonical", "canonical"),
				textField("Chat Template", tmpl.Name, "chatml, zephyr, vicuna"),
				textField("Chat Template File", tmpl.File, "path/to/custom.tmpl"),
				textField("BOS Token", tmpl.BOSToken, "<s>"),
				textField("EOS Token", tmpl.EOSToken, "</s>"),
				boolField("Add Generation Prompt", tmpl.AddGenerationPrompt),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"transform"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--input", value)
				}
				if value := strings.TrimSpace(s.fields[1].Value()); value != "" {
					args = append(args, "--output", value)
				}
				if value := strings.TrimSpace(s.fields[2].Value()); value != "" {
					args = append(args, "--format", value)
				}
				if value := strings.TrimSpace(s.fields[3].Value()); value != "" {
					args = append(args, "--template-name", value)
				}
				if value := strings.TrimSpace(s.fields[4].Value()); value != "" {
					args = append(args, "--template-file", value)
				}
				// Token and generation-prompt flags are emitted unconditionally
				// so clearing a field or toggling the checkbox off in the TUI
				// actually overrides a persisted Configure default for this
				// run. The CLI resolver only falls back to persisted config
				// when the flag wasn't Changed, so gating on non-empty here
				// would silently ignore the TUI's intent.
				args = append(args, "--bos-token", s.fields[5].Value())
				args = append(args, "--eos-token", s.fields[6].Value())
				if s.fields[7].enabled {
					args = append(args, "--add-generation-prompt=true")
				} else {
					args = append(args, "--add-generation-prompt=false")
				}
				return args
			},
		}
	case screenValidate:
		return &formScreen{
			title:      "Validate",
			intro:      "Validate canonical JSONL before publication or downstream transforms.",
			configPath: configPath,
			fields: []formField{
				textField("Input Path", "", "exports/mnemosyne.jsonl"),
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"validate"}
				if value := strings.TrimSpace(s.fields[0].Value()); value != "" {
					args = append(args, "--input", value)
				}
				return args
			},
		}
	default:
		return nil
	}
}

func (s *formScreen) Init() tea.Cmd {
	if len(s.fields) == 0 {
		return nil
	}
	s.focus = s.nextFocusableFrom(0, 1)
	if s.fields[s.focus].kind == fieldText {
		return s.fields[s.focus].text.Focus()
	}
	return nil
}

func (s *formScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandResultMsg:
		s.running = false
		s.output = msg.output
		s.err = errorText(msg.err)
		s.refreshOutput()
		return s, nil

	case tea.KeyMsg:
		if len(s.fields) == 0 {
			return s, nil
		}
		switch msg.String() {
		case "tab", "shift+tab", "up", "down":
			return s, s.moveFocus(msg.String())
		case "ctrl+s":
			if !s.running {
				s.running = true
				s.err = ""
				s.output = ""
				s.lastArgs = s.buildArgs(s)
				return s, runCommand(s.configPath, s.lastArgs...)
			}
			return s, nil
		case "enter":
			if s.fields[s.focus].kind == fieldBool {
				s.fields[s.focus].enabled = !s.fields[s.focus].enabled
				return s, nil
			}
			if !s.running {
				s.running = true
				s.err = ""
				s.output = ""
				s.lastArgs = s.buildArgs(s)
				return s, runCommand(s.configPath, s.lastArgs...)
			}
			return s, nil
		case " ":
			if s.fields[s.focus].kind == fieldBool {
				s.fields[s.focus].enabled = !s.fields[s.focus].enabled
				return s, nil
			}
		case "pgup", "pgdown":
			if s.outputReady {
				var cmd tea.Cmd
				s.outputVP, cmd = s.outputVP.Update(msg)
				return s, cmd
			}
			return s, nil
		}
		if s.fields[s.focus].kind == fieldText {
			var cmd tea.Cmd
			s.fields[s.focus].text, cmd = s.fields[s.focus].text.Update(msg)
			return s, cmd
		}
	}

	// Forward other messages (cursor blinks, resizes) to focused text input.
	if len(s.fields) > 0 && s.fields[s.focus].kind == fieldText {
		var cmd tea.Cmd
		s.fields[s.focus].text, cmd = s.fields[s.focus].text.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *formScreen) View() string {
	header := lipgloss.JoinVertical(lipgloss.Left,
		common.TitleStyle.Render(s.title),
		common.SubtitleStyle.Render(s.intro),
	)

	fieldsPanel := common.Panel("Inputs", s.renderFields(), s.contentWidth())

	var status string
	if len(s.lastArgs) > 0 {
		status = common.MutedStyle.Render("$ ") + common.ValueStyle.Render(commandPreview(s.lastArgs))
	}
	if s.running {
		status = common.AccentStyle.Render("Running…") + "  " + common.MutedStyle.Render(commandPreview(s.lastArgs))
	}

	var output string
	if s.err != "" {
		output = common.Panel("Error", common.ErrorStyle.Render(s.err), s.contentWidth())
	} else if strings.TrimSpace(s.output) != "" {
		s.layoutOutput(header, fieldsPanel, status)
		output = common.Panel("Output", s.outputVP.View(), s.contentWidth())
	}

	return joinBlocks(header, fieldsPanel, status, output)
}

func (s *formScreen) FooterHints() string {
	return common.HintLine("tab/↑↓ move", "space toggles", "ctrl+s run", "esc back")
}

func (s *formScreen) FooterStatus() string {
	if s.running {
		return "running…"
	}
	if s.err != "" {
		return "error"
	}
	if strings.TrimSpace(s.output) != "" {
		return "ok"
	}
	return ""
}

func (s *formScreen) contentWidth() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

func (s *formScreen) maxLabelWidth() int {
	max := 0
	for _, f := range s.fields {
		if w := lipgloss.Width(f.label); w > max {
			max = w
		}
	}
	return max
}

func (s *formScreen) renderFields() string {
	keyW := s.maxLabelWidth() + 2
	lines := []string{}
	for i, f := range s.fields {
		prefix := "  "
		labelStyle := common.LabelStyle
		if i == s.focus {
			prefix = common.FocusStyle.Render("▸ ")
			labelStyle = common.FocusStyle
		}
		var value string
		switch f.kind {
		case fieldText:
			value = f.text.View()
		case fieldBool:
			if f.enabled {
				value = common.SuccessStyle.Render("[✓] on")
			} else {
				value = common.MutedStyle.Render("[ ] off")
			}
		}
		label := labelStyle.Copy().Width(keyW).Render(f.label)
		lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, prefix, label, " ", value))
		if f.help != "" && i == s.focus {
			lines = append(lines, common.MutedStyle.Render("    "+f.help))
		}
	}
	return strings.Join(lines, "\n")
}

func (s *formScreen) resizeFields() {
	keyW := s.maxLabelWidth() + 2
	// contentWidth - panel border(2) - panel padding(2) - prefix(2) - label(keyW) - space(1)
	tiWidth := s.contentWidth() - keyW - 7
	if tiWidth < 12 {
		tiWidth = 12
	}
	for i := range s.fields {
		if s.fields[i].kind == fieldText {
			s.fields[i].text.Width = tiWidth
		}
	}
}

func (s *formScreen) resizeOutput() {
	if !s.outputReady {
		return
	}
	w := s.contentWidth() - 4
	if w < 20 {
		w = 20
	}
	s.outputVP.Width = w
}

func (s *formScreen) refreshOutput() {
	w := s.contentWidth() - 4
	if w < 20 {
		w = 20
	}
	if !s.outputReady {
		s.outputVP = viewport.New(w, 4)
		s.outputReady = true
	} else {
		s.outputVP.Width = w
	}
	s.outputVP.SetContent(s.output)
	s.outputVP.GotoTop()
}

func (s *formScreen) layoutOutput(header, fields, status string) {
	if !s.outputReady {
		return
	}
	above := joinBlocks(header, fields, status)
	remaining := s.height - lipgloss.Height(above) - 2 // spacer + panel title
	h := remaining - 2                                 // border top+bottom
	if h < 3 {
		h = 3
	}
	s.outputVP.Height = h
}

func (s *formScreen) moveFocus(key string) tea.Cmd {
	if len(s.fields) == 0 {
		return nil
	}
	if s.fields[s.focus].kind == fieldText {
		s.fields[s.focus].text.Blur()
	}
	dir := 1
	if key == "shift+tab" || key == "up" {
		dir = -1
	}
	s.focus = s.nextFocusableFrom((s.focus+dir+len(s.fields))%len(s.fields), dir)
	if s.fields[s.focus].kind == fieldText {
		return s.fields[s.focus].text.Focus()
	}
	return nil
}

func (s *formScreen) nextFocusableFrom(start, dir int) int {
	if len(s.fields) == 0 {
		return 0
	}
	// Every field is focusable; preserved as a hook if we add skip-logic later.
	_ = dir
	return start
}

func runCommand(configPath string, args ...string) tea.Cmd {
	return func() tea.Msg {
		exe, err := os.Executable()
		if err != nil {
			return commandResultMsg{args: args, err: err}
		}

		cmdArgs := append([]string{}, args...)
		if configPath != "" {
			cmdArgs = append([]string{"--config", configPath}, cmdArgs...)
		}
		cmd := exec.Command(exe, cmdArgs...)
		output, err := cmd.CombinedOutput()
		return commandResultMsg{
			args:   cmdArgs,
			output: strings.TrimSpace(string(output)),
			err:    err,
		}
	}
}

func appendListFlags(args []string, flag string, values []string) []string {
	for _, value := range values {
		args = append(args, flag, value)
	}
	return args
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func commandPreview(args []string) string {
	return "mnemosyne " + strings.Join(args, " ")
}
