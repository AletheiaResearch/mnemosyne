package nativeexport

import "context"

type codexRedactor struct{}

// Codex returns the Redactor for Codex session JSONL files
// (~/.codex/sessions/<date>/<session>.jsonl).
func Codex() Redactor { return codexRedactor{} }

func (codexRedactor) Format() string { return "codex" }

func (r codexRedactor) Redact(ctx context.Context, srcPath, dstPath string, opts Options) (Result, error) {
	return redactFile(ctx, srcPath, dstPath, r.Format(), opts, codexPreProcess)
}

// codexPreProcess strips image attachments from Codex session lines when the
// caller opted out of attachments. Two variants carry image payloads:
//
//   - response_item + payload.type == "message": payload.content[] may include
//     entries with type "input_image".
//   - event_msg + payload.type == "user_message": payload.images and
//     payload.local_images hold user-attached images (strings or objects).
//
// Everything else passes through unchanged; the generic walker handles
// string-level redaction.
func codexPreProcess(line map[string]any, opts Options) (map[string]any, bool) {
	if opts.AttachImages {
		return line, true
	}
	payload, ok := line["payload"].(map[string]any)
	if !ok {
		return line, true
	}
	switch line["type"] {
	case "response_item":
		if payload["type"] == "message" {
			if content, ok := payload["content"].([]any); ok {
				payload["content"] = filterOutType(content, "input_image")
			}
		}
	case "event_msg":
		if payload["type"] == "user_message" {
			delete(payload, "images")
			delete(payload, "local_images")
		}
	}
	return line, true
}

// filterOutType returns content with any map entry whose "type" equals
// dropType removed. Non-map entries are preserved untouched.
func filterOutType(content []any, dropType string) []any {
	out := make([]any, 0, len(content))
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if typ, _ := block["type"].(string); typ == dropType {
			continue
		}
		out = append(out, block)
	}
	return out
}
