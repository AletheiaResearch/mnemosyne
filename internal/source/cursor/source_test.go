package cursor

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

type bubbleSpec struct {
	ID      string
	Payload map[string]any
}

type composerSpec struct {
	ID               string
	Name             string // composer.name -> Title
	ModelName        string // composer.modelConfig.modelName -> Model
	SelectedModelID  string // composer.modelConfig.selectedModels[0].modelId (when ModelName unset)
	CreatedAt        int64  // composer.createdAt (epoch ms) -> StartedAt
	Headers          []string
	Workspace        string // adds workspaceUris to every bubble when set
	WorkspaceURI     string // composer.workspaceIdentifier.uri as a "file://" string
	WorkspaceURIPath string // composer.workspaceIdentifier.uri as a VS Code URI object
	Bubbles          []bubbleSpec
	Empty            bool   // emit composerData with no headers (a draft/subagent)
	Subagent         bool   // emit composer.subagentInfo (marks a sub-agent session)
	ParentComposerID string // composer.subagentInfo.parentComposerId when set
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
		composerData := map[string]any{"composerId": comp.ID}
		if !comp.Empty {
			headers := make([]map[string]any, 0, len(comp.Headers))
			for _, id := range comp.Headers {
				headers = append(headers, map[string]any{"bubbleId": id})
			}
			composerData["fullConversationHeadersOnly"] = headers
		}
		if comp.Name != "" {
			composerData["name"] = comp.Name
		}
		if comp.ModelName != "" {
			composerData["modelConfig"] = map[string]any{"modelName": comp.ModelName}
		} else if comp.SelectedModelID != "" {
			composerData["modelConfig"] = map[string]any{
				"selectedModels": []map[string]any{{"modelId": comp.SelectedModelID}},
			}
		}
		if comp.CreatedAt != 0 {
			composerData["createdAt"] = comp.CreatedAt
		}
		if comp.WorkspaceURI != "" {
			composerData["workspaceIdentifier"] = map[string]any{"uri": comp.WorkspaceURI}
		} else if comp.WorkspaceURIPath != "" {
			composerData["workspaceIdentifier"] = map[string]any{"uri": map[string]any{
				"scheme": "file", "path": comp.WorkspaceURIPath, "fsPath": comp.WorkspaceURIPath,
			}}
		}
		if comp.Subagent {
			info := map[string]any{"subagentTypeName": "search"}
			if comp.ParentComposerID != "" {
				info["parentComposerId"] = comp.ParentComposerID
			}
			composerData["subagentInfo"] = info
		}
		raw, err := json.Marshal(composerData)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`insert into cursorDiskKV (key, value) values (?, ?)`, "composerData:"+comp.ID, raw); err != nil {
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

func tLine(role string, blocks ...map[string]any) map[string]any {
	content := make([]any, 0, len(blocks))
	for _, b := range blocks {
		content = append(content, b)
	}
	return map[string]any{"role": role, "message": map[string]any{"content": content}}
}

func tText(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

func tToolUse(name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "name": name, "input": input}
}

// writeTranscript writes a transcript JSONL under
// <root>/<projectDir>/agent-transcripts/[<sid>/]<sid>.jsonl and returns the
// file path. nested toggles the agent-transcripts/<sid>/<sid>.jsonl layout.
func writeTranscript(t *testing.T, root, projectDir, sid string, nested bool, lines []map[string]any) string {
	t.Helper()
	dir := filepath.Join(root, projectDir, "agent-transcripts")
	if nested {
		dir = filepath.Join(dir, sid)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sid+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func newTestSource(dbPath, projectsRoot string) *Source {
	return &Source{
		dbPath:       dbPath,
		projectsRoot: projectsRoot,
	}
}

// writeSubagentTranscript writes a sub-agent transcript under
// <root>/<projectDir>/agent-transcripts/<parentID>/subagents/<subID>.jsonl.
func writeSubagentTranscript(t *testing.T, root, projectDir, parentID, subID string, lines []map[string]any) string {
	t.Helper()
	dir := filepath.Join(root, projectDir, "agent-transcripts", parentID, "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, subID+".jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, line := range lines {
		if err := enc.Encode(line); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func extractAll(t *testing.T, src *Source) []schema.Record {
	t.Helper()
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var records []schema.Record
	for _, g := range groupings {
		if err := src.Extract(t.Context(), g, source.ExtractionContext{}, func(r schema.Record) error {
			records = append(records, r)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return records
}

func recordByID(records []schema.Record, id string) (schema.Record, bool) {
	for _, r := range records {
		if r.RecordID == id {
			return r, true
		}
	}
	return schema.Record{}, false
}

// ---------------------------------------------------------------------------
// Transcript reader (primary for transcript-only sessions)
// ---------------------------------------------------------------------------

func TestExtractTranscriptOnlySession(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-projectx", "sess-1", false, []map[string]any{
		tLine("user", tText("please read main.go")),
		tLine("assistant", tText("on it"), tToolUse("read_file", map[string]any{"path": "main.go"})),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)

	records := extractAll(t, src)
	rec, ok := recordByID(records, "sess-1")
	if !ok {
		t.Fatalf("expected record sess-1, got %+v", records)
	}
	if rec.Origin != "cursor" {
		t.Fatalf("origin = %q", rec.Origin)
	}
	if rec.WorkingDir != "/home/user/projectx" {
		t.Fatalf("working_dir = %q", rec.WorkingDir)
	}
	if rec.Model != "cursor/unknown" {
		t.Fatalf("model = %q", rec.Model)
	}
	if rec.Usage.UserTurns != 1 || rec.Usage.AssistantTurns != 1 {
		t.Fatalf("turn counts = %+v", rec.Usage)
	}
	if rec.Usage.ToolCalls != 1 {
		t.Fatalf("tool calls = %d", rec.Usage.ToolCalls)
	}
	asst := rec.Turns[1]
	if asst.Text != "on it" {
		t.Fatalf("assistant text = %q", asst.Text)
	}
	if len(asst.ToolCalls) != 1 || asst.ToolCalls[0].Tool != "read_file" {
		t.Fatalf("tool calls = %+v", asst.ToolCalls)
	}
	if in, ok := asst.ToolCalls[0].Input.(map[string]any); !ok || in["path"] != "main.go" {
		t.Fatalf("tool input = %+v", asst.ToolCalls[0].Input)
	}
}

func TestExtractTranscriptNestedLayout(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-projectx", "nested-1", true, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)
	records := extractAll(t, src)
	if _, ok := recordByID(records, "nested-1"); !ok {
		t.Fatalf("expected nested transcript record, got %+v", records)
	}
}

// ---------------------------------------------------------------------------
// Global-state reader (composerData + bubbleId)
// ---------------------------------------------------------------------------

func TestExtractGlobalStateSessionFromBubbles(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:           "comp-1",
			Name:         "Fix the parser",
			ModelName:    "claude-4.5-sonnet",
			CreatedAt:    1_700_000_000_000,
			WorkspaceURI: "file:///home/user/repo",
			Headers:      []string{"b1", "b2", "b3"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "fix it", "createdAt": "2026-04-17T10:00:00Z"}},
				{ID: "b2", Payload: map[string]any{
					"type":       float64(2),
					"text":       "here is the fix",
					"createdAt":  "2026-04-17T10:00:05Z",
					"thinking":   map[string]any{"text": "let me think"},
					"tokenCount": map[string]any{"inputTokens": float64(200), "outputTokens": float64(80)},
				}},
				{ID: "b3", Payload: map[string]any{
					"type":      float64(2),
					"createdAt": "2026-04-17T10:00:10Z",
					"toolFormerData": map[string]any{
						"name":   "read_file",
						"status": "completed",
						"params": `{"path":"main.go"}`,
						"result": `{"content":"code"}`,
					},
					"tokenCount": map[string]any{"inputTokens": float64(10), "outputTokens": float64(0)},
				}},
			},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))

	records := extractAll(t, src)
	rec, ok := recordByID(records, "comp-1")
	if !ok {
		t.Fatalf("expected comp-1, got %+v", records)
	}
	if rec.Title != "Fix the parser" {
		t.Fatalf("title = %q", rec.Title)
	}
	if rec.Model != "claude-4.5-sonnet" {
		t.Fatalf("model = %q", rec.Model)
	}
	if rec.WorkingDir != "/home/user/repo" {
		t.Fatalf("working_dir = %q", rec.WorkingDir)
	}
	if rec.Usage.InputTokens != 210 || rec.Usage.OutputTokens != 80 {
		t.Fatalf("tokens = %+v", rec.Usage)
	}
	if rec.Usage.UserTurns != 1 || rec.Usage.AssistantTurns != 2 {
		t.Fatalf("turn counts = %+v", rec.Usage)
	}
	if rec.Turns[1].Reasoning != "let me think" {
		t.Fatalf("reasoning = %q", rec.Turns[1].Reasoning)
	}
	tool := rec.Turns[2]
	if len(tool.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %+v", tool.ToolCalls)
	}
	if tool.ToolCalls[0].Tool != "read_file" || tool.ToolCalls[0].Status != "completed" {
		t.Fatalf("tool call = %+v", tool.ToolCalls[0])
	}
	if tool.ToolCalls[0].Output == nil {
		t.Fatalf("expected tool output, got nil")
	}
}

func TestExtractSkipsEmptyComposers(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{ID: "draft", Empty: true},
		{
			ID:           "real",
			WorkspaceURI: "file:///home/user/repo",
			Headers:      []string{"b1"},
			Bubbles:      []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	records := extractAll(t, src)
	if _, ok := recordByID(records, "draft"); ok {
		t.Fatalf("empty composer should be skipped")
	}
	if _, ok := recordByID(records, "real"); !ok {
		t.Fatalf("real composer missing: %+v", records)
	}
}

// ---------------------------------------------------------------------------
// Merge / dedup across both stores
// ---------------------------------------------------------------------------

func TestExtractMergesOverlappingSessionPreferringBubbles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Same session id in both stores. The transcript carries prose + a tool
	// call but no reasoning/results; the bubbles carry reasoning + tool output.
	writeTranscript(t, root, "home-user-repo", "shared", false, []map[string]any{
		tLine("user", tText("do the thing")),
		tLine("assistant", tText("doing the thing"), tToolUse("read_file", map[string]any{"path": "x.go"})),
	})
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:        "shared",
			ModelName: "gpt-5",
			Headers:   []string{"b1", "b2"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "do the thing"}},
				{ID: "b2", Payload: map[string]any{
					"type":     float64(2),
					"text":     "doing the thing",
					"thinking": map[string]any{"text": "deep thought"},
					"toolFormerData": map[string]any{
						"name":   "read_file",
						"status": "completed",
						"params": `{"path":"x.go"}`,
						"result": "file body",
					},
				}},
			},
		},
	})
	src := newTestSource(dbPath, root)

	records := extractAll(t, src)
	// Exactly one record for the shared session — never two.
	count := 0
	for _, r := range records {
		if r.RecordID == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 merged record for shared session, got %d: %+v", count, records)
	}
	rec, _ := recordByID(records, "shared")
	// Bubble backbone: reasoning + tool output present (transcripts lack both).
	foundReasoning := false
	foundOutput := false
	for _, turn := range rec.Turns {
		if turn.Reasoning != "" {
			foundReasoning = true
		}
		for _, call := range turn.ToolCalls {
			if call.Output != nil {
				foundOutput = true
			}
		}
	}
	if !foundReasoning {
		t.Fatalf("merged record dropped bubble reasoning: %+v", rec.Turns)
	}
	if !foundOutput {
		t.Fatalf("merged record dropped bubble tool output: %+v", rec.Turns)
	}
	// Workspace comes from the transcript project-dir (authoritative).
	if rec.WorkingDir != "/home/user/repo" {
		t.Fatalf("working_dir = %q", rec.WorkingDir)
	}
}

