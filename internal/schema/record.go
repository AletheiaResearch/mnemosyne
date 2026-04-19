package schema

// MaxJSONLineBytes is the maximum size of a single canonical-JSONL
// record any reader must accept. Extraction allows records this large,
// so every downstream step (validate, transform, attest, card
// summaries) has to agree or otherwise large-but-valid exports become
// unreadable mid-pipeline.
const MaxJSONLineBytes = 64 * 1024 * 1024

type Record struct {
	RecordID   string         `json:"record_id"`
	Origin     string         `json:"origin"`
	Grouping   string         `json:"grouping"`
	Model      string         `json:"model"`
	Branch     string         `json:"branch,omitempty"`
	StartedAt  string         `json:"started_at,omitempty"`
	EndedAt    string         `json:"ended_at,omitempty"`
	WorkingDir string         `json:"working_dir,omitempty"`
	Title      string         `json:"title,omitempty"`
	Turns      []Turn         `json:"turns"`
	Usage      Usage          `json:"usage"`
	Extensions map[string]any `json:"extensions,omitempty"`
	Provenance *Provenance    `json:"_mnemosyne,omitempty"`
}

type Turn struct {
	Role        string         `json:"role"`
	Timestamp   string         `json:"timestamp,omitempty"`
	Text        string         `json:"text,omitempty"`
	Reasoning   string         `json:"reasoning,omitempty"`
	ToolCalls   []ToolCall     `json:"tool_calls,omitempty"`
	Attachments []ContentBlock `json:"attachments,omitempty"`
	Extensions  map[string]any `json:"extensions,omitempty"`
}

type ToolCall struct {
	Tool   string      `json:"tool"`
	Input  any         `json:"input"`
	Output *ToolOutput `json:"output,omitempty"`
	Status string      `json:"status,omitempty"`
}

type ToolOutput struct {
	Text    string         `json:"text,omitempty"`
	Content []ContentBlock `json:"content,omitempty"`
	Raw     any            `json:"raw,omitempty"`
}
