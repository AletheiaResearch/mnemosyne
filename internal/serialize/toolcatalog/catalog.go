package toolcatalog

import (
	"bytes"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.json
var catalogFS embed.FS

// LoadRaw returns the raw JSON tools file for the given origin, or (nil,
// false) if no bundled catalog matches. Origin is matched case-insensitively
// against the bundled filenames.
func LoadRaw(origin string) ([]byte, bool) {
	if origin == "" {
		return nil, false
	}
	name := strings.ToLower(origin) + ".json"
	data, err := catalogFS.ReadFile(name)
	if err != nil {
		return nil, false
	}
	return bytes.TrimSpace(data), true
}

// Origins returns the sorted list of origins that have a bundled catalog.
func Origins() []string {
	entries, err := catalogFS.ReadDir(".")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(out)
	return out
}

// LoadAsReader returns the catalog bytes wrapped in a reader for feeding to
// serialize.ReadToolSchemas. Returns nil when no catalog matches.
func LoadAsReader(origin string) *bytes.Reader {
	data, ok := LoadRaw(origin)
	if !ok {
		return nil
	}
	return bytes.NewReader(data)
}

// ErrUnknownOrigin is returned by MustLoadRaw when no catalog exists.
var ErrUnknownOrigin = fmt.Errorf("no bundled tool catalog for origin")
