package serialize

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func sampleRecord(turns []schema.Turn) schema.Record {
	return schema.Record{
		RecordID: "rec-1",
		Model:    "test-model",
		Turns:    turns,
	}
}

func renderBuiltin(t *testing.T, name string, opts TemplateOptions, record schema.Record) string {
	t.Helper()
	tmpl, err := NewBuiltinTemplate(name, opts)
	if err != nil {
		t.Fatalf("NewBuiltinTemplate(%q): %v", name, err)
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload, ok := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	if !ok {
		t.Fatalf("unexpected payload type %T", out)
	}
	if payload.ID != record.RecordID {
		t.Errorf("ID = %q, want %q", payload.ID, record.RecordID)
	}
	return payload.Content
}

func TestBuiltinTemplateNames(t *testing.T) {
	names := BuiltinTemplateNames()
	for _, want := range []string{"chatml", "zephyr", "vicuna"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("builtin %q missing from %v", want, names)
		}
	}
}

func TestChatMLTemplate(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi there"},
	})

	got := renderBuiltin(t, "chatml", TemplateOptions{}, record)
	want := "<|im_start|>user\nhello<|im_end|>\n<|im_start|>assistant\nhi there<|im_end|>\n"
	if got != want {
		t.Errorf("chatml output mismatch\ngot:  %q\nwant: %q", got, want)
	}

	got = renderBuiltin(t, "chatml", TemplateOptions{AddGenerationPrompt: true}, record)
	want = "<|im_start|>user\nhello<|im_end|>\n<|im_start|>assistant\nhi there<|im_end|>\n<|im_start|>assistant\n"
	if got != want {
		t.Errorf("chatml with add_generation_prompt mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestZephyrTemplate(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "system", Text: "be helpful"},
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "hey"},
	})

	got := renderBuiltin(t, "zephyr", TemplateOptions{EOSToken: "</s>"}, record)
	want := "<|system|>\nbe helpful</s>\n<|user|>\nhi</s>\n<|assistant|>\nhey</s>\n"
	if got != want {
		t.Errorf("zephyr output mismatch\ngot:  %q\nwant: %q", got, want)
	}

	got = renderBuiltin(t, "zephyr", TemplateOptions{EOSToken: "</s>", AddGenerationPrompt: true}, record)
	want = "<|system|>\nbe helpful</s>\n<|user|>\nhi</s>\n<|assistant|>\nhey</s>\n<|assistant|>\n"
	if got != want {
		t.Errorf("zephyr add_generation_prompt mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestVicunaTemplate(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "system", Text: "  system rules  "},
		{Role: "user", Text: "  hello  "},
		{Role: "assistant", Text: "  world  "},
	})

	got := renderBuiltin(t, "vicuna", TemplateOptions{BOSToken: "<s>", EOSToken: "</s>"}, record)
	want := "<s>system rules\n\nUSER: hello\nASSISTANT: world</s>\n"
	if got != want {
		t.Errorf("vicuna output mismatch\ngot:  %q\nwant: %q", got, want)
	}

	got = renderBuiltin(t, "vicuna", TemplateOptions{BOSToken: "<s>", EOSToken: "</s>", AddGenerationPrompt: true}, record)
	want = "<s>system rules\n\nUSER: hello\nASSISTANT: world</s>\nASSISTANT:"
	if got != want {
		t.Errorf("vicuna add_generation_prompt mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestVicunaMergesSameRoleRuns(t *testing.T) {
	// vicuna's template still enforces strict alternation internally, but
	// the Go-template projection merges consecutive same-role messages
	// before rendering so tool-call-then-final-reply traces (and other
	// pathological inputs) don't abort.
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "first"},
		{Role: "user", Text: "second"},
	})

	got := renderBuiltin(t, "vicuna", TemplateOptions{BOSToken: "<s>", EOSToken: "</s>"}, record)
	want := "<s>USER: first\nsecond\n"
	if got != want {
		t.Errorf("vicuna same-role merge mismatch\ngot:  %q\nwant: %q", got, want)
	}
}

func TestNewBuiltinTemplateUnknown(t *testing.T) {
	_, err := NewBuiltinTemplate("does-not-exist", TemplateOptions{})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "chatml") {
		t.Errorf("error should list available templates, got %q", err)
	}
}

func TestNewFileTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.tmpl")
	body := `{{range .Messages}}[{{.Role}}] {{.Content}}
{{end -}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	tmpl, err := NewFileTemplate(path, TemplateOptions{})
	if err != nil {
		t.Fatalf("NewFileTemplate: %v", err)
	}
	out, err := tmpl.Serialize(sampleRecord([]schema.Turn{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "yo"},
	}))
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	want := "[user] hi\n[assistant] yo\n"
	if payload.Content != want {
		t.Errorf("file template output mismatch\ngot:  %q\nwant: %q", payload.Content, want)
	}
	if !strings.HasPrefix(tmpl.Name(), "template:file(") {
		t.Errorf("file template name = %q, want template:file(...) prefix", tmpl.Name())
	}
}

func TestTemplateDescriptionFromComment(t *testing.T) {
	tmpl, err := NewBuiltinTemplate("chatml", TemplateOptions{})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	desc := tmpl.Description()
	if !strings.Contains(desc, "ChatML") {
		t.Errorf("expected description to mention ChatML, got %q", desc)
	}
}

func TestBuildTemplateMessagesFlags(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "first"},
		{Role: "assistant", Text: "second", Reasoning: "thinking..."},
		{Role: "user", Text: "third"},
	})

	msgs := buildTemplateMessages(record)
	if len(msgs) != len(record.Turns) {
		t.Fatalf("got %d messages, want %d", len(msgs), len(record.Turns))
	}
	for i, msg := range msgs {
		if msg.Index != i {
			t.Errorf("msg[%d].Index = %d, want %d", i, msg.Index, i)
		}
		wantLast := i == len(msgs)-1
		if msg.IsLast != wantLast {
			t.Errorf("msg[%d].IsLast = %v, want %v", i, msg.IsLast, wantLast)
		}
	}
	if msgs[1].Reasoning != "thinking..." {
		t.Errorf("Reasoning = %q, want propagation of turn.Reasoning", msgs[1].Reasoning)
	}
	if msgs[0].Reasoning != "" {
		t.Errorf("msg[0].Reasoning = %q, want empty", msgs[0].Reasoning)
	}
}

func TestBuildTemplateMessagesEmpty(t *testing.T) {
	msgs := buildTemplateMessages(sampleRecord(nil))
	if len(msgs) != 0 {
		t.Errorf("want empty slice, got %d entries", len(msgs))
	}
}

func TestTemplateFromInlineSource(t *testing.T) {
	source := `{{range .Messages}}{{.Index}}:{{.Role}}={{.Content}}{{if .IsLast}}!{{end}}
{{end -}}`
	tmpl, err := newTemplate("inline", source, TemplateOptions{})
	if err != nil {
		t.Fatalf("newTemplate: %v", err)
	}
	out, err := tmpl.Serialize(sampleRecord([]schema.Turn{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "yo"},
	}))
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	want := "0:user=hi\n1:assistant=yo!\n"
	if payload.Content != want {
		t.Errorf("inline template mismatch\ngot:  %q\nwant: %q", payload.Content, want)
	}
}

func TestTemplateRecordAccessible(t *testing.T) {
	source := `model={{.Record.Model}};id={{.Record.RecordID}};bos={{.BOSToken}};eos={{.EOSToken}}`
	tmpl, err := newTemplate("inline-record", source, TemplateOptions{
		BOSToken: "<s>",
		EOSToken: "</s>",
	})
	if err != nil {
		t.Fatalf("newTemplate: %v", err)
	}
	out, err := tmpl.Serialize(sampleRecord([]schema.Turn{{Role: "user", Text: "x"}}))
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	want := "model=test-model;id=rec-1;bos=<s>;eos=</s>"
	if payload.Content != want {
		t.Errorf("content mismatch\ngot:  %q\nwant: %q", payload.Content, want)
	}
}

func TestTemplateBuiltinRendersEmptyMessages(t *testing.T) {
	cases := []struct {
		name string
		opts TemplateOptions
	}{
		{"chatml", TemplateOptions{}},
		{"zephyr", TemplateOptions{EOSToken: "</s>"}},
		{"vicuna", TemplateOptions{BOSToken: "<s>", EOSToken: "</s>"}},
	}
	empty := sampleRecord(nil)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := NewBuiltinTemplate(tc.name, tc.opts)
			if err != nil {
				t.Fatalf("NewBuiltinTemplate(%q): %v", tc.name, err)
			}
			if _, err := tmpl.Serialize(empty); err != nil {
				t.Fatalf("Serialize empty record: %v", err)
			}
		})
	}
}

func TestNewTemplateParseError(t *testing.T) {
	_, err := newTemplate("broken", "{{ .Messages", TemplateOptions{})
	if err == nil {
		t.Fatal("expected parse error for unterminated action")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error should mention template name, got %q", err)
	}
}

func TestTemplateRaiseExceptionPropagates(t *testing.T) {
	source := `{{raiseException "boom"}}`
	tmpl, err := newTemplate("inline-raise", source, TemplateOptions{})
	if err != nil {
		t.Fatalf("newTemplate: %v", err)
	}
	_, err = tmpl.Serialize(sampleRecord(nil))
	if err == nil {
		t.Fatal("expected error from raiseException")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q missing raise message", err)
	}
}

func TestJinjaInlineTemplate(t *testing.T) {
	source := `{% for m in messages %}[{{m.role}}] {{m.content}}{% if not loop.last %}
{% endif %}{% endfor %}`
	tmpl, err := newJinjaTemplate("inline.jinja", source, TemplateOptions{})
	if err != nil {
		t.Fatalf("newJinjaTemplate: %v", err)
	}
	if tmpl.Engine() != "jinja" {
		t.Errorf("Engine() = %q, want jinja", tmpl.Engine())
	}
	out, err := tmpl.Serialize(sampleRecord([]schema.Turn{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "yo"},
	}))
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	want := "[user] hi\n[assistant] yo"
	if payload.Content != want {
		t.Errorf("jinja inline mismatch\ngot:  %q\nwant: %q", payload.Content, want)
	}
}

func TestJinjaSurfacesToolCallsAndToolRole(t *testing.T) {
	source := `{% for m in messages %}{{m.role}}{% if m.tool_calls is defined %}(calls={{m.tool_calls[0].function.name}}){% endif %}{% if m.content %}:{{m.content}}{% endif %}
{% endfor %}`
	tmpl, err := newJinjaTemplate("inline-tools.jinja", source, TemplateOptions{})
	if err != nil {
		t.Fatalf("newJinjaTemplate: %v", err)
	}
	record := schema.Record{
		RecordID: "rec-1",
		Turns: []schema.Turn{
			{Role: "user", Text: "go"},
			{Role: "assistant", ToolCalls: []schema.ToolCall{{
				Tool:   "search",
				Input:  map[string]any{"q": "paris"},
				Output: &schema.ToolOutput{Text: "ok"},
			}}},
			{Role: "assistant", Text: "done"},
		},
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	})
	if !strings.Contains(payload.Content, "assistant(calls=search)") {
		t.Errorf("missing tool_calls projection: %q", payload.Content)
	}
	if !strings.Contains(payload.Content, "tool") || !strings.Contains(payload.Content, ":ok") {
		t.Errorf("missing tool role message with output: %q", payload.Content)
	}
}

func TestToolOutputSplitsIntoToolMessage(t *testing.T) {
	record := schema.Record{
		Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{{
				Tool:   "search",
				Output: &schema.ToolOutput{Text: "answer"},
			}}},
		},
	}
	msgs := buildTemplateMessages(record)
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages (assistant + tool), got %d", len(msgs))
	}
	if msgs[0].Role != "assistant" || len(msgs[0].ToolCalls) != 1 {
		t.Errorf("assistant message missing tool_calls: %+v", msgs[0])
	}
	if msgs[1].Role != "tool" || msgs[1].Content != "answer" {
		t.Errorf("tool message mismatch: %+v", msgs[1])
	}
	if !msgs[1].IsLast {
		t.Errorf("synthetic tool message should be last")
	}
	if msgs[0].IsLast {
		t.Errorf("assistant should not be last when a tool follows")
	}
}

func TestHermesJinjaRendersWithToolCall(t *testing.T) {
	tmpl, err := NewBuiltinTemplate("hermes", TemplateOptions{
		AddGenerationPrompt: true,
		Tools: []ToolSchema{{
			Type:        "function",
			Name:        "get_weather",
			Description: "Return weather",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string", "description": "The city"},
				},
				"required": []string{"city"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	if tmpl.Engine() != "jinja" {
		t.Errorf("hermes should use jinja engine, got %s", tmpl.Engine())
	}
	record := schema.Record{
		RecordID: "rec-1",
		Turns: []schema.Turn{
			{Role: "user", Text: "weather in paris"},
			{Role: "assistant", ToolCalls: []schema.ToolCall{{
				Tool:   "get_weather",
				Input:  map[string]any{"city": "Paris"},
				Output: &schema.ToolOutput{Text: "18C, sunny"},
			}}},
			{Role: "assistant", Text: "It's 18C."},
		},
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	content := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}).Content
	for _, want := range []string{
		"<|im_start|>system",
		"<tools>",
		"get_weather",
		"<tool_call>",
		`"arguments": {"city":"Paris"}`,
		"<|im_start|>tool",
		"<tool_response>",
		"18C, sunny",
		"<|im_start|>assistant",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("hermes output missing %q\nfull output:\n%s", want, content)
		}
	}
}

func TestBuiltinTemplateNamesIncludesJinja(t *testing.T) {
	names := BuiltinTemplateNames()
	for _, want := range []string{"chatml", "hermes", "deepseekr1", "llama3.1_json"} {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q from builtins list: %v", want, names)
		}
	}
}

func TestToolOutputRawFallbackRendersAsJSON(t *testing.T) {
	record := schema.Record{
		RecordID: "rec-raw",
		Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{{
				Tool:   "compute",
				Output: &schema.ToolOutput{Raw: map[string]any{"value": 42}},
			}}},
		},
	}
	msgs := buildTemplateMessages(record)
	if len(msgs) != 2 || msgs[1].Role != "tool" {
		t.Fatalf("want assistant + tool messages, got %+v", msgs)
	}
	if msgs[1].Content != `{"value":42}` {
		t.Errorf("tool message content = %q, want JSON of Raw", msgs[1].Content)
	}

	// String Raw passes through unchanged.
	record.Turns[0].ToolCalls[0].Output = &schema.ToolOutput{Raw: "already text"}
	msgs = buildTemplateMessages(record)
	if msgs[1].Content != "already text" {
		t.Errorf("string Raw = %q, want passthrough", msgs[1].Content)
	}

	// Text wins over Raw when both are present.
	record.Turns[0].ToolCalls[0].Output = &schema.ToolOutput{Text: "explicit", Raw: map[string]any{"ignored": true}}
	msgs = buildTemplateMessages(record)
	if msgs[1].Content != "explicit" {
		t.Errorf("Text should win over Raw: got %q", msgs[1].Content)
	}
}

func TestJinjaAttachmentsAreExposedAsSiblingField(t *testing.T) {
	// `content` must remain a plain string so string-oriented templates
	// (hermes, deepseek*, chatml, zephyr, vicuna) render correctly even
	// when a turn carries attachments. Rich payloads are exposed via the
	// sibling `attachments` field, which multimodal templates iterate.
	source := `{% for m in messages -%}
{{ m.role }}|{{ m.content }}|
{%- if m.attachments is defined %}
{%- for a in m.attachments %}{{ a.type }}{% if a.text %}:{{ a.text }}{% endif %}{% if a.url %}@{{ a.url }}{% endif %};{% endfor %}
{%- endif %}
{% endfor %}`
	tmpl, err := newJinjaTemplate("inline-attach.jinja", source, TemplateOptions{})
	if err != nil {
		t.Fatalf("newJinjaTemplate: %v", err)
	}
	record := schema.Record{
		RecordID: "rec-attach",
		Turns: []schema.Turn{
			{Role: "user", Text: "describe this", Attachments: []schema.ContentBlock{
				{Type: "image", URL: "https://example.com/cat.png"},
			}},
			{Role: "assistant", Text: "a cat"},
		},
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	content := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}).Content
	if !strings.Contains(content, "user|describe this|") {
		t.Errorf("content should be the plain-string text, got: %q", content)
	}
	if !strings.Contains(content, "image@https://example.com/cat.png;") {
		t.Errorf("attachment should be exposed via the sibling field: %q", content)
	}
	if !strings.Contains(content, "assistant|a cat|") {
		t.Errorf("attachment-free turn should still render cleanly: %q", content)
	}
}

func TestStringOrientedJinjaTemplatesNeverRenderBlockArrays(t *testing.T) {
	// Regression guard: attachment-bearing turns must not leak a literal
	// content-block array (e.g. [{"type": "image", ...}]) into templates
	// that string-concat message.content — hermes and deepseekr1 both did
	// so before attachments moved to a sibling field.
	record := schema.Record{
		RecordID: "rec-attach",
		Turns: []schema.Turn{
			{Role: "user", Text: "what is this?", Attachments: []schema.ContentBlock{
				{Type: "image", URL: "https://example.com/cat.png"},
			}},
			{Role: "assistant", Text: "a cat"},
		},
	}
	for _, name := range []string{"hermes", "deepseekr1", "deepseekv3", "deepseekv31", "xlam_llama"} {
		tmpl, err := NewBuiltinTemplate(name, TemplateOptions{})
		if err != nil {
			t.Fatalf("NewBuiltinTemplate(%q): %v", name, err)
		}
		out, err := tmpl.Serialize(record)
		if err != nil {
			t.Fatalf("%s Serialize: %v", name, err)
		}
		content := out.(struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Content string `json:"content"`
		}).Content
		if strings.Contains(content, `"type": "image"`) || strings.Contains(content, `[{"type":`) {
			t.Errorf("%s leaked a content-block array into the prompt:\n%s", name, content)
		}
		if !strings.Contains(content, "what is this?") {
			t.Errorf("%s lost the user text: %s", name, content)
		}
	}
}

func TestLlama32JsonEmitsImageToken(t *testing.T) {
	tmpl, err := NewBuiltinTemplate("llama3.2_json", TemplateOptions{BOSToken: "<|begin_of_text|>"})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	record := schema.Record{
		RecordID: "rec-img",
		Turns: []schema.Turn{
			{Role: "user", Text: "what is this?", Attachments: []schema.ContentBlock{
				{Type: "image", URL: "https://example.com/x.png"},
			}},
		},
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	content := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}).Content
	if !strings.Contains(content, "<|image|>") {
		t.Errorf("llama3.2_json should emit <|image|> for image attachments:\n%s", content)
	}
	if !strings.Contains(content, "what is this?") {
		t.Errorf("llama3.2_json should still include the user text:\n%s", content)
	}
}

func TestHermesRendersWithInferredToolSchema(t *testing.T) {
	// InferTools does not populate per-property descriptions; hermes pipes
	// them through `trim`. Regression guard: an inferred schema (as returned
	// by InferTools, not the bundled catalog) must still render through
	// hermes without gonja erroring on the missing description.
	toolCall := schema.ToolCall{
		Tool:  "search",
		Input: map[string]any{"query": "paris"},
	}
	record := schema.Record{
		RecordID: "rec-1",
		Turns: []schema.Turn{
			{Role: "user", Text: "lookup paris"},
			{Role: "assistant", ToolCalls: []schema.ToolCall{toolCall}},
		},
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(record); err != nil {
		t.Fatal(err)
	}
	tools, err := InferTools(&buf)
	if err != nil {
		t.Fatalf("InferTools: %v", err)
	}
	tmpl, err := NewBuiltinTemplate("hermes", TemplateOptions{Tools: tools})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	if _, err := tmpl.Serialize(record); err != nil {
		t.Fatalf("hermes Serialize with inferred tools: %v", err)
	}
}

func TestGoTemplatesSkipSyntheticToolMessages(t *testing.T) {
	// vicuna enforces strict user/assistant alternation via raiseException.
	// Two cases matter:
	//   - synthetic tool-role messages (Output attached to a ToolCall) must
	//     be dropped before Go rendering
	//   - an assistant turn that emitted only a tool call, followed by the
	//     assistant's final reply, must collapse into a single assistant
	//     row — otherwise the alternation check fires on `assistant ->
	//     assistant`.
	cases := []struct {
		name  string
		turns []schema.Turn
	}{
		{
			name: "output_attached_to_toolcall",
			turns: []schema.Turn{
				{Role: "user", Text: "weather?"},
				{Role: "assistant", Text: "one moment", ToolCalls: []schema.ToolCall{{
					Tool:   "get_weather",
					Input:  map[string]any{"city": "Paris"},
					Output: &schema.ToolOutput{Text: "18C"},
				}}},
				{Role: "user", Text: "thanks"},
				{Role: "assistant", Text: "welcome"},
			},
		},
		{
			name: "pure_toolcall_then_assistant_reply",
			turns: []schema.Turn{
				{Role: "user", Text: "weather?"},
				{Role: "assistant", ToolCalls: []schema.ToolCall{{
					Tool:  "get_weather",
					Input: map[string]any{"city": "Paris"},
				}}},
				{Role: "assistant", Text: "Paris is 18C"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := schema.Record{RecordID: "rec-" + tc.name, Turns: tc.turns}
			tmpl, err := NewBuiltinTemplate("vicuna", TemplateOptions{BOSToken: "<s>", EOSToken: "</s>"})
			if err != nil {
				t.Fatalf("NewBuiltinTemplate: %v", err)
			}
			out, err := tmpl.Serialize(record)
			if err != nil {
				t.Fatalf("vicuna rejected tool-using trace: %v", err)
			}
			content := out.(struct {
				ID      string `json:"id"`
				Model   string `json:"model"`
				Content string `json:"content"`
			}).Content
			if strings.Contains(content, "TOOL:") || strings.Contains(content, "[tool]") {
				t.Errorf("synthetic tool role leaked into go-text render: %q", content)
			}
		})
	}
}

func TestLlama4JsonEmitsImageToken(t *testing.T) {
	// Upstream bug: render_message called is_array_of_type_objects(data) on
	// an undefined `data`, so multimodal turns fell through to tojson and
	// the <|image|> marker never emitted.
	tmpl, err := NewBuiltinTemplate("llama4_json", TemplateOptions{BOSToken: "<|begin_of_text|>"})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	record := schema.Record{
		RecordID: "rec-img",
		Turns: []schema.Turn{
			{Role: "user", Text: "what is this?", Attachments: []schema.ContentBlock{
				{Type: "image", URL: "https://example.com/x.png"},
			}},
		},
	}
	out, err := tmpl.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	content := out.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}).Content
	if !strings.Contains(content, "<|image|>") {
		t.Errorf("llama4_json should emit <|image|> for image attachments:\n%s", content)
	}
	if !strings.Contains(content, "what is this?") {
		t.Errorf("llama4_json should still include user text:\n%s", content)
	}
}

