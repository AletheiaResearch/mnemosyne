package cursor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	dbPath string
}

func New(dbPath string) *Source {
	if dbPath == "" {
		dbPath = defaultPath()
	}
	return &Source{dbPath: dbPath}
}

func (s *Source) Name() string {
	return "cursor"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`select key, value from cursorDiskKV where key like 'composerData:%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type aggregate struct {
		records int
	}
	grouped := make(map[string]*aggregate)
	total := 0
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		composerID := strings.TrimPrefix(key, "composerData:")
		workspace := s.workspaceForComposer(db, composerID, value)
		if workspace == "" {
			workspace = source.EstimateUnknownLabel(s.Name())
		}
		if grouped[workspace] == nil {
			grouped[workspace] = &aggregate{}
		}
		grouped[workspace].records++
		total++
	}

	var fileSize int64
	if info, err := os.Stat(s.dbPath); err == nil {
		fileSize = info.Size()
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for id, agg := range grouped {
		label := id
		if id != source.EstimateUnknownLabel(s.Name()) {
			label = filepath.Base(id)
		}
		size := int64(0)
		if total > 0 {
			size = fileSize / int64(total) * int64(agg.records)
		}
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "cursor:" + label,
			Origin:           s.Name(),
			EstimatedRecords: agg.records,
			EstimatedBytes:   size,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	rows, err := db.Query(`select key, value from cursorDiskKV where key like 'composerData:%'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		composerID := strings.TrimPrefix(key, "composerData:")
		workspace := s.workspaceForComposer(db, composerID, value)
		if workspace == "" {
			workspace = source.EstimateUnknownLabel(s.Name())
		}
		if workspace != grouping.ID {
			continue
		}
		record, err := s.extractComposer(db, composerID, value, grouping.DisplayLabel, workspace)
		if err != nil || len(record.Turns) == 0 {
			continue
		}
		if err := emit(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) LookupSession(_ context.Context, sessionID string) (schema.Record, bool, error) {
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return schema.Record{}, false, nil
	}
	defer db.Close()

	var value []byte
	if err := db.QueryRow(`select value from cursorDiskKV where key = ?`, "composerData:"+sessionID).Scan(&value); err != nil {
		if err == sql.ErrNoRows {
			return schema.Record{}, false, nil
		}
		return schema.Record{}, false, err
	}
	workspace := s.workspaceForComposer(db, sessionID, value)
	record, err := s.extractComposer(db, sessionID, value, "", workspace)
	if err != nil {
		return schema.Record{}, false, err
	}
	return record, len(record.Turns) > 0, nil
}

func (s *Source) workspaceForComposer(db *sql.DB, composerID string, value []byte) string {
	headers := composerHeaders(value)
	limit := min(5, len(headers))
	for _, bubbleID := range headers[:limit] {
		payload, err := s.loadBubble(db, composerID, bubbleID)
		if err != nil {
			continue
		}
		uris := source.ExtractSlice(payload, "workspaceUris")
		if len(uris) == 0 {
			continue
		}
		if uri, ok := uris[0].(string); ok && uri != "" {
			return strings.TrimPrefix(uri, "file://")
		}
	}
	return ""
}

func (s *Source) extractComposer(db *sql.DB, composerID string, value []byte, grouping string, workspace string) (schema.Record, error) {
	headers := composerHeaders(value)
	record := schema.Record{
		RecordID:   composerID,
		Origin:     s.Name(),
		Grouping:   grouping,
		Model:      "cursor/unknown",
		WorkingDir: workspace,
		Turns:      make([]schema.Turn, 0),
	}

	for _, bubbleID := range headers {
		payload, err := s.loadBubble(db, composerID, bubbleID)
		if err != nil {
			continue
		}
		turnType := source.IntNumber(payload["type"])
		turn := schema.Turn{
			Timestamp: source.NormalizeTimestamp(payload["createdAt"]),
			Text:      source.ExtractString(payload, "text"),
		}
		if thinking := source.ExtractMap(payload, "thinking"); thinking != nil {
			turn.Reasoning = source.ExtractString(thinking, "text")
		}
		if modelInfo := source.ExtractMap(payload, "modelInfo"); modelInfo != nil {
			record.Model = source.FirstNonEmpty(source.ExtractString(modelInfo, "modelName"), record.Model)
		}
		if tokenCount := source.ExtractMap(payload, "tokenCount"); tokenCount != nil {
			record.Usage.InputTokens += source.IntNumber(tokenCount["inputTokens"])
			record.Usage.OutputTokens += source.IntNumber(tokenCount["outputTokens"])
		}
		if tool := source.ExtractMap(payload, "toolFormerData"); tool != nil {
			params := normalizeCursorPayload(tool["params"])
			if nested, ok := params.(map[string]any); ok {
				params = unwrapCursorToolParameters(nested)
			}
			result := normalizeCursorPayload(tool["result"])
			call := schema.ToolCall{
				Tool:   normalizeCursorToolName(source.ExtractString(tool, "name")),
				Input:  params,
				Status: normalizeCursorStatus(tool["status"]),
			}
			if result != nil {
				call.Output = &schema.ToolOutput{Raw: result}
				if text, ok := result.(string); ok {
					call.Output.Text = text
				}
			}
			turn.ToolCalls = append(turn.ToolCalls, call)
		}
		switch turnType {
		case 1:
			turn.Role = "user"
		case 2:
			turn.Role = "assistant"
		default:
			continue
		}
		record.Turns = append(record.Turns, turn)
		record.StartedAt = source.EarliestTimestamp(record.StartedAt, turn.Timestamp)
		record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
	}

	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)
	return record, nil
}

func (s *Source) loadBubble(db *sql.DB, composerID, bubbleID string) (map[string]any, error) {
	key := fmt.Sprintf("bubbleId:%s:%s", composerID, bubbleID)
	var raw []byte
	if err := db.QueryRow(`select value from cursorDiskKV where key = ?`, key).Scan(&raw); err != nil {
		return nil, err
	}
	return source.DecodeJSONObject(raw)
}

func composerHeaders(value []byte) []string {
	var payload map[string]any
	if err := json.Unmarshal(value, &payload); err != nil {
		return nil
	}
	for _, key := range []string{"fullConversationHeadersOnly", "conversation"} {
		items := source.ExtractSlice(payload, key)
		if len(items) == 0 {
			continue
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			header, ok := item.(map[string]any)
			if !ok {
				continue
			}
			bubbleID := source.ExtractString(header, "bubbleId")
			if bubbleID != "" {
				out = append(out, bubbleID)
			}
		}
		return out
	}
	return nil
}

func normalizeCursorPayload(value any) any {
	switch typed := value.(type) {
	case string:
		return source.JSONString(typed)
	default:
		return typed
	}
}

func unwrapCursorToolParameters(payload map[string]any) any {
	tools := source.ExtractSlice(payload, "tools")
	if len(tools) == 0 {
		return payload
	}
	first, ok := tools[0].(map[string]any)
	if !ok {
		return payload
	}
	parameters := first["parameters"]
	if parameters == nil {
		return payload
	}
	return parameters
}

func normalizeCursorToolName(name string) string {
	switch {
	case strings.HasPrefix(name, "mcp_"):
		parts := strings.Split(name, "_")
		return parts[len(parts)-1]
	case strings.HasPrefix(name, "mcp-"):
		parts := strings.Split(name, "-")
		return parts[len(parts)-1]
	default:
		return name
	}
}

func normalizeCursorStatus(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return source.ExtractString(typed, "status", "state")
	default:
		return ""
	}
}

func defaultPath() string {
	home := source.HomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb")
	default:
		return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	}
}
