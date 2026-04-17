# Input Categories: Common Patterns

The following files each describe one category of coding-assistant
conversation source that a tool of this class must be able to consume.
This file collects the patterns and requirements that apply to every
category, so that per-category files can focus on what makes that
category distinct.

## Abstract extractor contract

For each supported origin category, the utility exposes two abstract
operations:

- **Discovery**: inspect the host filesystem to determine whether the
  origin category has data on this machine, and if so, enumerate the
  workspace or project groupings it organizes records under. Each
  discovered grouping yields: a canonical directory-level identifier
  used internally for routing, a display label shown to the user, a
  record count estimate, a size estimate in bytes, and the category
  label.
- **Extraction**: given a grouping identifier and an anonymization
  session, yield a stream of normalized records conforming to
  `03-output-format.md`.

Discovery must be safe to invoke at any time; extraction is invoked
only from within the `extract` capability.

## Universal pre-conditions

Every extractor must:

- Treat the source data as read-only.
- Tolerate a missing storage root: discovery returns an empty list,
  not an error.
- Tolerate permission-denied errors on individual files: log a
  non-fatal warning, skip the file, and continue with the next.
- Tolerate schema drift: unknown fields are ignored; missing fields
  fall back to neutral defaults (absent timestamps, zero token
  counts, empty tool-invocation list).
- Tolerate malformed individual records (for example a corrupt line
  in a newline-delimited JSON file): the malformed record is
  skipped with a warning; the rest of the file is still processed.

## Universal field derivation

Every extractor produces records carrying the semantic fields listed
in `03-output-format.md`. The per-category files below state how each
extractor derives each field. The following derivations are universal:

- **Record identity.** Prefer an identifier the source assigns. If
  none exists, synthesize one from the source filename or from a
  hash of the canonicalized record.
- **Origin category.** Set from the extractor that produced the
  record.
- **Grouping label.** The display label computed during discovery.
- **Anonymization.** Every textual value in every record passes
  through the anonymizer. Path-shaped values (see below) are
  anonymized via the path-specific rule. Command-shaped values are
  secret-redacted before anonymization. Every other string is
  anonymized via the text rule.
- **Timestamps.** Each source's timestamp representation is
  normalized to ISO-8601 UTC. If the source stores a timestamp as
  a numeric epoch in milliseconds, the extractor converts
  accordingly.
- **Token counters.** Populated from the source's own reported
  usage when available; zero otherwise.

## Keys treated as paths

When walking the nested input argument structure of a tool
invocation, the extractor looks for keys whose names
conventionally stand for filesystem paths — including short and
long forms, snake-case and camel-case variants, and conventional
abbreviations for a working directory or a destination file or
directory. Values under any such key are anonymized via the path
rule even if the surrounding context would have called for the
text rule. The specific set of recognized key names is an
implementation decision; at minimum it covers the most common
terms any coding assistant uses to name a filesystem argument.

## Keys treated as commands

Keys whose names conventionally stand for a shell command or a
short abbreviation thereof carry values that are first run
through the secret-redaction pass (because users paste tokens
into command strings) and then through the text anonymization
rule. The exact set of key names is an implementation decision.

## Large-binary skip

If a string value encountered during extraction is above a size
threshold (typical default: 4096 bytes) and either begins with a
`data:` URL prefix with a base64 marker or, after stripping ANSI
control sequences, consists of contiguous base64-looking characters
with no byte below printable ASCII, the extractor leaves the value
unchanged. This avoids corrupting large inline image or binary
payloads during anonymization. The default threshold is an
implementation decision.

## Reasoning-trace opt-out

Assistant turns may carry a "reasoning" string reflecting the
model's private deliberation. When the caller of the `extract`
capability passes the no-reasoning flag, every extractor omits
this string entirely from its output records.

## Per-category file template

Each per-category file covers:

- **Category profile.** What kind of assistant product produces data
  of this shape — described by storage pattern, not by name. What
  command shape. Where the data lives on the host filesystem,
  described as a category of location.
- **Discovery behavior.** What the extractor walks to enumerate
  groupings, what it uses for the display label, how it counts
  records and measures size.
- **Extraction behavior.** How the extractor turns raw records into
  normalized ones — including any category-specific state machine,
  two-pass traversal, or cross-record correlation.
- **Field derivation.** Per-field rules specific to this category.
- **Deduplication.** Whether this category warrants the
  fingerprint-based deduplication behavior described in
  `03-output-format.md`, and if so, the fingerprint rule.
- **Inherent limitations.** What information is unavailable from
  the source and therefore absent from the output.
