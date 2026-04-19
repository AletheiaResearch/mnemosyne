package cursor

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	_ "modernc.org/sqlite"
)

type bubbleSpec struct {
	ID      string
	Payload map[string]any
}

type composerSpec struct {
	ID       string
	Headers  []string
	Workspace string
	Bubbles  []bubbleSpec
}

func buildCursorDB(t *testing.T, composers []composerSpec) string {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "state.vscdb")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`create table cursorDiskKV (key text primary key, value blob)`); err != nil {
		t.Fatal(err)
	}

	for _, comp := range composers {
		headers := make([]map[string]any, 0, len(comp.Headers))
		for _, id := range comp.Headers {
			headers = append(headers, map[string]any{"bubbleId": id})
		}
		composerData := map[string]any{
			"fullConversationHeadersOnly": headers,
		}
		raw, err := json.Marshal(composerData)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`insert into cursorDiskKV (key, value) values (?, ?)`,
			"composerData:"+comp.ID, raw); err != nil {
			t.Fatal(err)
		}

		for _, bubble := range comp.Bubbles {
			payload := bubble.Payload
			if payload == nil {
				payload = map[string]any{}
			}
			if comp.Workspace != "" {
				if _, ok := payload["workspaceUris"]; !ok {
					payload["workspaceUris"] = []any{"file://" + comp.Workspace}
				}
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			key := "bubbleId:" + comp.ID + ":" + bubble.ID
			if _, err := db.Exec(`insert into cursorDiskKV (key, value) values (?, ?)`, key, raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dbPath
}

func TestDiscoverResolvesWorkspaceFromBubbles(t *testing.T) {
	t.Parallel()

	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:        "comp-1",
			Workspace: "/tmp/cursor-project",
			Headers:   []string{"b1", "b2"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi", "createdAt": "2026-04-17T10:00:00Z"}},
				{ID: "b2", Payload: map[string]any{"type": float64(2), "text": "hello", "createdAt": "2026-04-17T10:00:05Z"}},
			},
		},
		{
			ID:      "comp-unknown",
			Headers: []string{"b3"},
			Bubbles: []bubbleSpec{
				{ID: "b3", Payload: map[string]any{"type": float64(1), "text": "orphan", "createdAt": "2026-04-17T10:00:00Z"}},
			},
		},
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
	if g, ok := byID["/tmp/cursor-project"]; !ok {
		t.Fatalf("missing resolved workspace grouping: %+v", byID)
	} else if g.DisplayLabel != "cursor:cursor-project" {
		t.Fatalf("display label = %q", g.DisplayLabel)
	}
	if _, ok := byID[source.EstimateUnknownLabel("cursor")]; !ok {
		t.Fatalf("missing unknown grouping: %+v", byID)
	}
}

func TestExtractBuildsRecordFromBubbleChain(t *testing.T) {
	t.Parallel()

	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:        "comp-1",
			Workspace: "/tmp/cursor-project",
			Headers:   []string{"b1", "b2", "b3"},
			Bubbles: []bubbleSpec{
				{
					ID: "b1",
					Payload: map[string]any{
						"type":      float64(1),
						"text":      "hello",
						"createdAt": "2026-04-17T10:00:00Z",
					},
				},
				{
					ID: "b2",
					Payload: map[string]any{
						"type":      float64(2),
						"text":      "hi there",
						"createdAt": "2026-04-17T10:00:05Z",
						"thinking":  map[string]any{"text": "reasoning"},
						"modelInfo": map[string]any{"modelName": "cursor-sonnet"},
						"tokenCount": map[string]any{
							"inputTokens":  float64(200),
							"outputTokens": float64(80),
						},
					},
				},
				{
					ID: "b3",
					Payload: map[string]any{
						"type":      float64(2),
						"text":      "",
						"createdAt": "2026-04-17T10:00:10Z",
						"toolFormerData": map[string]any{
							"name":   "mcp-filesystem-read_file",
							"status": map[string]any{"status": "completed"},
							"params": `{"tools":[{"parameters":{"path":"main.go"}}]}`,
							"result": `{"content":"code"}`,
						},
					},
				},
			},
		},
	})

	src := New(dbPath)
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/cursor-project",
		DisplayLabel: "cursor:cursor-project",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d: %+v", len(records), records)
	}
	record := records[0]
	if record.Model != "cursor-sonnet" {
		t.Fatalf("model = %q", record.Model)
	}
	if record.WorkingDir != "/tmp/cursor-project" {
		t.Fatalf("working_dir = %q", record.WorkingDir)
	}
	if record.Usage.InputTokens != 200 || record.Usage.OutputTokens != 80 {
		t.Fatalf("tokens = %+v", record.Usage)
	}
	if record.Usage.UserTurns != 1 || record.Usage.AssistantTurns != 2 {
		t.Fatalf("turn counts = %+v", record.Usage)
	}
	if len(record.Turns) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(record.Turns))
	}

	assistant := record.Turns[1]
	if assistant.Reasoning != "reasoning" {
		t.Fatalf("reasoning = %q", assistant.Reasoning)
	}

	toolTurn := record.Turns[2]
	if len(toolTurn.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d: %+v", len(toolTurn.ToolCalls), toolTurn.ToolCalls)
	}
	call := toolTurn.ToolCalls[0]
	if call.Tool != "read_file" { // normalized from mcp-filesystem-read_file
		t.Fatalf("expected normalized tool name, got %q", call.Tool)
	}
	if call.Status != "completed" {
		t.Fatalf("status = %q", call.Status)
	}
	params, ok := call.Input.(map[string]any)
	if !ok {
		t.Fatalf("expected unwrapped parameters map, got %T: %+v", call.Input, call.Input)
	}
	if params["path"] != "main.go" {
		t.Fatalf("expected unwrapped path parameter, got %+v", params)
	}
}

