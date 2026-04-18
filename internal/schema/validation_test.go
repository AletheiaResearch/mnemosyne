package schema

import (
	"strings"
	"testing"
)

func validRecord() Record {
	return Record{
		RecordID: "rec-1",
		Origin:   "test",
		Grouping: "project",
		Model:    "model",
		Turns: []Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "hi"},
		},
	}
}

func TestValidateRecord(t *testing.T) {
	t.Parallel()

	if err := ValidateRecord(validRecord()); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateRecordAcceptsRFC3339Timestamps(t *testing.T) {
	t.Parallel()
	rec := validRecord()
	rec.StartedAt = "2026-04-17T10:00:00Z"
	rec.EndedAt = "2026-04-17T11:00:00Z"
	rec.Turns[0].Timestamp = "2026-04-17T10:00:01Z"
	if err := ValidateRecord(rec); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestValidateRecordAcceptsAlternativeTurnContent(t *testing.T) {
	t.Parallel()

	cases := map[string]func(*Turn){
		"reasoning": func(tn *Turn) { tn.Text = ""; tn.Reasoning = "thought" },
		"tool_calls": func(tn *Turn) {
			tn.Text = ""
			tn.ToolCalls = []ToolCall{{Tool: "bash"}}
		},
		"attachments": func(tn *Turn) {
			tn.Text = ""
			tn.Attachments = []ContentBlock{{Type: "image"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			rec := validRecord()
			mutate(&rec.Turns[1])
			if err := ValidateRecord(rec); err != nil {
				t.Fatalf("%s: unexpected error: %v", name, err)
			}
		})
	}
}

func TestValidateRecordRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Record)
		want   string
	}{
		{"record_id", func(r *Record) { r.RecordID = "   " }, "record_id"},
		{"origin", func(r *Record) { r.Origin = "" }, "origin"},
		{"grouping", func(r *Record) { r.Grouping = "" }, "grouping"},
		{"model", func(r *Record) { r.Model = "" }, "model"},
		{"turns", func(r *Record) { r.Turns = nil }, "turns"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := validRecord()
			tc.mutate(&rec)
			err := ValidateRecord(rec)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateRecordRejectsMalformedTimestamps(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*Record)
		want   string
	}{
		{"started_at", func(r *Record) { r.StartedAt = "yesterday" }, "started_at"},
		{"ended_at", func(r *Record) { r.EndedAt = "soon" }, "ended_at"},
		{"turn_timestamp", func(r *Record) { r.Turns[0].Timestamp = "nope" }, "turn 0 timestamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := validRecord()
			tc.mutate(&rec)
			err := ValidateRecord(rec)
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestValidateRecordRejectsUnknownTurnRole(t *testing.T) {
	t.Parallel()
	rec := validRecord()
	rec.Turns[1].Role = "system"
	err := ValidateRecord(rec)
	if err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Fatalf("expected invalid role error, got %v", err)
	}
}

func TestValidateRecordRejectsEmptyTurnContent(t *testing.T) {
	t.Parallel()
	rec := validRecord()
	rec.Turns[1] = Turn{Role: "assistant"}
	err := ValidateRecord(rec)
	if err == nil || !strings.Contains(err.Error(), "no content") {
		t.Fatalf("expected no-content error, got %v", err)
	}
}
