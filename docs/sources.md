# Sources

Supported source categories:

- `claudecode`: flat per-project JSONL with optional sub-agent traces.
- `codex`: structured event JSONL from active and archived session trees.
- `cursor`: editor key-value SQLite database.
- `supplied`: user-supplied pre-normalized JSONL grouped by directory.
- `gemini`: hashed workspace directories with per-session JSON documents.
- `kimi`: hashed workspace directories with per-session `context.jsonl`.
- `openclaw`: header-plus-events JSONL grouped by working directory.
- `opencode`: embedded relational database with session/message/part rows.
- `orchestrator`: Conductor-style orchestration database grouped by repository.

Current implementation notes:

- all sources are read-only.
- absent storage roots are treated as empty discovery results.
- malformed files or rows are skipped with warnings.
- per-source field preservation details are implemented in the matching package under `internal/source/`.
