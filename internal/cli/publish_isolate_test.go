package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AletheiaResearch/mnemosyne/internal/card"
	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

const hfShimScript = `#!/bin/bash
set -e
mode="$1"
shift

log_file="${MNEMOSYNE_HF_LOG:?}"
echo "ARGS:$mode $*" >> "$log_file"

case "$mode" in
  auth)
    if [ "$1" = "whoami" ]; then
      echo '{"name":"shimuser"}'
    fi
    ;;
  repos)
    # repos create <id> --repo-type dataset --exist-ok
    echo "created"
    ;;
  upload)
    # upload <repoID> <localPath> <pathInRepo> --repo-type dataset --quiet
    if [ -n "$MNEMOSYNE_UPLOAD_COPY_DIR" ]; then
      mkdir -p "$MNEMOSYNE_UPLOAD_COPY_DIR"
      cp "$2" "$MNEMOSYNE_UPLOAD_COPY_DIR/$(basename $3)"
    fi
    echo "uploaded"
    ;;
  download)
    # download <repoID> <pathInRepo> --repo-type dataset --local-dir <dir> --quiet
    path_in_repo="$2"
    local_dir=""
    while [ $# -gt 0 ]; do
      case "$1" in
        --local-dir) local_dir="$2"; shift 2 ;;
        *) shift ;;
      esac
    done
    if [ -n "$MNEMOSYNE_REMOTE_MANIFEST" ] && [ -f "$MNEMOSYNE_REMOTE_MANIFEST" ]; then
      mkdir -p "$local_dir/$(dirname $path_in_repo)"
      cp "$MNEMOSYNE_REMOTE_MANIFEST" "$local_dir/$path_in_repo"
    else
      echo "EntryNotFoundError: File $path_in_repo does not exist" >&2
      exit 1
    fi
    ;;
  *)
    echo "unknown mode $mode" >&2
    exit 2
    ;;
esac
`

type hfShim struct {
	dir       string
	logPath   string
	uploadDir string
}

func installHFShim(t *testing.T) *hfShim {
	t.Helper()
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "hf")
	if err := os.WriteFile(shimPath, []byte(hfShimScript), 0o755); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	shim := &hfShim{
		dir:       dir,
		logPath:   filepath.Join(t.TempDir(), "hf.log"),
		uploadDir: t.TempDir(),
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MNEMOSYNE_HF_LOG", shim.logPath)
	t.Setenv("MNEMOSYNE_UPLOAD_COPY_DIR", shim.uploadDir)
	return shim
}

func (s *hfShim) setRemoteManifest(t *testing.T, path string) {
	t.Helper()
	t.Setenv("MNEMOSYNE_REMOTE_MANIFEST", path)
}

func (s *hfShim) clearLog(t *testing.T) {
	t.Helper()
	if err := os.Truncate(s.logPath, 0); err != nil && !os.IsNotExist(err) {
		t.Fatalf("truncate log: %v", err)
	}
}

func (s *hfShim) readLog(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(s.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		t.Fatalf("read log: %v", err)
	}
	return string(data)
}

func (s *hfShim) uploadedFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(s.uploadDir)
	if err != nil {
		t.Fatalf("read upload dir: %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

func (s *hfShim) clearUploadDir(t *testing.T) {
	t.Helper()
	entries, _ := os.ReadDir(s.uploadDir)
	for _, entry := range entries {
		_ = os.Remove(filepath.Join(s.uploadDir, entry.Name()))
	}
}

func seedIsolateConfig(t *testing.T, cfgPath string) (stagingDir string, sessionPaths []string) {
	t.Helper()
	canonicalPath := filepath.Join(t.TempDir(), "canonical.jsonl")
	if err := os.WriteFile(canonicalPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write canonical: %v", err)
	}
	info, err := os.Stat(canonicalPath)
	if err != nil {
		t.Fatalf("stat canonical: %v", err)
	}

	stagingDir = t.TempDir()
	ccDir := filepath.Join(stagingDir, "claudecode")
	if err := os.MkdirAll(ccDir, 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	sessionA := filepath.Join(ccDir, "a.jsonl")
	sessionB := filepath.Join(ccDir, "b.jsonl")
	bodyA := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"a"}]}}` + "\n")
	bodyB := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"b"}]}}` + "\n")
	if err := os.WriteFile(sessionA, bodyA, 0o644); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(sessionB, bodyB, 0o644); err != nil {
		t.Fatalf("write b: %v", err)
	}
	hashBytes := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}

	cfg := config.Default()
	cfg.DestinationRepo = "shimuser/demo"
	cfg.IsolateExport = true
	cfg.AttachImages = false
	cfg.LastAttest = &config.LastAttest{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		FilePath:  canonicalPath,
		FileSize:  info.Size(),
	}
	cfg.ReviewerStatements = &config.ReviewerStatements{
		IdentityScan:    "I scanned the export for my full name Test Reviewer and grep confirmed no matches remain.",
		EntityInterview: "I reviewed for company, project, and tool references and found none requiring additional redaction.",
		ManualReview:    "I performed a manual review of 20 random samples and confirmed redactions look correct.",
	}
	cfg.VerificationRecord = &config.VerificationRecord{
		FullName:          "Test Reviewer",
		ManualSampleCount: 5,
	}
	cfg.LastExtract = &config.LastExtract{
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		RecordCount:       2,
		Scope:             "claudecode",
		OutputPath:        canonicalPath,
		IsolateStagingDir: stagingDir,
		IsolateSessions: []config.IsolateSession{
			{
				File:         "claudecode/a.jsonl",
				Format:       "claudecode",
				SourcePath:   "/original/a.jsonl",
				StagingPath:  sessionA,
				SourceHash:   hashBytes(bodyA),
				RedactionKey: "v1:v1:sha256:cfg:strip-images",
				RedactedHash: hashBytes(bodyA),
				Lines:        1,
			},
			{
				File:         "claudecode/b.jsonl",
				Format:       "claudecode",
				SourcePath:   "/original/b.jsonl",
				StagingPath:  sessionB,
				SourceHash:   hashBytes(bodyB),
				RedactionKey: "v1:v1:sha256:cfg:strip-images",
				RedactedHash: hashBytes(bodyB),
				Lines:        1,
			},
		},
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	return stagingDir, []string{sessionA, sessionB}
}

