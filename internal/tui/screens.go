package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

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
	value       string
	placeholder string
	help        string
	kind        fieldKind
}

type formScreen struct {
	title      string
	intro      string
	configPath string
	fields     []formField
	focus      int
	running    bool
	lastArgs   []string
	output     string
	err        string
	buildArgs  func(*formScreen) []string
	width      int
	height     int
}

func (s *formScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
}

func newScreen(key screenKey, configPath string) tea.Model {
	cfg, _ := config.Load(configPath)
	switch key {
	case screenSurvey:
		return newSurveyScreen(configPath)
	case screenConfigure:
		return &formScreen{
			title:      "Configure",
			intro:      "Edit persisted scope, dataset target, exclusions, redactions, and handles. Use comma-separated values for list fields.",
			configPath: configPath,
			fields: []formField{
				{label: "Scope", value: cfg.OriginScope, placeholder: "all"},
				{label: "Destination Repo", value: cfg.DestinationRepo, placeholder: "username/mnemosyne-traces"},
				{label: "Exclude Groupings", value: strings.Join(cfg.ExcludedGroupings, ", "), placeholder: "orchestrator:demo"},
				{label: "Literal Redactions", value: strings.Join(cfg.CustomRedactions, ", "), placeholder: "secret-token"},
				{label: "Handles", value: strings.Join(cfg.CustomHandles, ", "), placeholder: "octocat"},
				{label: "Confirm Scope", value: boolString(cfg.ScopeConfirmed), kind: fieldBool},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"configure"}
				if value := s.fields[0].value; strings.TrimSpace(value) != "" {
					args = append(args, "--scope", strings.TrimSpace(value))
				}
				if value := s.fields[1].value; strings.TrimSpace(value) != "" {
					args = append(args, "--destination-repo", strings.TrimSpace(value))
				}
				args = appendListFlags(args, "--exclude", splitCSV(s.fields[2].value))
				args = appendListFlags(args, "--redact", splitCSV(s.fields[3].value))
				args = appendListFlags(args, "--handle", splitCSV(s.fields[4].value))
				if isTrue(s.fields[5].value) {
					args = append(args, "--confirm-scope")
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
				{label: "Scope", value: cfg.OriginScope, placeholder: "all"},
				{label: "Output Path", value: defaultOutput, placeholder: "exports/mnemosyne-YYYYMMDDTHHMMSSZ.jsonl"},
				{label: "Include All Groupings", value: "false", kind: fieldBool},
				{label: "Omit Reasoning", value: "false", kind: fieldBool},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"extract"}
				if value := strings.TrimSpace(s.fields[0].value); value != "" {
					args = append(args, "--scope", value)
				}
				if value := strings.TrimSpace(s.fields[1].value); value != "" {
					args = append(args, "--output", value)
				}
				if isTrue(s.fields[2].value) {
					args = append(args, "--include-all")
				}
				if isTrue(s.fields[3].value) {
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
				{label: "Export File", value: defaultFile, placeholder: "exports/mnemosyne.jsonl"},
				{label: "Full Name", placeholder: "Jane Doe"},
				{label: "Skip Name Scan", value: "false", kind: fieldBool},
				{label: "Identity Scan", placeholder: "Reviewed direct identifiers and residual mentions."},
				{label: "Entity Scan", placeholder: "Reviewed entities and sensitive references."},
				{label: "Manual Review", placeholder: "Reviewed sampled records after redaction."},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"attest"}
				if value := strings.TrimSpace(s.fields[0].value); value != "" {
					args = append(args, "--file", value)
				}
				if value := strings.TrimSpace(s.fields[1].value); value != "" {
					args = append(args, "--full-name", value)
				}
				if isTrue(s.fields[2].value) {
					args = append(args, "--skip-name-scan")
				}
				if value := strings.TrimSpace(s.fields[3].value); value != "" {
					args = append(args, "--identity-scan", value)
				}
				if value := strings.TrimSpace(s.fields[4].value); value != "" {
					args = append(args, "--entity-scan", value)
				}
				if value := strings.TrimSpace(s.fields[5].value); value != "" {
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
				{label: "Dataset Repo", value: cfg.DestinationRepo, placeholder: "username/mnemosyne-traces"},
				{label: "Publication Attestation", placeholder: "I approve publication of this reviewed export."},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"publish"}
				if value := strings.TrimSpace(s.fields[0].value); value != "" {
					args = append(args, "--repo", value)
				}
				if value := strings.TrimSpace(s.fields[1].value); value != "" {
					args = append(args, "--publish-attestation", value)
				}
				return args
			},
		}
	case screenRunlog:
		return newRunlogScreen(configPath)
	case screenTransform:
		return &formScreen{
			title:      "Transform",
			intro:      "Transform canonical JSONL into another serializer format.",
			configPath: configPath,
			fields: []formField{
				{label: "Input Path", placeholder: "exports/mnemosyne.jsonl"},
				{label: "Output Path", placeholder: "exports/mnemosyne-flat.jsonl"},
				{label: "Format", value: "canonical", placeholder: "canonical"},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"transform"}
				if value := strings.TrimSpace(s.fields[0].value); value != "" {
					args = append(args, "--input", value)
				}
				if value := strings.TrimSpace(s.fields[1].value); value != "" {
					args = append(args, "--output", value)
				}
				if value := strings.TrimSpace(s.fields[2].value); value != "" {
					args = append(args, "--format", value)
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
				{label: "Input Path", placeholder: "exports/mnemosyne.jsonl"},
			},
			buildArgs: func(s *formScreen) []string {
				args := []string{"validate"}
				if value := strings.TrimSpace(s.fields[0].value); value != "" {
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
	return nil
}

func (s *formScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandResultMsg:
		s.running = false
		s.output = msg.output
		s.err = errorText(msg.err)
		return s, nil
	case tea.KeyMsg:
		if s.running || len(s.fields) == 0 {
			return s, nil
		}
		current := &s.fields[s.focus]
		switch msg.String() {
		case "tab", "down":
			s.focus = (s.focus + 1) % len(s.fields)
		case "shift+tab", "up":
			s.focus--
			if s.focus < 0 {
				s.focus = len(s.fields) - 1
			}
		case "ctrl+s":
			s.running = true
			s.err = ""
			s.lastArgs = s.buildArgs(s)
			return s, runCommand(s.configPath, s.lastArgs...)
		case "ctrl+u":
			if current.kind == fieldText {
				current.value = ""
			}
		case "backspace":
			if current.kind == fieldText {
				current.value = trimLastRune(current.value)
			}
		case " ":
			if current.kind == fieldBool {
				current.value = boolString(!isTrue(current.value))
				return s, nil
			}
			current.value += " "
		case "j":
			if current.kind == fieldBool {
				s.focus = (s.focus + 1) % len(s.fields)
				return s, nil
			}
			if msg.Type == tea.KeyRunes {
				current.value += string(msg.Runes)
			}
		case "k":
			if current.kind == fieldBool {
				s.focus--
				if s.focus < 0 {
					s.focus = len(s.fields) - 1
				}
				return s, nil
			}
			if msg.Type == tea.KeyRunes {
				current.value += string(msg.Runes)
			}
		default:
			if current.kind == fieldBool {
				if msg.String() == "enter" {
					current.value = boolString(!isTrue(current.value))
				}
				return s, nil
			}
			if msg.Type == tea.KeyRunes {
				current.value += string(msg.Runes)
			}
		}
	}
	return s, nil
}

func (s *formScreen) View() string {
	parts := []string{
		common.TitleStyle.Render(s.title),
		"",
		common.SubtitleStyle.Render(s.intro),
		"",
	}
	for idx, field := range s.fields {
		prefix := "  "
		label := common.LabelStyle.Render(field.label)
		if idx == s.focus {
			prefix = common.FocusStyle.Render("▸ ")
			label = common.FocusStyle.Render(field.label)
		}
		value := field.value
		if strings.TrimSpace(value) == "" && field.placeholder != "" {
			value = common.SubtleStyle.Render(field.placeholder)
		} else {
			value = common.ValueStyle.Render(value)
		}
		if field.kind == fieldBool {
			if isTrue(field.value) {
				value = common.SuccessStyle.Render("[✓] on")
			} else {
				value = common.MutedStyle.Render("[ ] off")
			}
		}
		parts = append(parts, fmt.Sprintf("%s%s %s", prefix, label, value))
		if field.help != "" {
			parts = append(parts, "  "+common.MutedStyle.Render(field.help))
		}
	}
	if len(s.lastArgs) > 0 {
		parts = append(parts, "", common.MutedStyle.Render("Last command: ")+commandPreview(s.lastArgs))
	}
	if s.running {
		parts = append(parts, "", common.AccentStyle.Render("Running…"))
	}
	if s.err != "" {
		parts = append(parts, "", common.ErrorStyle.Render("Error: ")+s.err)
	}
	if strings.TrimSpace(s.output) != "" {
		parts = append(parts, "", s.output)
	}
	return strings.Join(parts, "\n")
}

func (s *formScreen) FooterHints() string {
	return common.HintLine("tab/↑↓ move", "type to edit", "space toggles", "ctrl+s run", "esc back")
}

func (s *formScreen) FooterStatus() string {
	if s.running {
		return "running…"
	}
	if s.err != "" {
		return "error"
	}
	return ""
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

func trimLastRune(value string) string {
	runes := []rune(value)
	if len(runes) == 0 {
		return value
	}
	return string(runes[:len(runes)-1])
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func isTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
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
