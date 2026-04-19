// Package nativeexport streams native-format agent session JSONL files through
// the mnemosyne redaction pipeline without reshaping the original schema. Each
// source line is decoded, redacted field-by-field via redact.ApplyAny (which
// routes strings to path/url/command/text modes based on key heuristics), and
// re-encoded into a staging file. The source file on disk is never modified.
//
// The walker preserves unknown fields byte-for-byte; format-specific Redactor
// implementations only need to supply a pre-pass that strips image attachments
// when AttachImages is false.
package nativeexport

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
)

// maxLineBytes matches the cap used by the transform CLI scanner. Large tool
// outputs can exceed the stdlib default.
const maxLineBytes = 64 * 1024 * 1024

// Options controls a single Redact invocation.
type Options struct {
	Pipeline     *redact.Pipeline
	AttachImages bool
	RedactionKey string
}

// Result summarises what the redactor produced so it can be folded into the
// extract summary and the manifest.mnemosyne file.
type Result struct {
	SourcePath   string
	StagingPath  string
	Format       string
	SourceHash   string
	RedactedHash string
	RedactionKey string
	SessionID    string
	Lines        int
	AttachImages bool
}

// Redactor walks one native session file and writes a redacted copy.
type Redactor interface {
	Format() string
	Redact(ctx context.Context, srcPath, dstPath string, opts Options) (Result, error)
}

// ForOrigin returns the Redactor responsible for a record's Origin field, or
// (nil, false) if the origin does not have a native-format handler yet.
func ForOrigin(origin string) (Redactor, bool) {
	switch origin {
	case "claudecode":
		return ClaudeCode(), true
	case "codex":
		return Codex(), true
	default:
		return nil, false
	}
}

// preProcess is implemented by format-specific redactors to tweak a decoded
// JSON line before the generic walker runs. The returned bool reports whether
// the line should be written (false drops the line entirely, e.g. if image
// stripping leaves nothing meaningful). The default implementation keeps
// everything.
type preProcess func(line map[string]any, opts Options) (map[string]any, bool)

// sessionDetect returns the logical session id carried by a decoded line, or
// "" when the line is not the one that names the session. redactFile adopts
// the first non-empty value as Result.SessionID — subsequent matches are
// ignored so the first carrier in the stream wins.
type sessionDetect func(line map[string]any) string

// redactFile performs the streaming read/walk/write and hashing shared by all
// formats. Each format supplies a preProcess hook for its image stripping
// logic and an optional sessionDetect for extracting the session id in-band.
func redactFile(ctx context.Context, srcPath, dstPath, format string, opts Options, pre preProcess, detect sessionDetect) (result Result, err error) {
	if opts.Pipeline == nil {
		return Result{}, errors.New("nativeexport: Options.Pipeline is required")
	}
	if pre == nil {
		pre = func(line map[string]any, _ Options) (map[string]any, bool) { return line, true }
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return Result{}, fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if closeErr := srcFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close source: %w", closeErr)
		}
	}()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return Result{}, fmt.Errorf("ensure staging dir: %w", err)
	}
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return Result{}, fmt.Errorf("create staging file: %w", err)
	}
	// A failed Close on the staging file means buffered bytes may not have
	// reached disk even though dstHasher already consumed them, so the
	// RedactedHash would no longer describe the on-disk artifact. Surface
	// the error instead of swallowing it.
	defer func() {
		if closeErr := dstFile.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close staging file: %w", closeErr)
		}
	}()

	srcHasher := sha256.New()
	dstHasher := sha256.New()

	src := io.TeeReader(srcFile, srcHasher)
	dstWriter := bufio.NewWriter(io.MultiWriter(dstFile, dstHasher))

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 1024*1024), maxLineBytes)

	lines := 0
	sessionID := ""
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		rewritten, foundID, processErr := processLine(raw, opts, pre, detect)
		if processErr != nil {
			return Result{}, fmt.Errorf("process line %d: %w", lines+1, processErr)
		}
		if sessionID == "" && foundID != "" {
			sessionID = foundID
		}
		if rewritten == nil {
			continue
		}
		if _, err := dstWriter.Write(rewritten); err != nil {
			return Result{}, fmt.Errorf("write line: %w", err)
		}
		if _, err := dstWriter.Write([]byte{'\n'}); err != nil {
			return Result{}, fmt.Errorf("write newline: %w", err)
		}
		lines++
	}
	if err := scanner.Err(); err != nil {
		return Result{}, fmt.Errorf("scan: %w", err)
	}
	if err := dstWriter.Flush(); err != nil {
		return Result{}, fmt.Errorf("flush: %w", err)
	}
	if err := dstFile.Sync(); err != nil {
		return Result{}, fmt.Errorf("sync staging file: %w", err)
	}

	// Drain the source into the hasher in case the scanner ignored a trailing
	// partial line without a newline.
	if _, err := io.Copy(io.Discard, src); err != nil {
		return Result{}, fmt.Errorf("drain source: %w", err)
	}

	return Result{
		SourcePath:   srcPath,
		StagingPath:  dstPath,
		Format:       format,
		SourceHash:   hashValue(srcHasher),
		RedactedHash: hashValue(dstHasher),
		RedactionKey: opts.RedactionKey,
		SessionID:    sessionID,
		Lines:        lines,
		AttachImages: opts.AttachImages,
	}, nil
}

func processLine(raw []byte, opts Options, pre preProcess, detect sessionDetect) (out []byte, sessionID string, err error) {
	var line map[string]any
	if unmarshalErr := json.Unmarshal(raw, &line); unmarshalErr != nil {
		// Pass-through for non-object lines (rare). Apply a plain text redact
		// pass so secrets embedded in free-form lines don't leak.
		return []byte(opts.Pipeline.ApplyText(string(raw))), "", nil
	}
	if detect != nil {
		sessionID = detect(line)
	}
	line, keep := pre(line, opts)
	if !keep {
		return nil, sessionID, nil
	}
	walked := opts.Pipeline.ApplyAny(line, "")
	out, err = json.Marshal(walked)
	return out, sessionID, err
}

func hashValue(h hash.Hash) string {
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