func TestLookupSessionReturnsComposerAsRecord(t *testing.T) {
	t.Parallel()

	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:        "target",
			Workspace: "/tmp/x",
			Headers:   []string{"b1"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi", "createdAt": "2026-04-17T10:00:00Z"}},
			},
		},
	})

	src := New(dbPath)

	record, found, err := src.LookupSession(t.Context(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected found")
	}
	if record.RecordID != "target" {
		t.Fatalf("record id = %q", record.RecordID)
	}
	if record.WorkingDir != "/tmp/x" {
		t.Fatalf("working_dir = %q", record.WorkingDir)
	}

	_, found, err = src.LookupSession(t.Context(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected not found")
	}
}

func TestNormalizeCursorToolNameHandlesMcpPrefixes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"mcp_filesystem_readFile", "readFile"},
		{"mcp-filesystem-read_file", "read_file"},
		{"read_file", "read_file"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeCursorToolName(tc.in); got != tc.want {
			t.Fatalf("normalizeCursorToolName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeCursorStatusHandlesMapAndString(t *testing.T) {
	t.Parallel()
	if got := normalizeCursorStatus("completed"); got != "completed" {
		t.Fatalf("string status lost, got %q", got)
	}
	if got := normalizeCursorStatus(map[string]any{"status": "failed"}); got != "failed" {
		t.Fatalf("map status lost, got %q", got)
	}
	if got := normalizeCursorStatus(map[string]any{"state": "running"}); got != "running" {
		t.Fatalf("map state fallback lost, got %q", got)
	}
	if got := normalizeCursorStatus(float64(1)); got != "" {
		t.Fatalf("unknown type should be empty, got %q", got)
	}
}

func TestDiscoverReturnsNilWhenDatabaseMissing(t *testing.T) {
	t.Parallel()

	src := New(filepath.Join(t.TempDir(), "missing.vscdb"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings, got %+v", groupings)
	}
}
