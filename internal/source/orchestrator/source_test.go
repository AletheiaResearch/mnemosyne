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

func TestDiscoverReturnsGroupingsWithSessionCounts(t *testing.T) {
	t.Parallel()

	dbPath := newTestDBWithStatements(t,
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, sent_at text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', 'git@github.com:example/demo.git', '/tmp/demo')`,
		`insert into repositories values ('repo-2', 'other', 'git@github.com:example/other.git', '/tmp/other')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature', '', 'main')`,
		`insert into workspaces values ('ws-2', 'repo-2', 'feature', '', 'main')`,
		`insert into sessions values ('s1', 'ws-1', 'codex', 'e1', '', '')`,
		`insert into sessions values ('s2', 'ws-1', 'codex', 'e2', '', '')`,
		`insert into sessions values ('s3', 'ws-2', 'codex', 'e3', '', '')`,
	)

	src := newSource(dbPath, map[string]source.SessionLookup{})
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
	if g, ok := byID["repo-1"]; !ok || g.EstimatedRecords != 2 {
		t.Fatalf("expected repo-1 with 2 sessions, got %+v", g)
	}
	if g, ok := byID["repo-2"]; !ok || g.EstimatedRecords != 1 {
		t.Fatalf("expected repo-2 with 1 session, got %+v", g)
	}
	if byID["repo-1"].DisplayLabel != "orchestrator:demo" {
		t.Fatalf("expected repo label in display, got %q", byID["repo-1"].DisplayLabel)
	}
}

func TestExtractFallsBackToOrchestratorWhenExternalLookupEmpty(t *testing.T) {
	t.Parallel()

	dbPath := newTestDB(t)
	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{found: false},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
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
	meta := source.ExtractMap(record.Extensions, "orchestrator")
	if source.ExtractString(meta, "content_source") != "orchestrator" {
		t.Fatalf("expected content_source=orchestrator fallback, got %+v", meta)
	}
	if boolValue(meta["external_store_preferred"]) {
		t.Fatalf("external store should not be preferred when lookup returns nothing")
	}
}

