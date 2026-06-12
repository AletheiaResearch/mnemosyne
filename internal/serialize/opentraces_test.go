package serialize

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func serializeOpenTraces(t *testing.T, record schema.Record) openTracesRecord {
	t.Helper()
	out, err := OpenTraces{}.Serialize(record)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	payload, ok := out.(openTracesRecord)
	if !ok {
		t.Fatalf("unexpected payload type %T", out)
	}
	return payload
}

func TestOpenTracesFullRecord(t *testing.T) {
	record := schema.Record{
		RecordID:   "rec-1",
		Origin:     "claudecode",
		Grouping:   "claudecode:proj",
		Model:      "claude-sonnet-4-5",
		Branch:     "main",
		StartedAt:  "2026-01-02T03:04:05Z",
		EndedAt:    "2026-01-02T04:05:06Z",
		WorkingDir: "/home/user/proj",
		Title:      "Fix the frobnicator",
		Turns: []schema.Turn{
			{Role: "user", Timestamp: "2026-01-02T03:04:05Z", Text: "please fix"},
			{
				Role:      "assistant",
				Text:      "on it",
				Reasoning: "thinking hard",
				ToolCalls: []schema.ToolCall{
					{
						Tool:   "read_file",
						Input:  map[string]any{"path": "main.go"},
						Output: &schema.ToolOutput{Text: "package main"},
						Status: "success",
					},
					{Tool: "run", Input: "go test", Status: "error"},
				},
			},
			{Role: "user", Attachments: []schema.ContentBlock{{Type: "image", MediaType: "image/png", Data: "abc"}}},
			{Role: "assistant", Text: "done"},
		},
		Usage:      schema.Usage{UserTurns: 2, AssistantTurns: 2, ToolCalls: 2, InputTokens: 100, OutputTokens: 50},
		Extensions: map[string]any{"claudecode": map[string]any{"version": "2.0"}},
		Provenance: &schema.Provenance{SourceID: "session-abc", SourcePath: "/tmp/x.jsonl"},
	}

	payload := serializeOpenTraces(t, record)

	if payload.SchemaVersion != "0.7.0" {
		t.Errorf("SchemaVersion = %q, want %q", payload.SchemaVersion, "0.7.0")
	}
	wantTraceID := uuid.NewSHA1(openTracesNamespace, []byte("rec-1")).String()
	if payload.TraceID != wantTraceID {
		t.Errorf("TraceID = %q, want %q", payload.TraceID, wantTraceID)
	}
	if payload.SessionID != "session-abc" {
		t.Errorf("SessionID = %q, want %q", payload.SessionID, "session-abc")
	}
	if payload.TimestampStart != record.StartedAt || payload.TimestampEnd != record.EndedAt {
		t.Errorf("timestamps = %q/%q, want %q/%q", payload.TimestampStart, payload.TimestampEnd, record.StartedAt, record.EndedAt)
	}
	if payload.ExecutionContext != "devtime" {
		t.Errorf("ExecutionContext = %q, want %q", payload.ExecutionContext, "devtime")
	}
	if payload.Task == nil || payload.Task.Description != record.Title {
		t.Errorf("Task = %+v, want description %q", payload.Task, record.Title)
	}
	if payload.Agent.Name != "claude-code" || payload.Agent.Model != record.Model {
		t.Errorf("Agent = %+v, want {claude-code %s}", payload.Agent, record.Model)
	}
	if payload.Environment == nil || payload.Environment.VCS.Type != "git" || payload.Environment.VCS.Branch != "main" {
		t.Errorf("Environment = %+v, want git/main", payload.Environment)
	}

	if len(payload.Steps) != 4 {
		t.Fatalf("len(Steps) = %d, want 4", len(payload.Steps))
	}
	for i, wantRole := range []string{"user", "agent", "user", "agent"} {
		if payload.Steps[i].StepIndex != i {
			t.Errorf("Steps[%d].StepIndex = %d, want %d", i, payload.Steps[i].StepIndex, i)
		}
		if payload.Steps[i].Role != wantRole {
			t.Errorf("Steps[%d].Role = %q, want %q", i, payload.Steps[i].Role, wantRole)
		}
	}
	if payload.Steps[0].Content != "please fix" || payload.Steps[0].Timestamp != "2026-01-02T03:04:05Z" {
		t.Errorf("Steps[0] = %+v", payload.Steps[0])
	}
	if payload.Steps[1].ReasoningContent != "thinking hard" {
		t.Errorf("Steps[1].ReasoningContent = %q", payload.Steps[1].ReasoningContent)
	}

	calls := payload.Steps[1].ToolCalls
	if len(calls) != 2 {
		t.Fatalf("len(Steps[1].ToolCalls) = %d, want 2", len(calls))
	}
	if calls[0].ToolCallID != "call_1_0_read_file" || calls[0].ToolName != "read_file" {
		t.Errorf("ToolCalls[0] = %+v", calls[0])
	}
	if calls[0].Input["path"] != "main.go" {
		t.Errorf("ToolCalls[0].Input = %v, want map input passed through", calls[0].Input)
	}
	if calls[1].ToolCallID != "call_1_1_run" {
		t.Errorf("ToolCalls[1].ToolCallID = %q", calls[1].ToolCallID)
	}
	if calls[1].Input["value"] != "go test" {
		t.Errorf("ToolCalls[1].Input = %v, want non-map input wrapped as value", calls[1].Input)
	}

	observations := payload.Steps[1].Observations
	if len(observations) != 2 {
		t.Fatalf("len(Steps[1].Observations) = %d, want 2", len(observations))
	}
	if observations[0].SourceCallID != "call_1_0_read_file" || observations[0].Content != "package main" || observations[0].Error != "" {
		t.Errorf("Observations[0] = %+v", observations[0])
	}
	if observations[1].SourceCallID != "call_1_1_run" || observations[1].Error != "error" || observations[1].Content != "" {
		t.Errorf("Observations[1] = %+v", observations[1])
	}

	if payload.Steps[2].Content != "[attachments omitted]" {
		t.Errorf("Steps[2].Content = %q, want attachment placeholder", payload.Steps[2].Content)
	}

	wantMetrics := openTracesMetrics{TotalSteps: 4, TotalInputTokens: 100, TotalOutputTokens: 50}
	if payload.Metrics != wantMetrics {
		t.Errorf("Metrics = %+v, want %+v", payload.Metrics, wantMetrics)
	}

	mnemosyne, ok := payload.Metadata["mnemosyne"].(map[string]any)
	if !ok {
		t.Fatalf("Metadata = %v, want mnemosyne key", payload.Metadata)
	}
	if mnemosyne["record_id"] != "rec-1" || mnemosyne["origin"] != "claudecode" || mnemosyne["grouping"] != "claudecode:proj" || mnemosyne["working_dir"] != "/home/user/proj" {
		t.Errorf("Metadata[mnemosyne] = %v", mnemosyne)
	}
	if _, ok := mnemosyne["extensions"]; !ok {
		t.Errorf("Metadata[mnemosyne] missing extensions: %v", mnemosyne)
	}
	if mnemosyne["provenance"] != record.Provenance {
		t.Errorf("Metadata[mnemosyne].provenance = %v, want record provenance", mnemosyne["provenance"])
	}
}

