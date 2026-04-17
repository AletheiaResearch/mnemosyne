package openclaw

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
		root = source.Expand("~/.openclaw/agents")
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "openclaw"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	files, err := source.CollectFiles(s.root, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl" && filepath.Base(filepath.Dir(path)) == "sessions"
	})
	if err != nil {
		return nil, err
	}

	type aggregate struct {
		records int
		bytes   int64
	}
	grouped := make(map[string]*aggregate)
	for _, path := range files {
		cwd := source.EstimateUnknownLabel(s.Name())
		_ = source.ReadJSONLines(path, func(line int, raw []byte) error {
			if line != 1 {
				return os.ErrClosed
			}
			var header map[string]any
			if err := json.Unmarshal(raw, &header); err != nil {
				return os.ErrClosed
			}
			candidate := source.ExtractString(header, "cwd")
			if candidate != "" {
				cwd = candidate
			}
			return os.ErrClosed
		})
		if grouped[cwd] == nil {
			grouped[cwd] = &aggregate{}
		}
		grouped[cwd].records++
		if info, err := os.Stat(path); err == nil {
			grouped[cwd].bytes += info.Size()
		}
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for id, agg := range grouped {
		label := source.DisplayLabelFromPath(id)
		if id == source.EstimateUnknownLabel(s.Name()) {
			label = id
		}
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "openclaw:" + label,
			Origin:           s.Name(),
			EstimatedRecords: agg.records,
			EstimatedBytes:   agg.bytes,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	files, err := source.CollectFiles(s.root, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl" && filepath.Base(filepath.Dir(path)) == "sessions"
	})
	if err != nil {
		return err
	}

	for _, path := range files {
		record, err := s.parseFile(path)
		if err != nil {
			continue
		}
		switch {
		case grouping.ID == source.EstimateUnknownLabel(s.Name()):
			if strings.TrimSpace(record.WorkingDir) != "" {
				continue
			}
		case record.WorkingDir != grouping.ID:
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
		return filepath.Ext(path) == ".jsonl" && filepath.Base(filepath.Dir(path)) == "sessions"
	})
	if err != nil {
		return schema.Record{}, false, err
	}
	for _, path := range files {
		record, err := s.parseFile(path)
		if err != nil {
			continue
		}
		if record.RecordID == sessionID {
			return record, true, nil
		}
	}
	return schema.Record{}, false, nil
}

func (s *Source) parseFile(path string) (schema.Record, error) {
	lines := make([]map[string]any, 0)
	err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
		line, err := source.DecodeJSONObject(raw)
		if err != nil {
			return nil
		}
		lines = append(lines, line)
		return nil
	})
	if err != nil || len(lines) == 0 {
		return schema.Record{}, err
	}

	header := lines[0]
	if source.ExtractString(header, "type") != "session" {
		return schema.Record{}, os.ErrInvalid
	}

	results := make(map[string]*schema.ToolOutput)
	for _, line := range lines[1:] {
		if source.ExtractString(line, "type") != "message" {
			continue
		}
		message := source.ExtractMap(line, "message")
		if source.ExtractString(message, "role") != "toolResult" {
			continue
		}
		content := make([]schema.ContentBlock, 0)
		for _, item := range source.ExtractSlice(message, "content") {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			content = append(content, schema.ContentBlock{
				Type: "text",
				Text: source.ExtractString(block, "text"),
			})
		}
		results[source.ExtractString(message, "toolCallId")] = &schema.ToolOutput{
			Content: content,
			Text:    joinBlockText(content),
		}
	}

	record := schema.Record{
		RecordID:   source.ExtractString(header, "id"),
		Origin:     s.Name(),
		WorkingDir: source.ExtractString(header, "cwd"),
		StartedAt:  source.NormalizeTimestamp(header["timestamp"]),
		Model:      "openclaw/unknown",
		Turns:      make([]schema.Turn, 0),
	}

	for _, line := range lines[1:] {
		switch source.ExtractString(line, "type") {
		case "model_change":
			provider := source.ExtractString(line, "provider")
			modelID := source.ExtractString(line, "modelId")
			record.Model = strings.Trim(strings.TrimSpace(provider+"/"+modelID), "/")
		case "message":
			message := source.ExtractMap(line, "message")
			role := source.ExtractString(message, "role")
			switch role {
			case "user", "assistant":
				turn := schema.Turn{
					Role:      role,
					Timestamp: source.NormalizeTimestamp(line["timestamp"]),
				}
				for _, item := range source.ExtractSlice(message, "content") {
					block, ok := item.(map[string]any)
					if !ok {
						continue
					}
					switch source.ExtractString(block, "type") {
					case "text":
						turn.Text += source.ExtractString(block, "text")
					case "thinking":
						if turn.Reasoning != "" {
							turn.Reasoning += "\n"
						}
						turn.Reasoning += source.ExtractString(block, "text")
					case "toolCall":
						call := schema.ToolCall{
							Tool:   source.ExtractString(block, "name"),
							Input:  source.JSONString(source.ExtractString(block, "arguments")),
							Output: results[source.ExtractString(block, "id")],
						}
						if call.Output != nil && strings.Contains(strings.ToLower(joinBlockText(call.Output.Content)), "error") {
							call.Status = "error"
						} else {
							call.Status = "success"
						}
						turn.ToolCalls = append(turn.ToolCalls, call)
					}
				}
				record.Turns = append(record.Turns, turn)
				record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
			}
		case "bashExecution":
			exitCode := 1
			if value, ok := line["exitCode"].(float64); ok {
				exitCode = int(value)
			}
			status := "error"
			if exitCode == 0 {
				status = "success"
			}
			record.Turns = append(record.Turns, schema.Turn{
				Role:      "assistant",
				Timestamp: source.NormalizeTimestamp(line["timestamp"]),
				ToolCalls: []schema.ToolCall{{
					Tool: "bash",
					Input: map[string]any{
						"command": source.ExtractString(line, "command"),
					},
					Output: &schema.ToolOutput{
						Text: source.ExtractString(line, "output"),
						Raw: map[string]any{
							"exit_code": exitCode,
						},
					},
					Status: status,
				}},
			})
		}
	}

	record.Usage = schema.Usage{
		UserTurns:      source.CountTurns(record.Turns, "user"),
		AssistantTurns: source.CountTurns(record.Turns, "assistant"),
		ToolCalls:      source.CountToolCalls(record.Turns),
	}
	if record.EndedAt == "" {
		record.EndedAt = record.StartedAt
	}
	return record, nil
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