func runPublish(t *testing.T, rt *runtime, args ...string) (string, error) {
	t.Helper()
	cmd := newPublishCommand(rt)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestPublishIsolate_FirstRunUploadsAllSessions(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	seedIsolateConfig(t, cfgPath)

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}

	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("publish: %v", err)
	}

	uploaded := shim.uploadedFiles(t)
	expected := map[string]bool{
		"a.jsonl":             true,
		"b.jsonl":             true,
		card.ManifestFileName: true,
		"README.md":           true,
	}
	if len(uploaded) != len(expected) {
		t.Errorf("uploaded files = %v, want %d", uploaded, len(expected))
	}
	for _, name := range uploaded {
		if !expected[name] {
			t.Errorf("unexpected upload %q", name)
		}
	}
}

func TestPublishIsolate_SecondRunSkipsUnchanged(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	seedIsolateConfig(t, cfgPath)

	// First run: publish normally, then capture the manifest it produced
	// and feed it back as the "remote" manifest for the second run.
	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	remoteManifest := filepath.Join(shim.uploadDir, card.ManifestFileName)
	if _, err := os.Stat(remoteManifest); err != nil {
		t.Fatalf("manifest not uploaded: %v", err)
	}
	// Copy the manifest out since clearUploadDir will wipe the dir.
	manifestCopy := filepath.Join(t.TempDir(), card.ManifestFileName)
	data, err := os.ReadFile(remoteManifest)
	if err != nil {
		t.Fatalf("read remote manifest: %v", err)
	}
	if err := os.WriteFile(manifestCopy, data, 0o644); err != nil {
		t.Fatalf("write manifest copy: %v", err)
	}
	shim.setRemoteManifest(t, manifestCopy)
	shim.clearLog(t)
	shim.clearUploadDir(t)

	// Second run with the remote manifest advertising the same hashes.
	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("second publish: %v\nhf log:\n%s", err, shim.readLog(t))
	}

	uploaded := shim.uploadedFiles(t)
	uploadedSet := make(map[string]bool, len(uploaded))
	for _, name := range uploaded {
		uploadedSet[name] = true
	}
	if uploadedSet["a.jsonl"] || uploadedSet["b.jsonl"] {
		t.Errorf("second run should not reupload unchanged sessions; uploaded: %v\nhf log:\n%s", uploaded, shim.readLog(t))
	}
	if !uploadedSet[card.ManifestFileName] {
		t.Errorf("manifest should be re-uploaded on every run")
	}
}

