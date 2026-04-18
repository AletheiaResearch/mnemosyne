package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestSelectSerializerBuiltinTemplate(t *testing.T) {
	s, err := selectSerializer("", "chatml", "", serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("selectSerializer: %v", err)
	}
	if !strings.HasPrefix(s.Name(), "template:chatml") {
		t.Errorf("want template:chatml name, got %q", s.Name())
	}
}

func TestSelectSerializerFileTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.tmpl")
	if err := os.WriteFile(path, []byte("{{range .Messages}}{{.Role}}\n{{end}}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := selectSerializer("", "", path, serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("selectSerializer: %v", err)
	}
	if !strings.Contains(s.Name(), "template:file(") {
		t.Errorf("want file template name, got %q", s.Name())
	}
}

func TestSelectSerializerFormatFallback(t *testing.T) {
	s, err := selectSerializer("canonical", "", "", serialize.TemplateOptions{})
	if err != nil {
		t.Fatalf("selectSerializer: %v", err)
	}
	if s.Name() != "canonical" {
		t.Errorf("want canonical, got %q", s.Name())
	}
}

func TestSelectSerializerMutuallyExclusive(t *testing.T) {
	_, err := selectSerializer("", "chatml", "/tmp/x.tmpl", serialize.TemplateOptions{})
	if err == nil {
		t.Fatal("expected mutually-exclusive error")
	}
}

func TestSelectSerializerUnknownFormat(t *testing.T) {
	_, err := selectSerializer("does-not-exist", "", "", serialize.TemplateOptions{})
	if err == nil {
		t.Fatal("expected unknown serializer error")
	}
}

var errFailWrite = errors.New("flush failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailWrite
}
