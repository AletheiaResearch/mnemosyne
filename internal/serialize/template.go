package serialize

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/nikolalohinski/gonja/v2"
	gexec "github.com/nikolalohinski/gonja/v2/exec"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

//go:embed templates/*.tmpl templates/*.jinja
var builtinTemplateFS embed.FS

type engineKind int

const (
	engineGoText engineKind = iota
	engineJinja
)

// Template renders a canonical Record through a user-selected chat template.
//
// Two rendering engines are supported:
//   - Go text/template (files ending in .tmpl)
//   - Jinja2 via gonja (files ending in .jinja or .j2)
//
// The engine is picked from the file extension for builtins and user files;
// programmatic callers pick via newTemplate (Go) or newJinjaTemplate (Jinja).
type Template struct {
	name    string
	source  string
	options TemplateOptions
	engine  engineKind
	goTmpl  *template.Template
	jTmpl   *gexec.Template
}

// TemplateOptions carries render-time variables that are surfaced to templates.
type TemplateOptions struct {
	BOSToken            string
	EOSToken            string
	AddGenerationPrompt bool
	Tools               []ToolSchema
}

// ToolSchema describes one callable tool available to the assistant.
// The shape mirrors the OpenAI / Hermes / Llama-3.1 tool-use convention so
// templates ported from upstream render correctly without translation.
type ToolSchema struct {
	Type        string         `json:"type,omitempty"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Return      map[string]any `json:"return,omitempty"`
}

type templateContext struct {
	Messages            []templateMessage
	BOSToken            string
	EOSToken            string
	AddGenerationPrompt bool
	Tools               []ToolSchema
	Record              schema.Record
}

type templateMessage struct {
	Role        string
	Content     string
	Reasoning   string
	Timestamp   string
	Index       int
	IsLast      bool
	ToolCalls   []templateToolCall
	ToolCallID  string
	Name        string
	Attachments []schema.ContentBlock
}

type templateToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments map[string]any
}

// BuiltinTemplateNames returns the sorted list of template names embedded in
// the binary. Extensions are stripped; both .tmpl and .jinja files are listed.
func BuiltinTemplateNames() []string {
	entries, err := builtinTemplateFS.ReadDir("templates")
	if err != nil {
		return nil
	}
	seen := make(map[string]struct{})
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := templateNameFromFile(e.Name())
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func templateNameFromFile(filename string) string {
	for _, ext := range []string{".tmpl", ".jinja", ".j2"} {
		if strings.HasSuffix(filename, ext) {
			return strings.TrimSuffix(filename, ext)
		}
	}
	return ""
}

// NewBuiltinTemplate loads an embedded template by its short name. The
// extension of the bundled file picks the engine.
func NewBuiltinTemplate(name string, opts TemplateOptions) (*Template, error) {
	if name == "" {
		return nil, errors.New("builtin template name is required")
	}
	for _, ext := range []string{".tmpl", ".jinja", ".j2"} {
		data, err := builtinTemplateFS.ReadFile("templates/" + name + ext)
		if err != nil {
			continue
		}
		label := "template:" + name
		if ext == ".tmpl" {
			return newTemplate(label, string(data), opts)
		}
		return newJinjaTemplate(label, string(data), opts)
	}
	return nil, fmt.Errorf("unknown builtin template %q (available: %s)", name, strings.Join(BuiltinTemplateNames(), ", "))
}

// NewFileTemplate loads a template from disk, dispatching engine by extension.
func NewFileTemplate(path string, opts TemplateOptions) (*Template, error) {
	if path == "" {
		return nil, errors.New("template file path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", path, err)
	}
	label := "template:file(" + filepath.Base(path) + ")"
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jinja" || ext == ".j2" {
		return newJinjaTemplate(label, string(data), opts)
	}
	return newTemplate(label, string(data), opts)
}

func newTemplate(name, source string, opts TemplateOptions) (*Template, error) {
	tmpl, err := template.New(name).Funcs(templateFuncs()).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}
	return &Template{
		name:    name,
		source:  source,
		options: opts,
		engine:  engineGoText,
		goTmpl:  tmpl,
	}, nil
}

func newJinjaTemplate(name, source string, opts TemplateOptions) (*Template, error) {
	tmpl, err := gonja.FromString(source)
	if err != nil {
		return nil, fmt.Errorf("parse jinja %s: %w", name, err)
	}
	return &Template{
		name:    name,
		source:  source,
		options: opts,
		engine:  engineJinja,
		jTmpl:   tmpl,
	}, nil
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
		switch {
		case strings.HasPrefix(line, "{{/*"):
			line = strings.TrimPrefix(line, "{{/*")
			line = strings.TrimSuffix(line, "*/}}")
			line = strings.TrimSuffix(line, "*/ -}}")
			return strings.TrimSpace(line)
		case strings.HasPrefix(line, "{#"):
			line = strings.TrimPrefix(line, "{#")
			line = strings.TrimSuffix(line, "#}")
			line = strings.TrimSuffix(line, "-#}")
			line = strings.TrimPrefix(line, "-")
			return strings.TrimSpace(line)
		}
		break
	}
	if t.engine == engineJinja {
		return "Render records through a Jinja2 chat template."
	}
	return "Render records through a Go text/template chat template."
}

// Engine returns a short name of the engine used, for diagnostics.
func (t *Template) Engine() string {
	if t.engine == engineJinja {
		return "jinja"
	}
	return "go-text-template"
}

func (t *Template) Serialize(record schema.Record) (any, error) {
	messages := buildTemplateMessages(record)
	switch t.engine {
	case engineJinja:
		return t.serializeJinja(record, messages)
	default:
		return t.serializeGo(record, messages)
	}
}

func (t *Template) serializeGo(record schema.Record, messages []templateMessage) (any, error) {
	ctx := templateContext{
		Messages:            stripToolMessages(messages),
		BOSToken:            t.options.BOSToken,
		EOSToken:            t.options.EOSToken,
		AddGenerationPrompt: t.options.AddGenerationPrompt,
		Tools:               t.options.Tools,
		Record:              record,
	}
	var buf bytes.Buffer
	if err := t.goTmpl.Execute(&buf, ctx); err != nil {
		return nil, fmt.Errorf("render %s for record %s: %w", t.name, record.RecordID, err)
	}
	return wrapPayload(record, buf.String()), nil
}

func (t *Template) serializeJinja(record schema.Record, messages []templateMessage) (any, error) {
	ctx := gexec.NewContext(map[string]any{
		"messages":              messagesToJinja(messages),
		"bos_token":             t.options.BOSToken,
		"eos_token":             t.options.EOSToken,
		"add_generation_prompt": t.options.AddGenerationPrompt,
		"tools":                 toolsToJinja(t.options.Tools),
		"record":                record,
	})
	out, err := t.jTmpl.ExecuteToString(ctx)
	if err != nil {
		return nil, fmt.Errorf("render %s for record %s: %w", t.name, record.RecordID, err)
	}
	return wrapPayload(record, out), nil
}

func wrapPayload(record schema.Record, content string) any {
	return struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}{
		ID:      record.RecordID,
		Model:   record.Model,
		Content: content,
	}
}

func messagesToJinja(messages []templateMessage) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for i, m := range messages {
		entry := map[string]any{
			"role":    m.Role,
			"content": m.Content,
			"index":   m.Index,
			"is_last": m.IsLast,
		}
		if len(m.Attachments) > 0 {
			entry["attachments"] = attachmentsToJinja(m.Attachments)
		}
		// Precomputed adjacency so templates can avoid loop.previtem /
		// loop.nextitem, which gonja doesn't implement.
		if i > 0 {
			entry["prev_role"] = messages[i-1].Role
		}
		if i < len(messages)-1 {
			entry["next_role"] = messages[i+1].Role
		}
		if m.Reasoning != "" {
			entry["reasoning"] = m.Reasoning
		}
		if m.Timestamp != "" {
			entry["timestamp"] = m.Timestamp
		}
		if m.Name != "" {
			entry["name"] = m.Name
		}
		if m.ToolCallID != "" {
			entry["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				calls = append(calls, map[string]any{
					"id":   tc.ID,
					"type": tc.Type,
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}
			entry["tool_calls"] = calls
		}
		out = append(out, entry)
	}
	return out
}

// attachmentsToJinja projects per-message attachments into an OpenAI-style
// list of content blocks. Exposed as a sibling `attachments` field rather
// than folded into `content` so string-oriented templates (hermes, deepseek,
// chatml, zephyr, vicuna) keep working with a plain-string content; only
// multimodal-aware bundled templates iterate `attachments` to emit image
// tokens.
func attachmentsToJinja(atts []schema.ContentBlock) []map[string]any {
	out := make([]map[string]any, 0, len(atts))
	for _, att := range atts {
		block := map[string]any{"type": att.Type}
		if att.Text != "" {
			block["text"] = att.Text
		}
		if att.MediaType != "" {
			block["media_type"] = att.MediaType
		}
		if att.Data != "" {
			block["data"] = att.Data
		}
		if att.URL != "" {
			block["url"] = att.URL
		}
		if att.Name != "" {
			block["name"] = att.Name
		}
		out = append(out, block)
	}
	return out
}

func toolsToJinja(tools []ToolSchema) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		toolType := tool.Type
		if toolType == "" {
			toolType = "function"
		}
		fn := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"parameters":  tool.Parameters,
		}
		if tool.Return != nil {
			fn["return"] = tool.Return
		}
		out = append(out, map[string]any{
			"type":     toolType,
			"function": fn,
		})
	}
	return out
}

// stripToolMessages drops synthetic `tool`-role messages for Go text/template
// rendering and merges consecutive same-role runs. The bundled Go templates
// (chatml, zephyr, vicuna) are string-only and have no structured tool
// vocabulary; vicuna in particular enforces strict user/assistant alternation,
// so an assistant turn that carried only a tool call must not leave two
// adjacent assistant rows once the tool row is gone. Same-role runs are
// concatenated with a blank line so the final assistant reply still renders.
func stripToolMessages(msgs []templateMessage) []templateMessage {
	kept := make([]templateMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "tool" {
			continue
		}
		if n := len(kept); n > 0 && kept[n-1].Role == m.Role {
			prev := &kept[n-1]
			if m.Content != "" {
				if prev.Content != "" {
					prev.Content += "\n"
				}
				prev.Content += m.Content
			}
			continue
		}
		kept = append(kept, m)
	}
	for i := range kept {
		kept[i].Index = i
		kept[i].IsLast = false
	}
	if len(kept) > 0 {
		kept[len(kept)-1].IsLast = true
	}
	return kept
}

func buildTemplateMessages(record schema.Record) []templateMessage {
	msgs := make([]templateMessage, 0, len(record.Turns))
	for i, turn := range record.Turns {
		base := templateMessage{
			Role:        turn.Role,
			Content:     turn.Text,
			Reasoning:   turn.Reasoning,
			Timestamp:   turn.Timestamp,
			Index:       len(msgs),
			Attachments: turn.Attachments,
		}
		if len(turn.ToolCalls) > 0 && turn.Role == "assistant" {
			base.ToolCalls = projectToolCalls(turn.ToolCalls, i)
		}
		msgs = append(msgs, base)
		// Split tool outputs into synthetic tool-role messages so Jinja
		// templates that iterate messages see the standard shape.
		for j, call := range turn.ToolCalls {
			if call.Output == nil {
				continue
			}
			msgs = append(msgs, templateMessage{
				Role:       "tool",
				Content:    toolOutputText(call.Output),
				Name:       call.Tool,
				ToolCallID: toolCallID(i, j, call),
				Index:      len(msgs),
			})
		}
	}
	for i := range msgs {
		msgs[i].Index = i
	}
	if len(msgs) > 0 {
		msgs[len(msgs)-1].IsLast = true
	}
	return msgs
}

func projectToolCalls(calls []schema.ToolCall, turnIdx int) []templateToolCall {
	out := make([]templateToolCall, 0, len(calls))
	for j, call := range calls {
		out = append(out, templateToolCall{
			ID:        toolCallID(turnIdx, j, call),
			Type:      "function",
			Name:      call.Tool,
			Arguments: toolArguments(call.Input),
		})
	}
	return out
}

func toolCallID(turnIdx, callIdx int, call schema.ToolCall) string {
	return fmt.Sprintf("call_%d_%d_%s", turnIdx, callIdx, call.Tool)
}

func toolArguments(input any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	if m, ok := input.(map[string]any); ok {
		return m
	}
	return map[string]any{"value": input}
}

func toolOutputText(out *schema.ToolOutput) string {
	if out == nil {
		return ""
	}
	if out.Text != "" {
		return out.Text
	}
	if len(out.Content) > 0 {
		var b strings.Builder
		for _, block := range out.Content {
			if block.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(block.Text)
			}
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	// Several source adapters populate only Raw for non-string tool results
	// (opencode, gemini). Fall back to JSON so synthetic `tool` messages
	// retain their payload instead of rendering empty.
	if out.Raw != nil {
		if s, ok := out.Raw.(string); ok {
			return s
		}
		if data, err := json.Marshal(out.Raw); err == nil {
			return string(data)
		}
	}
	return ""
}
