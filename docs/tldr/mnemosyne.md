# mnemosyne

> Export coding-assistant histories to a unified, anonymized dataset.
> Supports Claude Code, Codex, Cursor, Gemini CLI, and others.
> More information: <https://github.com/AletheiaResearch/mnemosyne>.

- Launch the interactive TUI:

`mnemosyne`

- Inspect saved state and discover available conversation groupings:

`mnemosyne survey`

- Configure the destination dataset repository:

`mnemosyne configure --destination-repo {{username/traces}}`

- Extract every detected source's conversations into a canonical JSONL archive:

`mnemosyne extract --include-all --output {{path/to/export.jsonl}}`

- Validate a canonical JSONL export:

`mnemosyne validate --input {{path/to/export.jsonl}}`

- Record an attestation after manual review of an export:

`mnemosyne attest --skip-name-scan --identity-scan "{{declined name scan}}" --entity-scan "{{note}}" --manual-review "{{note}}"`

- Publish an attested export to the configured repository:

`mnemosyne publish --publish-attestation "{{notes}}"`

- Transform a canonical JSONL export into another serializer format:

`mnemosyne transform --input {{in.jsonl}} --format {{sharegpt}} --output {{out.jsonl}}`