func TestAllBuiltinTemplatesLoadAndRender(t *testing.T) {
	// Every name returned by BuiltinTemplateNames must parse and render a
	// minimal record. Guards against gonja-incompat syntax (e.g. {% break %},
	// block-set) silently slipping into the bundled catalog.
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "yo"},
	})
	for _, name := range BuiltinTemplateNames() {
		t.Run(name, func(t *testing.T) {
			tmpl, err := NewBuiltinTemplate(name, TemplateOptions{
				BOSToken: "<s>",
				EOSToken: "</s>",
			})
			if err != nil {
				t.Fatalf("NewBuiltinTemplate(%q): %v", name, err)
			}
			if _, err := tmpl.Serialize(record); err != nil {
				t.Fatalf("Serialize(%q): %v", name, err)
			}
		})
	}
}

func TestNewFileTemplateDispatchesByExtension(t *testing.T) {
	dir := t.TempDir()
	jpath := filepath.Join(dir, "x.jinja")
	if err := os.WriteFile(jpath, []byte(`{% for m in messages %}{{m.role}}{% endfor %}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	tmpl, err := NewFileTemplate(jpath, TemplateOptions{})
	if err != nil {
		t.Fatalf("NewFileTemplate: %v", err)
	}
	if tmpl.Engine() != "jinja" {
		t.Errorf(".jinja should dispatch to jinja engine, got %s", tmpl.Engine())
	}
}
