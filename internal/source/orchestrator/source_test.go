package orchestrator

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	_ "modernc.org/sqlite"
)

func TestExtractFallsBackToPerMessageModel(t *testing.T) {
	t.Parallel()

	dbPath := newTestDB(t)
	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{},
	})
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].Model != "gpt-5" {
		t.Fatalf("expected per-message model to win, got %q", records[0].Model)
	}
}

func TestExtractPrefersExternalSessionContent(t *testing.T) {
	t.Parallel()

	dbPath := newTestDB(t)
	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{
			record: schema.Record{
				RecordID:   "external-1",
				Origin:     "codex",
				Grouping:   "codex:demo",
				Model:      "gpt-5-codex",
				WorkingDir: "/other/worktree",
				Turns: []schema.Turn{
					{Role: "user", Text: "from external"},
					{Role: "assistant", Text: "preferred"},
				},
				Usage: schema.Usage{
					UserTurns:      1,
					AssistantTurns: 1,
				},
			},
			found: true,
		},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}

	record := records[0]
	if record.RecordID != "external-1" {
		t.Fatalf("expected external session id to become record id, got %q", record.RecordID)
	}
	if len(record.Turns) != 2 || record.Turns[0].Text != "from external" {
		t.Fatalf("expected external turns to be preserved, got %+v", record.Turns)
	}
	if record.WorkingDir != "/tmp/demo/feature-branch" {
		t.Fatalf("expected orchestrator working dir override, got %q", record.WorkingDir)
	}
	meta := source.ExtractMap(record.Extensions, "orchestrator")
	if source.ExtractString(meta, "content_source") != "codex" {
		t.Fatalf("expected external content source metadata, got %+v", meta)
	}
	if !boolValue(meta["external_store_preferred"]) {
		t.Fatalf("expected external store to be preferred, got %+v", meta)
	}
}

func TestExtractAdoptsScopedExternalRecordID(t *testing.T) {
	t.Parallel()

	dbPath := newTestDB(t)
	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{
			record: schema.Record{
				RecordID: "project-alpha/external-1",
				Origin:   "claudecode",
				Turns: []schema.Turn{
					{Role: "user", Text: "scoped"},
					{Role: "assistant", Text: "reply"},
				},
			},
			found: true,
		},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].RecordID != "project-alpha/external-1" {
		t.Fatalf("expected scoped external record id to win, got %q", records[0].RecordID)
	}
	meta := source.ExtractMap(records[0].Extensions, "orchestrator")
	if source.ExtractString(meta, "external_session_id") != "external-1" {
		t.Fatalf("expected bare external_session_id to remain in metadata, got %+v", meta)
	}
}

func TestExtractHandlesSchemasWithoutSentAtAndKeepsFirstMessageModel(t *testing.T) {
	t.Parallel()

	dbPath := newTestDBWithStatements(t,
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', 'git@github.com:example/demo.git', '/tmp/demo')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature-branch', '', 'main')`,
		`insert into sessions values ('sess-1', 'ws-1', 'codex', 'external-1', '', 'Investigate bug')`,
		`insert into session_messages values ('sess-1', 'assistant', 'first', '', 'gpt-5-a', '2026-04-17T10:00:00Z')`,
		`insert into session_messages values ('sess-1', 'assistant', 'second', '', 'gpt-5-b', '2026-04-17T10:01:00Z')`,
	)

	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{},
	})
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		records = append(records, record)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	if records[0].Model != "gpt-5-a" {
		t.Fatalf("expected first message model to win, got %q", records[0].Model)
	}
	if records[0].Turns[0].Timestamp != "2026-04-17T10:00:00Z" {
		t.Fatalf("expected created_at fallback timestamp, got %q", records[0].Turns[0].Timestamp)
	}
}

func newTestDB(t *testing.T) string {
	t.Helper()

	return newTestDBWithStatements(t,
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, sent_at text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', 'git@github.com:example/demo.git', '/tmp/demo')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature-branch', '', 'main')`,
		`insert into sessions values ('sess-1', 'ws-1', 'codex', 'external-1', '', 'Investigate bug')`,
		`insert into session_messages values ('sess-1', 'assistant', 'hello', '', 'gpt-5', '2026-04-17T10:00:00Z', '2026-04-17T10:00:00Z')`,
	)
}

func newTestDBWithStatements(t *testing.T, statements ...string) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "orchestrator.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return dbPath
}

type mockLookup struct {
	record schema.Record
	found  bool
	err    error
}

func (m mockLookup) LookupSession(context.Context, string) (schema.Record, bool, error) {
	return m.record, m.found, m.err
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}
