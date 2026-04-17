package schema

type Provenance struct {
	SourcePath   string         `json:"source_path,omitempty"`
	SourceID     string         `json:"source_id,omitempty"`
	SourceOrigin string         `json:"source_origin,omitempty"`
	ExtractedAt  string         `json:"extracted_at,omitempty"`
	Version      string         `json:"version,omitempty"`
	Extensions   map[string]any `json:"extensions,omitempty"`
}
