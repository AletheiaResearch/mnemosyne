# Appendix: Known Source Products

## Purpose and scope

The abstract input-category documents under `04-input-categories/`
describe *storage shapes* rather than products. This appendix names
the real-world products currently known to produce data that fits
each shape and gives the specific paths, file-format details, and
schema facts an implementer needs in order to read those products'
data.

Everything in this appendix is **interoperability information**:
facts about how third-party products happen to store their data on
disk. These are facts about those products, not creative material
derived from the reference implementation. When a product updates
its storage format, this appendix needs updating; the abstract
specs should not.

Always verify the current on-disk shape against an actual install
before shipping an extractor. Products in this space change
storage formats across versions, and this appendix is a snapshot.

## Category → product map

| Abstract category | Known third-party products |
|---|---|
| 01 — Flat per-project JSONL with sub-agents | Claude Code (Anthropic) |
| 02 — Structured-event JSONL | Codex CLI (OpenAI) |
| 03 — Editor-embedded key-value DB | Cursor |
| 04 — User-supplied pre-normalized | none; this category covers user-authored or custom-harness inputs |
| 05 — Hashed-path per-session JSON | Gemini CLI (Google) |
| 06 — Hashed-path per-session context JSONL | Kimi CLI (Moonshot) |
| 07 — Header-plus-events JSONL with per-agent folders | OpenClaw |
| 08 — Embedded relational DB | opencode |
| 09 — Parallel-workspace orchestrator | Conductor |

A single implementation typically targets several of these at once.
The reference snapshot these docs were derived from targeted all of
them.

---

## Claude Code (category 01)

**Storage root.** Per-user home: `~/.claude/projects/`

**Per-workspace folder name.** The workspace's absolute path with
filesystem separators replaced by `-`. Example: `/Users/alice/code/proj`
becomes `-Users-alice-code-proj`. Workspaces whose paths differ only
by a character coinciding with the separator collide in this
encoding.

**Per-conversation file.** `<project-folder>/<session-id>.jsonl`.

**Sub-agent files.** `<project-folder>/<session-id>/subagents/agent-*.jsonl`
(one file per sub-agent spawned during the conversation). Merge and
sort by timestamp when reassembling a sub-agent session; the
resulting record's id is `<session-id>:subagents` if a root-level
`<session-id>.jsonl` also exists, else just `<session-id>`.

**Per-line schema (key fields).**

Common on every entry:
- `type`: `"user"`, `"assistant"`, or other.
- `timestamp`: ISO-8601 UTC string.
- `sessionId`: conversation identifier.
- `cwd`, `gitBranch`, `version`: typically present on early entries.

`type: "user"` entries:
- `message.content`: string, or list of blocks where each block has
  `type: "text"` (carries `text`) or `type: "tool_result"` (carries
  `tool_use_id`, `content`, `is_error`).
- Tool-result entries also carry `toolUseResult` at entry level with
  structured extras (stdout, exit code, `oldString`/`newString`/
  `structuredPatch` for edits, `content` for file creates), plus
  optional `sourceToolAssistantUUID`.

`type: "assistant"` entries:
- `message.content`: list of blocks, each with `type` in
  `{"text","thinking","tool_use"}`.
- `tool_use` block carries `id`, `name`, `input`.
- `message.model`: model identifier.
- `message.usage`: `{input_tokens, cache_read_input_tokens, output_tokens}`.
  The extractor sums `input_tokens + cache_read_input_tokens` into
  the record's input-token total.

**Correlation.** Build a map from `tool_use.id` to the later
`tool_result` block carrying the same `tool_use_id`, plus the
outer entry's `toolUseResult` structured payload.

**Redundant-field pruning.** When the outer `toolUseResult`
duplicates data already present in the assistant's `tool_use.input`
(notably `oldString`, `newString`, `structuredPatch`, and the
`content` field for create results), drop the duplicates from the
emitted `output.raw` to avoid ~2× inflation of the largest records.

---

## Codex CLI (category 02)

**Storage root.** `~/.codex/sessions/` (active) and
`~/.codex/archived_sessions/` (rotated out). Sessions dir is nested
by date; walk recursively.

**Per-conversation file.** `*.jsonl`.

**Per-line schema.** Each line is `{type, timestamp, payload}`.
Relevant `type` values:

- `session_meta`: `payload.id` (session id), `payload.cwd`,
  `payload.model_provider`, `payload.git.branch`.
