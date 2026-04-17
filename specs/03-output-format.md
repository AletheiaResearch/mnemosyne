# Output Format

The utility's local extraction emits a stream of structured records to a
single file. This document specifies the semantic content each record
must carry, the structural shape it takes, and the constraints the
writer must honour. Implementers choose the specific field names and
small encoding details; the semantic contract is fixed.

> **Note on field names.** The illustrative schema shown later in this
> document uses placeholder field names chosen by this specification for
> the purpose of giving structure to the examples. The implementer
> selects their own names. The semantic content each field represents
> is the contract; the spelling of the name is not. Two implementations
> can both conform to this specification while using different field
> spellings, as long as each field's semantic content is preserved.

## File shape

- One record per line.
- Each line is a complete, self-contained JSON document.
- UTF-8 encoding, no byte-order mark.
- Lines terminated by a single line-feed character.
- Empty lines are not permitted between records.
- The file has no header or footer; the first and last lines are
  records.

This shape is chosen so that downstream tooling can stream-parse the
file without first loading it into memory.

## Companion files

When the utility uploads the records file to a remote platform
(`publish`), it also uploads:

- A **metadata manifest**: a single JSON document describing the
  export as a whole — total counts, breakdowns, aggregate usage
  statistics, the export timestamp.
- A **human-readable description**: a Markdown document suitable
  for display on the platform's repository page. Content includes
  a summary of the export, the tables from the metadata manifest,
  a small example of the record shape, and a snippet showing
  downstream consumers how to load the data.

Neither companion file is written during local extraction. They are
constructed in-memory at publish time.

## Record content

Each record carries one conversation between a user and a coding
assistant. The semantic content is:

| Concept | Meaning | Cardinality |
|---|---|---|
| Record identity | A stable, unique identifier for this record. Derived from the source data when the source supplies one; otherwise synthesized in a deterministic way (for example from the source filename). | exactly one |
| Origin category | Which kind of coding-assistant product produced the source data this record was built from. Values are from the enumerated set supported by the utility. | exactly one |
| Grouping label | The canonical display label of the workspace or project this conversation was tied to at the source. | exactly one |
| Model identifier | Canonical string naming the model that handled the conversation. Records for which no identifier can be determined are dropped, not exported. | exactly one |
| Branch (optional) | The version-control branch the conversation was conducted on, if the source recorded one. | zero or one |
| Start timestamp | Timestamp of the earliest turn in the record, in ISO-8601 UTC. Absent if no turn bears a timestamp. | zero or one |
| End timestamp | Timestamp of the latest turn in the record, in ISO-8601 UTC. Absent if no turn bears a timestamp. | zero or one |
| Turns | The ordered list of exchanges between user and assistant. | exactly one list; must not be empty, else the record is dropped |
| Usage tally | Aggregate counters for the record as a whole. | exactly one |
| Working directory (optional) | The anonymized representation of the directory the conversation was run from. Some origin categories supply this; others do not. | zero or one |

## Turn content

Each entry in the turns list represents one user or assistant message.
A turn carries:

| Concept | Meaning | Cardinality |
|---|---|---|
| Speaker role | Either "user" or "assistant". No other values are produced. | exactly one |
| Timestamp | ISO-8601 UTC timestamp of this turn, if the source supplied one. | zero or one |
| Text content | The human-readable text portion of the turn, after anonymization and redaction. May be absent for a turn that carries only attached content or only tool invocations. | zero or one |
| Reasoning trace | For assistant turns, an optional free-form string representing the assistant's private reasoning. Present only when the source supplies it and the caller did not suppress it via the relevant flag. | zero or one |
| Tool invocations | For assistant turns, an ordered list of tool calls initiated by the assistant during the turn. Each invocation carries the tool name, the input arguments, the paired output, and a status. | zero or one |
| Attached content | An optional ordered list of structured content parts — typically attachments such as images or referenced files. Used when the source supplies structured user content that cannot be collapsed into plain text. | zero or one |

A turn must not be written if it contains none of text, reasoning trace,
tool invocations, or attached content.

## Tool-invocation content

Each tool invocation carries:

| Concept | Meaning | Cardinality |
|---|---|---|
| Tool name | The source's name for the invoked tool. | exactly one |
| Input arguments | A nested structure (object or scalar) representing the arguments the assistant passed. Path-shaped values are passed through the anonymizer; command-shaped values are redacted for secrets before anonymization. All other strings are anonymized. | exactly one |
| Output | A structure carrying the result of the invocation. Carries at minimum a normalized text field (when a text summary can be produced) and optionally a structured raw field preserving source-specific extras (exit code, stderr, timing, structured result payloads). Either or both may be absent. | zero or one |
| Status | A short string (typically "success" or "error") summarizing the outcome. Absent when the source did not supply one. | zero or one |

## Attached-content shape