// ---------------------------------------------------------------------------
// Discover
// ---------------------------------------------------------------------------

func TestDiscoverGroupsByWorkspaceWithoutDoublePrefix(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-projectx", "t1", false, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected 1 grouping, got %+v", groupings)
	}
	if got := groupings[0].DisplayLabel; got != "cursor:home/user/projectx" {
		t.Fatalf("display label = %q (double-prefix regression?)", got)
	}
	if groupings[0].Origin != "cursor" {
		t.Fatalf("origin = %q", groupings[0].Origin)
	}
}

func TestDiscoverUnknownWorkspaceNotDoublePrefixed(t *testing.T) {
	t.Parallel()
	// A composer with no workspace signal at all -> "cursor:unknown", never
	// "cursor:cursor:unknown".
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:      "comp-x",
			Headers: []string{"b1"},
			Bubbles: []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "orphan"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected 1 grouping, got %+v", groupings)
	}
	if got := groupings[0].DisplayLabel; got != "cursor:unknown" {
		t.Fatalf("display label = %q, want cursor:unknown", got)
	}
}

func TestDiscoverReturnsNilWhenStoresMissing(t *testing.T) {
	t.Parallel()
	src := newTestSource(filepath.Join(t.TempDir(), "missing.vscdb"), filepath.Join(t.TempDir(), "missing-projects"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings, got %+v", groupings)
	}
}

// ---------------------------------------------------------------------------
// LookupSession (orchestrator dependency)
// ---------------------------------------------------------------------------

func TestLookupSessionByComposerID(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:           "target",
			WorkspaceURI: "file:///home/user/repo",
			Headers:      []string{"b1"},
			Bubbles:      []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	rec, found, err := src.LookupSession(t.Context(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected found")
	}
	if rec.RecordID != "target" || rec.WorkingDir != "/home/user/repo" {
		t.Fatalf("record = %+v", rec)
	}

	_, found, err = src.LookupSession(t.Context(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected not found for missing id")
	}
}

func TestLookupSessionByTranscriptID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-projectx", "tsess", false, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello there")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)
	rec, found, err := src.LookupSession(t.Context(), "tsess")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected found for transcript id")
	}
	if rec.RecordID != "tsess" {
		t.Fatalf("record id = %q", rec.RecordID)
	}
}

