# Canonical Schema

Mnemosyne writes newline-delimited JSON. Each line is one `Record`.

- `record_id`: stable record identifier.
- `origin`: source category.
- `grouping`: canonical project or repository label.
- `model`: model identifier.
- `branch`: optional branch name.
- `started_at` / `ended_at`: optional RFC3339 timestamps.
- `working_dir`: optional anonymized working directory.
- `turns`: ordered list of user and assistant turns.
- `usage`: record-level counters for turns, tool calls, and tokens.
- `_mnemosyne`: extractor provenance metadata.

Turns contain `role`, optional `timestamp`, `text`, optional `reasoning`, optional `tool_calls`, and optional `attachments`.

Tool calls contain `tool`, `input`, optional `output`, and optional `status`. Tool outputs may include both `text` and rich `content` blocks.

Attachments and tool-output blocks use `type`, optional `text`, optional `media_type`, optional `data`, optional `url`, and optional `name`.

Validation rules:

- `record_id`, `origin`, `grouping`, and `model` are required.
- `turns` must be non-empty.
- every turn role is `user` or `assistant`.
- every turn carries at least one of text, reasoning, tool calls, or attachments.
- timestamps, when present, are RFC3339.

The JSON Schema source of truth lives in [../schemas/canonical-v1.json](../schemas/canonical-v1.json).