- `turn_context`: `payload.cwd`, `payload.model`.
- `response_item`: `payload.type` ∈ `{message, function_call,
  custom_tool_call, function_call_output, custom_tool_call_output,
  reasoning}`.
  - `message` with `role: user`: `payload.content[]` may contain
    `type: "input_image"` parts with `image_url` (data URL or
    `file://`).
  - `function_call`: `payload.name`, `payload.arguments` (may be a
    JSON-serialized string), `payload.call_id`.
  - `custom_tool_call`: same, plus `payload.input` (often a raw
    patch string).
  - `function_call_output`: `payload.call_id`, `payload.output` (a
    text blob with lines `Exit code: N`, `Wall time: T`, and
    `Output:` followed by captured output).
  - `custom_tool_call_output`: `payload.call_id`, `payload.output`
    (JSON-serialized with `.output`, `.metadata.exit_code`,
    `.metadata.duration_seconds`).
  - `reasoning`: `payload.summary[]` with `text` fields.
- `event_msg`: `payload.type` ∈ `{token_count, agent_reasoning,
  user_message, agent_message}`.
  - `token_count`: `payload.info.total_token_usage.{input_tokens,
    cached_input_tokens, output_tokens}`. Track the running maxima
    and use them as the final usage tally.
  - `agent_reasoning`: `payload.text` (deduplicate against
    `response_item reasoning`).
  - `user_message`: `payload.message`, plus `payload.images[]` and
    `payload.local_images[]` for attached content.
  - `agent_message`: `payload.message` — flush pending tool calls
    and reasoning into an assistant turn at this point.

**Correlation.** Map `call_id` on `function_call` /
`custom_tool_call` to the matching `*_output` payload.

