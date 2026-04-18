# Custom tool catalogs for unsupported harnesses

`mnemosyne transform` renders tool-aware chat templates by feeding each
record's observed tool calls through a `tools` list in the template context.
For harnesses that ship with mnemosyne (`claudecode`, `codex`) the built-in
catalog in `internal/serialize/toolcatalog/` fills in descriptions and
strict parameter types that inference can't recover from a trace. Every
other harness falls back to the inferred list: tool names + JSON-schema
types seen in actual calls, with no descriptions.

This doc is for the **few users** who want fully-described tool schemas for
a harness we don't bundle — typically because they're distilling a bespoke
agent stack and need the upstream template (hermes, deepseek-r1, etc.) to
emit accurate function signatures.

## Option 1 — pass a tools file at transform time

The simplest path. Hand-author a JSON file that matches either the
OpenAI-style wrapped shape:

```json
[
  {
    "type": "function",
    "function": {
      "name": "search",
      "description": "Search the knowledge base",
      "parameters": {
        "type": "object",
        "properties": {
          "query": { "type": "string", "description": "Query text" }
        },
        "required": ["query"]
      }
    }
  }
]
```

or the flat shape:

```json
[
  {
    "name": "search",
    "description": "Search the knowledge base",
    "parameters": { "type": "object", "properties": { "query": { "type": "string" } } }
  }
]
```

and run:

```
mnemosyne transform \
  --input canonical.jsonl \
  --output rendered.jsonl \
  --template-name hermes \
  --tools-file my-harness-tools.json
```

`--tools-file` replaces the entire inference + catalog pipeline, so you're
in full control. This is the recommended escape hatch.

The accepted JSON shape is formalised in
[`schemas/tool-catalog-v1.json`](../../schemas/tool-catalog-v1.json); point
your editor or `ajv`/`jsonschema` at it to validate a hand-authored file.

## Option 2 — instrument your harness with AGENTS.md / CLAUDE.md

Works when you don't want to maintain a separate tools file and your
harness honors agent-instruction files (Claude Code, Codex, Cursor, etc.)

Add a section to the harness's `AGENTS.md` or `CLAUDE.md` (or equivalent)
that tells the model to emit its tool catalog at session start in a
parseable format. Example:

````markdown
## Tool catalog instrumentation

At the start of every session, before responding to the user, emit a
`<mnemosyne-tools>` block listing every tool available to you, formatted
as JSON. Use this exact shape:

```json
[
  {
    "name": "tool_name",
    "description": "one-line description",
    "parameters": {
      "type": "object",
      "properties": {
        "param_name": { "type": "string", "description": "..." }
      },
      "required": ["param_name"]
    }
  }
]
```

Wrap the JSON in `<mnemosyne-tools>...</mnemosyne-tools>` tags on the very
first assistant turn. Only emit it once per session.
````

Then after extraction, read the emitted block out of the first assistant
turn's text and convert it into a tools file. A simple extractor:

```bash
jq -r '.turns[] | select(.role=="assistant") | .text' canonical.jsonl \
  | head -n 1 \
  | sed -n 's/.*<mnemosyne-tools>\(.*\)<\/mnemosyne-tools>.*/\1/p' \
  > my-harness-tools.json
```

Then feed it with `--tools-file my-harness-tools.json` as in Option 1.

This trades authoring effort for runtime cooperation: the model has to
actually follow the instruction. For anything mission-critical, prefer
Option 1.

## When you don't need any of this

If you're rendering through `chatml`, `zephyr`, or `vicuna`, the tools
list is ignored — those templates don't surface function signatures. Pass
`--no-tools` if you want to skip inference entirely.
