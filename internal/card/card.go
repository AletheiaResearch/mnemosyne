package card

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

type Breakdown struct {
	Records      int `json:"records"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Summary struct {
	RecordCount    int                  `json:"record_count"`
	SkippedRecords int                  `json:"skipped_records"`
	RedactionCount int                  `json:"redaction_count"`
	InputTokens    int                  `json:"input_tokens"`
	OutputTokens   int                  `json:"output_tokens"`
	PerModel       map[string]Breakdown `json:"per_model"`
	PerGrouping    map[string]Breakdown `json:"per_grouping"`
	ExportedAt     string               `json:"exported_at,omitempty"`
}

func SummarizeFile(path string) (Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, err
	}
	defer file.Close()

	summary := Summary{
		PerModel:    make(map[string]Breakdown),
		PerGrouping: make(map[string]Breakdown),
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var record schema.Record
		if err := json.Unmarshal(raw, &record); err != nil {
			continue
		}
		summary.RecordCount++
		summary.RedactionCount += strings.Count(string(raw), redact.PlaceholderMarker)
		summary.InputTokens += record.Usage.InputTokens
		summary.OutputTokens += record.Usage.OutputTokens
		update(summary.PerModel, record.Model, record)
		update(summary.PerGrouping, record.Grouping, record)
	}
	return summary, scanner.Err()
}

func RenderDescription(summary Summary, fileName string, license string) string {
	var builder strings.Builder
	builder.WriteString("# Mnemosyne Dataset\n\n")
	builder.WriteString("Local conversation archive exported for archival, usage analysis, research contribution, and reproducibility.\n\n")
	builder.WriteString("## Summary\n\n")
	builder.WriteString("- Records: " + itoa(summary.RecordCount) + "\n")
	builder.WriteString("- Input tokens: " + itoa(summary.InputTokens) + "\n")
	builder.WriteString("- Output tokens: " + itoa(summary.OutputTokens) + "\n")
	builder.WriteString("- Redaction markers: " + itoa(summary.RedactionCount) + "\n")
	if license != "" {
		builder.WriteString("- License: " + license + "\n")
	}
	builder.WriteString("\n## Per Model\n\n")
	builder.WriteString(renderTable(summary.PerModel))
	builder.WriteString("\n## Per Grouping\n\n")
	builder.WriteString(renderTable(summary.PerGrouping))
	builder.WriteString("\n## Load Example\n\n")
	builder.WriteString("```python\n")
	builder.WriteString("import json\n\n")
	builder.WriteString("with open(\"" + fileName + "\", \"r\", encoding=\"utf-8\") as fh:\n")
	builder.WriteString("    records = [json.loads(line) for line in fh]\n")
	builder.WriteString("```\n")
	return builder.String()
}

func update(rows map[string]Breakdown, key string, record schema.Record) {
	row := rows[key]
	row.Records++
	row.InputTokens += record.Usage.InputTokens
	row.OutputTokens += record.Usage.OutputTokens
	rows[key] = row
}

func renderTable(rows map[string]Breakdown) string {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	builder.WriteString("| Key | Records | Input Tokens | Output Tokens |\n")
	builder.WriteString("| --- | ---: | ---: | ---: |\n")
	for _, key := range keys {
		row := rows[key]
		builder.WriteString("| " + key + " | " + itoa(row.Records) + " | " + itoa(row.InputTokens) + " | " + itoa(row.OutputTokens) + " |\n")
	}
	return builder.String()
}

func itoa(value int) string {
	return strconv.Itoa(value)
}
