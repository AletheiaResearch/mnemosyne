package serialize

import (
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

func TestVicunaAlternationFailure(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "first"},
		{Role: "user", Text: "second"},
	})

	tmpl, err := NewBuiltinTemplate("vicuna", TemplateOptions{})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}
	if _, err := tmpl.Serialize(record); err == nil {
		t.Fatal("expected alternation error, got nil")
	} else if !strings.Contains(err.Error(), "alternate") {
		t.Errorf("error %q does not mention alternation", err)
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
