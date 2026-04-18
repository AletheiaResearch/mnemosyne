package card

import (
	"strings"
	"testing"
)

func TestRenderIsolateDescription_MentionsFormatsAndFingerprints(t *testing.T) {
	t.Parallel()
	header := ManifestHeader{
		PipelineFingerprint: "v1",
		ConfigFingerprint:   "sha256:cfg",
		AttachImages:        true,
		ExportedAt:          "2026-04-18T12:00:00Z",
	}
	entries := []ManifestEntry{
		{File: "claudecode/a.jsonl", Format: "claudecode", Lines: 5},
		{File: "claudecode/b.jsonl", Format: "claudecode", Lines: 3},
		{File: "codex/c.jsonl", Format: "codex", Lines: 7},
	}
	out := RenderIsolateDescription(header, entries, "MIT")

	for _, want := range []string{
		"Mnemosyne Isolate Dataset",
		"Agent Trace Viewer",
		"Sessions: 3",
		"Total lines: 15",
		"Attach images: yes",
		"Pipeline fingerprint: `v1`",
		"Config fingerprint: `sha256:cfg`",
		"License: MIT",
		"| claudecode | 2 |",
		"| codex | 1 |",
		"manifest.mnemosyne",
		`load_dataset("json", data_files="claudecode/*.jsonl"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered README missing %q. Got:\n%s", want, out)
		}
	}
}
