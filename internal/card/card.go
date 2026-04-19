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

// RenderIsolateDescription renders the README.md uploaded alongside
// manifest.mnemosyne and per-session JSONL files in isolate mode. The layout
// mirrors HuggingFace's Agent Trace Viewer expectations so datasets are
// auto-tagged `Traces` and viewable per-session.
func RenderIsolateDescription(header ManifestHeader, entries []ManifestEntry, license string) string {
	var builder strings.Builder
	builder.WriteString("# Mnemosyne Isolate Dataset\n\n")
	builder.WriteString("Native Claude Code and Codex session traces exported by mnemosyne with per-session fidelity preserved. ")
	builder.WriteString("Each session is stored under its original format so the [HuggingFace Agent Trace Viewer](https://huggingface.co/changelog/agent-trace-viewer) ")
	builder.WriteString("renders them individually.\n\n")

	byFormat := make(map[string]int)
	totalLines := 0
	for _, entry := range entries {
		byFormat[entry.Format]++
		totalLines += entry.Lines
	}

	keySummary := SummarizeRedactionKeys(entries)
	builder.WriteString("## Summary\n\n")
	builder.WriteString("- Sessions: " + itoa(len(entries)) + "\n")
	builder.WriteString("- Total lines: " + itoa(totalLines) + "\n")
	builder.WriteString("- Attach images: " + describeAttachImages(keySummary, header.AttachImages) + "\n")
	if header.PipelineFingerprint != "" {
		builder.WriteString("- Pipeline fingerprint (this publish): `" + header.PipelineFingerprint + "`\n")
	}
	if header.ConfigFingerprint != "" {
		builder.WriteString("- Config fingerprint (this publish): `" + header.ConfigFingerprint + "`\n")
	}
	if header.ExportedAt != "" {
		builder.WriteString("- Exported at: " + header.ExportedAt + "\n")
	}
	if license != "" {
		builder.WriteString("- License: " + license + "\n")
	}

	if keySummary.Mixed() || len(keySummary.ByKey) > 1 {
		builder.WriteString("\n## Sessions by Redaction Key\n\n")
		builder.WriteString("This dataset contains sessions published under more than one redaction configuration. ")
		builder.WriteString("Each session is authoritative for its own redaction key; the per-publish fingerprints above describe only the latest publish.\n\n")
		keys := make([]string, 0, len(keySummary.ByKey))
		for key := range keySummary.ByKey {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteString("| Redaction key | Sessions |\n| --- | ---: |\n")
		for _, key := range keys {
			display := key
			if display == "" {
				display = "(unknown)"
			}
			builder.WriteString("| `" + display + "` | " + itoa(keySummary.ByKey[key]) + " |\n")
		}
	}

	if len(byFormat) > 0 {
		builder.WriteString("\n## Sessions by Format\n\n")
		formats := make([]string, 0, len(byFormat))
		for format := range byFormat {
			formats = append(formats, format)
		}
		sort.Strings(formats)
		builder.WriteString("| Format | Sessions |\n| --- | ---: |\n")
		for _, format := range formats {
			builder.WriteString("| " + format + " | " + itoa(byFormat[format]) + " |\n")
		}
	}

	builder.WriteString("\n## Layout\n\n")
	builder.WriteString("```\n")
	builder.WriteString("claudecode/<hash>/<session>.jsonl   # native Claude Code session lines\n")
	builder.WriteString("codex/<hash>/<session>.jsonl        # native Codex session lines\n")
	builder.WriteString("manifest.mnemosyne                   # per-session hashes + redaction keys\n")
	builder.WriteString("README.md                            # this file\n")
	builder.WriteString("```\n")
	builder.WriteString("\nThe `<hash>` subdirectory namespaces same-named sessions from different source directories so moves don't collide.\n")

	builder.WriteString("\n## Load Example\n\n")
	builder.WriteString("```python\n")
	builder.WriteString("from datasets import load_dataset\n\n")
	builder.WriteString("claudecode = load_dataset(\"json\", data_files=\"claudecode/**/*.jsonl\", split=\"train\")\n")
	builder.WriteString("codex = load_dataset(\"json\", data_files=\"codex/**/*.jsonl\", split=\"train\")\n")
	builder.WriteString("```\n")

	return builder.String()
}

func boolYesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

// describeAttachImages renders the dataset-wide image-attachment state
// from the observed per-session redaction keys. Unknown-suffix entries
// are indeterminate — they coexist with keep or strip sessions as a
// genuine mix rather than getting silently absorbed into the majority.
// Falls back to the header's per-publish claim only when no entry has
// a recognisable key (e.g. a freshly-seeded dataset with empty keys).
func describeAttachImages(summary RedactionKeySummary, publishAttach bool) string {
	switch {
	case summary.Mixed():
		return "mixed (" + itoa(summary.Keep) + " keep / " + itoa(summary.Strip) + " strip)"
	case summary.Keep > 0 && summary.Unknown > 0:
		return "mixed (" + itoa(summary.Keep) + " keep / " + itoa(summary.Unknown) + " unknown)"
	case summary.Strip > 0 && summary.Unknown > 0:
		return "mixed (" + itoa(summary.Strip) + " strip / " + itoa(summary.Unknown) + " unknown)"
	case summary.AllKeep():
		return "yes"
	case summary.AllStrip():
		return "no"
	default:
		return boolYesNo(publishAttach)
	}
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
