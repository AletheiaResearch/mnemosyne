package opencode

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	_ "modernc.org/sqlite"
)

type sessionSpec struct {
	ID          string
	Directory   string
	TimeCreated string
	TimeUpdated string
	Messages    []messageSpec
}

type messageSpec struct {
	ID      string
	Role    string
	Model   *modelSpec
	Tokens  *tokenSpec
	Created string
	Parts   []map[string]any
}

type modelSpec struct {
	ProviderID string
	ModelID    string
}

type tokenSpec struct {
	Input      int
	Output     int
	CacheRead  int
	CacheWrite int
}

func buildOpencodeDB(t *testing.T, sessions []sessionSpec) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	schemaStmts := []string{
		`create table session (id text primary key, directory text, time_created text, time_updated text)`,
		`create table message (id text primary key, session_id text, data blob, time_created text)`,
		`create table part (id integer primary key autoincrement, message_id text, data blob, time_created text)`,
	}
	for _, stmt := range schemaStmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	partCount := 0
	for _, sess := range sessions {
		if _, err := db.Exec(
			`insert into session (id, directory, time_created, time_updated) values (?, ?, ?, ?)`,
			sess.ID, sess.Directory, sess.TimeCreated, sess.TimeUpdated,
		); err != nil {
			t.Fatal(err)
		}
		for _, msg := range sess.Messages {
			payload := map[string]any{"role": msg.Role}
			if msg.Model != nil {
				payload["model"] = map[string]any{
					"providerID": msg.Model.ProviderID,
					"modelID":    msg.Model.ModelID,
				}
			}
			if msg.Tokens != nil {
				payload["tokens"] = map[string]any{
					"input":  msg.Tokens.Input,
					"output": msg.Tokens.Output,
					"cache": map[string]any{
						"read":  msg.Tokens.CacheRead,
						"write": msg.Tokens.CacheWrite,
					},
				}
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(
				`insert into message (id, session_id, data, time_created) values (?, ?, ?, ?)`,
				msg.ID, sess.ID, raw, msg.Created,
			); err != nil {
				t.Fatal(err)
			}
			for _, part := range msg.Parts {
				raw, err := json.Marshal(part)
				if err != nil {
					t.Fatal(err)
				}
				partCount++
				if _, err := db.Exec(
					`insert into part (message_id, data, time_created) values (?, ?, ?)`,
					msg.ID, raw, msg.Created,
				); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return dbPath
}

func TestDiscoverGroupsSessionsByDirectory(t *testing.T) {
	t.Parallel()

	dbPath := buildOpencodeDB(t, []sessionSpec{
		{ID: "s1", Directory: "/tmp/a", TimeCreated: "2026-04-17T10:00:00Z", TimeUpdated: "2026-04-17T10:01:00Z"},
		{ID: "s2", Directory: "/tmp/a", TimeCreated: "2026-04-17T10:02:00Z", TimeUpdated: "2026-04-17T10:03:00Z"},
		{ID: "s3", Directory: "", TimeCreated: "2026-04-17T10:04:00Z", TimeUpdated: "2026-04-17T10:05:00Z"},
	})

	src := New(dbPath)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 2 {
		t.Fatalf("expected 2 groupings, got %d: %+v", len(groupings), groupings)
	}

	byID := make(map[string]source.Grouping)
	for _, g := range groupings {
		byID[g.ID] = g
	}
	if g, ok := byID["/tmp/a"]; !ok || g.EstimatedRecords != 2 {
		t.Fatalf("unexpected /tmp/a grouping: %+v", g)
	}
	if g, ok := byID[source.EstimateUnknownLabel("opencode")]; !ok || g.EstimatedRecords != 1 {
		t.Fatalf("expected unknown grouping with 1 record, got %+v", g)
	}
}

func TestExtractAssemblesPartsIntoTurns(t *testing.T) {
	t.Parallel()

	dbPath := buildOpencodeDB(t, []sessionSpec{
		{
			ID:          "s1",
			Directory:   "/tmp/project",
			TimeCreated: "2026-04-17T10:00:00Z",
			TimeUpdated: "2026-04-17T10:10:00Z",
			Messages: []messageSpec{
				{
					ID:      "m1",
					Role:    "user",
					Created: "2026-04-17T10:00:10Z",
					Parts: []map[string]any{
						{"type": "text", "text": "hello"},
					},
				},
				{
					ID:      "m2",
					Role:    "assistant",
					Model:   &modelSpec{ProviderID: "anthropic", ModelID: "claude-sonnet-4-6"},
					Tokens:  &tokenSpec{Input: 100, Output: 50, CacheRead: 10, CacheWrite: 5},
					Created: "2026-04-17T10:00:20Z",
					Parts: []map[string]any{
						{"type": "reasoning", "text": "thinking..."},
						{"type": "text", "text": "done"},
						{"type": "tool", "tool": "shell",
							"state": map[string]any{
								"status": "completed",
								"input":  map[string]any{"cmd": "ls"},
								"output": "ok",
							}},
						{"type": "file", "mime": "image/png", "url": "data:image/png;base64,AAA"},
					},
				},
			},
		},
	})

	src := New(dbPath)
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/project",
		DisplayLabel: "opencode:project",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	record := records[0]
	if record.Model != "anthropic/claude-sonnet-4-6" {
		t.Fatalf("model = %q", record.Model)
	}
	if record.WorkingDir != "/tmp/project" {
		t.Fatalf("working_dir = %q", record.WorkingDir)
	}
	// cache read + write + input = 10+5+100
	if record.Usage.InputTokens != 115 || record.Usage.OutputTokens != 50 {
		t.Fatalf("usage = %+v", record.Usage)
	}
	if record.Usage.UserTurns != 1 || record.Usage.AssistantTurns != 1 {
		t.Fatalf("turn counts = %+v", record.Usage)
	}
	if record.Usage.ToolCalls != 1 {
		t.Fatalf("tool calls = %d", record.Usage.ToolCalls)
	}

	if len(record.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(record.Turns))
	}
	assistant := record.Turns[1]
	if assistant.Reasoning != "thinking..." {
		t.Fatalf("reasoning = %q", assistant.Reasoning)
	}
	if assistant.Text != "done" {
		t.Fatalf("text = %q", assistant.Text)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("tool calls = %+v", assistant.ToolCalls)
	}
	if assistant.ToolCalls[0].Status != "success" {
		t.Fatalf("expected 'completed' to be normalized to 'success', got %q", assistant.ToolCalls[0].Status)
	}
	if len(assistant.Attachments) != 1 || assistant.Attachments[0].Type != "image" {
		t.Fatalf("attachments = %+v", assistant.Attachments)
	}
	if assistant.Attachments[0].Data == "" {
		t.Fatalf("expected data URL to populate attachment.Data, got %+v", assistant.Attachments[0])
	}
}

func TestExtractFiltersByDirectory(t *testing.T) {
	t.Parallel()

	dbPath := buildOpencodeDB(t, []sessionSpec{
		{
			ID:          "keep",
			Directory:   "/tmp/keep",
			TimeCreated: "2026-04-17T10:00:00Z",
			TimeUpdated: "2026-04-17T10:10:00Z",
			Messages: []messageSpec{
				{ID: "m1", Role: "user", Created: "2026-04-17T10:00:10Z",
					Parts: []map[string]any{{"type": "text", "text": "hi"}}},
			},
		},
		{
			ID:          "drop",
			Directory:   "/tmp/drop",
			TimeCreated: "2026-04-17T10:00:00Z",
			TimeUpdated: "2026-04-17T10:10:00Z",
			Messages: []messageSpec{
				{ID: "m2", Role: "user", Created: "2026-04-17T10:00:10Z",
					Parts: []map[string]any{{"type": "text", "text": "hi"}}},
			},
		},
	})

	src := New(dbPath)

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/keep",
		DisplayLabel: "opencode:keep",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RecordID != "keep" {
		t.Fatalf("expected only keep, got %+v", records)
	}
}

func TestLookupSessionReturnsSessionWithoutSchemaCoupling(t *testing.T) {
	t.Parallel()

	dbPath := buildOpencodeDB(t, []sessionSpec{
		{
			ID:          "target",
			Directory:   "/tmp/project",
			TimeCreated: "2026-04-17T10:00:00Z",
			TimeUpdated: "2026-04-17T10:10:00Z",
			Messages: []messageSpec{
				{ID: "m1", Role: "user", Created: "2026-04-17T10:00:10Z",
					Parts: []map[string]any{{"type": "text", "text": "hello"}}},
				{ID: "m2", Role: "assistant", Created: "2026-04-17T10:00:20Z",
					Parts: []map[string]any{{"type": "text", "text": "world"}}},
			},
		},
	})

	src := New(dbPath)
	record, found, err := src.LookupSession(t.Context(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected session to be found")
	}
	if record.RecordID != "target" {
		t.Fatalf("record id = %q", record.RecordID)
	}
	if len(record.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(record.Turns))
	}

	_, found, err = src.LookupSession(t.Context(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected not found")
	}
}

func TestDiscoverReturnsNilWhenDatabaseMissing(t *testing.T) {
	t.Parallel()

	src := New(filepath.Join(t.TempDir(), "missing.db"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings, got %+v", groupings)
	}
}