// ---------------------------------------------------------------------------
// Sub-agents
// ---------------------------------------------------------------------------

func TestSubagentEmittedAsTaggedRecord(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-repo", "parent-1", true, []map[string]any{
		tLine("user", tText("spawn a subagent")),
		tLine("assistant", tText("spawning"), tToolUse("Task", map[string]any{"prompt": "go"})),
	})
	writeSubagentTranscript(t, root, "home-user-repo", "parent-1", "sub-1", []map[string]any{
		tLine("user", tText("subtask")),
		tLine("assistant", tText("subtask done")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)

	records := extractAll(t, src)
	parent, ok := recordByID(records, "parent-1")
	if !ok {
		t.Fatalf("parent record missing: %+v", records)
	}
	if parent.Provenance == nil || parent.Provenance.SourceOrigin != "cursor" {
		t.Fatalf("parent should be a plain cursor record: %+v", parent.Provenance)
	}
	sub, ok := recordByID(records, "sub-1")
	if !ok {
		t.Fatalf("subagent record missing: %+v", records)
	}
	if sub.Provenance == nil || sub.Provenance.SourceOrigin != "cursor-subagent" {
		t.Fatalf("subagent SourceOrigin = %+v, want cursor-subagent", sub.Provenance)
	}
	meta, _ := sub.Provenance.Extensions["subagent"].(map[string]any)
	if meta == nil || meta["parent_session_id"] != "parent-1" {
		t.Fatalf("subagent parent link missing: %+v", sub.Provenance.Extensions)
	}
	// Both share the parent's workspace (sub-agent transcripts inherit the project dir).
	if sub.WorkingDir != "/home/user/repo" {
		t.Fatalf("subagent working_dir = %q", sub.WorkingDir)
	}
}

func TestSubagentComposerDetectedViaSubagentInfo(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:           "sub-comp",
			WorkspaceURI: "file:///home/user/repo",
			Subagent:     true,
			Headers:      []string{"b1"},
			Bubbles:      []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(2), "text": "did the subtask"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	records := extractAll(t, src)
	rec, ok := recordByID(records, "sub-comp")
	if !ok {
		t.Fatalf("expected sub-comp record, got %+v", records)
	}
	if rec.Provenance == nil || rec.Provenance.SourceOrigin != "cursor-subagent" {
		t.Fatalf("composer subagent not tagged: %+v", rec.Provenance)
	}
}

// subagentInfo is authoritative: a composer flagged as a sub-agent is tagged
// even when its only transcript is a top-level-named file (the observed id
// collision where a sub-agent id equals an unrelated top-level basename).
func TestSubagentInfoOverridesTopLevelTranscriptClassification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-repo", "collide", true, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello")),
	})
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:       "collide",
			Subagent: true,
			Headers:  []string{"b1"},
			Bubbles:  []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(2), "text": "sub work"}}},
		},
	})
	src := newTestSource(dbPath, root)
	records := extractAll(t, src)
	rec, ok := recordByID(records, "collide")
	if !ok {
		t.Fatalf("expected collide record, got %+v", records)
	}
	if rec.Provenance == nil || rec.Provenance.SourceOrigin != "cursor-subagent" {
		t.Fatalf("subagentInfo should win classification: %+v", rec.Provenance)
	}
}

