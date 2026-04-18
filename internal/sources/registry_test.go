package sources

import (
	"testing"
)

func TestRegistryReturnsAllSourcesWithUniqueNames(t *testing.T) {
	t.Parallel()

	sources := Registry()
	want := []string{
		"claudecode",
		"codex",
		"cursor",
		"gemini",
		"kimi",
		"openclaw",
		"opencode",
		"orchestrator",
		"supplied",
	}
	if len(sources) != len(want) {
		t.Fatalf("expected %d sources, got %d", len(want), len(sources))
	}

	seen := make(map[string]struct{}, len(sources))
	for _, src := range sources {
		name := src.Name()
		if name == "" {
			t.Fatalf("source with empty Name(): %T", src)
		}
		if _, dup := seen[name]; dup {
			t.Fatalf("duplicate source name %q", name)
		}
		seen[name] = struct{}{}
	}
	for _, name := range want {
		if _, ok := seen[name]; !ok {
			t.Fatalf("missing source %q (registered names: %v)", name, seen)
		}
	}
}

func TestRegistryReturnsIndependentSlices(t *testing.T) {
	t.Parallel()

	a := Registry()
	b := Registry()
	if len(a) != len(b) {
		t.Fatalf("inconsistent registry size: %d vs %d", len(a), len(b))
	}
	if len(a) == 0 {
		t.Fatal("registry is empty")
	}
	// Mutating one slice must not corrupt the other.
	a[0] = nil
	if b[0] == nil {
		t.Fatalf("registry returned a shared backing array")
	}
}
