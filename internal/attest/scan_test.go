package attest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectFileChangeHandlesNanosecondAttestTimestamp(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "export.jsonl")
	if err := os.WriteFile(path, []byte("{\"record_id\":\"rec-1\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	modTime := time.Date(2026, 4, 17, 12, 0, 0, 123456789, time.UTC)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if DetectFileChange(path, info.Size(), modTime.Format(time.RFC3339Nano)) {
		t.Fatal("expected unchanged file when attestation timestamp includes nanoseconds")
	}
}
