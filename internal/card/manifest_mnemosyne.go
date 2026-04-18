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
//
// PipelineFingerprint, ConfigFingerprint, and AttachImages describe the
// publish event that produced this manifest — i.e. the redaction rules
// and image policy the publisher applied to sessions uploaded in this
// publish. They do NOT describe the entire dataset. Retained remote
// sessions may carry different redaction keys; the authoritative source
// of truth for what each individual session was redacted under is the
// per-entry RedactionKey. Use that (or the aggregate derived from the
// entries) whenever the question is about the dataset rather than this
// specific publish event.
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

// contentKey is the identity tuple that uniquely describes the bytes of
// a session and the redaction posture they were produced under. The File
// field is deliberately excluded: it is a repo-path *label*, not the
// session's identity, and rewriting a local File to adopt a remote one
// is safe when the content tuple matches.
func contentKey(entry ManifestEntry) string {
	return entry.SourceHash + "|" + entry.RedactionKey + "|" + entry.RedactedHash
}

// DiffManifestSessions compares local entries against what the remote manifest
// already holds and decides which sessions still need to be uploaded.
//
// Decision per local entry:
//  1. A remote entry exists with the same File and an identical content
//     tuple → no upload; the local entry passes through unchanged.
//  2. A remote entry exists with a different File but an identical
//     content tuple → no upload. The local entry's File is *rewritten*
//     to match the remote's so that downstream MergeManifestEntries
//     collapses them into a single record. This is how a moved source
//     (e.g. a Codex session aging into archived_sessions/) avoids
//     accumulating as a duplicate in the repo.
//  3. Otherwise → upload. Either the remote has no matching content or
//     the content diverges (source, key, or redacted bytes changed).
//
// redacted_hash participates in the comparison because the published
// manifest advertises it for the uploaded bytes; skipping an upload
// while overwriting the manifest's redacted_hash would leave the repo
// advertising bytes it does not contain.
//
// The returned `aligned` slice mirrors `local` 1:1 but with any adopted
// Files applied. Callers must pass `aligned` (not the raw `local`) to
// MergeManifestEntries and use it to resolve the upload destination for
// entries still in `toUpload`. `toRetain` contains remote entries that
// survive because no local entry replaces them (by File or by content).
func DiffManifestSessions(local, remote []ManifestEntry) (toUpload, toRetain, aligned []ManifestEntry) {
	remoteByFile := make(map[string]ManifestEntry, len(remote))
	remoteByContent := make(map[string]ManifestEntry, len(remote))
	for _, entry := range remote {
		remoteByFile[entry.File] = entry
		if key := contentKey(entry); key != "||" {
			remoteByContent[key] = entry
		}
	}

	aligned = make([]ManifestEntry, 0, len(local))
	toUpload = make([]ManifestEntry, 0)
	claimedRemoteFiles := make(map[string]struct{}, len(local))

	for _, entry := range local {
		if existing, ok := remoteByFile[entry.File]; ok &&
			existing.SourceHash == entry.SourceHash &&
			existing.RedactionKey == entry.RedactionKey &&
			existing.RedactedHash == entry.RedactedHash {
			aligned = append(aligned, entry)
			claimedRemoteFiles[entry.File] = struct{}{}
			continue
		}
		if existing, ok := remoteByContent[contentKey(entry)]; ok && existing.File != entry.File {
			if _, already := claimedRemoteFiles[existing.File]; !already {
				rewritten := entry
				rewritten.File = existing.File
				aligned = append(aligned, rewritten)
				claimedRemoteFiles[existing.File] = struct{}{}
				continue
			}
		}
		aligned = append(aligned, entry)
		toUpload = append(toUpload, entry)
		claimedRemoteFiles[entry.File] = struct{}{}
	}

	toRetain = make([]ManifestEntry, 0)
	for _, entry := range remote {
		if _, replaced := claimedRemoteFiles[entry.File]; replaced {
			continue
		}
		toRetain = append(toRetain, entry)
	}
	return toUpload, toRetain, aligned
}

// MergeManifestEntries produces the new entries list for publication: any
// local entry replaces a same-file remote entry; remote entries that don't
// collide with a local file are preserved. Order: local first (stable), then
// retained remote (stable).
//
// Callers publishing after DiffManifestSessions MUST pass the aligned
// local slice — not the raw pre-diff slice — so that any local entry
// whose File was rewritten to adopt a remote path collapses cleanly
// with that remote entry instead of accumulating alongside it.
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

// RedactionKeySummary describes the observed redaction-key mix across a
// list of manifest entries. Keep / Strip count sessions whose redaction
// key ends in "keep-images" / "strip-images" respectively; Unknown
// counts entries with no recognisable suffix (legacy or empty keys).
type RedactionKeySummary struct {
	Keep    int
	Strip   int
	Unknown int
	// ByKey maps the full redaction_key string to its session count so
	// callers can surface every distinct pipeline/config variant present.
	ByKey map[string]int
}

// Mixed reports whether the entries represent more than one image mode.
// "Mixed" is only true when both keep and strip modes are observed;
// unknown-only entries are not considered a mix with themselves.
func (s RedactionKeySummary) Mixed() bool {
	return s.Keep > 0 && s.Strip > 0
}

// AllKeep reports whether every entry with a recognisable redaction key
// is in keep-images mode. Returns false when there are no keep-mode
// entries at all, even if everything is strip-mode.
func (s RedactionKeySummary) AllKeep() bool {
	return s.Keep > 0 && s.Strip == 0
}

// AllStrip reports whether every entry with a recognisable redaction key
// is in strip-images mode.
func (s RedactionKeySummary) AllStrip() bool {
	return s.Strip > 0 && s.Keep == 0
}

// SummarizeRedactionKeys counts the image-mode suffix of every entry's
// redaction_key. It is the observed, aggregate truth for a set of
// manifest entries — use it when describing the dataset as a whole, as
// opposed to a single publish event (header.AttachImages).
func SummarizeRedactionKeys(entries []ManifestEntry) RedactionKeySummary {
	summary := RedactionKeySummary{ByKey: make(map[string]int)}
	for _, entry := range entries {
		summary.ByKey[entry.RedactionKey]++
		switch {
		case strings.HasSuffix(entry.RedactionKey, ":keep-images"):
			summary.Keep++
		case strings.HasSuffix(entry.RedactionKey, ":strip-images"):
			summary.Strip++
		default:
			summary.Unknown++
		}
	}
	return summary
}
