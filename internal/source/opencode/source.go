package opencode

import (
	"context"
	"database/sql"
	"encoding/json"
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
	return "opencode"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	rows, err := db.Query(`select coalesce(directory, ''), count(*) from session group by coalesce(directory, '')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totalSessions := 0
	type aggregate struct {
		count int
	}
	grouped := make(map[string]aggregate)
	for rows.Next() {
		var directory string
		var count int
		if err := rows.Scan(&directory, &count); err != nil {
			continue
		}
		grouped[directory] = aggregate{count: count}
		totalSessions += count
	}

	var fileSize int64
	if info, err := os.Stat(s.dbPath); err == nil {
		fileSize = info.Size()
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for id, agg := range grouped {
		groupID := id
		if groupID == "" {
			groupID = source.EstimateUnknownLabel(s.Name())
		}
		label := groupID
		if groupID != source.EstimateUnknownLabel(s.Name()) {
			label = filepath.Base(groupID)
		}
		size := int64(0)
		if totalSessions > 0 {
			size = fileSize / int64(totalSessions) * int64(agg.count)
		}
		groupings = append(groupings, source.Grouping{
			ID:               groupID,
			DisplayLabel:     "opencode:" + label,
			Origin:           s.Name(),
			EstimatedRecords: agg.count,
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

	rows, err := db.Query(`select id, coalesce(directory, ''), time_created, time_updated from session`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID string
		var directory string
		var createdAt any
		var updatedAt any
		if err := rows.Scan(&sessionID, &directory, &createdAt, &updatedAt); err != nil {
			continue
		}
		groupID := directory
		if groupID == "" {
			groupID = source.EstimateUnknownLabel(s.Name())
		}
		if groupID != grouping.ID {
			continue
		}
		record, err := s.extractSession(db, sessionID, grouping.DisplayLabel, directory, createdAt, updatedAt)
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

	var directory string
	var createdAt any
	var updatedAt any
	if err := db.QueryRow(`select coalesce(directory, ''), time_created, time_updated from session where id = ?`, sessionID).Scan(&directory, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return schema.Record{}, false, nil
		}
		return schema.Record{}, false, err
	}
	record, err := s.extractSession(db, sessionID, "", directory, createdAt, updatedAt)
	if err != nil {
		return schema.Record{}, false, err
	}
	return record, len(record.Turns) > 0, nil
}

func (s *Source) extractSession(db *sql.DB, sessionID, grouping, directory string, createdAt, updatedAt any) (schema.Record, error) {
	record := schema.Record{
		RecordID:   sessionID,
		Origin:     s.Name(),
		Grouping:   grouping,
		Model:      "opencode/unknown",
		WorkingDir: directory,
		StartedAt:  source.NormalizeTimestamp(createdAt),
		EndedAt:    source.NormalizeTimestamp(updatedAt),
		Turns:      make([]schema.Turn, 0),
	}

	rows, err := db.Query(`select id, data, time_created from message where session_id = ? order by time_created asc`, sessionID)
	if err != nil {
		return record, err
	}
	defer rows.Close()

	for rows.Next() {
		var messageID string
		var raw []byte
		var created any
		if err := rows.Scan(&messageID, &raw, &created); err != nil {
			continue
		}
		payload, err := source.DecodeJSONObject(raw)
		if err != nil {
			continue
		}
		role := source.ExtractString(payload, "role")
		turn := schema.Turn{
			Role:      role,
			Timestamp: source.NormalizeTimestamp(created),
		}

		if model := source.ExtractMap(payload, "model"); model != nil {
			provider := source.ExtractString(model, "providerID")
			modelID := source.ExtractString(model, "modelID")
			record.Model = firstNonEmpty(strings.Trim(strings.TrimSpace(provider+"/"+modelID), "/"), record.Model)
		}
		if tokens := source.ExtractMap(payload, "tokens"); tokens != nil {
			record.Usage.InputTokens += intNumber(tokens["input"])
			record.Usage.OutputTokens += intNumber(tokens["output"])
			if cache := source.ExtractMap(tokens, "cache"); cache != nil {
				record.Usage.InputTokens += intNumber(cache["read"]) + intNumber(cache["write"])
			}
		}

		partRows, err := db.Query(`select data from part where message_id = ? order by time_created asc`, messageID)
		if err != nil {
			continue
		}
		for partRows.Next() {
			var partRaw []byte
			if err := partRows.Scan(&partRaw); err != nil {
				continue
			}
			part, err := source.DecodeJSONObject(partRaw)
			if err != nil {
				continue
			}
			switch source.ExtractString(part, "type") {
			case "text":
				turn.Text += source.ExtractString(part, "text")
			case "reasoning":
				if turn.Reasoning != "" {
					turn.Reasoning += "\n"
				}
				turn.Reasoning += source.ExtractString(part, "text")
			case "tool":
				state := source.ExtractMap(part, "state")
				status := source.ExtractString(state, "status")
				if status == "completed" {
					status = "success"
				}
				call := schema.ToolCall{
					Tool:   source.ExtractString(part, "tool"),
					Input:  state["input"],
					Status: status,
				}
				if output := state["output"]; output != nil {
					call.Output = &schema.ToolOutput{Raw: output}
					if text, ok := output.(string); ok {
						call.Output.Text = text
					}
				}
				turn.ToolCalls = append(turn.ToolCalls, call)
			case "file":
				urlValue := source.ExtractString(part, "url")
				block := schema.ContentBlock{
					Type:      attachmentType(source.ExtractString(part, "mime")),
					MediaType: source.ExtractString(part, "mime"),
				}
				switch {
				case strings.HasPrefix(urlValue, "data:"):
					block.Data = urlValue
				default:
					block.URL = urlValue
				}
				turn.Attachments = append(turn.Attachments, block)
			}
		}
		partRows.Close()

		if turn.Text != "" || turn.Reasoning != "" || len(turn.ToolCalls) > 0 || len(turn.Attachments) > 0 {
			record.Turns = append(record.Turns, turn)
		}
		record.StartedAt = source.EarliestTimestamp(record.StartedAt, turn.Timestamp)
		record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
	}

	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)
	return record, nil
}

func defaultPath() string {
	home := source.HomeDir()
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "opencode", "opencode.db")
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
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