func TestPublishIsolate_DetectsChangedSession(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	_, sessions := seedIsolateConfig(t, cfgPath)

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	remoteManifest := filepath.Join(shim.uploadDir, card.ManifestFileName)
	manifestCopy := filepath.Join(t.TempDir(), card.ManifestFileName)
	data, err := os.ReadFile(remoteManifest)
	if err != nil {
		t.Fatalf("read remote manifest: %v", err)
	}
	if err := os.WriteFile(manifestCopy, data, 0o644); err != nil {
		t.Fatalf("write manifest copy: %v", err)
	}
	shim.setRemoteManifest(t, manifestCopy)
	shim.clearLog(t)
	shim.clearUploadDir(t)

	// Rewrite the staging file for session A and sync the config's cached
	// hashes to the new disk content, mirroring what extract --isolate
	// would have done. Only the changed session should reupload.
	newBody := []byte(`{"changed":true}` + "\n")
	if err := os.WriteFile(sessions[0], newBody, 0o644); err != nil {
		t.Fatalf("mutate staging: %v", err)
	}
	newSum := sha256.Sum256(newBody)
	newHash := "sha256:" + hex.EncodeToString(newSum[:])
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.LastExtract.IsolateSessions[0].RedactedHash = newHash
	cfg.LastExtract.IsolateSessions[0].SourceHash = newHash
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("second publish: %v", err)
	}

	uploaded := shim.uploadedFiles(t)
	uploadedSet := make(map[string]bool, len(uploaded))
	for _, name := range uploaded {
		uploadedSet[name] = true
	}
	if !uploadedSet["a.jsonl"] {
		t.Errorf("changed session a.jsonl should reupload; got: %v", uploaded)
	}
	if uploadedSet["b.jsonl"] {
		t.Errorf("unchanged session b.jsonl should not reupload; got: %v", uploaded)
	}
}

// When a source file moves on disk between publishes (e.g. a Codex
// session aging from sessions/ into archived_sessions/, or a Claude
// project directory rename), its staging File changes because the hash
// prefix is derived from the source's parent directory. The bytes are
// unchanged, so the content tuple still matches the remote. Publish
// must recognise the relocation by content, skip the upload, and retain
// a single manifest entry for those bytes — no duplicate.
func TestPublishIsolate_SourceRelocationDoesNotDuplicate(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	seedIsolateConfig(t, cfgPath)

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	// Snapshot the manifest produced by the first publish; this is what
	// the second publish will see as "remote".
	remoteManifest := filepath.Join(shim.uploadDir, card.ManifestFileName)
	manifestCopy := filepath.Join(t.TempDir(), card.ManifestFileName)
	data, err := os.ReadFile(remoteManifest)
	if err != nil {
		t.Fatalf("read remote manifest: %v", err)
	}
	if err := os.WriteFile(manifestCopy, data, 0o644); err != nil {
		t.Fatalf("write manifest copy: %v", err)
	}
	shim.setRemoteManifest(t, manifestCopy)
	shim.clearLog(t)
	shim.clearUploadDir(t)

	// Simulate a source-file move: the bytes (and thus SourceHash /
	// RedactionKey / RedactedHash) are unchanged, but the File label now
	// points at a new hashed subdirectory — just like it would after a
	// user renamed the source's parent directory and re-ran extract.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.LastExtract.IsolateSessions[0].File = "claudecode/aaaabbbb/a.jsonl"
	cfg.LastExtract.IsolateSessions[1].File = "claudecode/ccccdddd/b.jsonl"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("second publish: %v\nhf log:\n%s", err, shim.readLog(t))
	}

	uploaded := shim.uploadedFiles(t)
	for _, name := range uploaded {
		if name == "a.jsonl" || name == "b.jsonl" {
			t.Errorf("relocation must not re-upload unchanged bytes; got upload of %q\nhf log:\n%s", name, shim.readLog(t))
		}
	}

	// The second publish's manifest must contain exactly one entry per
	// original File (no duplicates) and must preserve the remote's path
	// label rather than adopting the relocated one, so retained remote
	// consumers still resolve the same URL.
	newManifestPath := filepath.Join(shim.uploadDir, card.ManifestFileName)
	newManifestBytes, err := os.ReadFile(newManifestPath)
	if err != nil {
		t.Fatalf("read new manifest: %v", err)
	}
	_, entries, err := card.ParseManifestMnemosyne(bytes.NewReader(newManifestBytes))
	if err != nil {
		t.Fatalf("parse new manifest: %v", err)
	}
	seen := make(map[string]int, len(entries))
	for _, entry := range entries {
		seen[entry.File]++
	}
	if seen["claudecode/a.jsonl"] != 1 || seen["claudecode/b.jsonl"] != 1 {
		t.Errorf("expected single entries at remote paths; got map: %v\nentries: %+v", seen, entries)
	}
	if seen["claudecode/aaaabbbb/a.jsonl"] != 0 || seen["claudecode/ccccdddd/b.jsonl"] != 0 {
		t.Errorf("relocated local paths must not appear in published manifest; got map: %v", seen)
	}
}

