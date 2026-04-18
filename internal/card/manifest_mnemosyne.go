package card

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ManifestMnemosyneVersion identifies the manifest schema. Bump when
// Header/Entry field semantics change in a breaking way.
const ManifestMnemosyneVersion = "v1"

// ManifestHeader is the first line of a manifest.mnemosyne file.
type ManifestHeader struct {
	Kind                string             `json:"kind"`
	Version             string             `json:"version"`
	Tool                string             `json:"tool,omitempty"`
	ExportedAt          string             `json:"exported_at,omitempty"`
	PipelineFingerprint string             `json:"pipeline_fingerprint,omitempty"`
	ConfigFingerprint   string             `json:"config_fingerprint,omitempty"`
	SessionCount        int                `json:"session_count"`
	RecordCount         int                `json:"record_count,omitempty"`
	AttachImages        bool               `json:"attach_images"`
	Attestation         *ManifestAttestion `json:"attestation,omitempty"`
}

// ManifestAttestion mirrors the subset of the canonical attestation record we
// want downstream consumers to see alongside the per-session entries.
type ManifestAttestion struct {
	Timestamp          string `json:"timestamp,omitempty"`
	FullNameScanned    bool   `json:"full_name_scanned"`
	FullNameMatches    int    `json:"full_name_matches"`
	ManualSampleCount  int    `json:"manual_sample_count,omitempty"`
	StatementsAccepted int    `json:"statements_accepted,omitempty"`
}

// ManifestEntry describes a single session file in the dataset.
type ManifestEntry struct {
	Kind         string `json:"kind"`
	File         string `json:"file"`
	Format       string `json:"format"`
	SourceHash   string `json:"source_hash"`
	RedactionKey string `json:"redaction_key"`
	RedactedHash string `json:"redacted_hash"`
	Lines        int    `json:"lines,omitempty"`
	Turns        int    `json:"turns,omitempty"`
}

// RenderManifestMnemosyne serialises a header + entries as JSONL. The header
// is always the first line (kind="header"); entries are kind="session".
func RenderManifestMnemosyne(header ManifestHeader, entries []ManifestEntry) ([]byte, error) {
	header.Kind = "header"
	if header.Version == "" {
		header.Version = ManifestMnemosyneVersion
	}
	header.SessionCount = len(entries)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(header); err != nil {
		return nil, fmt.Errorf("encode header: %w", err)
	}
	for i, entry := range entries {
		entry.Kind = "session"
		if err := enc.Encode(entry); err != nil {
			return nil, fmt.Errorf("encode entry %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// ParseManifestMnemosyne parses a manifest.mnemosyne JSONL stream. Blank lines
// are skipped. Returns an error if the first non-blank line is not a header or
// any subsequent line is not a session entry.
func ParseManifestMnemosyne(r io.Reader) (ManifestHeader, []ManifestEntry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var header ManifestHeader
	entries := make([]ManifestEntry, 0)
	seenHeader := false
	for scanner.Scan() {
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var probe struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return ManifestHeader{}, nil, fmt.Errorf("decode kind: %w", err)
		}
		switch probe.Kind {
		case "header":
			if seenHeader {
				return ManifestHeader{}, nil, fmt.Errorf("multiple headers in manifest")
			}
			if err := json.Unmarshal(raw, &header); err != nil {
				return ManifestHeader{}, nil, fmt.Errorf("decode header: %w", err)
			}
			seenHeader = true
		case "session":
			if !seenHeader {
				return ManifestHeader{}, nil, fmt.Errorf("session entry before header")
			}
			var entry ManifestEntry
			if err := json.Unmarshal(raw, &entry); err != nil {
				return ManifestHeader{}, nil, fmt.Errorf("decode entry: %w", err)
			}
			entries = append(entries, entry)
		default:
			return ManifestHeader{}, nil, fmt.Errorf("unknown kind %q", probe.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return ManifestHeader{}, nil, fmt.Errorf("scan: %w", err)
	}
	if !seenHeader {
		return ManifestHeader{}, nil, fmt.Errorf("manifest missing header")
	}
	return header, entries, nil
}

// DiffManifestSessions compares local entries against what the remote manifest
// already holds. An entry is in toUpload when the remote has no file with the
// same name, or when any of source_hash, redaction_key, or redacted_hash
// differ. redacted_hash is part of the comparison because it is what the
// published manifest advertises for the uploaded bytes — skipping an upload
// while overwriting the manifest's redacted_hash would leave the repo
// advertising bytes it does not contain. toRetain contains remote entries
// that survive (still exist locally unchanged, or only exist remotely).
func DiffManifestSessions(local, remote []ManifestEntry) (toUpload, toRetain []ManifestEntry) {
	remoteByFile := make(map[string]ManifestEntry, len(remote))
	for _, entry := range remote {
		remoteByFile[entry.File] = entry
	}
	localByFile := make(map[string]ManifestEntry, len(local))
	for _, entry := range local {
		localByFile[entry.File] = entry
	}

	toUpload = make([]ManifestEntry, 0)
	for _, entry := range local {
		existing, ok := remoteByFile[entry.File]
		if !ok ||
			existing.SourceHash != entry.SourceHash ||
			existing.RedactionKey != entry.RedactionKey ||
			existing.RedactedHash != entry.RedactedHash {
			toUpload = append(toUpload, entry)
		}
	}

	toRetain = make([]ManifestEntry, 0)
	for _, entry := range remote {
		if _, replaced := localByFile[entry.File]; replaced {
			continue
		}
		toRetain = append(toRetain, entry)
	}
	return toUpload, toRetain
}

// MergeManifestEntries produces the new entries list for publication: any
// local entry replaces a same-file remote entry; remote entries that don't
// collide with a local file are preserved. Order: local first (stable), then
// retained remote (stable).
func MergeManifestEntries(local, remote []ManifestEntry) []ManifestEntry {
	localFiles := make(map[string]struct{}, len(local))
	for _, entry := range local {
		localFiles[entry.File] = struct{}{}
	}
	merged := make([]ManifestEntry, 0, len(local)+len(remote))
	merged = append(merged, local...)
	for _, entry := range remote {
		if _, replaced := localFiles[entry.File]; replaced {
			continue
		}
		merged = append(merged, entry)
	}
	return merged
}

// ManifestFileName is the conventional filename inside the uploaded dataset.
const ManifestFileName = "manifest.mnemosyne"

// NormalizeFile returns a path with forward slashes, which is how the
// manifest stores file references regardless of host OS.
func NormalizeFile(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
