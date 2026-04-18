package nativeexport

import (
	"context"
	"path/filepath"
	"strings"
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
	// Claude Code session files are named <UUID>.jsonl; the filename is the
	// session's stable logical id (source.go derives record_id the same way).
	// No in-band hook is needed — the filename survives moves between
	// project directories, which is exactly the identity we want.
	if result.SessionID == "" {
		base := filepath.Base(srcPath)
		result.SessionID = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return result, nil
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
