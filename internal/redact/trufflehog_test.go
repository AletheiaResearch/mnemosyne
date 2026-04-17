package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrufflehogScanUsesStdinSubcommand(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "trufflehog")
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\"\n/bin/cat\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	out, err := TrufflehogScan("secret payload")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "stdin --json --only-verified=false") {
		t.Fatalf("expected stdin subcommand, got %q", out)
	}
	if !strings.Contains(out, "secret payload") {
		t.Fatalf("expected stdin payload to be forwarded, got %q", out)
	}
}