// A sub-agent composer whose bubble rows have been GC'd must still fall back to
// its sub-agent transcript AND remain tagged as a sub-agent.
func TestSubagentComposerFallbackStaysTagged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:       "sub-gc",
			Subagent: true,
			Headers:  []string{"gone1"},
			Bubbles:  nil, // bubble rows GC'd
		},
	})
	writeSubagentTranscript(t, root, "home-user-repo", "parent-z", "sub-gc", []map[string]any{
		tLine("user", tText("subtask")),
		tLine("assistant", tText("recovered subtask output")),
	})
	src := newTestSource(dbPath, root)
	records := extractAll(t, src)
	rec, ok := recordByID(records, "sub-gc")
	if !ok {
		t.Fatalf("subagent dropped instead of transcript fallback: %+v", records)
	}
	if rec.Usage.AssistantTurns == 0 {
		t.Fatalf("fallback produced no turns: %+v", rec.Usage)
	}
	if rec.Provenance == nil || rec.Provenance.SourceOrigin != "cursor-subagent" {
		t.Fatalf("subagent tag lost on transcript fallback: %+v", rec.Provenance)
	}
	if sub, _ := rec.Provenance.Extensions["subagent"].(map[string]any); sub == nil || sub["parent_session_id"] != "parent-z" {
		t.Fatalf("parent link lost on fallback: %+v", rec.Provenance.Extensions)
	}
}