Each entry in the attached-content list describes one non-text piece of
user-supplied content. Supported kinds are at minimum:

- An image payload, carried either as an inline base64 blob with a
  media-type tag or as a URL reference.
- A document payload, carried either as a base64 blob with a media-
  type tag or as a URL reference.

For file-backed references, the URL path component is anonymized
using the same rules as any other path (see
`05-sensitive-content-handling.md`). For inline base64 blobs, the
payload bytes are left intact; anonymization does not recurse into
base64 contents.

## Usage tally

The usage-tally object carries numeric counters for the record as a
whole. The required counters are:

| Counter | Meaning |
|---|---|
| User-turn count | Number of user-role turns in the record. |
| Assistant-turn count | Number of assistant-role turns in the record. |
| Tool-invocation count | Total number of tool invocations across all assistant turns. |
| Input-token count | The source's reported count of input tokens consumed, summed across all turns. Where the source distinguishes cached from non-cached input, the counter sums both. |
| Output-token count | The source's reported count of output tokens produced, summed across all turns. |

A source that does not report token counts yields zeros for the token
counters rather than omitting them.

## Skipping and deduplication rules

During extraction the writer must:

1. **Drop empty records.** If the candidate record has zero turns
   after extraction, the record is skipped (and does not appear in
   the final file).
2. **Drop records with no model identifier.** If the source did not
   supply a model identifier and no fallback could be computed, the
   record is skipped. A counter of skipped records is reported in
   the extraction summary.
3. **Drop abandoned or marker-only records.** If the source's model
   field is a synthetic placeholder for an abandoned or crashed
   session, the record is skipped.
4. **Deduplicate where warranted.** For origin categories that can
   produce duplicate records (see the relevant input-category file),
   the writer computes a canonical fingerprint of the record and
   skips subsequent records with the same fingerprint.

## Metadata manifest

The manifest accompanying an upload carries:

- Total record count.
- Skipped record count from the last extraction.
- Total redaction count.
- Per-model breakdown: for each distinct model identifier, the number
  of records and the aggregate input/output token counts.
- Per-grouping breakdown: for each grouping that appears in the
  file, the number of records and the aggregate token counts.
- Aggregate input-token count across all records.
- Aggregate output-token count across all records.
- Export timestamp (ISO-8601 UTC).

Both breakdowns are keyed by the canonical normalized form of the
underlying string (see "Canonicalization of breakdown keys" below).

The manifest's own key names are an implementer choice. The semantic
contents above are required; the spellings used to label them in the
manifest file itself are at the implementer's discretion, subject only
to the manifest being self-describing enough that a downstream
consumer can interpret it without external documentation.

## Canonicalization of breakdown keys

Breakdown keys are canonicalized by a stable transformation the
implementer chooses, applied consistently across the utility. The
purpose of canonicalization is to keep aggregations stable: two
records whose underlying identifiers are the same up to incidental
formatting differences must contribute to the same breakdown row.
A typical transformation strips whitespace, drops origin-category
prefixes where present, and replaces common word-separator
punctuation with a single canonical separator. Whether to lowercase
is an implementer judgement call — some platforms are
case-sensitive in identifier semantics, and aggressive lowercasing
can collide distinct identifiers; conservative implementations
preserve case and rely on punctuation normalization alone.

## Illustrative schema

The following example shows the structural shape of one record,
using field names invented here for illustration only; the
implementer must choose their own names. This example is
normative only for structure and semantics, not for names.

```
{
  "record_id":           "<string>",
  "origin":              "<string, one of the supported origin identifiers>",
  "grouping":            "<string>",
  "model":               "<string>",
  "branch":              "<string or absent>",
  "started_at":          "<ISO-8601 UTC string or absent>",
  "ended_at":            "<ISO-8601 UTC string or absent>",
  "working_dir":         "<string or absent>",
  "turns": [
    {
      "role":         "user",
      "timestamp":    "<ISO-8601 UTC string or absent>",
      "text":         "<string or absent>",
      "attachments":  [ /* see attached-content shape */ ]
    },
    {
      "role":         "assistant",
      "timestamp":    "<ISO-8601 UTC string or absent>",
      "text":         "<string or absent>",
      "reasoning":    "<string or absent>",
      "tool_calls": [
        {
          "tool":   "<string>",
          "input":  { /* arbitrary nested structure */ },
          "output": {
            "text": "<string or absent>",
            "raw":  { /* arbitrary nested structure or absent */ }
          },
          "status": "<string or absent>"
        }
      ]
    }
  ],
  "usage": {
    "user_turns":          0,
    "assistant_turns":     0,
    "tool_calls":          0,
    "input_tokens":        0,
    "output_tokens":       0
  }
}
```

The example above uses placeholder names. Implementers select names
that suit their language conventions; consumers of the file should
discover the names from the manifest and the description rather
than assume any particular spelling.
