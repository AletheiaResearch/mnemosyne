# Input Category 5: Hashed-Path Project Directories With Per-Session JSON Documents

## Category profile

This category covers a command-line coding assistant that organizes
per-workspace data under directories whose names are fixed-length
cryptographic-digest hashes of the workspace's absolute path. Inside
each such directory is a "chats" subdirectory containing one JSON
document per conversation.

Storage location: under a per-user application-data directory for
the assistant, in a subdirectory used for ephemeral project state.

Per-conversation storage shape:

- A single JSON document whose top level carries session identifier,
  start timestamp, last-updated timestamp, and a list of messages.
- Each message carries a type marker ("user" or the assistant's
  own identifier), a timestamp, and content. User content can be
  a string or a list of structured parts (text, inline data,
  file-data references, function-calls, function-responses).
  Assistant content carries optional thoughts (reasoning), a model
  identifier, tokens info, tool calls, and tool results.

## Discovery behavior

1. Walk the ephemeral-project-state root. Skip a special binary
   subdirectory by name.
2. For each child directory with a "chats" subdirectory containing
   one or more session JSON files, treat it as a grouping.
3. Resolve the hash-named directory to a human-readable display
   label by the following procedure:
   - Keep an in-memory mapping from digest to absolute path,
     populated by walking the user's home directory (and, on
     platforms that expose multiple drives, each drive root)
     and digesting every visible non-hidden child directory.
     The mapping is populated lazily on first need.
   - If the digest is not present in the mapping, walk the
     session files themselves looking for tool-invocation
     arguments whose path field, when truncated to any leading
     prefix, digests to the target directory name. If a match
     is found, record it and use the corresponding path.
   - Otherwise, fall back to displaying a short truncation of
     the digest.
4. Record count per grouping is the number of session files in
   the chats subdirectory. Size is the sum of file byte sizes.

## Extraction behavior

For each session file:

1. Load the JSON document. If parse fails, warn and skip.
2. Initialize record metadata from the document's top-level
   session identifier and timestamps.
3. Walk messages in order:
   - User messages whose content is a string produce user-role
     turns with that string as text.
   - User messages whose content is a list iterate over parts:
     - Text parts contribute to the turn's text.
     - Inline-data parts contribute an attached-content entry
       of the appropriate kind (image if the media-type begins
       with the image prefix, otherwise document).
     - File-data parts contribute an attached-content entry
       with a URL-style source, the URL's path anonymized.
     - Function-call parts are treated as the user
       replaying a tool call; they contribute a tool-use
       entry with a generated correlation identifier.
     - Function-response parts pair with the most recent
       function-call for the same tool name and contribute a
       tool-result entry.
   - Assistant messages produce assistant-role turns.
     Thoughts' description fields concatenate into the
     reasoning trace. Tool calls are parsed per the
     per-tool-name rules (see below). Token counters
     accumulate from the message's tokens block (cached
     input counts are added to the input-token total).

### Tool-call argument handling

Because different tool names in this category take
different argument shapes, the extractor applies a
name-dispatched parser that anonymizes paths, preserves
scalar arguments, and anonymizes free-text arguments. The
categories of tool names handled with special shapes
include:

- File-read tools (single path argument).
- File-write tools (path plus content).
- In-place edit tools (path, old string, new string,
  optional instruction).
- Shell-command tools (single command argument).
- Multi-file-read tools (list of paths, plus a structured
  output preserving each file's path and content).
- Search tools (all arguments anonymized as text).
- List-directory tools (directory path plus optional
  ignore patterns).
- Glob tools (pattern preserved as-is).
- Web-search and web-fetch tools (all string arguments
  anonymized).

For any tool name the extractor does not recognize, it
falls back to anonymizing every string argument as text.

### Tool-output handling

Some tool names have output shapes worth parsing:

- The multi-file-read tool's output is a stream of
  per-file sections delimited by a marker line; the
  extractor splits on those markers and emits a
  structured list of `{path, content}` pairs, each with
  path and content anonymized.
- The shell-command tool's output is a key/value text
  dump with a handful of leading markers (command,
  directory, output, exit code); the extractor parses
  those into a structured object and anonymizes each
  value.
- All other tools' outputs become a simple text-only
  output payload.

## Field derivation

| Field | Source |
|---|---|
| Record identity | The session identifier from the document, or the filename stem as fallback. |
| Model | The first assistant message that declared one. |
| Start/end timestamps | Document-level start timestamp and last-updated timestamp. |
| Working directory | Not directly stored; may be recovered from the hash-to-path mapping during discovery but is not re-emitted on every record. |

## Deduplication

This category's source data can produce near-duplicate records
across sessions (typically because the same conversation is
persisted through multiple checkpoints). The extractor computes a
fingerprint per record as follows:

1. Take the canonicalized record (with the grouping label
   removed, because two near-duplicate records may have been
   grouped under different resolved paths for the same
   underlying hash).
2. Serialize to a canonical JSON form (sorted keys, compact
   separators).
3. Compute a cryptographic digest of the serialization.
4. Keep a set of seen digests during a single extraction and
   skip any record whose digest is already present.

## Inherent limitations

- The hash-to-path mapping is best-effort. Workspaces that have
  been renamed or deleted since the conversation was recorded
  will display with a truncated-digest label.
- The ephemeral-project-state root may contain directories not
  related to conversations; the extractor skips a known
  binary-storage subdirectory by name but cannot detect other
  unrelated content.
- Branch information is not stored.
