package orchestrator

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	_ "modernc.org/sqlite"
)

func TestExtractFallsBackToPerMessageModel(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "orchestrator.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for _, statement := range []string{
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, sent_at text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', 'git@github.com:example/demo.git', '/tmp/demo')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature-branch', '', 'main')`,
		`insert into sessions values ('sess-1', 'ws-1', 'codex', 'external-1', '', 'Investigate bug')`,
		`insert into session_messages values ('sess-1', 'assistant', 'hello', '', 'gpt-5', '2026-04-17T10:00:00Z', '2026-04-17T10:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	src := New(dbPath)
	var records []schema.Record
	err = src.Extract(t.Context(), source.Grouping{
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
