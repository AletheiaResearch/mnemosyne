package cli

import (
	"bytes"
	"errors"
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

var errFailWrite = errors.New("flush failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errFailWrite
}
