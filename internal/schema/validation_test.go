package schema

import "testing"

func TestValidateRecord(t *testing.T) {
	t.Parallel()

	record := Record{
		RecordID: "rec-1",
		Origin:   "test",
		Grouping: "project",
		Model:    "model",
		Turns: []Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "hi"},
		},
	}

	if err := ValidateRecord(record); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
