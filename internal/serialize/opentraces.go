package serialize

import (
	"github.com/google/uuid"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

// openTracesSchemaVersion pins the emitted schema_version. The mapping
// targets https://www.opentraces.ai/schema/latest at this version.
const openTracesSchemaVersion = "0.7.0"

// openTracesNamespace seeds deterministic v5 trace IDs so re-running
// transform over the same input yields byte-identical output. The reference
// capture tooling assigns random UUIDs, but mnemosyne records already carry
// a stable upstream identity, and content_hash excludes trace_id by design,
// so determinism costs nothing.
var openTracesNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("https://github.com/AletheiaResearch/mnemosyne/opentraces"))

type OpenTraces struct{}

func (OpenTraces) Name() string {
	return "opentraces"
}

func (OpenTraces) Description() string {
	return "Emit records as OpenTraces v0.7.0 trace records."
}

// The wire types declare only the fields mnemosyne can populate. The
// OpenTraces reference models default-fill everything absent, so omitting
// empty optionals keeps lines lean while staying schema-valid. Fields the
// schema requires carry no omitempty.
//
// content_hash is deliberately not emitted: the reference implementation
// hashes the fully default-filled model as Python-canonical JSON (sorted
// keys, ", "/": " separators, ensure_ascii, Python float repr), which Go
// cannot reproduce byte-for-byte. A divergent hash would defeat the
// field's cross-contributor dedup purpose, and the reference serializer
// recomputes it at write time anyway.
type openTracesRecord struct {
	SchemaVersion    string                 `json:"schema_version"`
	TraceID          string                 `json:"trace_id"`
	SessionID        string                 `json:"session_id"`
	TimestampStart   string                 `json:"timestamp_start,omitempty"`
	TimestampEnd     string                 `json:"timestamp_end,omitempty"`
	ExecutionContext string                 `json:"execution_context,omitempty"`
	Task             *openTracesTask        `json:"task,omitempty"`
	Agent            openTracesAgent        `json:"agent"`
	Environment      *openTracesEnvironment `json:"environment,omitempty"`
	Steps            []openTracesStep       `json:"steps"`
	Metrics          openTracesMetrics      `json:"metrics"`
	Metadata         map[string]any         `json:"metadata,omitempty"`
}

type openTracesTask struct {
	Description string `json:"description,omitempty"`
}

type openTracesAgent struct {
	Name  string `json:"name"`
	Model string `json:"model,omitempty"`
}

type openTracesEnvironment struct {
	VCS openTracesVCS `json:"vcs"`
}

type openTracesVCS struct {
	Type   string `json:"type"`
	Branch string `json:"branch,omitempty"`
}

type openTracesStep struct {
	StepIndex        int                     `json:"step_index"`
	Role             string                  `json:"role"`
	Content          string                  `json:"content,omitempty"`
	ReasoningContent string                  `json:"reasoning_content,omitempty"`
	Timestamp        string                  `json:"timestamp,omitempty"`
	ToolCalls        []openTracesToolCall    `json:"tool_calls,omitempty"`
	Observations     []openTracesObservation `json:"observations,omitempty"`
}

type openTracesToolCall struct {
	ToolCallID string         `json:"tool_call_id"`
	ToolName   string         `json:"tool_name"`
	Input      map[string]any `json:"input,omitempty"`
}

type openTracesObservation struct {
	SourceCallID string `json:"source_call_id"`
	Content      string `json:"content,omitempty"`
	Error        string `json:"error,omitempty"`
}

type openTracesMetrics struct {
	TotalSteps        int `json:"total_steps"`
	TotalInputTokens  int `json:"total_input_tokens"`
	TotalOutputTokens int `json:"total_output_tokens"`
}

