package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/serialize"
)

func TestTransformRecordsReturnsFlushError(t *testing.T) {
	t.Parallel()

	input := bytes.NewBufferString("{\"record_id\":\"rec-1\",\"origin\":\"test\",\"grouping\":\"demo\",\"model\":\"model\",\"turns\":[{\"role\":\"user\",\"text\":\"hello\"}]}\n")
	err := transformRecords(input, failingWriter{}, serialize.Canonical{})
	if !errors.Is(err, errFailWrite) {
		t.Fatalf("expected flush error, got %v", err)
	}
}

// transformCmdWithFlags mirrors newTransformCommand's flag registration so tests
// can control cobra's flag.Changed state precisely.
func transformCmdWithFlags(set map[string]string) *cobra.Command {
	cmd := &cobra.Command{Use: "transform"}
	var (
		input, output, format                              string
		templateName, templateFile, bosToken, eosToken     string
		addGenerationPrompt                                bool
	)
	cmd.Flags().StringVar(&input, "input", "", "")
	cmd.Flags().StringVar(&output, "output", "", "")
	cmd.Flags().StringVar(&format, "format", "canonical", "")
	cmd.Flags().StringVar(&templateName, "template-name", "", "")
	cmd.Flags().StringVar(&templateFile, "template-file", "", "")
	cmd.Flags().StringVar(&bosToken, "bos-token", "", "")
	cmd.Flags().StringVar(&eosToken, "eos-token", "", "")
	cmd.Flags().BoolVar(&addGenerationPrompt, "add-generation-prompt", false, "")
	for name, value := range set {
		if err := cmd.Flags().Set(name, value); err != nil {
			panic(err)
		}
	}
	return cmd
}

func TestResolveExplicitTemplateNameWins(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{"template-name": "chatml"})
	s, err := resolveTransformSerializer(cmd, config.ChatTemplate{File: "/persisted.tmpl"}, transformFlags{
		Format:       "canonical",
		TemplateName: "chatml",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Name(), "template:chatml") {
		t.Errorf("want template:chatml, got %q", s.Name())
	}
}

func TestResolveExplicitFormatBeatsPersistedTemplate(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{"format": "flat"})
	s, err := resolveTransformSerializer(cmd, config.ChatTemplate{Name: "chatml"}, transformFlags{
		Format: "flat",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != "flat" {
		t.Errorf("want flat, got %q", s.Name())
	}
}

func TestResolvePersistedNameUsedWhenNoFlagsSet(t *testing.T) {
	cmd := transformCmdWithFlags(nil)
	s, err := resolveTransformSerializer(cmd, config.ChatTemplate{Name: "zephyr", EOSToken: "</s>"}, transformFlags{
		Format: "canonical",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.Name(), "template:zephyr") {
		t.Errorf("want template:zephyr, got %q", s.Name())
	}
}

func TestResolvePersistedFileUsedWhenNoFlagsSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.tmpl")
	if err := os.WriteFile(path, []byte("{{range .Messages}}{{.Role}}\n{{end}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := transformCmdWithFlags(nil)
	s, err := resolveTransformSerializer(cmd, config.ChatTemplate{File: path}, transformFlags{
		Format: "canonical",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Name(), "template:file(") {
		t.Errorf("want template:file(...), got %q", s.Name())
	}
}

func TestResolvePersistedTokensFillInForExplicitTemplate(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{"template-name": "zephyr"})
	s, err := resolveTransformSerializer(cmd,
		config.ChatTemplate{EOSToken: "</s>", AddGenerationPrompt: true},
		transformFlags{TemplateName: "zephyr", Format: "canonical"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	tmpl, ok := s.(*serialize.Template)
	if !ok {
		t.Fatalf("expected *serialize.Template, got %T", s)
	}
	payload, err := tmpl.Serialize(sampleTransformRecord())
	if err != nil {
		t.Fatal(err)
	}
	body := payload.(struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}).Content
	if !strings.Contains(body, "</s>") {
		t.Errorf("expected persisted EOS in content: %q", body)
	}
	if !strings.HasSuffix(body, "<|assistant|>\n") {
		t.Errorf("expected persisted add_generation_prompt to trigger trailing assistant tag: %q", body)
	}
}

func TestResolveMutuallyExclusiveExplicitFlags(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{
		"template-name": "chatml",
		"template-file": "/tmp/x.tmpl",
	})
	_, err := resolveTransformSerializer(cmd, config.ChatTemplate{}, transformFlags{
		TemplateName: "chatml",
		TemplateFile: "/tmp/x.tmpl",
	}, nil)
	if err == nil {
		t.Fatal("expected mutually exclusive error")
	}
}

func TestResolveUnknownFormat(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{"format": "does-not-exist"})
	_, err := resolveTransformSerializer(cmd, config.ChatTemplate{}, transformFlags{
		Format: "does-not-exist",
	}, nil)
	if err == nil {
		t.Fatal("expected unknown serializer error")
	}
}

func sampleTransformRecord() schema.Record {
	return schema.Record{
		RecordID: "rec-1",
		Model:    "test-model",
		Turns: []schema.Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "hi"},
		},
	}
}