// ---------------------------------------------------------------------------
// Fallback / robustness
// ---------------------------------------------------------------------------

func TestComposerFallsBackToTranscriptWhenBubblesMissing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Composer headers (and name/model/createdAt) survive but the referenced
	// bubble rows are gone (GC'd).
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:           "shared",
			Name:         "Real Title",
			ModelName:    "claude-4.5-sonnet",
			CreatedAt:    1_700_000_000_000,
			WorkspaceURI: "file:///home/user/repo",
			Headers:      []string{"gone1", "gone2"},
			Bubbles:      nil, // no bubble rows inserted
		},
	})
	writeTranscript(t, root, "home-user-repo", "shared", true, []map[string]any{
		tLine("user", tText("still here")),
		tLine("assistant", tText("recovered from transcript")),
	})
	src := newTestSource(dbPath, root)

	records := extractAll(t, src)
	rec, ok := recordByID(records, "shared")
	if !ok {
		t.Fatalf("session dropped instead of falling back to transcript: %+v", records)
	}
	if rec.Usage.AssistantTurns == 0 {
		t.Fatalf("transcript fallback produced no assistant turns: %+v", rec.Usage)
	}
	// Composer-derived metadata must survive the fallback (it lives in
	// composerData, not the GC'd bubble rows).
	if rec.Title != "Real Title" {
		t.Fatalf("fallback lost title: %q", rec.Title)
	}
	if rec.Model != "claude-4.5-sonnet" {
		t.Fatalf("fallback lost model: %q", rec.Model)
	}
	if rec.StartedAt == "" {
		t.Fatalf("fallback lost StartedAt")
	}
	// Provenance reflects that the turns came from the transcript.
	if rec.Provenance == nil || rec.Provenance.SourcePath == "" || filepath.Ext(rec.Provenance.SourcePath) != ".jsonl" {
		t.Fatalf("fallback provenance should point at the transcript: %+v", rec.Provenance)
	}
}