func TestPublishIsolate_RejectsTamperedStaging(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	_, sessions := seedIsolateConfig(t, cfgPath)

	// Mutate the staging file on disk without updating the config. The
	// manifest still advertises the original redacted_hash; publish must
	// refuse to ship bytes that don't match.
	if err := os.WriteFile(sessions[0], []byte(`{"tampered":true}`+"\n"), 0o644); err != nil {
		t.Fatalf("tamper staging: %v", err)
	}

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	_, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	)
	if err == nil {
		t.Fatalf("expected tamper-detection error, got nil")
	}
	if !strings.Contains(err.Error(), "changed after extract") {
		t.Fatalf("expected changed-after-extract error, got: %v", err)
	}

	uploaded := shim.uploadedFiles(t)
	for _, name := range uploaded {
		if name == "a.jsonl" || name == card.ManifestFileName || name == "README.md" {
			t.Errorf("nothing should ship after tamper detection; saw %q", name)
		}
	}
	// The dataset repo must not be created when preflight fails: a
	// successful publish has a `repos create` in the hf log, so its
	// absence confirms the remote was left untouched.
	if log := shim.readLog(t); strings.Contains(log, "repos create") {
		t.Errorf("repos create invoked despite preflight failure:\n%s", log)
	}
}

// The manifest header must describe the posture the uploaded bytes were
// produced under (extract time), not whatever config happens to be
// loaded at publish time. Mutating attach_images / custom_redactions
// between extract and publish must not change what the header advertises
// for the already-redacted session bytes.
func TestPublishIsolate_HeaderTracksExtractNotCurrentConfig(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	seedIsolateConfig(t, cfgPath)

	// Mutate the current config to advertise a different posture than
	// the one baked into each IsolateSession.RedactionKey. A correctly
	// implemented publish ignores this and reads posture from the key.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.AttachImages = true
	cfg.CustomRedactions = []string{"newly-added-after-extract"}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	if _, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	); err != nil {
		t.Fatalf("publish: %v\nhf log:\n%s", err, shim.readLog(t))
	}

	manifestPath := filepath.Join(shim.uploadDir, card.ManifestFileName)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	header, _, err := card.ParseManifestMnemosyne(bytes.NewReader(manifestBytes))
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	// Session keys in the seed are "v1:v1:sha256:cfg:strip-images".
	if header.AttachImages {
		t.Errorf("header.AttachImages = true; want false (derived from extract-time key, not mutated cfg.AttachImages=true)")
	}
	if header.PipelineFingerprint != "v1" {
		t.Errorf("header.PipelineFingerprint = %q, want %q", header.PipelineFingerprint, "v1")
	}
	if header.ConfigFingerprint != "sha256:cfg" {
		t.Errorf("header.ConfigFingerprint = %q, want %q (derived from extract-time key, not mutated cfg)", header.ConfigFingerprint, "sha256:cfg")
	}
}

// Two IsolateSessions with different RedactionKeys can only arise from
// resumed/interleaved extracts; publish can't emit a single coherent
// header for them. It must fail fast with a clear re-run instruction
// before any remote side effect.
func TestPublishIsolate_RejectsMismatchedRedactionKeys(t *testing.T) {
	shim := installHFShim(t)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	seedIsolateConfig(t, cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cfg.LastExtract.IsolateSessions[1].RedactionKey = "v1:v1:sha256:cfg:keep-images"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	_, err = runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	)
	if err == nil || !strings.Contains(err.Error(), "mismatched redaction keys") {
		t.Fatalf("expected mismatched-keys error, got: %v", err)
	}
	if log := shim.readLog(t); strings.Contains(log, "repos create") {
		t.Errorf("repos create must not run when preflight fails:\n%s", log)
	}
}

func TestPublishIsolate_RequiresPriorExtract(t *testing.T) {
	shim := installHFShim(t)
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	// Minimal valid attested config with NO IsolateSessions.
	canonicalPath := filepath.Join(t.TempDir(), "canonical.jsonl")
	if err := os.WriteFile(canonicalPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write canonical: %v", err)
	}
	info, _ := os.Stat(canonicalPath)
	cfg := config.Default()
	cfg.IsolateExport = true
	cfg.LastAttest = &config.LastAttest{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		FilePath:  canonicalPath,
		FileSize:  info.Size(),
	}
	cfg.ReviewerStatements = &config.ReviewerStatements{
		IdentityScan:    "I scanned the export for my full name Test Reviewer and grep confirmed no matches remain.",
		EntityInterview: "I reviewed for company, project, and tool references and found none requiring additional redaction.",
		ManualReview:    "I performed a manual review of 20 random samples and confirmed redactions look correct.",
	}
	cfg.VerificationRecord = &config.VerificationRecord{FullName: "Test Reviewer", ManualSampleCount: 5}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	rt := &runtime{configPath: cfgPath, logger: newLogger(false), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	_, err := runPublish(t, rt,
		"--isolate",
		"--publish-attestation", "I approve and authorize mnemosyne to publish and upload this archive.",
	)
	if err == nil || !strings.Contains(err.Error(), "extract --isolate") {
		t.Fatalf("expected extract-first error, got: %v", err)
	}
	if log := shim.readLog(t); strings.Contains(log, "repos create") {
		t.Errorf("repos create must not run when preflight fails:\n%s", log)
	}
}
