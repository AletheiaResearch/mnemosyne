package serialize

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func jsonlFromRecords(t *testing.T, records ...schema.Record) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	return &buf
}

func TestInferToolsCollectsDistinctNames(t *testing.T) {
	buf := jsonlFromRecords(t,
		schema.Record{Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "alpha", Input: map[string]any{"x": "hello"}},
				{Tool: "beta", Input: map[string]any{"n": float64(3)}},
			}},
		}},
		schema.Record{Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "alpha", Input: map[string]any{"x": "hi"}},
			}},
		}},
	)
	tools, err := InferTools(buf)
	if err != nil {
		t.Fatalf("InferTools: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(tools), tools)
	}
	if tools[0].Name != "alpha" || tools[1].Name != "beta" {
		t.Errorf("tools should be sorted by name, got %q, %q", tools[0].Name, tools[1].Name)
	}
	alphaProps := tools[0].Parameters["properties"].(map[string]any)
	if alphaProps["x"].(map[string]any)["type"] != "string" {
		t.Errorf("alpha.x type = %v, want string", alphaProps["x"])
	}
}

func TestInferToolsRequiredOnlyWhenSeenEveryCall(t *testing.T) {
	buf := jsonlFromRecords(t,
		schema.Record{Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "search", Input: map[string]any{"q": "a", "limit": float64(5)}},
			}},
		}},
		schema.Record{Turns: []schema.Turn{
			{Role: "assistant", ToolCalls: []schema.ToolCall{
				{Tool: "search", Input: map[string]any{"q": "b"}}, // no limit
			}},
		}},
	)
	tools, err := InferTools(buf)
	if err != nil {
		t.Fatalf("InferTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	required, _ := tools[0].Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "q" {
		t.Errorf("required = %v, want [q]", required)
	}
}

func TestInferToolsIgnoresNonMapInput(t *testing.T) {
	buf := jsonlFromRecords(t, schema.Record{Turns: []schema.Turn{
		{Role: "assistant", ToolCalls: []schema.ToolCall{
			{Tool: "plain", Input: "not a map"},
		}},
	}})
	tools, err := InferTools(buf)
	if err != nil {
		t.Fatalf("InferTools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("want 1 tool, got %d", len(tools))
	}
	props := tools[0].Parameters["properties"].(map[string]any)
	if len(props) != 0 {
		t.Errorf("non-map input should produce empty properties, got %+v", props)
	}
}

func TestMergeToolCatalogOverlaysDescriptionsAndDropsUnobserved(t *testing.T) {
	inferred := []ToolSchema{
		{Type: "function", Name: "read_file", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
		}},
	}
	catalog := []ToolSchema{
		{Type: "function", Name: "read_file", Description: "Read a file"},
		{Type: "function", Name: "never_called", Description: "Not observed in data"},
	}
	merged := MergeToolCatalog(inferred, catalog)
	if len(merged) != 1 {
		t.Fatalf("want only observed tools, got %d", len(merged))
	}
	if merged[0].Description != "Read a file" {
		t.Errorf("description not overlayed: %+v", merged[0])
	}
	if merged[0].Parameters == nil {
		t.Errorf("catalog entry without parameters should inherit inferred params")
	}
}

func TestReadToolSchemasSupportsWrappedAndFlat(t *testing.T) {
	body := `[
		{"type":"function","function":{"name":"wrapped","description":"via function"}},
		{"name":"flat","description":"top-level"}
	]`
	tools, err := ReadToolSchemas(strings.NewReader(body))
	if err != nil {
		t.Fatalf("ReadToolSchemas: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(tools))
	}
	byName := map[string]ToolSchema{}
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	if byName["wrapped"].Description != "via function" {
		t.Errorf("wrapped desc = %q", byName["wrapped"].Description)
	}
	if byName["flat"].Description != "top-level" {
		t.Errorf("flat desc = %q", byName["flat"].Description)
	}
}
