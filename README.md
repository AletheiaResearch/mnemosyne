# Mnemosyne

[![codecov](https://codecov.io/gh/AletheiaResearch/mnemosyne/graph/badge.svg?token=30GNZ9jczC)](https://codecov.io/gh/AletheiaResearch/mnemosyne)

Mirror: [Codeberg](https://codeberg.org/AletheiaResearch/mnemosyne)

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

## Documentation

- `man mnemosyne` — full reference, plus one page per subcommand
  (`man mnemosyne-extract`, etc.). Installed by the Homebrew cask;
  building from source: `make docs && man -l man/man1/mnemosyne.1`.
- `tldr mnemosyne` — concise example-driven help, served via
  [tldr-pages](https://github.com/tldr-pages/tldr).
- `ethical-tldr mnemosyne` — the same examples via the
  [ethical-tldr](https://codeberg.org/small-hack/ethical-tldr) fork.

The tldr source lives at [`docs/tldr/mnemosyne.md`](docs/tldr/mnemosyne.md);
see that directory's README for submission instructions.

## License

MIT for the tool. Datasets you produce carry whatever license you assign at publish time.
