# Input Category 7: Header-Plus-Events JSONL With Nested Per-Agent Folders

## Category profile

This category covers a command-line coding assistant organized as a
collection of named agents, each with its own configuration and its
own conversation log. Each individual conversation is a single
newline-delimited JSON file whose first line is an explicit session
header and whose subsequent lines are typed event records.

Storage location: under a per-user application-data directory for
the assistant, in a subdirectory enumerating agents. Each agent
subdirectory contains its own conversations subdirectory of
newline-delimited JSON files.

Per-conversation file shape:

- The first line is a session header: a JSON object whose `type`
  field is a header marker, carrying the session identifier, an
  optional session timestamp, and the working directory.
- Subsequent lines have `type` markers distinguishing message
  records (user, assistant, or tool-result messages) from
  special event records such as model-change events and
  shell-execution events.
- Assistant messages carry a content list of typed blocks (text,
  thinking, tool-call).
- Tool-result messages are keyed to their originating call by a
  correlation identifier, carry an is-error flag, and contain
  result text.
- Shell-execution events carry a command, its output, and its
  exit code as a single self-contained record that the
  extractor treats as a single tool invocation.

## Discovery behavior

1. Walk the agents subdirectory. For each agent, walk its
   conversations subdirectory.
2. For each conversation file, read the first line and confirm
   it is a session header. Extract the working directory.
3. Index conversations by working directory. Each distinct
   working directory is a grouping.
4. Record count is the number of conversation files per
   grouping. Size is the sum of file byte sizes.

## Extraction behavior

For each conversation file:

1. Parse every line. Skip files whose first line is not a session
   header or is corrupt.
2. **First pass: correlation table.** Walk the entries; for
   every tool-result message, extract the result text and the
   error flag, keyed by the correlation identifier.
3. **Second pass: assembly.** Walk the entries in order:
   - Model-change events update the record's model identifier
     (formatted as `provider/model` when a provider is
     present).
   - User messages contribute a user-role turn whose text is
     the concatenation of text-type content blocks.
   - Assistant messages contribute an assistant-role turn
     whose content list is walked to assemble text,
     reasoning, and tool invocations. Each tool-call block
     is paired with its result from the correlation table.
   - Shell-execution events contribute a synthetic
     assistant-role turn containing a single tool invocation
     whose tool name is `bash` (or the category's own name
     for shell commands), whose input is the command, and
     whose output carries the output text and exit code.
     The status is derived from the exit code: zero means
     success, nonzero or absent-plus-output means error.
   - Usage counters from the assistant-message's usage block
     accumulate into the record's usage tally. Cached-read
     counts are added to the input-token total.

## Field derivation

| Field | Source |
|---|---|
| Record identity | Session header's identifier, or the file-name stem as fallback. |
| Model | First model-change event or first assistant-message model field; otherwise a synthetic "unknown" marker for this category. |
| Branch | Not available in the source. |
| Working directory | Session header, anonymized. |
| Timestamps | Session header's optional timestamp, plus per-message timestamps. Numeric epoch-millisecond timestamps are converted. |

## Deduplication

No cross-record deduplication.

## Inherent limitations

- Branch information is not persisted.
- Shell-execution events are exposed as tool invocations named
  after the shell tool even when the conversation's actual
  invocation was through a different surface, because the
  source collapses them into one event type.
- Multiple agents pointing at the same working directory produce
  multiple conversations under the same grouping; the extractor
  does not distinguish by agent in the grouping list.
