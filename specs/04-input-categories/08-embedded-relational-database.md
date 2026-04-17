# Input Category 8: Embedded Relational Database With Session, Message, and Part Rows

## Category profile

This category covers a coding assistant that stores its conversation
history in an embedded relational database (a single-file SQL store)
with a normalized schema: one row per conversation, one row per
message within a conversation, and one row per content "part" within
a message. Each row's rich content is stored as JSON within a column.

Storage location: under a per-user application-data directory for
the assistant, in a subdirectory dedicated to its own state. The
database file itself is conventionally named within that subtree.

Schema shape:

- A conversations table with columns for: conversation identifier,
  working directory, creation timestamp, last-update timestamp.
- A messages table with columns for: message identifier, foreign
  key to conversation, a JSON blob of message-level metadata
  (role, model provider and id, token counters), and creation
  timestamp.
- A parts table with columns for: part identifier, foreign key to
  message, a JSON blob of part content, and creation timestamp.
  Parts express text, reasoning, file attachments, and tool
  invocations.

## Discovery behavior

1. Open the database in read-only mode.
2. Enumerate conversations, ordered by last-update descending.
3. Index conversations by working directory; unknown or empty
   working directories collect under a placeholder.
4. Record count per grouping is the number of conversation
   identifiers indexed under that working directory. Size is
   estimated proportionally from the total database file size.

## Extraction behavior

For each conversation identifier in the selected grouping:

1. Read the conversation row to obtain the working directory,
   creation timestamp, and last-update timestamp. If the
   working directory does not match the grouping, skip the
   record.
2. Read the conversation's messages in chronological order.
3. For each message:
   - Parse the message-metadata JSON for role, model, and
     token counters. A message's model is typically expressed
     as a provider-plus-model pair; format as
     `provider/model` when both are present.
   - Read all parts belonging to the message in chronological
     order.
   - Dispatch on role:
     - User messages walk their parts. Text parts contribute
       text to the turn. File-typed parts become attached
       content entries:
       - Parts whose URL begins with a data-URL prefix become
         base64-payload attachments.
       - Parts whose URL begins with `file://` become URL
         attachments with the path portion anonymized.
       - Other URLs become URL attachments with the URL
         anonymized as text.
       Attachments whose media-type begins with the image
       prefix are tagged as images; others as documents.
     - Assistant messages walk their parts. Text parts
       contribute to the turn's text. Reasoning parts
       contribute to the turn's reasoning. Tool parts
       contribute tool invocations with status (normalized:
       "completed" becomes "success") and output (taken from
       the part's state-output text).
4. Token counters from each message accumulate into the
   record's usage tally. Cache-read and cache-write counts are
   added to the input-token total.
5. Build the record; stamp origin category and grouping label.

## Field derivation

| Field | Source |
|---|---|
| Record identity | The conversation identifier. |
| Model | First message that declares one; otherwise a synthetic "unknown" marker for this category. |
| Branch | Not available. |
| Working directory | The conversation row's directory column, anonymized. |
| Start/end timestamps | The conversation row's creation and last-update columns, normalized to ISO-8601 UTC. |

## Deduplication

No cross-record deduplication.

## Inherent limitations

- The database may be in use by the assistant when the extractor
  opens it. The extractor opens read-only; it does not block the
  assistant's writes but may occasionally see a snapshot that is
  slightly behind the live state.
- Per-row byte sizes are not exposed by the database; grouping
  size figures are proportional estimates of the full
  database-file size.
- Branch information is not stored.
- A conversation whose messages reference parts the database does
  not contain (because of a partial write or schema mismatch)
  will be missing those parts' content in the output; the
  extractor does not fabricate missing parts.
