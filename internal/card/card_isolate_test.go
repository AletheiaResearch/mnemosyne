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
		"Pipeline fingerprint (this publish): `v1`",
		"Config fingerprint (this publish): `sha256:cfg`",
		"License: MIT",
		"| claudecode | 2 |",
		"| codex | 1 |",
		"manifest.mnemosyne",
		`load_dataset("json", data_files="claudecode/**/*.jsonl"`,
		`load_dataset("json", data_files="codex/**/*.jsonl"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered README missing %q. Got:\n%s", want, out)
		}
	}
}

// When the merged entries include both keep-images and strip-images
// sessions, the README must not claim a single dataset-wide image mode
// — that would misrepresent the retained remote sessions. Instead it
// shows "mixed (keep/strip)" and a per-key breakdown so a reader can
// see that the dataset is heterogeneous.
func TestRenderIsolateDescription_MixedAttachImagesSurfaced(t *testing.T) {
	t.Parallel()
	header := ManifestHeader{
		PipelineFingerprint: "v1",
		ConfigFingerprint:   "sha256:cfg-new",
		AttachImages:        true,
		ExportedAt:          "2026-04-18T12:00:00Z",
	}
	entries := []ManifestEntry{
		{File: "claudecode/a.jsonl", Format: "claudecode", RedactionKey: "v1:v1:sha256:cfg-new:keep-images", Lines: 5},
		{File: "codex/b.jsonl", Format: "codex", RedactionKey: "v1:v1:sha256:cfg-old:strip-images", Lines: 3},
	}
	out := RenderIsolateDescription(header, entries, "")

	if !strings.Contains(out, "Attach images: mixed (1 keep / 1 strip)") {
		t.Errorf("README should flag mixed image modes; got:\n%s", out)
	}
	if !strings.Contains(out, "## Sessions by Redaction Key") {
		t.Errorf("README should include redaction-key breakdown; got:\n%s", out)
	}
	if !strings.Contains(out, "v1:v1:sha256:cfg-new:keep-images") ||
		!strings.Contains(out, "v1:v1:sha256:cfg-old:strip-images") {
		t.Errorf("README should list every distinct redaction_key; got:\n%s", out)
	}
	// The raw dataset-wide claim must not appear — it would be
	// factually wrong given the mix.
	if strings.Contains(out, "Attach images: yes\n") {
		t.Errorf("README must not advertise dataset-wide yes when mix exists; got:\n%s", out)
	}
}

// The Layout block documents where session files actually live in the
// repo. Since extract namespaces sessions under a hash subdirectory
// (to avoid collisions when the same session name appears in multiple
// source directories), the README must advertise that structure — a
// user copy-pasting the example must load real files, not empty globs.
func TestRenderIsolateDescription_LayoutMatchesExtractPaths(t *testing.T) {
	t.Parallel()
	out := RenderIsolateDescription(ManifestHeader{}, nil, "")
	for _, want := range []string{
		"claudecode/<hash>/<session>.jsonl",
		"codex/<hash>/<session>.jsonl",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Layout block missing %q; got:\n%s", want, out)
		}
	}
}

// When every retained entry is strip-images the dataset-wide claim must
// say "no", even if this publish alone kept images (the publisher did
// not upload any new sessions, so the dataset is entirely pre-existing).
func TestRenderIsolateDescription_AllStripBeatsHeaderKeep(t *testing.T) {
	t.Parallel()
	header := ManifestHeader{AttachImages: true}
	entries := []ManifestEntry{
		{File: "claudecode/a.jsonl", RedactionKey: "v1:v1:sha256:cfg:strip-images"},
	}
	out := RenderIsolateDescription(header, entries, "")
	if !strings.Contains(out, "Attach images: no") {
		t.Errorf("README should reflect observed strip-images; got:\n%s", out)
	}
}
