package schema

import (
	"fmt"
	"strings"
	"time"
)

func ValidateRecord(record Record) error {
	if strings.TrimSpace(record.RecordID) == "" {
		return fmt.Errorf("record_id is required")
	}
	if strings.TrimSpace(record.Origin) == "" {
		return fmt.Errorf("origin is required")
	}
	if strings.TrimSpace(record.Grouping) == "" {
		return fmt.Errorf("grouping is required")
	}
	if strings.TrimSpace(record.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if len(record.Turns) == 0 {
		return fmt.Errorf("turns must not be empty")
	}
	if record.StartedAt != "" {
		if _, err := time.Parse(time.RFC3339, record.StartedAt); err != nil {
			return fmt.Errorf("started_at: %w", err)
		}
	}
	if record.EndedAt != "" {
		if _, err := time.Parse(time.RFC3339, record.EndedAt); err != nil {
			return fmt.Errorf("ended_at: %w", err)
		}
	}
	for idx, turn := range record.Turns {
		if turn.Role != "user" && turn.Role != "assistant" {
			return fmt.Errorf("turn %d: invalid role %q", idx, turn.Role)
		}
		if turn.Timestamp != "" {
			if _, err := time.Parse(time.RFC3339, turn.Timestamp); err != nil {
				return fmt.Errorf("turn %d timestamp: %w", idx, err)
			}
		}
		if turn.Text == "" && turn.Reasoning == "" && len(turn.ToolCalls) == 0 && len(turn.Attachments) == 0 {
			return fmt.Errorf("turn %d has no content", idx)
		}
	}
	return nil
}