var errFailWrite = errors.New("flush failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailWrite
}

type chatMLPayload struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content string `json:"content"`
}

func encodeRecordsJSONL(t *testing.T, records ...schema.Record) string {
	t.Helper()
	var buf bytes.Buffer
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal record: %v", err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	return buf.String()
}

func TestTransformRecordsChatMLEndToEnd(t *testing.T) {
	t.Parallel()

	record := schema.Record{
		RecordID: "rec-42",
		Model:    "mock-model",
		Turns: []schema.Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "world"},
		},
	}
	input := encodeRecordsJSONL(t, record)

	tmpl, err := serialize.NewBuiltinTemplate("chatml", serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}

	var out bytes.Buffer
	if err := transformRecords(bytes.NewBufferString(input), &out, tmpl); err != nil {
		t.Fatalf("transformRecords: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d: %q", len(lines), out.String())
	}

	var payload chatMLPayload
	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.ID != record.RecordID || payload.Model != record.Model {
		t.Errorf("payload header mismatch: %+v", payload)
	}
	if !strings.Contains(payload.Content, "<|im_start|>user\nhello<|im_end|>") {
		t.Errorf("chatml content missing user block: %q", payload.Content)
	}
	if !strings.Contains(payload.Content, "<|im_start|>assistant\nworld<|im_end|>") {
		t.Errorf("chatml content missing assistant block: %q", payload.Content)
	}
}

func TestTransformRecordsMultipleRecords(t *testing.T) {
	t.Parallel()

	records := []schema.Record{
		{RecordID: "rec-1", Model: "m", Turns: []schema.Turn{{Role: "user", Text: "a"}}},
		{RecordID: "rec-2", Model: "m", Turns: []schema.Turn{{Role: "user", Text: "b"}}},
		{RecordID: "rec-3", Model: "m", Turns: []schema.Turn{{Role: "user", Text: "c"}}},
	}
	input := encodeRecordsJSONL(t, records...)

	tmpl, err := serialize.NewBuiltinTemplate("chatml", serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}

	var out bytes.Buffer
	if err := transformRecords(bytes.NewBufferString(input), &out, tmpl); err != nil {
		t.Fatalf("transformRecords: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != len(records) {
		t.Fatalf("want %d lines, got %d", len(records), len(lines))
	}
	for i, line := range lines {
		var payload chatMLPayload
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatalf("line %d unmarshal: %v", i, err)
		}
		if payload.ID != records[i].RecordID {
			t.Errorf("line %d ID = %q, want %q", i, payload.ID, records[i].RecordID)
		}
	}
}

func TestTransformRecordsInvalidJSON(t *testing.T) {
	t.Parallel()

	input := bytes.NewBufferString("{not valid json}\n")
	var out bytes.Buffer
	err := transformRecords(input, &out, serialize.Canonical{})
	if err == nil {
		t.Fatal("expected json parse error")
	}
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Errorf("want *json.SyntaxError, got %T: %v", err, err)
	}
}

