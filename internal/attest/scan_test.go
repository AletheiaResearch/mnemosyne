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

// TestScanFileCoversTrufflehogDetectors guards the safety scan against
// the regex-only regression: a leaked PostHog personal key (handled by
// a trufflehog scanner, not the legacy regex set) must show up as a
// token hit so LastAttest.BuiltInFindings stays truthful.
func TestScanFileCoversTrufflehogDetectors(t *testing.T) {
	t.Parallel()

	// Assembled at runtime so the bare literal never appears in source.
	phxKey := "phx" + "_abcdefghijklmnopqrstuvwxyz0123456789"

	path := filepath.Join(t.TempDir(), "export.jsonl")
	line := `{"record_id":"rec-1","text":"Authorization: Bearer ` + phxKey + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := ScanFile(path, "", true)
	if err != nil {
		t.Fatalf("ScanFile: %v", err)
	}
	if report.Findings.TokenCount == 0 {
		t.Fatalf("expected attestation scan to flag a phx_ key via trufflehog, got findings=%+v",
			report.Findings)
	}
}
