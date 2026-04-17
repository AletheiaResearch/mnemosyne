# Mnemosyne

Export coding-assistant conversation histories to a unified, anonymized dataset.

Supports Claude Code, Codex, Cursor, Gemini CLI, Kimi CLI, OpenClaw, opencode, and Conductor-style orchestrators. Output stays local unless you explicitly publish.

## Install

    brew install AletheiaResearch/tap/mnemosyne

## Usage

Run without arguments for an interactive TUI:

    mnemosyne

Or use subcommands directly:

    mnemosyne survey
    mnemosyne configure --destination-repo username/traces
    mnemosyne extract
    mnemosyne attest --identity-scan "..." --entity-scan "..." --manual-review "..."
    mnemosyne publish --publish-attestation "..."

The shorthand `mem` is installed as a symlink.

## Configuration and format

See [docs/schema.md](docs/schema.md) for the output record format and [docs/sources.md](docs/sources.md) for per-source extraction details.

## License

MIT for the tool. Datasets you produce carry whatever license you assign at publish time.
