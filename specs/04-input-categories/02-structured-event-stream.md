# Input Category 2: Structured-Event JSONL With Meta and Turn-Context Records

## Category profile

This category covers a command-line coding assistant that writes, for
each conversation, a single newline-delimited JSON file whose entries
form a typed event stream. Event types include session metadata, turn
context, assistant-response items, and various event messages. A
separate archive folder holds finalized or rotated-out sessions.

Storage location: under a per-user application data directory, in a
subfolder dedicated to "sessions," with session files typically
arranged in a nested date-based folder tree. Archived sessions live
in a sibling subfolder dedicated to "archived sessions."

Per-conversation storage shape:

- The first usable entry is a session-metadata record carrying the
  conversation identifier, the working directory, the git branch,
  and the model-provider identifier.
- Subsequent entries are typed. Relevant types include
  turn-context (which can carry model identifier and refreshed
  working-directory info), response-item (which carries user
  messages, assistant messages, tool function-calls, custom
  tool calls, function-call outputs, and reasoning summaries),
  and event-message (which carries token-count updates, agent
  reasoning events, user-message events, and agent-message
  events).

## Discovery behavior

1. Walk the sessions folder recursively and enumerate every
   newline-delimited JSON file. Do the same for the archive
   folder at the top level.
2. For each file, probe until the first metadata or turn-context
   entry is found, and take the working directory from there.
3. Index the files by working directory. Each distinct working
   directory becomes one grouping.
4. Records count is the number of files that claim that working
   directory. Size is the sum of the files' byte sizes.
5. The grouping's canonical identifier is the working-directory
   string itself; the display label is the origin-category
   prefix concatenated with the base name of the working
   directory.

A placeholder marker is used for any file that declares no
working directory; all such files collect under a single
"unknown working directory" grouping.

## Extraction behavior

For each file belonging to the selected working directory, the
extractor runs a stateful pass:

1. **First pass: correlation table.** Walk all entries; for every
   response-item entry of type function-call-output or
   custom-tool-call-output, extract the output text, parse out
   any embedded exit code and timing, and store under the
   call's correlation identifier.
2. **Second pass: event assembly.** Carry state across entries:
   - A metadata entry sets working directory (if not already
     set), model-provider identifier, git branch, and the
     conversation's official identifier.
   - A turn-context entry may update the working directory and
     the model identifier.
   - A response-item of user-message type with image-input
     parts contributes pending attached-content parts to the
     next user turn.
   - A response-item of function-call type contributes a
     pending tool invocation with the correlation identifier
     stashed for later resolution.
   - A response-item of custom-tool-call type does the same,
     with a raw-patch style input.
   - A response-item of reasoning type contributes a pending
     reasoning-trace string (deduplicated against previously
     seen reasoning in this record).
   - An event-message of token-count type updates the
     record's running maximum-observed input-token and
     output-token counts. The final usage tally uses these
     observed maxima, not a sum.
   - An event-message of agent-reasoning type contributes
     another reasoning-trace string.
   - An event-message of user-message type flushes any
     pending assistant content into an assistant turn, then
     emits the user turn.
   - An event-message of agent-message type flushes any
     pending reasoning and tool invocations into an
     assistant turn.
3. At end-of-file, any remaining pending content is flushed
   into a final turn.
4. When the observed working directory differs from the
   selected grouping's working directory, the record is
   discarded. This accounts for single files that were moved
   between groupings during the conversation.

## Field derivation

| Field | Source |
|---|---|
| Record identity | From the session-metadata entry when present; otherwise the file-name stem. |
| Model | The turn-context model field if present; otherwise the metadata model-provider concatenated with a category-specific suffix; otherwise a synthetic "unknown" marker. |
| Branch | From the session-metadata entry. |
| Working directory | From the metadata entry, anonymized via the path rule. |
| Tool invocations | Assembled from function-call and custom-tool-call response items, paired with outputs via the correlation table. |
| Attached content | Built from input-image parts of user response items and from images listed in user-message events. URL-encoded base64 payloads become base64 attachments; file-URI references become URL attachments whose path portion is anonymized. |
| Timestamps | ISO-8601 UTC timestamps are taken as-is; numeric epoch-millisecond timestamps are converted. |

## Deduplication

No cross-record deduplication is performed for this category.

## Inherent limitations

- The token counters are observed maxima rather than sums. If
  the source reports a running total (rather than incrementals),
  this yields the correct final value; if it reports
  incrementals, this will under-count. The choice is dictated by
  the source's own semantics.
- A conversation split across multiple working directories (for
  example, a user who renamed a folder mid-conversation) may be
  dropped if its working directory at the time of assembly
  doesn't match the grouping under consideration.
- Reasoning traces can appear both as response-item reasoning
  and as event-message reasoning; duplicates are suppressed.
