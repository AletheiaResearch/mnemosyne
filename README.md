# Mnemosyne

[![codecov](https://codecov.io/gh/AletheiaResearch/mnemosyne/graph/badge.svg?token=30GNZ9jczC)](https://codecov.io/gh/AletheiaResearch/mnemosyne)

Export coding-assistant conversation histories to a unified, anonymized dataset.

Supports Claude Code, Codex, Cursor, Gemini CLI, Kimi CLI, OpenClaw, opencode, and Conductor-style orchestrators. Output stays local unless you explicitly publish.

## Install

    brew install --cask AletheiaResearch/tap/mnemosyne

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

## License

MIT for the tool. Datasets you produce carry whatever license you assign at publish time.
