# tldr-pages submission

`mnemosyne.md` is the canonical tldr-pages entry for `mnemosyne`. It is
submitted, unchanged, to two upstream repositories:

- **tldr-pages** — <https://github.com/tldr-pages/tldr> — drop the file at
  `pages/common/mnemosyne.md` and open a pull request.
- **ethical-tldr** — <https://codeberg.org/small-hack/ethical-tldr> — same
  Markdown schema (it is a tldr-pages fork); same destination path, same
  PR shape.

If the two upstreams ever diverge in style or required fields, split this
file into `mnemosyne-tldr.md` and `mnemosyne-ethical.md` and update the
submission target above.

## Validating before submission

The tldr-pages repo ships a linter at `.lint/`. From a clone of that repo:

    cp /path/to/mnemosyne/docs/tldr/mnemosyne.md pages/common/
    npm install --prefix scripts
    npx tldr-lint pages/common/mnemosyne.md

After it lands and clients refresh their cache, `tldr mnemosyne` (or
`ethical-tldr mnemosyne`) renders the page locally.

## Updating the page

Keep this in sync with `internal/cli/*.go` whenever a flag mentioned here
changes. Up to eight examples is the tldr-pages limit; trim less common
flows first if a new example must be added.
