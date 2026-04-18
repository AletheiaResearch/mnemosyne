package serialize

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

// InferTools scans a canonical JSONL stream and synthesizes a best-effort
// list of tool schemas from the tool calls it observes.
//
// For each distinct tool name, it merges parameter names seen across every
// call, infers a JSON-schema "type" from the first observed value per
// parameter, and marks a parameter required only when it appears in every
// observation of that tool. Descriptions, return types, and strict typing
// are not recovered — that requires an authoritative catalog (see
// MergeToolCatalog).
func InferTools(r io.Reader) ([]ToolSchema, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	type paramStats struct {
		typeName string
		seen     int
	}
	type toolStats struct {
		calls  int
		params map[string]*paramStats
	}
	stats := make(map[string]*toolStats)

	for scanner.Scan() {
		var record schema.Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("infer tools: %w", err)
		}
		for _, turn := range record.Turns {
			for _, call := range turn.ToolCalls {
				if call.Tool == "" {
					continue
				}
				entry := stats[call.Tool]
				if entry == nil {
					entry = &toolStats{params: make(map[string]*paramStats)}
					stats[call.Tool] = entry
				}
				entry.calls++
				args, ok := call.Input.(map[string]any)
				if !ok {
					continue
				}
				for name, value := range args {
					p := entry.params[name]
					if p == nil {
						p = &paramStats{typeName: jsonSchemaType(value)}
						entry.params[name] = p
					}
					p.seen++
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(stats))
	for name := range stats {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ToolSchema, 0, len(names))
	for _, name := range names {
		entry := stats[name]
		properties := make(map[string]any, len(entry.params))
		required := make([]string, 0)
		paramNames := make([]string, 0, len(entry.params))
		for n := range entry.params {
			paramNames = append(paramNames, n)
		}
		sort.Strings(paramNames)
		for _, pname := range paramNames {
			p := entry.params[pname]
			properties[pname] = map[string]any{"type": p.typeName}
			if p.seen == entry.calls {
				required = append(required, pname)
			}
		}
		parameters := map[string]any{
			"type":       "object",
			"properties": properties,
		}
		if len(required) > 0 {
			parameters["required"] = required
		}
		out = append(out, ToolSchema{
			Type:       "function",
			Name:       name,
			Parameters: parameters,
		})
	}
	return out, nil
}

// jsonSchemaType maps a runtime JSON value to a JSON-schema primitive name.
func jsonSchemaType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32, int, int32, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "string"
	}
}

// MergeToolCatalog overlays authoritative schemas onto inferred ones. When a
// tool name matches, the catalog entry replaces the inferred one while
// preserving any catalog-only fields (descriptions, return types). Catalog
// tools that weren't observed in the data are NOT appended — we only describe
// tools the dataset actually exercises.
func MergeToolCatalog(inferred []ToolSchema, catalog []ToolSchema) []ToolSchema {
	if len(catalog) == 0 {
		return inferred
	}
	byName := make(map[string]ToolSchema, len(catalog))
	for _, tool := range catalog {
		byName[tool.Name] = tool
	}
	out := make([]ToolSchema, 0, len(inferred))
	for _, tool := range inferred {
		if cat, ok := byName[tool.Name]; ok {
			merged := cat
			if merged.Type == "" {
				merged.Type = tool.Type
			}
			if merged.Parameters == nil {
				merged.Parameters = tool.Parameters
			}
			out = append(out, merged)
			continue
		}
		out = append(out, tool)
	}
	return out
}

// ReadToolSchemas decodes a JSON tools file into []ToolSchema. The file may
// be a flat array of tool objects or the OpenAI-style {type, function: {...}}
// wrapper; both are accepted.
func ReadToolSchemas(r io.Reader) ([]ToolSchema, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse tools file: %w", err)
	}
	out := make([]ToolSchema, 0, len(raw))
	for _, entry := range raw {
		tool := ToolSchema{}
		if fn, ok := entry["function"].(map[string]any); ok {
			tool.Type, _ = entry["type"].(string)
			tool.Name, _ = fn["name"].(string)
			tool.Description, _ = fn["description"].(string)
			if params, ok := fn["parameters"].(map[string]any); ok {
				tool.Parameters = params
			}
			if ret, ok := fn["return"].(map[string]any); ok {
				tool.Return = ret
			}
		} else {
			tool.Type, _ = entry["type"].(string)
			tool.Name, _ = entry["name"].(string)
			tool.Description, _ = entry["description"].(string)
			if params, ok := entry["parameters"].(map[string]any); ok {
				tool.Parameters = params
			}
			if ret, ok := entry["return"].(map[string]any); ok {
				tool.Return = ret
			}
		}
		if tool.Type == "" {
			tool.Type = "function"
		}
		if tool.Name != "" {
			out = append(out, tool)
		}
	}
	return out, nil
}
