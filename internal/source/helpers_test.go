package source

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadJSONLinesHandlesLinesLargerThanEightMiB(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.jsonl")
	line := `{"text":"` + strings.Repeat("a", 9*1024*1024) + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	count := 0
	if err := ReadJSONLines(path, func(_ int, raw []byte) error {
		count++
		if len(raw) == 0 {
			t.Fatal("expected non-empty line payload")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one line, got %d", count)
	}
}
