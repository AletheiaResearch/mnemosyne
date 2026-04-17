# Input Category 6: Hashed-Path Project Directories With Per-Session Context JSONL

## Category profile

This category covers a command-line coding assistant that organizes
per-workspace data under directories whose names are fixed-length
cryptographic-digest hashes of the workspace's absolute path. Inside
each per-project directory, each individual conversation lives in its
own session sub-directory, and each session sub-directory holds a
single newline-delimited JSON file containing the message history.

A separate configuration file at the top level of the assistant's
data directory enumerates the known workspaces with their absolute
paths, enabling reverse-lookup from hash to display label.

Storage location: under a per-user application-data directory for
the assistant. The configuration file is a sibling of the
per-workspace storage subtree.

Per-session file shape:

- Each line is a JSON object with a `role` field. Supported roles
  include "user," "assistant," and an internal usage marker.
- Assistant lines carry a content list of typed blocks (text and
  reasoning/thought). They may carry a list of tool-call objects
  alongside the content.
- Tool-call objects nest a function wrapper whose name and
  arguments follow a provider-standard shape; arguments may be
  serialized as a JSON string.
- A special role acts as a usage marker that updates the token
  counter rather than contributing to the message list.

## Discovery behavior

1. If the configuration file exists and is parseable, read its
   list of known workspace paths; compute each workspace's digest
   for later mapping.
2. Walk the storage root. For each per-project directory (named
   by digest):
   - Enumerate its session sub-directories.
   - Count sessions as the number of sub-directories containing a
     context-file named by convention.
   - Sum the byte sizes of those context files.
   - If the project's digest is in the mapping, use the
     workspace basename as the display label; otherwise use a
     truncated digest.
   - The grouping's canonical identifier is the resolved
     workspace path when known, and the digest otherwise.

## Extraction behavior

For each session sub-directory belonging to the selected grouping,
read the per-session file:

1. Parse each line in order.
2. User-role lines contribute user-role turns with string content,
   anonymized.
3. Assistant-role lines produce assistant-role turns. The content
   list is walked:
   - Text blocks contribute to the turn's text.
   - Reasoning blocks contribute to the turn's reasoning (when
     the reasoning opt-in is active).
   - Tool-call siblings are parsed as tool invocations; argument
     strings that are themselves JSON-serialized strings are
     re-parsed before being passed through the shared
     tool-input anonymizer.
4. The usage-marker role updates the record's output-token
   counter by taking the maximum of its prior value and the new
   reported value.

## Field derivation

| Field | Source |
|---|---|
| Record identity | The session sub-directory name. |
| Model | A category-default constant assigned at discovery time if no per-record model identifier is present. |
| Branch | Not available. |
| Working directory | The resolved workspace path, anonymized. |
| Start/end timestamps | Not available in this category; absent in the record. |
| Tokens (input) | Not reported by this category; zero. |

## Deduplication

No cross-record deduplication.

## Inherent limitations

- Timestamps are not reported by the source; turn-level and
  record-level timestamp fields are absent.
- Only output tokens are reported; input-token count is always
  zero.
- Branch information is not reported.
- When the source's configuration file is missing or corrupt,
  every grouping displays as a truncated digest.
- The model identifier is a category-level default; records
  cannot distinguish between different model versions within
  this category.
