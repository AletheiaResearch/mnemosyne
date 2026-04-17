# Input Category 1: Flat Per-Project JSONL With Optional Sub-Agent Sub-Traces

## Category profile

This category covers a command-line coding assistant that, for each
workspace on the user's machine, accumulates one folder of records
under a per-user configuration root. Each folder contains one
newline-delimited JSON file per conversation, and may contain
subordinate folders holding one JSONL per sub-agent spawned during a
conversation.

Storage location: under a per-user application data directory, in a
subfolder dedicated to "projects." The name of each per-workspace
folder is derived from the absolute filesystem path of the workspace
at the time the conversation occurred, with the path separators
encoded into a single-character substitute so the name is a single
directory component.

Per-conversation storage shape:

- Each line of the per-conversation JSONL is a JSON object whose
  `type` field distinguishes the record kind (user message,
  assistant message, tool-result message, and so on).
- Tool results are stored as user-role records that reference the
  originating tool invocation through a correlation identifier.
- Assistant records carry the model identifier, usage counters,
  and an ordered list of content blocks (text, reasoning, or
  tool-invocation).

## Discovery behavior

1. Walk the per-workspace folder root. For each direct child that
   is a directory, enumerate its top-level newline-delimited JSON
   files and any sub-agent JSONL files in a nested sub-folder
   (conventionally named for sub-agents).
2. Count the records as the sum of top-level JSONL files and
   sub-agent session folders.
3. Measure size as the sum of the byte sizes of all those files.
4. Compute the display label by decoding the encoded workspace
   path and returning a human-friendly suffix: drop the encoded
   home-directory prefix and any conventional user-level
   subdirectory names (such as `Documents`, `Downloads`,
   `Desktop`), and join the remainder back with the same
   separator used in the encoding.

## Extraction behavior

For every candidate JSONL file the extractor performs two passes:

1. **First pass: correlation table.** Walk all entries; for every
   entry that contains a tool-result block (identified by a
   correlation-identifier field), record the result's text and
   its success/error status under that identifier.
2. **Second pass: assembly.** Walk the entries in order:
   - Pick up the working directory, branch, client version, and
     record identifier from the first entry that carries them.
   - User entries contribute a user-role turn whose text is
     either the bare string content or a concatenation of the
     text blocks within a list-shaped content.
   - Assistant entries contribute an assistant-role turn whose
     text, reasoning, and tool invocations are drawn from the
     entry's content blocks. Each tool-invocation block is
     paired with its result from the correlation table, joined
     by the correlation identifier. The first assistant entry
     fixes the record's model identifier.
   - Token counters accumulate from each assistant entry's
     usage block; cached input counts are added to the
     input-token total.

Tool results handled in this category can carry auxiliary
structured data (for example created-file contents, edit deltas,
patch structures). The extractor attempts to deduplicate this
auxiliary payload against the assistant-side tool input (because
the same data often appears on both sides) and against the
canonical text output (same reason). Fields holding identical
values in both places are dropped from the output's raw side.

### Sub-agent sessions

Some conversations are accompanied by a sub-folder containing one
or more JSONL files, each corresponding to a sub-agent spawned
during the parent conversation. These are assembled into a
single synthetic record:

- Merge all entries in chronological order of timestamp.
- Run the same two-pass assembly as above.
- If a top-level JSONL with the same base name exists, the
  synthetic record's identifier is suffixed with a disambiguating
  marker; otherwise it inherits the base name directly.

## Field derivation

| Field | Source |
|---|---|
| Record identity | The per-conversation filename stem, or the correlation identifier written in the first entry, whichever appears first. |
| Grouping label | Decoded workspace-path suffix, as described under "Discovery behavior." |
| Model | First assistant entry's model identifier. |
| Branch | First entry carrying a branch field. |
| Working directory | First entry carrying a working-directory field, anonymized via the path rule. |
| Start/end timestamps | Earliest and latest user/assistant entry timestamps. |
| Turns | The assembled list from the second pass. |
| Usage tally | Accumulated user-turn, assistant-turn, tool-invocation counts, plus token totals. |

## Deduplication

No cross-record deduplication is performed for this category.

## Inherent limitations

- The encoded-path convention discards information about
  distinguishing workspaces whose absolute paths differed only in
  a character that coincides with the encoding separator; such
  collisions are displayed under a single grouping.
- If a conversation was abandoned before the first assistant
  turn, no model identifier can be derived, and the record is
  dropped per the rule in `03-output-format.md`.
- Auxiliary tool-result payloads that do not duplicate the
  canonical text are preserved faithfully, but their internal
  structure is not normalized beyond key-level recursion.
