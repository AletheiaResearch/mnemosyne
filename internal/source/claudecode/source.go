package claudecode

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	root string
}

func New(root string) *Source {
	if root == "" {
		root = source.Expand("~/.claude/projects")
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "claudecode"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	if !source.DirExists(s.root) {
		return nil, nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}
	groupings := make([]source.Grouping, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		projectDir := filepath.Join(s.root, entry.Name())
		files, err := source.CollectFiles(projectDir, func(path string, _ os.DirEntry) bool {
			return filepath.Ext(path) == ".jsonl"
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
		groupings = append(groupings, source.Grouping{
			ID:               entry.Name(),
			DisplayLabel:     "claudecode:" + s.groupingLabel(files, entry.Name()),
			Origin:           s.Name(),
			EstimatedRecords: len(files),
			EstimatedBytes:   bytes,
		})
	}
	return groupings, nil
}

func (s *Source) groupingLabel(files []string, dirName string) string {
	cwd := probeCwd(files)
	if cwd != "" {
		if base := filepath.Base(cwd); base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return decodeProjectName(dirName)
}

func probeCwd(files []string) string {
	for _, path := range files {
		var found string
		_ = source.ReadJSONLines(path, func(_ int, raw []byte) error {
			line, err := source.DecodeJSONObject(raw)
			if err != nil {
				return nil
			}
			if cwd := source.ExtractString(line, "cwd"); cwd != "" {
				found = cwd
				return os.ErrClosed
			}
			return nil
		})
		if found != "" {
			return found
		}
	}
	return ""
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, extractCtx source.ExtractionContext, emit func(schema.Record) error) error {
	projectDir := filepath.Join(s.root, grouping.ID)
	if !source.DirExists(projectDir) {
		return nil
	}

	files, err := source.CollectFiles(projectDir, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl" && filepath.Dir(path) == projectDir
	})
	if err != nil {
		return err
	}
	for _, path := range files {
		record, err := s.parseSession(path)
		if err != nil {
			source.ReportWarning(extractCtx, "claudecode skipped %s: %v", path, err)
			continue
		}
		if len(record.Turns) == 0 {
			continue
		}
		record.Grouping = grouping.DisplayLabel
		if err := emit(record); err != nil {
			return err
		}
	}

	sessionDirs, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}
	for _, entry := range sessionDirs {
		if !entry.IsDir() {
			continue
		}
		subagentDir := filepath.Join(projectDir, entry.Name(), "subagents")
		if !source.DirExists(subagentDir) {
			continue
		}
		record, err := s.parseSubagents(projectDir, entry.Name(), subagentDir)
		if err != nil {
			source.ReportWarning(extractCtx, "claudecode skipped %s: %v", subagentDir, err)
			continue
		}
		if len(record.Turns) == 0 {
			continue
		}
		record.Grouping = grouping.DisplayLabel
		if err := emit(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) LookupSession(_ context.Context, sessionID string) (schema.Record, bool, error) {
	files, err := source.CollectFiles(s.root, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl" && filepath.Base(path) == sessionID+".jsonl"
	})
	if err != nil {
		return schema.Record{}, false, err
	}
	for _, path := range files {
		record, err := s.parseSession(path)
		if err != nil {
			continue
		}
		if record.RecordID == sessionID {
			return record, true, nil
		}
	}
	return schema.Record{}, false, nil
}

func (s *Source) parseSession(path string) (schema.Record, error) {
	entries := make([]map[string]any, 0)
	err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
		line, err := source.DecodeJSONObject(raw)
		if err == nil {
			entries = append(entries, line)
		}
		return nil
	})
	if err != nil {
		return schema.Record{}, err
	}
	recordID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return assembleClaudeRecord(entries, recordID), nil
}

func (s *Source) parseSubagents(projectDir, sessionID, subagentDir string) (schema.Record, error) {
	files, err := source.CollectFiles(subagentDir, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl"
	})
	if err != nil || len(files) == 0 {
		return schema.Record{}, err
	}
	entries := make([]map[string]any, 0)
	for _, path := range files {
		if err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
			line, err := source.DecodeJSONObject(raw)
			if err == nil {
				entries = append(entries, line)
			}
			return nil
		}); err != nil {
			return schema.Record{}, err
		}
	}
	slices.SortFunc(entries, func(a, b map[string]any) int {
		return strings.Compare(source.NormalizeTimestamp(a["timestamp"]), source.NormalizeTimestamp(b["timestamp"]))
	})

	recordID := sessionID
	if source.FileExists(filepath.Join(projectDir, sessionID+".jsonl")) {
		recordID += ":subagents"
	}
	return assembleClaudeRecord(entries, recordID), nil
}