func TestExtractNormalizesAgentTypeToDispatchLookup(t *testing.T) {
	t.Parallel()

	// The sessions table stores 'Claude Code' but we only registered 'claudecode'.
	// normalizeAgentType must collapse the variant to claudecode so the lookup dispatches.
	dbPath := newTestDBWithStatements(t,
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, sent_at text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', '', '/tmp/demo')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature-branch', '', 'main')`,
		`insert into sessions values ('sess-1', 'ws-1', 'Claude Code', 'external-1', '', '')`,
		`insert into session_messages values ('sess-1', 'assistant', 'from db', '', 'sonnet', '2026-04-17T10:00:00Z', '2026-04-17T10:00:00Z')`,
	)

	src := newSource(dbPath, map[string]source.SessionLookup{
		"claudecode": mockLookup{
			record: schema.Record{
				RecordID: "external-1",
				Origin:   "claudecode",
				Turns:    []schema.Turn{{Role: "assistant", Text: "from external"}},
			},
			found: true,
		},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
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
	if records[0].Turns[0].Text != "from external" {
		t.Fatalf("expected external turns to win via normalized agent type, got %+v", records[0].Turns)
	}
}

func TestExtractUsesOrchestratorWhenLookupErrors(t *testing.T) {
	t.Parallel()

	dbPath := newTestDB(t)
	src := newSource(dbPath, map[string]source.SessionLookup{
		"codex": mockLookup{err: assertErr("boom")},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected fallback record, got %d", len(records))
	}
	meta := source.ExtractMap(records[0].Extensions, "orchestrator")
	if source.ExtractString(meta, "content_source") != "orchestrator" {
		t.Fatalf("expected orchestrator to own content when lookup errors, got %+v", meta)
	}
}

func TestExtractDecodesPayloadIntoToolCalls(t *testing.T) {
	t.Parallel()

	dbPath := newTestDBWithStatements(t,
		`create table repositories (id text primary key, name text, remote_origin text, path text)`,
		`create table workspaces (id text primary key, repository_id text, label text, codename text, branch text)`,
		`create table sessions (id text primary key, workspace_id text, agent_type text, external_session_id text, model text, title text)`,
		`create table session_messages (session_id text, role text, content text, payload text, model text, sent_at text, created_at text)`,
		`insert into repositories values ('repo-1', 'demo', '', '/tmp/demo')`,
		`insert into workspaces values ('ws-1', 'repo-1', 'feature-branch', '', 'main')`,
		`insert into sessions values ('sess-1', 'ws-1', 'codex', '', '', '')`,
		`insert into session_messages values ('sess-1', 'user', 'hi', '', '', '2026-04-17T10:00:00Z', '2026-04-17T10:00:00Z')`,
		`insert into session_messages values ('sess-1', 'assistant', '', '{"text":"done","reasoning":"steps","tool_calls":[{"name":"shell","input":{"cmd":"ls"},"status":"ok","output":"README"}]}', 'sonnet', '2026-04-17T10:00:01Z', '2026-04-17T10:00:01Z')`,
	)

	src := newSource(dbPath, map[string]source.SessionLookup{})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "repo-1",
		DisplayLabel: "orchestrator:demo",
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
	if record.Usage.UserTurns != 1 || record.Usage.AssistantTurns != 1 {
		t.Fatalf("turn counts = %+v", record.Usage)
	}
	assistant := record.Turns[1]
	if assistant.Text != "done" || assistant.Reasoning != "steps" {
		t.Fatalf("assistant payload decode failed: %+v", assistant)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected tool call from payload, got %+v", assistant.ToolCalls)
	}
	call := assistant.ToolCalls[0]
	if call.Tool != "shell" || call.Status != "ok" {
		t.Fatalf("tool call = %+v", call)
	}
	if call.Output == nil || call.Output.Text != "README" {
		t.Fatalf("expected output text from payload, got %+v", call.Output)
	}
}

func TestExtractDiscoversRenamedColumns(t *testing.T) {
	t.Parallel()

	dbPath := newTestDBWithStatements(t,
		`create table repository (id text primary key, display_name text, git_url text, root_path text)`,
		`create table workspace (id text primary key, repo_id text, directory_name text, nickname text, branch_name text)`,
		`create table session (id text primary key, workspace_id text, agent text, agent_session_id text, model_id text, name text)`,
		`create table messages (session_id text, role text, text text, rich_payload text, model_id text, timestamp text)`,
		`insert into repository values ('repo-1', 'demo-renamed', '', '/tmp/renamed')`,
		`insert into workspace values ('ws-1', 'repo-1', 'feature', 'codename', 'main')`,
		`insert into session values ('sess-1', 'ws-1', 'codex', '', 'sonnet-3', 'Session Title')`,
		`insert into messages values ('sess-1', 'user', 'hello', '', 'sonnet-3', '2026-04-17T10:00:00Z')`,
		`insert into messages values ('sess-1', 'assistant', 'world', '', 'sonnet-3', '2026-04-17T10:00:01Z')`,
	)

	src := newSource(dbPath, map[string]source.SessionLookup{})
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected one grouping, got %+v", groupings)
	}

	var records []schema.Record
	err = src.Extract(t.Context(), groupings[0], source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d: %+v", len(records), records)
	}
	if records[0].Title != "Session Title" {
		t.Fatalf("expected renamed title column, got %q", records[0].Title)
	}
	if records[0].Model != "sonnet-3" {
		t.Fatalf("expected renamed model column, got %q", records[0].Model)
	}
}

func TestNormalizeAgentType(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"Claude Code", "claudecode"},
		{"claude-code-cli", "claudecode"},
		{"anthropic", "claudecode"},
		{"Gemini", "gemini"},
		{"gemini_cli", "gemini"},
		{"KIMI", "kimi"},
		{"codex", "codex"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeAgentType(tc.in); got != tc.want {
			t.Fatalf("normalizeAgentType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRepoDisplayName(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"git@github.com:example/demo.git", "demo"},
		{"https://github.com/example/repo/", "repo"},
		{"plain-name", "plain-name"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := repoDisplayName(tc.in); got != tc.want {
			t.Fatalf("repoDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOpenReturnsErrorWhenTablesMissing(t *testing.T) {
	t.Parallel()

	dbPath := newTestDBWithStatements(t,
		`create table unrelated (id text primary key)`,
	)

	src := newSource(dbPath, nil)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatalf("Discover swallows errors, got %v", err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings for incompatible schema, got %+v", groupings)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

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
