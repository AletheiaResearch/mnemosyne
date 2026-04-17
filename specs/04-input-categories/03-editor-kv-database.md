# Input Category 3: Editor-Embedded Key-Value Database With Two-Level Keying

## Category profile

This category covers an editor-integrated coding assistant that stores
its conversation history inside an embedded key-value database,
alongside the editor's other per-user state. Conversations are stored
using a two-level keying scheme: an outer per-conversation key whose
row lists the conversation's turns, and an inner per-turn key whose
row holds each individual message's payload.

Storage location: within the editor's per-user application-data
directory, under a subdirectory used for global storage, as a single
embedded-database file. The exact directory location varies by
operating system — a tool of this class consults the platform's
conventional application-support root and descends into the editor's
subtree.

Database shape:

- An outer key namespace whose rows list every conversation: each
  row holds a conversation identifier and a JSON document that
  enumerates the conversation's turns, either in header-only form
  (listing just turn identifiers) or inlined.
- An inner key namespace whose rows hold individual turn payloads,
  keyed by the pair (conversation identifier, turn identifier).
  Each payload is a JSON document carrying the turn's role, text,
  model information, workspace URIs, tool-invocation data, and
  token-count data.

## Discovery behavior

1. Open the embedded database in read-only mode.
2. Query the outer namespace for all conversations. For each
   conversation, read the first turn's payload to determine the
   workspace URI the conversation was tied to.
3. Index conversations by their workspace URI (stripping any
   `file://` prefix). Unknown or empty workspace URIs collect
   under a placeholder.
4. Each distinct workspace URI is a grouping. Record count per
   grouping is the number of conversation identifiers indexed
   under it. Size is estimated by proportional allocation of the
   total database file size (the database does not reveal per-row
   byte sizes easily).

## Extraction behavior

For each conversation identifier in the selected grouping:

1. Read the outer payload. Obtain the ordered list of turn
   identifiers belonging to this conversation.
2. Batch-load every turn's payload in a single query.
3. Walk the turn identifiers in order and assemble:
   - Turns whose role marker indicates a user message produce
     user-role turns. The text is secret-redacted, then
     anonymized.
   - Turns whose role marker indicates an assistant message are
     inspected for a tool-invocation payload. If one is present,
     the turn becomes an assistant turn with a tool invocation
     populated from the payload's tool name, parameters,
     result, and status. The tool name is further normalized to
     strip any integration-server prefix encoded in it.
     Otherwise, the turn becomes an assistant turn carrying
     just text and optional reasoning.
4. Per-turn token counters (when the payload carries them)
   accumulate into the record's usage tally.
5. Timestamps stored as numeric epoch milliseconds are converted
   to ISO-8601 UTC.

## Field derivation

| Field | Source |
|---|---|
| Record identity | The conversation identifier. |
| Model | From the first turn payload that declares one; otherwise a synthetic "unknown" marker for this category. |
| Branch | Not available. |
| Working directory | The first workspace URI seen, stripped of `file://` prefix, anonymized. |
| Turns | Reassembled as above. |
| Status of tool invocations | From the status field of the tool-invocation payload. Values are normalized where the source reports nested status structures. |

## Deduplication

No cross-record deduplication.

## Handling of tool-payload variation

The tool-invocation payload in this category sometimes carries
parameters wrapped inside a container object with a nested list of
"tools" — in such cases the extractor peels the container and
extracts the inner parameters. JSON values encoded as strings inside
string fields are recursively parsed.

## Inherent limitations

- Source data does not expose per-conversation byte sizes; size
  estimates shown in discovery are proportional and approximate.
- Branch information is not stored; the extracted records
  therefore always omit the branch field.
- Reasoning traces are available only when the source populated a
  dedicated subdocument; not every version of this category's
  storage does so.
- Because the database is the editor's live state file, extraction
  runs should be performed when the editor is not actively
  writing; implementers may document this recommendation, but
  must not attempt to stop the editor.
