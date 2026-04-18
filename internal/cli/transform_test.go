package cli

import (
	"bytes"
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
	})
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
	})
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
	})
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
	})
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
	})
	if err == nil {
		t.Fatal("expected mutually exclusive error")
	}
}

func TestResolveUnknownFormat(t *testing.T) {
	cmd := transformCmdWithFlags(map[string]string{"format": "does-not-exist"})
	_, err := resolveTransformSerializer(cmd, config.ChatTemplate{}, transformFlags{
		Format: "does-not-exist",
	})
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
