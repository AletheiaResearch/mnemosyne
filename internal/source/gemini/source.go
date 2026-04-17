package gemini

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	root string
}

func New(root string) *Source {
	if root == "" {
		root = source.Expand("~/.gemini/tmp")
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "gemini"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	if !source.DirExists(s.root) {
		return nil, nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	pathMap := s.pathMap()
	groupings := make([]source.Grouping, 0)
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "bin" {
			continue
		}
		chatsDir := filepath.Join(s.root, entry.Name(), "chats")
		files, err := source.CollectFiles(chatsDir, func(path string, _ os.DirEntry) bool {
			return strings.HasPrefix(filepath.Base(path), "session-") && filepath.Ext(path) == ".json"
		})
		if err != nil || len(files) == 0 {
			continue
		}
		var bytes int64
		for _, path := range files {
			info, err := os.Stat(path)
			if err == nil {
				bytes += info.Size()
			}
		}
		id := entry.Name()
		label := id[:min(8, len(id))]
		if resolved, ok := pathMap[id]; ok {
			id = resolved
			label = filepath.Base(resolved)
		}
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "gemini:" + label,
			Origin:           s.Name(),
			EstimatedRecords: len(files),
			EstimatedBytes:   bytes,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	pathMap := s.pathMap()
	seen := make(map[string]struct{})
	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || filepath.Ext(path) != ".json" || !strings.HasPrefix(filepath.Base(path), "session-") {
			return nil
		}
		digest := filepath.Base(filepath.Dir(filepath.Dir(path)))
		groupID := digest
		if resolved, ok := pathMap[digest]; ok {
			groupID = resolved
		}
		if groupID != grouping.ID {
			return nil
		}
		record, err := s.parseFile(path, grouping.DisplayLabel)
		if err != nil || len(record.Turns) == 0 {
			return nil
		}
		fingerprint := source.HashSHA256(compactRecord(record))
		if _, exists := seen[fingerprint]; exists {
			return nil
		}
		seen[fingerprint] = struct{}{}
		return emit(record)
	})
	return err
}

func (s *Source) parseFile(path string, grouping string) (schema.Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return schema.Record{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return schema.Record{}, err
	}

	record := schema.Record{
		RecordID:  firstNonEmpty(source.ExtractString(payload, "sessionId"), strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
		Origin:    s.Name(),
		Grouping:  grouping,
		StartedAt: source.NormalizeTimestamp(payload["startTime"]),
		EndedAt:   source.NormalizeTimestamp(payload["lastUpdated"]),
		Model:     "gemini/unknown",
		Turns:     make([]schema.Turn, 0),
	}

	for _, item := range source.ExtractSlice(payload, "messages") {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		timestamp := source.NormalizeTimestamp(message["timestamp"])
		switch source.ExtractString(message, "type") {
		case "user":
			turn := schema.Turn{Role: "user", Timestamp: timestamp}
			switch content := message["content"].(type) {
			case string:
				turn.Text = content
			case []any:
				var toolEvents []any
				for _, part := range content {
					block, ok := part.(map[string]any)
					if !ok {
						continue
					}
					switch {
					case source.ExtractString(block, "text") != "":
						if turn.Text != "" {
							turn.Text += "\n"
						}
						turn.Text += source.ExtractString(block, "text")
					case source.ExtractMap(block, "inlineData") != nil:
						data := source.ExtractMap(block, "inlineData")
						turn.Attachments = append(turn.Attachments, schema.ContentBlock{
							Type:      attachmentType(source.ExtractString(data, "mimeType")),
							MediaType: source.ExtractString(data, "mimeType"),
							Data:      source.ExtractString(data, "data"),
						})
					case source.ExtractMap(block, "fileData") != nil:
						data := source.ExtractMap(block, "fileData")
						turn.Attachments = append(turn.Attachments, schema.ContentBlock{
							Type:      attachmentType(source.ExtractString(data, "mimeType")),
							MediaType: source.ExtractString(data, "mimeType"),
							URL:       source.ExtractString(data, "fileUri"),
						})
					case source.ExtractMap(block, "functionCall") != nil:
						toolEvents = append(toolEvents, block["functionCall"])
					case source.ExtractMap(block, "functionResponse") != nil:
						toolEvents = append(toolEvents, block["functionResponse"])
					}
				}
				if len(toolEvents) > 0 {
					if turn.Extensions == nil {
						turn.Extensions = make(map[string]any)
					}
					turn.Extensions["tool_events"] = toolEvents
				}
			}
			record.Turns = append(record.Turns, turn)
		case "gemini":
			turn := schema.Turn{
				Role:      "assistant",
				Timestamp: timestamp,
				Text:      source.ExtractString(message, "content"),
			}
			for _, thought := range source.ExtractSlice(message, "thoughts") {
				block, ok := thought.(map[string]any)
				if !ok {
					continue
				}
				text := source.ExtractString(block, "description")
				if text != "" {
					if turn.Reasoning != "" {
						turn.Reasoning += "\n"
					}
					turn.Reasoning += text
				}
			}
			for _, tool := range source.ExtractSlice(message, "toolCalls") {
				callMap, ok := tool.(map[string]any)
				if !ok {
					continue
				}
				call := schema.ToolCall{
					Tool: source.ExtractString(callMap, "name", "toolName"),
				}
				if args := callMap["args"]; args != nil {
					call.Input = args
				} else {
					call.Input = callMap
				}
				if output := callMap["output"]; output != nil {
					call.Output = &schema.ToolOutput{Raw: output}
				}
				call.Status = firstNonEmpty(source.ExtractString(callMap, "status"), statusFromOutput(call.Output))
				turn.ToolCalls = append(turn.ToolCalls, call)
			}
			tokens := source.ExtractMap(message, "tokens")
			record.Usage.InputTokens += intNumber(tokens["input"]) + intNumber(tokens["cached"])
			record.Usage.OutputTokens += intNumber(tokens["output"])
			record.Model = firstNonEmpty(source.ExtractString(message, "model"), record.Model)
			record.Turns = append(record.Turns, turn)
		}
	}

	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)
	return record, nil
}

func (s *Source) pathMap() map[string]string {
	home := source.HomeDir()
	if home == "" {
		return map[string]string{}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(home, entry.Name())
		out[source.HashSHA256(filepath.Clean(path))] = filepath.Clean(path)
	}
	return out
}

func compactRecord(record schema.Record) string {
	record.Grouping = ""
	data, _ := json.Marshal(record)
	return string(data)
}

func attachmentType(mime string) string {
	if strings.HasPrefix(mime, "image/") {
		return "image"
	}
	return "document"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func intNumber(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case json.Number:
		v, _ := typed.Int64()
		return int(v)
	default:
		return 0
	}
}

func statusFromOutput(output *schema.ToolOutput) string {
	if output == nil {
		return ""
	}
	if strings.Contains(strings.ToLower(output.Text), "error") {
		return "error"
	}
	return "success"
}