func TestInferToolsAndOriginsFromTempFile(t *testing.T) {
	t.Parallel()

	records := []schema.Record{
		{RecordID: "r-1", Origin: "claudecode", Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "Read", Input: map[string]any{"file_path": "/tmp/x"}},
			}},
		}},
		{RecordID: "r-2", Origin: "claudecode", Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "Bash", Input: map[string]any{"command": "ls"}},
			}},
		}},
	}

	path := filepath.Join(t.TempDir(), "in.jsonl")
	var buf bytes.Buffer
	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(data)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	tools, origins, err := inferToolsAndOrigins(path)
	if err != nil {
		t.Fatalf("inferToolsAndOrigins: %v", err)
	}
	if len(origins) != 1 || origins[0] != "claudecode" {
		t.Errorf("origins = %v, want [claudecode]", origins)
	}
	gotNames := []string{}
	for _, tool := range tools {
		gotNames = append(gotNames, tool.Name)
	}
	if len(gotNames) != 2 || gotNames[0] != "Bash" || gotNames[1] != "Read" {
		t.Errorf("tool names = %v, want sorted [Bash Read]", gotNames)
	}
}

func TestResolveTransformToolsReadsToolsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	toolsPath := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"custom","description":"from file"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	// input is irrelevant when tools-file is given, but the function opens it
	// lazily (only if tools-file is empty) so we can pass a missing path.
	tools, err := resolveTransformTools("/does-not-exist", toolsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "custom" {
		t.Errorf("got %+v, want single 'custom' tool", tools)
	}
}

func TestTransformRecordsAcceptsLargeLines(t *testing.T) {
	t.Parallel()

	// Build a record whose JSONL encoding exceeds the old 8 MiB cap but stays
	// under the 64 MiB limit the extract pipeline honours.
	big := strings.Repeat("x", 9*1024*1024)
	record := schema.Record{
		RecordID: "rec-big",
		Model:    "m",
		Turns:    []schema.Turn{{Role: "assistant", Text: big}},
	}
	input := encodeRecordsJSONL(t, record)

	var out bytes.Buffer
	if err := transformRecords(bytes.NewBufferString(input), &out, serialize.Canonical{}); err != nil {
		t.Fatalf("transformRecords rejected a 9 MiB line: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected transformed output")
	}
}

func TestInferToolsAndOriginsAcceptsLargeLines(t *testing.T) {
	t.Parallel()

	big := strings.Repeat("x", 9*1024*1024)
	record := schema.Record{
		RecordID: "rec-big",
		Origin:   "claudecode",
		Turns: []schema.Turn{
			{Role: "user", Text: big},
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "Read", Input: map[string]any{"file_path": "/tmp/x"}},
			}},
		},
	}
	path := filepath.Join(t.TempDir(), "big.jsonl")
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	tools, origins, err := inferToolsAndOrigins(path)
	if err != nil {
		t.Fatalf("inferToolsAndOrigins rejected 9 MiB line: %v", err)
	}
	if len(origins) != 1 || origins[0] != "claudecode" {
		t.Errorf("origins = %v, want [claudecode]", origins)
	}
	if len(tools) != 1 || tools[0].Name != "Read" {
		t.Errorf("tools = %+v, want single Read", tools)
	}
}

func TestTransformRecordsVicunaMergesSameRoleRuns(t *testing.T) {
	t.Parallel()

	// Adjacent same-role turns used to trip vicuna's alternation check
	// and abort the pipeline; the Go-template projection now merges them
	// so pathological inputs and tool-call-then-final-reply traces both
	// render cleanly.
	record := schema.Record{
		RecordID: "rec-merge",
		Turns: []schema.Turn{
			{Role: "user", Text: "one"},
			{Role: "user", Text: "two"},
		},
	}
	input := encodeRecordsJSONL(t, record)

	tmpl, err := serialize.NewBuiltinTemplate("vicuna", serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("NewBuiltinTemplate: %v", err)
	}

	var out bytes.Buffer
	if err := transformRecords(bytes.NewBufferString(input), &out, tmpl); err != nil {
		t.Fatalf("transformRecords: %v", err)
	}
	if !strings.Contains(out.String(), "USER: one\\ntwo") {
		t.Errorf("expected merged user content in output, got %s", out.String())
	}
}