func TestOpenTracesMinimalRecord(t *testing.T) {
	payload := serializeOpenTraces(t, schema.Record{
		RecordID: "rec-min",
		Turns:    []schema.Turn{{Role: "user", Text: "hi"}},
	})

	if payload.SessionID != "rec-min" {
		t.Errorf("SessionID = %q, want fallback to record_id", payload.SessionID)
	}
	if payload.Agent.Name != "unknown" {
		t.Errorf("Agent.Name = %q, want %q", payload.Agent.Name, "unknown")
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, absent := range []string{"task", "environment", "timestamp_start", "timestamp_end", "content_hash"} {
		if _, ok := wire[absent]; ok {
			t.Errorf("wire JSON unexpectedly contains %q", absent)
		}
	}
	for _, present := range []string{"schema_version", "trace_id", "session_id", "execution_context", "agent", "steps", "metrics", "metadata"} {
		if _, ok := wire[present]; !ok {
			t.Errorf("wire JSON missing %q", present)
		}
	}
}

func TestOpenTracesEmptyTurnsMarshalsStepsArray(t *testing.T) {
	payload := serializeOpenTraces(t, schema.Record{RecordID: "rec-empty"})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"steps":[]`) {
		t.Errorf("wire JSON = %s, want steps to marshal as []", data)
	}
}

func TestOpenTracesRoleMapping(t *testing.T) {
	cases := []struct {
		role string
		want string
	}{
		{"user", "user"},
		{"assistant", "agent"},
		{"system", "system"},
		{"tool", "user"},
	}
	for _, tc := range cases {
		payload := serializeOpenTraces(t, sampleRecord([]schema.Turn{{Role: tc.role, Text: "x"}}))
		if got := payload.Steps[0].Role; got != tc.want {
			t.Errorf("role %q mapped to %q, want %q", tc.role, got, tc.want)
		}
	}
}

func TestOpenTracesObservationFallbacks(t *testing.T) {
	cases := []struct {
		name        string
		call        schema.ToolCall
		wantPresent bool
		wantContent string
		wantError   string
	}{
		{
			name:        "text wins over raw",
			call:        schema.ToolCall{Tool: "t", Output: &schema.ToolOutput{Text: "text", Raw: map[string]any{"k": 1}}},
			wantPresent: true,
			wantContent: "text",
		},
		{
			name: "content blocks joined",
			call: schema.ToolCall{Tool: "t", Output: &schema.ToolOutput{Content: []schema.ContentBlock{
				{Type: "text", Text: "a"},
				{Type: "image"},
				{Type: "text", Text: "b"},
			}}},
			wantPresent: true,
			wantContent: "a\nb",
		},
		{
			name:        "raw string passthrough",
			call:        schema.ToolCall{Tool: "t", Output: &schema.ToolOutput{Raw: "raw text"}},
			wantPresent: true,
			wantContent: "raw text",
		},
		{
			name:        "raw value rendered as JSON",
			call:        schema.ToolCall{Tool: "t", Output: &schema.ToolOutput{Raw: map[string]any{"exit": 0}}},
			wantPresent: true,
			wantContent: `{"exit":0}`,
		},
		{
			name:        "error status routes text into error",
			call:        schema.ToolCall{Tool: "t", Output: &schema.ToolOutput{Text: "boom"}, Status: "error"},
			wantPresent: true,
			wantError:   "boom",
		},
		{
			name:        "error status without output",
			call:        schema.ToolCall{Tool: "t", Status: "error"},
			wantPresent: true,
			wantError:   "error",
		},
		{
			name: "no output no observation",
			call: schema.ToolCall{Tool: "t", Status: "success"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := serializeOpenTraces(t, sampleRecord([]schema.Turn{
				{Role: "assistant", ToolCalls: []schema.ToolCall{tc.call}},
			}))
			observations := payload.Steps[0].Observations
			if !tc.wantPresent {
				if len(observations) != 0 {
					t.Fatalf("Observations = %+v, want none", observations)
				}
				return
			}
			if len(observations) != 1 {
				t.Fatalf("len(Observations) = %d, want 1", len(observations))
			}
			obs := observations[0]
			if obs.SourceCallID != payload.Steps[0].ToolCalls[0].ToolCallID {
				t.Errorf("SourceCallID = %q, want %q", obs.SourceCallID, payload.Steps[0].ToolCalls[0].ToolCallID)
			}
			if obs.Content != tc.wantContent || obs.Error != tc.wantError {
				t.Errorf("observation = %+v, want content %q error %q", obs, tc.wantContent, tc.wantError)
			}
		})
	}
}

func TestOpenTracesNilToolInputOmitted(t *testing.T) {
	payload := serializeOpenTraces(t, sampleRecord([]schema.Turn{
		{Role: "assistant", ToolCalls: []schema.ToolCall{{Tool: "t"}}},
	}))
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"input"`) {
		t.Errorf("wire JSON = %s, want nil tool input omitted", data)
	}
}

