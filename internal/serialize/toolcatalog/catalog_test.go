package toolcatalog

import (
	"encoding/json"
	"testing"
)

func TestBundledOrigins(t *testing.T) {
	origins := Origins()
	for _, want := range []string{"claudecode", "codex"} {
		found := false
		for _, got := range origins {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing bundled catalog for %q: %v", want, origins)
		}
	}
}

func TestLoadRawReturnsParsableJSON(t *testing.T) {
	for _, origin := range Origins() {
		data, ok := LoadRaw(origin)
		if !ok {
			t.Errorf("LoadRaw(%q) = false", origin)
			continue
		}
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err != nil {
			t.Errorf("%s catalog is not valid JSON array: %v", origin, err)
			continue
		}
		if len(arr) == 0 {
			t.Errorf("%s catalog is empty", origin)
		}
	}
}

func TestLoadRawUnknownOrigin(t *testing.T) {
	if _, ok := LoadRaw("does-not-exist"); ok {
		t.Fatal("expected miss for unknown origin")
	}
	if _, ok := LoadRaw(""); ok {
		t.Fatal("expected miss for empty origin")
	}
}

func TestLoadRawIsCaseInsensitive(t *testing.T) {
	if _, ok := LoadRaw("ClaudeCode"); !ok {
		t.Fatal("expected case-insensitive match")
	}
}