func (OpenTraces) Serialize(record schema.Record) (any, error) {
	out := openTracesRecord{
		SchemaVersion:    openTracesSchemaVersion,
		SessionID:        openTracesSessionID(record),
		TimestampStart:   record.StartedAt,
		TimestampEnd:     record.EndedAt,
		ExecutionContext: "devtime",
		Agent:            openTracesAgentIdentity(record),
		Steps:            openTracesSteps(record.Turns),
		Metadata:         openTracesMetadata(record),
	}
	if record.Title != "" {
		out.Task = &openTracesTask{Description: record.Title}
	}
	if record.Branch != "" {
		out.Environment = &openTracesEnvironment{VCS: openTracesVCS{Type: "git", Branch: record.Branch}}
	}
	out.Metrics = openTracesMetrics{
		TotalSteps:        len(out.Steps),
		TotalInputTokens:  record.Usage.InputTokens,
		TotalOutputTokens: record.Usage.OutputTokens,
	}
	out.TraceID = uuid.NewSHA1(openTracesNamespace, []byte(record.RecordID)).String()
	return out, nil
}

func openTracesSessionID(record schema.Record) string {
	if record.Provenance != nil && record.Provenance.SourceID != "" {
		return record.Provenance.SourceID
	}
	return record.RecordID
}

func openTracesAgentIdentity(record schema.Record) openTracesAgent {
	name := record.Origin
	switch name {
	case "claudecode":
		// Match the agent name emitted by the reference capture tooling.
		name = "claude-code"
	case "":
		name = "unknown"
	}
	return openTracesAgent{Name: name, Model: record.Model}
}

func openTracesSteps(turns []schema.Turn) []openTracesStep {
	steps := make([]openTracesStep, 0, len(turns))
	for i, turn := range turns {
		step := openTracesStep{
			StepIndex:        i,
			Role:             openTracesRole(turn.Role),
			Content:          turn.Text,
			ReasoningContent: turn.Reasoning,
			Timestamp:        turn.Timestamp,
		}
		if step.Content == "" && len(turn.Attachments) > 0 {
			step.Content = "[attachments omitted]"
		}
		for j, call := range turn.ToolCalls {
			id := toolCallID(i, j, call)
			step.ToolCalls = append(step.ToolCalls, openTracesToolCall{
				ToolCallID: id,
				ToolName:   call.Tool,
				Input:      toolArguments(call.Input),
			})
			if observation, ok := openTracesObservationFor(id, call); ok {
				step.Observations = append(step.Observations, observation)
			}
		}
		steps = append(steps, step)
	}
	return steps
}

func openTracesRole(role string) string {
	switch role {
	case "user", "system":
		return role
	case "assistant":
		return "agent"
	default:
		// Canonical records only carry user and assistant turns; treat
		// anything unexpected as user input rather than attributing it to
		// the model.
		return "user"
	}
}

func openTracesObservationFor(id string, call schema.ToolCall) (openTracesObservation, bool) {
	text := toolOutputText(call.Output)
	if call.Status == "error" {
		if text == "" {
			text = "error"
		}
		return openTracesObservation{SourceCallID: id, Error: text}, true
	}
	if call.Output == nil {
		return openTracesObservation{}, false
	}
	return openTracesObservation{SourceCallID: id, Content: text}, true
}

// openTracesMetadata preserves the canonical fields that have no OpenTraces
// slot, plus the inputs of lossy mappings (origin rename, hashed trace_id),
// so the export stays reversible.
func openTracesMetadata(record schema.Record) map[string]any {
	mnemosyne := map[string]any{"record_id": record.RecordID}
	if record.Origin != "" {
		mnemosyne["origin"] = record.Origin
	}
	if record.Grouping != "" {
		mnemosyne["grouping"] = record.Grouping
	}
	if record.WorkingDir != "" {
		mnemosyne["working_dir"] = record.WorkingDir
	}
	if len(record.Extensions) > 0 {
		mnemosyne["extensions"] = record.Extensions
	}
	if record.Provenance != nil {
		mnemosyne["provenance"] = record.Provenance
	}
	return map[string]any{"mnemosyne": mnemosyne}
}
