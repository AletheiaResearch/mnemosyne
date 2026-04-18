package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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

func TestTransformRecordsAcceptsLargeLines(t *testing.T) {
	t.Parallel()

	rec := schema.Record{
		RecordID: "rec-big",
		Origin:   "test",
		Grouping: "demo",
		Model:    "model",
		Turns: []schema.Turn{
			{Role: "user", Text: strings.Repeat("x", 9*1024*1024)},
		},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')

	var out bytes.Buffer
	if err := transformRecords(bytes.NewReader(data), &out, serialize.Canonical{}); err != nil {
		t.Fatalf("transformRecords rejected a 9 MiB record: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("expected non-empty transform output")
	}
}

var errFailWrite = errors.New("flush failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailWrite
}