func assembleClaudeRecord(entries []map[string]any, recordID string) schema.Record {
	record := schema.Record{
		RecordID: recordID,
		Origin:   "claudecode",
		Model:    "",
		Turns:    make([]schema.Turn, 0),
	}
	results := make(map[string]*schema.ToolOutput)

	for _, entry := range entries {
		if source.ExtractString(entry, "type") != "user" {
			continue
		}
		message := source.ExtractMap(entry, "message")
		for _, item := range source.ExtractSlice(message, "content") {
			block, ok := item.(map[string]any)
			if !ok || source.ExtractString(block, "type") != "tool_result" {
				continue
			}
			content := parseToolResultContent(block["content"])
			output := &schema.ToolOutput{Content: content, Text: joinBlockText(content)}
			if raw := source.ExtractMap(entry, "toolUseResult"); len(raw) > 0 {
				output.Raw = raw
			}
			results[source.ExtractString(block, "tool_use_id")] = output
		}
	}

	for _, entry := range entries {
		typ := source.ExtractString(entry, "type")
		message := source.ExtractMap(entry, "message")
		timestamp := source.NormalizeTimestamp(entry["timestamp"])
		if record.StartedAt == "" {
			record.StartedAt = timestamp
		}
		record.EndedAt = source.LatestTimestamp(record.EndedAt, timestamp)
		if record.WorkingDir == "" {
			record.WorkingDir = source.ExtractString(entry, "cwd")
		}
		if record.Branch == "" {
			record.Branch = source.ExtractString(entry, "gitBranch")
		}

		switch typ {
		case "user":
			text := ""
			switch content := message["content"].(type) {
			case string:
				text = content
			case []any:
				for _, item := range content {
					block, ok := item.(map[string]any)
					if !ok {
						continue
					}
					if source.ExtractString(block, "type") == "text" {
						text += source.ExtractString(block, "text")
					}
				}
			}
			if strings.TrimSpace(text) != "" {
				record.Turns = append(record.Turns, schema.Turn{Role: "user", Timestamp: timestamp, Text: text})
			}
		case "assistant":
			turn := schema.Turn{Role: "assistant", Timestamp: timestamp}
			for _, item := range source.ExtractSlice(message, "content") {
				block, ok := item.(map[string]any)
				if !ok {
					continue
				}
				switch source.ExtractString(block, "type") {
				case "text":
					turn.Text += source.ExtractString(block, "text")
				case "thinking":
					thinkingText := source.ExtractString(block, "thinking")
					if thinkingText == "" {
						thinkingText = source.ExtractString(block, "text")
					}
					if thinkingText != "" {
						if turn.Reasoning != "" {
							turn.Reasoning += "\n"
						}
						turn.Reasoning += thinkingText
					}
				case "tool_use":
					call := schema.ToolCall{
						Tool:   source.ExtractString(block, "name"),
						Input:  block["input"],
						Output: results[source.ExtractString(block, "id")],
					}
					if call.Output != nil {
						call.Status = "success"
					}
					turn.ToolCalls = append(turn.ToolCalls, call)
				}
			}
			usage := source.ExtractMap(message, "usage")
			record.Usage.InputTokens += intNumber(usage["input_tokens"]) + intNumber(usage["cache_read_input_tokens"])
			record.Usage.OutputTokens += intNumber(usage["output_tokens"])
			if record.Model == "" {
				record.Model = source.ExtractString(message, "model")
			}
			if turn.Text != "" || turn.Reasoning != "" || len(turn.ToolCalls) > 0 {
				record.Turns = append(record.Turns, turn)
			}
		}
	}

	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)
	if record.Model == "" {
		record.Model = "claudecode/unknown"
	}
	return record
}

func parseToolResultContent(value any) []schema.ContentBlock {
	switch typed := value.(type) {
	case string:
		return []schema.ContentBlock{{Type: "text", Text: typed}}
	case []any:
		blocks := make([]schema.ContentBlock, 0, len(typed))
		for _, item := range typed {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			blocks = append(blocks, schema.ContentBlock{
				Type: "text",
				Text: source.ExtractString(block, "text"),
			})
		}
		return blocks
	default:
		return nil
	}
}

func joinBlockText(blocks []schema.ContentBlock) string {
	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func decodeProjectName(name string) string {
	if name == "" {
		return name
	}
	decoded := strings.ReplaceAll(name, "-", string(filepath.Separator))
	return source.DisplayLabelFromPath(decoded)
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