func TestOpenTracesDeterminism(t *testing.T) {
	record := sampleRecord([]schema.Turn{
		{Role: "user", Text: "hello"},
		{Role: "assistant", Text: "hi", ToolCalls: []schema.ToolCall{{Tool: "t", Input: map[string]any{"a": 1, "b": 2}}}},
	})

	first, err := json.Marshal(serializeOpenTraces(t, record))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	second, err := json.Marshal(serializeOpenTraces(t, record))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("serialization is not deterministic:\n%s\n%s", first, second)
	}

	payload := serializeOpenTraces(t, record)
	parsed, err := uuid.Parse(payload.TraceID)
	if err != nil {
		t.Fatalf("TraceID %q is not a UUID: %v", payload.TraceID, err)
	}
	if parsed.Version() != 5 {
		t.Errorf("TraceID version = %d, want 5", parsed.Version())
	}

	mutated := record
	mutated.Turns = append([]schema.Turn{}, record.Turns...)
	mutated.Turns[0].Text = "changed"
	if mutatedPayload := serializeOpenTraces(t, mutated); mutatedPayload.TraceID != payload.TraceID {
		t.Errorf("TraceID changed with content: %q vs %q", mutatedPayload.TraceID, payload.TraceID)
	}
}

func TestOpenTracesAgentNamePassthrough(t *testing.T) {
	record := sampleRecord(nil)
	record.Origin = "codex"
	payload := serializeOpenTraces(t, record)
	if payload.Agent.Name != "codex" {
		t.Errorf("Agent.Name = %q, want %q", payload.Agent.Name, "codex")
	}
}
