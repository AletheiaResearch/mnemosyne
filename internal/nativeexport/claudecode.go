package nativeexport

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/source/claudecode"
)

type claudeCodeRedactor struct{}

// ClaudeCode returns the Redactor for Claude Code session JSONL files
// (~/.claude/projects/<project>/<session>.jsonl).
func ClaudeCode() Redactor { return claudeCodeRedactor{} }

func (claudeCodeRedactor) Format() string { return "claudecode" }

func (r claudeCodeRedactor) Redact(ctx context.Context, srcPath, dstPath string, opts Options) (Result, error) {
	result, err := redactFile(ctx, srcPath, dstPath, r.Format(), opts, claudeCodePreProcess, nil)
	if err != nil {
		return result, err
	}
	// Claude Code session files live at ~/.claude/projects/<project>/<UUID>.jsonl.
	// The raw source derives the record_id as "<ProjectScope>/<UUID>" — a
	// hash of the project dir rather than the raw name, because that name
	// is path-derived and would otherwise leak local paths. Mirror the
	// same scoping here so manifest dedup keyed on (SessionID, Format)
	// matches the raw source's identity without leaking the project dir.
	if result.SessionID == "" {
		result.SessionID = claudecodeSessionID(srcPath)
	}
	return result, nil
}

func claudecodeSessionID(srcPath string) string {
	base := filepath.Base(srcPath)
	session := strings.TrimSuffix(base, filepath.Ext(base))
	projectID := filepath.Base(filepath.Dir(srcPath))
	if projectID == "" || projectID == "." || projectID == string(filepath.Separator) {
		return session
	}
	scope := claudecode.ProjectScope(projectID)
	if scope == "" {
		return session
	}
	return scope + "/" + session
}

// claudeCodePreProcess strips inline images from Claude Code message content
// when the caller opted out of attachments. Everything else passes through
// unchanged; the generic walker handles string-level redaction.
func claudeCodePreProcess(line map[string]any, opts Options) (map[string]any, bool) {
	if opts.AttachImages {
		return line, true
	}
	message, ok := line["message"].(map[string]any)
	if !ok {
		return line, true
	}
	content, ok := message["content"].([]any)
	if !ok {
		return line, true
	}
	message["content"] = stripImageBlocks(content)
	return line, true
}

// stripImageBlocks removes "image" content entries and walks into nested
// tool_result content arrays so nested images are also removed.
func stripImageBlocks(content []any) []any {
	out := make([]any, 0, len(content))
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if typ, _ := block["type"].(string); typ == "image" {
			continue
		}
		if nested, ok := block["content"].([]any); ok {
			block["content"] = stripImageBlocks(nested)
		}
		out = append(out, block)
	}
	return out
}
