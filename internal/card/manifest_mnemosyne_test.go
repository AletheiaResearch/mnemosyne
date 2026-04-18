package card

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func sampleEntries() []ManifestEntry {
	return []ManifestEntry{
		{File: "claudecode/a.jsonl", Format: "claudecode", SourceHash: "sha256:aaa", RedactionKey: "v1:keep", RedactedHash: "sha256:111", Lines: 3},
		{File: "codex/b.jsonl", Format: "codex", SourceHash: "sha256:bbb", RedactionKey: "v1:keep", RedactedHash: "sha256:222", Lines: 7},
	}
}

func TestRenderManifest_RoundTrip(t *testing.T) {
	t.Parallel()
	entries := sampleEntries()
	header := ManifestHeader{
		Tool:                "mnemosyne/test",
		ExportedAt:          "2026-04-18T12:00:00Z",
		PipelineFingerprint: "v1",
		ConfigFingerprint:   "sha256:cfg",
		AttachImages:        true,
		RecordCount:         2,
		Attestation: &ManifestAttestion{
			Timestamp:       "2026-04-18T11:58:00Z",
			FullNameScanned: true,
		},
	}
	data, err := RenderManifestMnemosyne(header, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	gotHeader, gotEntries, err := ParseManifestMnemosyne(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if gotHeader.Version != ManifestMnemosyneVersion {
		t.Errorf("header version = %q, want %q", gotHeader.Version, ManifestMnemosyneVersion)
	}
	if gotHeader.SessionCount != len(entries) {
		t.Errorf("header session_count = %d, want %d", gotHeader.SessionCount, len(entries))
	}
	if gotHeader.Kind != "header" {
		t.Errorf("header kind = %q", gotHeader.Kind)
	}
	if gotHeader.Attestation == nil || !gotHeader.Attestation.FullNameScanned {
		t.Errorf("attestation lost in round-trip: %+v", gotHeader.Attestation)
	}
	if len(gotEntries) != len(entries) {
		t.Fatalf("entries = %d, want %d", len(gotEntries), len(entries))
	}
	for i, got := range gotEntries {
		want := entries[i]
		if got.Kind != "session" {
			t.Errorf("entry %d kind = %q", i, got.Kind)
		}
		if got.File != want.File || got.SourceHash != want.SourceHash ||
			got.RedactionKey != want.RedactionKey || got.RedactedHash != want.RedactedHash ||
			got.Lines != want.Lines || got.Format != want.Format {
			t.Errorf("entry %d mismatch: got=%+v want=%+v", i, got, want)
		}
	}
}

func TestRenderManifest_LineCountMatchesEntries(t *testing.T) {
	t.Parallel()
	entries := sampleEntries()
	data, err := RenderManifestMnemosyne(ManifestHeader{}, entries)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			lines++
		}
	}
	if lines != len(entries)+1 {
		t.Fatalf("line count = %d, want %d (header + entries)", lines, len(entries)+1)
	}
}

func TestParseManifest_RejectsSessionBeforeHeader(t *testing.T) {
	t.Parallel()
	_, _, err := ParseManifestMnemosyne(strings.NewReader(`{"kind":"session","file":"a.jsonl"}`))
	if err == nil {
		t.Fatalf("expected error for session before header")
	}
}

func TestParseManifest_RejectsMissingHeader(t *testing.T) {
	t.Parallel()
	_, _, err := ParseManifestMnemosyne(strings.NewReader(""))
	if err == nil {
		t.Fatalf("expected error for empty input")
	}
}

func TestDiffManifestSessions_FourQuadrants(t *testing.T) {
	t.Parallel()
	local := []ManifestEntry{
		// unchanged in both hashes — should NOT upload.
		{File: "a.jsonl", SourceHash: "sha256:a", RedactionKey: "v1:keep", RedactedHash: "sha256:aa"},
		// source changed — must upload.
		{File: "b.jsonl", SourceHash: "sha256:b2", RedactionKey: "v1:keep", RedactedHash: "sha256:bb2"},
		// redaction key changed — must upload.
		{File: "c.jsonl", SourceHash: "sha256:c", RedactionKey: "v1:strip", RedactedHash: "sha256:cc2"},
		// new file — must upload.
		{File: "d.jsonl", SourceHash: "sha256:d", RedactionKey: "v1:keep", RedactedHash: "sha256:dd"},
	}
	remote := []ManifestEntry{
		{File: "a.jsonl", SourceHash: "sha256:a", RedactionKey: "v1:keep", RedactedHash: "sha256:aa"},
		{File: "b.jsonl", SourceHash: "sha256:b", RedactionKey: "v1:keep", RedactedHash: "sha256:bb"},
		{File: "c.jsonl", SourceHash: "sha256:c", RedactionKey: "v1:keep", RedactedHash: "sha256:cc"},
		// only-remote entry — stays in the retained set.
		{File: "z.jsonl", SourceHash: "sha256:z", RedactionKey: "v1:keep", RedactedHash: "sha256:zz"},
	}

	toUpload, toRetain := DiffManifestSessions(local, remote)
	uploadFiles := make([]string, 0, len(toUpload))
	for _, entry := range toUpload {
		uploadFiles = append(uploadFiles, entry.File)
	}
	sort.Strings(uploadFiles)
	wantUpload := []string{"b.jsonl", "c.jsonl", "d.jsonl"}
	if !reflect.DeepEqual(uploadFiles, wantUpload) {
		t.Errorf("upload files = %v, want %v", uploadFiles, wantUpload)
	}
	if len(toRetain) != 1 || toRetain[0].File != "z.jsonl" {
		t.Errorf("retained = %+v, want only z.jsonl", toRetain)
	}
}

func TestMergeManifestEntries_LocalWinsOnCollision(t *testing.T) {
	t.Parallel()
	local := []ManifestEntry{
		{File: "a.jsonl", SourceHash: "sha256:aNEW"},
	}
	remote := []ManifestEntry{
		{File: "a.jsonl", SourceHash: "sha256:aOLD"},
		{File: "z.jsonl", SourceHash: "sha256:z"},
	}
	merged := MergeManifestEntries(local, remote)
	if len(merged) != 2 {
		t.Fatalf("merged = %d entries", len(merged))
	}
	if merged[0].File != "a.jsonl" || merged[0].SourceHash != "sha256:aNEW" {
		t.Errorf("local should win on collision: %+v", merged[0])
	}
	if merged[1].File != "z.jsonl" {
		t.Errorf("remote-only entry lost: %+v", merged)
	}
}
