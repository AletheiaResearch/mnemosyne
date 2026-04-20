package codex

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
	activeRoot  string
	archiveRoot string
}

func New(activeRoot, archiveRoot string) *Source {
	if activeRoot == "" {
		activeRoot = source.Expand("~/.codex/sessions")
	}
	if archiveRoot == "" {
		archiveRoot = source.Expand("~/.codex/archived_sessions")
	}
	return &Source{activeRoot: activeRoot, archiveRoot: archiveRoot}
}

func (s *Source) Name() string {
	return "codex"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	files, err := s.sessionFiles()
	if err != nil {
		return nil, err
	}

	type aggregate struct {
		records int
		bytes   int64
	}
	grouped := make(map[string]*aggregate)
	for _, path := range files {
		groupID := s.probeGrouping(path)
		if grouped[groupID] == nil {
			grouped[groupID] = &aggregate{}
		}
		grouped[groupID].records++
		if info, err := os.Stat(path); err == nil {
			grouped[groupID].bytes += info.Size()
		}
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for id, agg := range grouped {
		label := id
		if id != source.EstimateUnknownLabel(s.Name()) {
			label = filepath.Base(id)
		}
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "codex:" + label,
			Origin:           s.Name(),
			EstimatedRecords: agg.records,
			EstimatedBytes:   agg.bytes,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, extractCtx source.ExtractionContext, emit func(schema.Record) error) error {
	files, err := s.sessionFiles()
	if err != nil {
		return err
	}
	for _, path := range files {
		record, err := s.parseFile(path)
		if err != nil {
			source.ReportWarning(extractCtx, "codex skipped %s: %v", path, err)
			continue
		}
		if len(record.Turns) == 0 {
			continue
		}
		groupID := record.WorkingDir
		if groupID == "" {
			groupID = source.EstimateUnknownLabel(s.Name())
		}
		if groupID != grouping.ID {
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
	files, err := s.sessionFiles()
	if err != nil {
		return schema.Record{}, false, err
	}
	for _, path := range files {
		if filepath.Base(path) != sessionID+".jsonl" {
			continue
		}
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

func (s *Source) sessionFiles() ([]string, error) {
	all := make([]string, 0)
	for _, root := range []string{s.activeRoot, s.archiveRoot} {
		files, err := source.CollectFiles(root, func(path string, _ os.DirEntry) bool {
			return filepath.Ext(path) == ".jsonl"
		})
		if err != nil {
			return nil, err
		}
		all = append(all, files...)
	}
	return all, nil
}

func (s *Source) probeGrouping(path string) string {
	groupID := source.EstimateUnknownLabel(s.Name())
	_ = source.ReadJSONLines(path, func(_ int, raw []byte) error {
		line, err := source.DecodeJSONObject(raw)
		if err != nil {
			return nil
		}
		payload := source.ExtractMap(line, "payload")
		switch source.ExtractString(line, "type") {
		case "session_meta":
			if cwd := source.ExtractString(payload, "cwd"); cwd != "" {
				groupID = cwd
				return os.ErrClosed
			}
		case "turn_context":
			if cwd := source.ExtractString(payload, "cwd"); cwd != "" {
				groupID = cwd
				return os.ErrClosed
			}
		}
		return nil
	})
	return groupID
}

func (s *Source) parseFile(path string) (schema.Record, error) {
	type line struct {
		Type      string         `json:"type"`
		Timestamp any            `json:"timestamp"`
		Payload   map[string]any `json:"payload"`
	}

	lines := make([]line, 0)
	err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
		var item line
		if err := json.Unmarshal(raw, &item); err == nil {
			lines = append(lines, item)
		}
		return nil
	})
	if err != nil {
		return schema.Record{}, err
	}

	results := make(map[string]*schema.ToolOutput)
	for _, item := range lines {
		if item.Type != "response_item" {
			continue
		}
		switch source.ExtractString(item.Payload, "type") {
		case "function_call_output":
			callID := source.ExtractString(item.Payload, "call_id")
			results[callID] = parseFunctionOutput(source.ExtractString(item.Payload, "output"))
		case "custom_tool_call_output":
			callID := source.ExtractString(item.Payload, "call_id")
			value := source.JSONString(source.ExtractString(item.Payload, "output"))
			output := &schema.ToolOutput{}
			switch typed := value.(type) {
			case map[string]any:
				output.Text = source.ExtractString(typed, "output")
				output.Raw = typed
			case string:
				output.Text = typed
			}
			results[callID] = output
		}
	}

	record := schema.Record{
		RecordID: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Origin:   s.Name(),
		Model:    "codex/unknown",
		Turns:    make([]schema.Turn, 0),
	}

	var pendingReasoning []string
	var pendingToolCalls []schema.ToolCall
	var pendingUserAttachments []schema.ContentBlock
	inputTokens := 0
	outputTokens := 0

	flushAssistant := func(text, timestamp string) {
		turn := schema.Turn{
			Role:      "assistant",
			Timestamp: timestamp,
			Text:      text,
			Reasoning: strings.Join(pendingReasoning, "\n"),
			ToolCalls: pendingToolCalls,
		}
		if turn.Text == "" && turn.Reasoning == "" && len(turn.ToolCalls) == 0 {
			return
		}
		appendDedup(&record.Turns, turn)
		pendingReasoning = nil
		pendingToolCalls = nil
	}

	flushUser := func(text, timestamp string, attachments []schema.ContentBlock) {
		if text == "" && len(attachments) == 0 {
			return
		}
		appendDedup(&record.Turns, schema.Turn{
			Role:        "user",
			Timestamp:   timestamp,
			Text:        text,
			Attachments: attachments,
		})
	}

	for _, item := range lines {
		timestamp := source.NormalizeTimestamp(item.Timestamp)
		record.StartedAt = source.EarliestTimestamp(record.StartedAt, timestamp)
		record.EndedAt = source.LatestTimestamp(record.EndedAt, timestamp)

		switch item.Type {
		case "session_meta":
			record.RecordID = source.FirstNonEmpty(source.ExtractString(item.Payload, "id"), record.RecordID)
			record.WorkingDir = source.FirstNonEmpty(record.WorkingDir, source.ExtractString(item.Payload, "cwd"))
			if git := source.ExtractMap(item.Payload, "git"); git != nil {
				record.Branch = source.FirstNonEmpty(record.Branch, source.ExtractString(git, "branch"))
			}
			record.Model = source.FirstNonEmpty(source.ExtractString(item.Payload, "model_provider"), record.Model)
		case "turn_context":
			record.WorkingDir = source.FirstNonEmpty(record.WorkingDir, source.ExtractString(item.Payload, "cwd"))
			record.Model = source.FirstNonEmpty(source.ExtractString(item.Payload, "model"), record.Model)
		case "response_item":
			switch source.ExtractString(item.Payload, "type") {
			case "message":
				role := source.ExtractString(item.Payload, "role")
				text, attachments := parseCodexMessageParts(source.ExtractSlice(item.Payload, "content"))
				if role == "user" {
					if len(attachments) > 0 {
						pendingUserAttachments = append(pendingUserAttachments, attachments...)
					}
					flushAssistant("", timestamp)
					flushUser(text, timestamp, pendingUserAttachments)
					pendingUserAttachments = nil
				} else if role == "assistant" {
					flushAssistant(text, timestamp)
				}
			case "function_call":
				callID := source.ExtractString(item.Payload, "call_id")
				pendingToolCalls = append(pendingToolCalls, schema.ToolCall{
					Tool:   source.ExtractString(item.Payload, "name"),
					Input:  source.JSONString(source.ExtractString(item.Payload, "arguments")),
					Output: results[callID],
					Status: statusFromOutput(results[callID]),
				})
			case "custom_tool_call":
				callID := source.ExtractString(item.Payload, "call_id")
				input := source.JSONString(source.ExtractString(item.Payload, "input"))
				if input == "" {
					input = source.JSONString(source.ExtractString(item.Payload, "arguments"))
				}
				pendingToolCalls = append(pendingToolCalls, schema.ToolCall{
					Tool:   source.ExtractString(item.Payload, "name"),
					Input:  input,
					Output: results[callID],
					Status: statusFromOutput(results[callID]),
				})
			case "reasoning":
				for _, summary := range source.ExtractSlice(item.Payload, "summary") {
					block, ok := summary.(map[string]any)
					if ok {
						text := source.ExtractString(block, "text")
						if text != "" && !slices.Contains(pendingReasoning, text) {
							pendingReasoning = append(pendingReasoning, text)
						}
					}
				}
			}
		case "event_msg":
			switch source.ExtractString(item.Payload, "type") {
			case "token_count":
				info := source.ExtractMap(item.Payload, "info")
				total := source.ExtractMap(info, "total_token_usage")
				input := source.IntNumber(total["input_tokens"]) + source.IntNumber(total["cached_input_tokens"])
				output := source.IntNumber(total["output_tokens"])
				if input > inputTokens {
					inputTokens = input
				}
				if output > outputTokens {
					outputTokens = output
				}
			case "agent_reasoning":
				text := source.ExtractString(item.Payload, "text")
				if text != "" && !slices.Contains(pendingReasoning, text) {
					pendingReasoning = append(pendingReasoning, text)
				}
			case "user_message":
				flushAssistant("", timestamp)
				attachments := append([]schema.ContentBlock{}, pendingUserAttachments...)
				attachments = append(attachments, parseCodexImages(item.Payload)...)
				flushUser(source.ExtractString(item.Payload, "message"), timestamp, attachments)
				pendingUserAttachments = nil
			case "agent_message":
				flushAssistant(source.ExtractString(item.Payload, "message"), timestamp)
			}
		}
	}

	flushAssistant("", record.EndedAt)
	record.Usage = schema.Usage{
		UserTurns:      source.CountTurns(record.Turns, "user"),
		AssistantTurns: source.CountTurns(record.Turns, "assistant"),
		ToolCalls:      source.CountToolCalls(record.Turns),
		InputTokens:    inputTokens,
		OutputTokens:   outputTokens,
	}
	record.Provenance = &schema.Provenance{
		SourcePath:   path,
		SourceID:     record.RecordID,
		SourceOrigin: "codex",
	}
	return record, nil
}

func parseCodexMessageParts(parts []any) (string, []schema.ContentBlock) {
	textParts := make([]string, 0)
	attachments := make([]schema.ContentBlock, 0)
	for _, item := range parts {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch source.ExtractString(part, "type") {
		case "input_text", "text", "output_text":
			textParts = append(textParts, source.ExtractString(part, "text"))
		case "input_image":
			imageURL := source.ExtractString(part, "image_url")
			attachments = append(attachments, codexAttachment(imageURL))
		}
	}
	return strings.Join(textParts, "\n"), attachments
}

func parseCodexImages(payload map[string]any) []schema.ContentBlock {
	attachments := make([]schema.ContentBlock, 0)
	for _, key := range []string{"images", "local_images"} {
		for _, item := range source.ExtractSlice(payload, key) {
			switch typed := item.(type) {
			case string:
				attachments = append(attachments, codexAttachment(typed))
			case map[string]any:
				attachments = append(attachments, codexAttachment(source.ExtractString(typed, "url", "path")))
			}
		}
	}
	return attachments
}

func codexAttachment(value string) schema.ContentBlock {
	if strings.HasPrefix(value, "data:") {
		return schema.ContentBlock{Type: "image", Data: value}
	}
	return schema.ContentBlock{Type: "image", URL: value}
}

func parseFunctionOutput(input string) *schema.ToolOutput {
	output := &schema.ToolOutput{Text: input}
	lines := strings.Split(input, "\n")
	raw := make(map[string]any)
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "Exit code:"):
			raw["exit_code"] = strings.TrimSpace(strings.TrimPrefix(line, "Exit code:"))
		case strings.HasPrefix(line, "Wall time:"):
			raw["wall_time"] = strings.TrimSpace(strings.TrimPrefix(line, "Wall time:"))
		}
	}
	if len(raw) > 0 {
		output.Raw = raw
	}
	return output
}

func appendDedup(turns *[]schema.Turn, turn schema.Turn) {
	if len(*turns) > 0 {
		last := (*turns)[len(*turns)-1]
		if last.Role == turn.Role && last.Text == turn.Text && last.Reasoning == turn.Reasoning &&
			len(last.Attachments) == len(turn.Attachments) && len(last.ToolCalls) == len(turn.ToolCalls) {
			return
		}
	}
	*turns = append(*turns, turn)
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
