package serialize

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

//go:embed templates/*.tmpl
var builtinTemplateFS embed.FS

// Template renders a canonical Record through a user-selected chat template.
//
// It exposes an HF-tokenizer-style context (Messages, BOSToken, EOSToken,
// AddGenerationPrompt) so templates ported from HuggingFace / NousResearch
// chat templates render naturally.
type Template struct {
	name    string
	source  string
	tmpl    *template.Template
	options TemplateOptions
}

// TemplateOptions carries render-time variables that are surfaced to templates.
type TemplateOptions struct {
	BOSToken            string
	EOSToken            string
	AddGenerationPrompt bool
}

type templateContext struct {
	Messages            []templateMessage
	BOSToken            string
	EOSToken            string
	AddGenerationPrompt bool
	Record              schema.Record
}

type templateMessage struct {
	Role      string
	Content   string
	Reasoning string
	Timestamp string
	Index     int
	IsLast    bool
}

// BuiltinTemplateNames returns the sorted list of template names embedded in
// the binary (e.g. "chatml", "zephyr", "vicuna").
func BuiltinTemplateNames() []string {
	entries, err := builtinTemplateFS.ReadDir("templates")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".tmpl")
		if name == e.Name() {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NewBuiltinTemplate loads an embedded template by its short name.
func NewBuiltinTemplate(name string, opts TemplateOptions) (*Template, error) {
	if name == "" {
		return nil, errors.New("builtin template name is required")
	}
	data, err := builtinTemplateFS.ReadFile("templates/" + name + ".tmpl")
	if err != nil {
		return nil, fmt.Errorf("unknown builtin template %q (available: %s)", name, strings.Join(BuiltinTemplateNames(), ", "))
	}
	return newTemplate("template:"+name, string(data), opts)
}

// NewFileTemplate loads a template from disk.
func NewFileTemplate(path string, opts TemplateOptions) (*Template, error) {
	if path == "" {
		return nil, errors.New("template file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", path, err)
	}
	label := "template:file(" + filepath.Base(path) + ")"
	return newTemplate(label, string(data), opts)
}

func newTemplate(name, source string, opts TemplateOptions) (*Template, error) {
	tmpl, err := template.New(name).Funcs(templateFuncs()).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return &Template{name: name, source: source, tmpl: tmpl, options: opts}, nil
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"trim": strings.TrimSpace,
		"mod": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a % b
		},
		"raiseException": func(msg string) (string, error) {
			return "", errors.New(msg)
		},
	}
}

func (t *Template) Name() string {
	return t.name
}

func (t *Template) Description() string {
	for _, line := range strings.Split(t.source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{{/*") {
			line = strings.TrimPrefix(line, "{{/*")
			line = strings.TrimSuffix(line, "*/}}")
			line = strings.TrimSuffix(line, "*/ -}}")
			return strings.TrimSpace(line)
		}
		break
	}
	return "Render records through a Go text/template chat template."
}

func (t *Template) Serialize(record schema.Record) (any, error) {
	ctx := templateContext{
		Messages:            buildTemplateMessages(record),
		BOSToken:            t.options.BOSToken,
		EOSToken:            t.options.EOSToken,
		AddGenerationPrompt: t.options.AddGenerationPrompt,
		Record:              record,
	}
	var buf bytes.Buffer
	if err := t.tmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("render %s for record %s: %w", t.name, record.RecordID, err)
	}
	return struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}{
		ID:      record.RecordID,
		Model:   record.Model,
		Content: buf.String(),
	}, nil
}

func buildTemplateMessages(record schema.Record) []templateMessage {
	msgs := make([]templateMessage, 0, len(record.Turns))
	for i, turn := range record.Turns {
		msgs = append(msgs, templateMessage{
			Role:      turn.Role,
			Content:   turn.Text,
			Reasoning: turn.Reasoning,
			Timestamp: turn.Timestamp,
			Index:     i,
		})
	}
	if len(msgs) > 0 {
		msgs[len(msgs)-1].IsLast = true
	}
	return msgs
}