func TestSubagentComposerOnlyGetsParentFromSubagentInfo(t *testing.T) {
	t.Parallel()
	// A sub-agent that lives only in global state (no transcript) still gets its
	// parent link from subagentInfo.parentComposerId.
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:               "sub-only",
			Subagent:         true,
			ParentComposerID: "the-parent",
			WorkspaceURI:     "file:///home/user/repo",
			Headers:          []string{"b1"},
			Bubbles:          []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(2), "text": "subtask output"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	records := extractAll(t, src)
	rec, ok := recordByID(records, "sub-only")
	if !ok {
		t.Fatalf("expected sub-only record, got %+v", records)
	}
	if rec.Provenance == nil || rec.Provenance.SourceOrigin != "cursor-subagent" {
		t.Fatalf("not tagged subagent: %+v", rec.Provenance)
	}
	sub, _ := rec.Provenance.Extensions["subagent"].(map[string]any)
	if sub == nil || sub["parent_session_id"] != "the-parent" {
		t.Fatalf("parent link from subagentInfo missing: %+v", rec.Provenance.Extensions)
	}
}

func TestDecodeProjectSentinelBucketsUnknown(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "empty-window", "win-1", false, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "absent.vscdb"), root)

	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 || groupings[0].DisplayLabel != "cursor:unknown" {
		t.Fatalf("empty-window sentinel should bucket as cursor:unknown, got %+v", groupings)
	}
	records := extractAll(t, src)
	rec, ok := recordByID(records, "win-1")
	if !ok {
		t.Fatalf("record missing: %+v", records)
	}
	if rec.WorkingDir != "" {
		t.Fatalf("sentinel working_dir should be empty, got %q", rec.WorkingDir)
	}
}

// ---------------------------------------------------------------------------
// Reasoning suppression, provenance, decoding edge paths
// ---------------------------------------------------------------------------