**Grouping key.** The working directory from `session_meta.payload.cwd`
or the first `turn_context`. Files whose working directory does not
match the selected grouping are skipped (a single file can only
live in one grouping's output).

---

## Cursor (category 03)

**Storage file (platform-dependent).**

- macOS: `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb`
- Linux: `~/.config/Cursor/User/globalStorage/state.vscdb`
- Windows: `~/AppData/Roaming/Cursor/User/globalStorage/state.vscdb`

**File format.** SQLite. Open read-only
(`file:<path>?mode=ro`, URI mode).

**Table of interest.** `cursorDiskKV(key TEXT, value TEXT/BLOB)`.
Two key families matter:

- `composerData:<composer-id>`: one row per conversation. Value is
  JSON with either `fullConversationHeadersOnly` or (older)
  `conversation`, each a list of `{bubbleId, type, ...}` entries
  naming the conversation's turns in order.
- `bubbleId:<composer-id>:<bubble-id>`: one row per turn. Value is
  JSON with:
  - `type`: `1` (user) or `2` (assistant).
  - `text`: the turn's visible text.
  - `thinking.text`: optional reasoning.
  - `workspaceUris[0]`: `file://...` URI of the workspace.
  - `modelInfo.modelName`: model identifier.
  - `tokenCount.inputTokens`, `tokenCount.outputTokens`.
  - `createdAt`: epoch milliseconds.
  - `toolFormerData`: `{name, params, result, status}`; `params`
    and `result` may be JSON-serialized strings; `params` may wrap
    parameters inside `{tools: [{parameters: ...}]}` — peel when
    that pattern appears.

**Workspace discovery.** Walk the first several turns' payloads
(not just the first) looking for a populated `workspaceUris`.

**Size estimation.** The database does not expose per-row sizes;
estimate proportionally from the file size divided by session
count.

**MCP-prefix tool-name normalization.** Tool names often carry a
multi-segment prefix identifying the MCP server (`mcp_<server>_<tool>`
or `mcp-<server>-user-<tool>`). Strip the prefix before emitting.

---

## Custom / user-supplied (category 04)

No specific third-party product. An implementation may offer a
pass-through ingest for user-authored newline-delimited JSON files
that already conform to the normalized output shape. The reference
snapshot placed these under a subdirectory of the utility's own
per-user configuration root (one immediate subdirectory per
grouping). A fresh implementation should pick its own convention
and document it; the essential requirement is minimum-field
validation (identifier, model, turns list) before pass-through, and
anonymization/redaction on text content only.

---

## Gemini CLI (category 05)

**Storage root.** `~/.gemini/tmp/`.

**Per-workspace folder name.** 64-character SHA-256 hex digest of
the workspace's absolute path. A sibling `bin/` directory is
unrelated and should be skipped.

**Per-conversation file.** `<hash-dir>/chats/session-*.json` — a
single JSON document per session.

**Reverse hash lookup (for display labels).** Build a map from
digest → absolute path by:
1. Walking direct children of the home directory (and each drive
   on platforms with multiple drive roots) and digesting their
   absolute paths.
2. If still unresolved, walking the session file's `toolCalls[]`
   arguments looking for a `file_path` or `path` whose any leading
   prefix digests to the target.
3. Falling back to an 8-char digest prefix as the display label.

**JSON document schema.**
- Top: `sessionId`, `startTime`, `lastUpdated`, `messages[]`.
- `messages[]` entries: `{type, timestamp, content, ...}`.
  - `type: "user"` entries: `content` is a string, or a list of
    parts (`text`, `inlineData.{mimeType,data}`,
    `fileData.{fileUri,mimeType}`, `functionCall.{id,name,args}`,
    `functionResponse.{id,name,response.output}`).
  - `type: "gemini"` entries: `model`, `tokens.{input,cached,output}`,
    `thoughts[].description`, `toolCalls[]`, `content` (string).

**Tool-call argument dispatch.** Different tool names take
different argument shapes: `read_file` (`file_path`), `write_file`
(`file_path`, `content`), `replace` (`file_path`, `old_string`,
`new_string`, optional `expected_replacements`, optional
`instruction`), `run_shell_command` (`command`), `read_many_files`
(`paths[]`), `search_file_content` / `grep_search` (free-text
args), `list_directory` (`dir_path`, `ignore[]`), `glob`
(`pattern`), `google_web_search`, `web_fetch`,
`codebase_investigator` (free-text). Unknown tool names fall
through to generic string-arg anonymization.

**Tool-output dispatch.** `read_many_files` output is a stream of
`--- <path> ---` sections; split into `{path, content}` pairs.
`run_shell_command` output uses `Command: / Directory: / Output: /
Exit Code:` markers; parse into a structured object.

**Deduplication.** Required. Near-duplicate session files arise
from mid-session checkpoints. Fingerprint by serializing the
record (sans grouping label) in a canonical JSON form with sorted
keys and compact separators, then SHA-256.

---

## Kimi CLI (category 06)

**Storage root.** `~/.kimi/`. Two pieces matter:

- `~/.kimi/sessions/<md5-of-cwd>/<session-id>/context.jsonl` — the
  conversation itself.
- `~/.kimi/kimi.json` — a config file with `work_dirs[].path`
  listing known workspaces, used to reverse-look-up the MD5 back
  to a readable path.

Note: this category uses **MD5** of the absolute path, not SHA-256.

**Per-line schema in `context.jsonl`.** Each line is an entry with
a `role`:
- `role: "user"`: string `content`.
- `role: "assistant"`: `content` is a list of blocks with
  `type: "text"` or `type: "think"`. Plus a sibling
  `tool_calls[].function.{name, arguments}` where `arguments` may
  be JSON-serialized.
- `role: "_usage"`: `token_count` integer; update the output-token
  counter by taking the max of prior and new values.

**Defaults.** Neither per-turn nor per-session timestamps are
reported. Input tokens are not reported (zero). Model defaults to
`kimi-k2` at extraction time when no per-record model is present.

---

## OpenClaw (category 07)

**Storage root.** `~/.openclaw/agents/`.

**Layout.** `<agents>/<agent-name>/sessions/*.jsonl`. Each agent
(the orchestrator's named agent instances) has its own sessions
folder.

**Per-conversation file.** A `.jsonl` whose first line is a session
header:

```
{"type": "session", "id": "...", "cwd": "...", "timestamp": "..."}
```

Subsequent lines carry `type` ∈ `{"message", "model_change"}` (and
possibly others — unknowns are ignored).

**Message lines.** `message.role` ∈
`{"user", "assistant", "toolResult", "bashExecution"}`.

- `user`/`assistant`: `content` is a list of blocks with
  `type` ∈ `{"text", "thinking", "toolCall"}`.
  - `toolCall`: `name`, `arguments`, `id`.
- `toolResult`: `toolCallId`, `isError`, `content` (list of
  `{type: "text", text}`) — index into a correlation map.
- `bashExecution`: `command`, `output`, `exitCode` — synthesize a
  one-call tool invocation with tool name `bash` and status
  derived from the exit code.

**Model-change lines.** `{type: "model_change", provider, modelId}`
— set `metadata.model = provider/modelId`.

**Assistant usage.** `message.usage.{input, cacheRead, output}`;
input total is `input + cacheRead`.

**Grouping key.** The header's `cwd`.

---

## opencode (category 08)

**Storage file.** `~/.local/share/opencode/opencode.db` (SQLite).

**Schema (columns of interest).**

- `session(id, directory, time_created, time_updated, ...)`.
  Timestamps are epoch milliseconds.
- `message(id, session_id, data, time_created, ...)`. `data` is a
  JSON blob with `{role, model.{providerID, modelID}, tokens.{input,
  output, cache.{read, write}}}`. Model emitted as
  `providerID/modelID` when both present.
- `part(id, message_id, data, time_created, ...)`. `data` is a JSON
  blob with `{type, ...}`:
  - `type: "text"`: `text`.
  - `type: "reasoning"`: `text`.
  - `type: "tool"`: `tool`, `state.{input, status, output}` —
    status `"completed"` is normalized to `"success"`.
  - `type: "file"`: `url`, `mime` — dispatch to attached-content
    entry (base64 when `url` starts with `data:`, URL otherwise;
    tagged `image` when mime starts with `image/`, else
    `document`).

**Grouping key.** `session.directory`. Rows with empty directories
collect under an unknown-directory bucket.

**Concurrency.** The database may be actively written by the
running client; open read-only.

---

## Conductor (category 09)

**Storage location.** Per-user application-data directory for
Conductor. The exact path varies by platform; this appendix does
not prescribe it because Conductor's on-disk layout has shifted
across versions. Consult the product's own release notes before
shipping, and verify against a live install.

**File format.** A single-file embedded SQL database.

**Tables of interest (abstract names; verify against the real
schema for your target release).**

- **Repositories table.** One row per registered repository. Carries
  a repository id, remote-origin URL (git-style), display name
  (typically the repository's leaf directory name), default-branch
  label, on-disk path of the canonical checkout, and creation /
  last-updated timestamps.
- **Workspaces table.** One row per parallel task-workspace. Carries
  a workspace id, foreign key to the repository, a directory-name
  label used for both the worktree directory and display, a branch
  name, a state label (active / archived / other), and
  creation / last-updated timestamps.
  - **Whimsical-label note.** Older releases gave new workspaces a
    codename (city names and similar) at creation time. Newer
    releases may use branch-derived or user-specified labels. A
    **deprecated codename column** may coexist with the newer label
    column; prefer the newer when both are populated, fall back to
    the deprecated one otherwise.
- **Sessions table.** One row per conversation. Carries the
  orchestrator's own session id, foreign key to the workspace, an
  agent-type label (which external agent ran this session —
  typically one of Claude Code, Codex, and so on), an **external
  session identifier** column pointing into the chosen agent's
  own store, a model identifier, a short user-authored title, and
  creation / last-updated / last-user-message timestamps.
  - **Historical naming trap.** The external-session-identifier
    column is typically named after the first agent Conductor
    supported (e.g., a `claude_session_id`-like name). It is used
    generically for all agent types. Route lookups by the
    agent-type column, not by the name of this column.
- **Session-messages table.** One row per turn. Carries a message
  id, foreign key to the session, a role, a textual content column
  (Markdown or agent-specific serialization), and optionally: a
  rich-payload column with the full structural turn, a per-message
  model id, a sent-at timestamp distinct from created-at, and an
  external-agent message id.

**Extraction strategies.** See `04-input-categories/09-parallel-workspace-orchestrator.md`
for the three extraction strategies (metadata-only, direct, and
join-and-prefer) and the deduplication rule. The join-and-prefer
strategy reads message bodies from the external agent's store
keyed by the sessions table's external-session-identifier column,
using whichever of the other input-category extractors (01, 02, 05,
and so on) corresponds to the sessions table's agent-type.

**Grouping invariant.** One grouping per repository, not per
workspace. Multiple parallel worktrees of the same repository all
roll up under the single repository grouping.

---

## Maintenance notes for this appendix

- Each product's on-disk format can and does change across
  releases. Pin the version range each category has been verified
  against when you add or update an entry.
- When a product adds a new agent backend, the mapping in the
  orchestrator category's sessions-table routing needs to expand
  accordingly. Do not hard-code the historical external-identifier
  column name.
- The list of known products is open-ended. New coding-assistant
  products appear frequently; new entries in this appendix are
  additive and do not invalidate the abstract category files.