func TestExtractSuppressReasoningDropsReasoningOnlyTurns(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:           "c1",
			WorkspaceURI: "file:///home/user/repo",
			Headers:      []string{"b1", "b2", "b3"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi"}},
				// reasoning-only assistant bubble -> becomes an empty shell once suppressed
				{ID: "b2", Payload: map[string]any{"type": float64(2), "thinking": map[string]any{"text": "only reasoning"}}},
				{ID: "b3", Payload: map[string]any{"type": float64(2), "text": "answer", "thinking": map[string]any{"text": "some reasoning"}}},
			},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var rec schema.Record
	for _, g := range groupings {
		if err := src.Extract(t.Context(), g, source.ExtractionContext{SuppressReasoning: true}, func(r schema.Record) error {
			rec = r
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, turn := range rec.Turns {
		if turn.Reasoning != "" {
			t.Fatalf("reasoning not suppressed: %+v", turn)
		}
		if turn.Text == "" && len(turn.ToolCalls) == 0 {
			t.Fatalf("content-less shell turn survived suppression: %+v", turn)
		}
	}
	// b2 (reasoning-only) is dropped; usage recomputed to 1 user + 1 assistant.
	if rec.Usage.UserTurns != 1 || rec.Usage.AssistantTurns != 1 {
		t.Fatalf("usage after suppression = %+v", rec.Usage)
	}
}

func TestNewUsesDefaultRootsWhenDBPathEmpty(t *testing.T) {
	t.Parallel()
	src := New("")
	if src.dbPath == "" || src.projectsRoot == "" {
		t.Fatalf("defaults not populated: dbPath=%q projectsRoot=%q", src.dbPath, src.projectsRoot)
	}
	if custom := New("/tmp/custom.vscdb"); custom.dbPath != "/tmp/custom.vscdb" {
		t.Fatalf("explicit dbPath = %q", custom.dbPath)
	}
}

// Greptile P2: when a composer exists but the DB is unavailable at extract time,
// the turns come entirely from the transcript, so provenance must point at the
// transcript and not claim global-state was read.
func TestBuildRecordComposerWithoutDBUsesTranscriptProvenance(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := writeTranscript(t, root, "home-user-repo", "s1", true, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", tText("hello")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "none.vscdb"), root)
	entry := &sessionEntry{id: "s1", hasComposer: true, transcriptPath: path, workspace: "/home/user/repo"}

	rec, ok := src.buildRecord(nil, entry)
	if !ok {
		t.Fatalf("expected a record from the transcript")
	}
	if rec.Provenance.SourcePath != path {
		t.Fatalf("SourcePath should be the transcript when the DB is nil, got %q", rec.Provenance.SourcePath)
	}
	if _, hasStores := rec.Provenance.Extensions["stores"]; hasStores {
		t.Fatalf("stores must not claim global-state when the DB was not read: %+v", rec.Provenance.Extensions)
	}
}

func TestGlobalStateUnwrapsToolParamsSelectedModelAndBubbleWorkspace(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:              "c1",
			SelectedModelID: "gpt-5-codex",     // modelConfig.selectedModels (no modelName)
			Workspace:       "/home/user/repo", // stamps workspaceUris on bubbles -> composerWorkspace fallback
			Headers:         []string{"b1", "b2", "b3"},
			Bubbles: []bubbleSpec{
				{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "go"}},
				{ID: "b2", Payload: map[string]any{"type": float64(2), "toolFormerData": map[string]any{
					"name":   "mcp-filesystem-read_file",
					"status": "completed",
					"params": `{"tools":[{"parameters":{"path":"main.go"}}]}`,
					"result": `{"content":"x"}`,
				}}},
				// An unknown bubble type (neither user nor assistant) is dropped.
				{ID: "b3", Payload: map[string]any{"type": float64(3), "text": "ignored"}},
			},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	rec, ok := recordByID(extractAll(t, src), "c1")
	if !ok {
		t.Fatalf("missing c1")
	}
	if rec.Model != "gpt-5-codex" {
		t.Fatalf("model from selectedModels = %q", rec.Model)
	}
	if rec.WorkingDir != "/home/user/repo" {
		t.Fatalf("workspace from bubble workspaceUris = %q", rec.WorkingDir)
	}
	var call *schema.ToolCall
	for i := range rec.Turns {
		for j := range rec.Turns[i].ToolCalls {
			call = &rec.Turns[i].ToolCalls[j]
		}
	}
	if call == nil {
		t.Fatalf("no tool call found: %+v", rec.Turns)
	}
	if call.Tool != "read_file" {
		t.Fatalf("tool name (mcp_ prefix) = %q", call.Tool)
	}
	params, ok := call.Input.(map[string]any)
	if !ok || params["path"] != "main.go" {
		t.Fatalf("unwrapped tool params = %+v", call.Input)
	}
}

// On real builds workspaceIdentifier.uri is a serialized VS Code URI object,
// not a "file://" string; the composer-only workspace fallback must read it.
func TestComposerWorkspaceFromURIObject(t *testing.T) {
	t.Parallel()
	dbPath := buildCursorDB(t, []composerSpec{
		{
			ID:               "obj",
			WorkspaceURIPath: "/home/user/repo",
			Headers:          []string{"b1"},
			Bubbles:          []bubbleSpec{{ID: "b1", Payload: map[string]any{"type": float64(1), "text": "hi"}}},
		},
	})
	src := newTestSource(dbPath, filepath.Join(t.TempDir(), "noprojects"))
	rec, ok := recordByID(extractAll(t, src), "obj")
	if !ok {
		t.Fatalf("missing obj record")
	}
	if rec.WorkingDir != "/home/user/repo" {
		t.Fatalf("workspace from URI object = %q", rec.WorkingDir)
	}
}

func TestTranscriptThinkingBlockBecomesReasoning(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTranscript(t, root, "home-user-repo", "t1", true, []map[string]any{
		tLine("user", tText("hi")),
		tLine("assistant", map[string]any{"type": "thinking", "thinking": "pondering"}, tText("answer")),
	})
	src := newTestSource(filepath.Join(t.TempDir(), "none.vscdb"), root)
	rec, ok := recordByID(extractAll(t, src), "t1")
	if !ok {
		t.Fatalf("missing t1")
	}
	asst := rec.Turns[1]
	if asst.Reasoning != "pondering" {
		t.Fatalf("reasoning = %q", asst.Reasoning)
	}
	if asst.Text != "answer" {
		t.Fatalf("text = %q", asst.Text)
	}
}

// ---------------------------------------------------------------------------
// Helper unit tests (retained from the original suite)
// ---------------------------------------------------------------------------

func TestHelperEdgeCases(t *testing.T) {
	t.Parallel()
	if bubbleRole(3) != "" {
		t.Fatalf("unknown bubble type should map to no role")
	}
	if normalizeTranscriptRole("system") != "" {
		t.Fatalf("non user/assistant role should map to empty")
	}
	if got := appendText("a", "b"); got != "a\nb" {
		t.Fatalf("appendText join = %q", got)
	}
	if got := normalizeCursorPayload(float64(42)); got != float64(42) {
		t.Fatalf("non-string payload should pass through, got %v", got)
	}
	if got := unwrapCursorToolParameters(map[string]any{"x": float64(1)}); got.(map[string]any)["x"] != float64(1) {
		t.Fatalf("payload without tools should pass through, got %+v", got)
	}
	if _, ok := unwrapCursorToolParameters(map[string]any{"tools": []any{map[string]any{}}}).(map[string]any); !ok {
		t.Fatalf("tools without parameters should return the payload")
	}
	if decodeProjectDir("") != "" {
		t.Fatalf("empty encoded name should decode to empty")
	}
	if !isSentinelProjectDir("1778089727984") {
		t.Fatalf("all-numeric window id should be a sentinel")
	}
	if isSentinelProjectDir("Users-quantumly-repo") {
		t.Fatalf("a real path-like name must not be a sentinel")
	}
	if got := workspaceURIToPath("file:///x"); got != "/x" {
		t.Fatalf("string URI -> %q", got)
	}
	if got := workspaceURIToPath(map[string]any{"path": "/p", "fsPath": "/fs"}); got != "/fs" {
		t.Fatalf("URI object should prefer fsPath, got %q", got)
	}
	if got := workspaceURIToPath(float64(3)); got != "" {
		t.Fatalf("non-string/non-object URI -> %q", got)
	}
}

func TestNormalizeCursorToolNameHandlesMcpPrefixes(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
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

func TestDecodeProjectDirResolvesHyphenatedNames(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// A real workspace whose final path segment legitimately contains hyphens.
	ws := filepath.Join(base, "vibe-replay")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	encoded := encodeWorkspacePath(ws)
	if got := decodeProjectDir(encoded); got != ws {
		t.Fatalf("decodeProjectDir(%q) = %q, want %q", encoded, got, ws)
	}
}

// encodeWorkspacePath mirrors Cursor's project-dir encoding: strip the leading
// separator and replace each separator with a dash.
func encodeWorkspacePath(path string) string {
	trimmed := path
	if len(trimmed) > 0 && trimmed[0] == filepath.Separator {
		trimmed = trimmed[1:]
	}
	out := make([]rune, 0, len(trimmed))
	for _, r := range trimmed {
		if r == filepath.Separator {
			out = append(out, '-')
		} else {
			out = append(out, r)
		}
	}
	return string(out)
}
