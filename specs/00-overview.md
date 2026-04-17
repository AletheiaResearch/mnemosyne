# Overview

## Scope

This specification describes a class of local utility that lets a user turn the
conversation histories accumulated by their command-line and editor-integrated
coding assistants into a single, anonymized, structured dataset that they can
keep locally or publish to a public dataset platform. The utility acts entirely
on data the user already has on their own machine: it does not intercept live
traffic, does not ship data anywhere without explicit approval, and does not
alter the source data at rest.

A tool of this class has four responsibilities:

1. **Locate and read** conversation histories produced by several different
   coding-assistant products, each of which stores data in its own shape and
   in its own location on the user's machine.
2. **Normalize** those disparate formats into one consistent record shape so
   that downstream consumers can work with all sources uniformly.
3. **Scrub** the normalized records for credentials, personal identifiers,
   and user-specified sensitive strings before they leave the local machine.
4. **Gate publication** behind an explicit review step in which the user (or
   an agent acting for the user) is required to inspect a local copy,
   attest to having performed specific review actions, and explicitly
   approve outbound transmission.

## Why a user runs this

A user who has accumulated months of coding-assistant conversations owns a
potentially valuable corpus: evidence of how they work, which problems they
solved, and how they directed an assistant to help. Reasons to export this
corpus include:

- Building a personal searchable archive.
- Contributing to an open dataset for evaluation or training.
- Sharing reproducible traces of work with collaborators.
- Studying one's own assistant-usage patterns.

The utility exists because this corpus is also a minefield of sensitive
information — pasted API keys, private paths, client names, authentication
tokens, internal hostnames, private email addresses — and a naive export
would leak all of it. The utility's principal value is not the format
conversion; it is the combination of format conversion with layered
redaction and a hard-stop review gate.

## High-level user experience

From the user's perspective the flow is linear and staged. Each stage has to
complete before the next can begin, and the utility records where the user
is in the sequence so that intermediate interruptions are safe.

1. **Helper install** (optional). The user installs a small integration so
   their favorite coding assistant agent can walk them through the remaining
   stages conversationally.
2. **Credential setup** (optional, only if publishing). The user authenticates
   against whatever public dataset platform they intend to publish to, using
   that platform's own credential mechanism. The utility reads the resulting
   credential via the platform's client library; it never stores the
   credential itself.
3. **Scope selection**. The user decides which origin category to draw from
   (one specific coding assistant, or all detected ones), reviews the list of
   per-workspace groupings the utility found, and excludes any groupings they
   do not want included. They also register any literal strings, usernames,
   or handles that must be redacted.
4. **Local extraction**. The utility reads every selected conversation,
   normalizes it, anonymizes identity-linked content, scans for and replaces
   secrets, and writes the result to a single newline-delimited JSON file on
   the local filesystem. No network traffic occurs during this step. Summary
   statistics are printed.
5. **Review and attestation**. The user (or their agent) inspects the local
   output using both the utility's built-in scan reports and their own
   judgement. The user must supply text attestations describing the specific
   checks they performed before the utility will allow the next step. The
   utility records which file was reviewed.
6. **Publication**. With an attested export on disk and a separate text
   statement that the user approved outbound transmission, the utility
   uploads the local file, a metadata manifest, and a human-readable
   description to the configured destination.

Steps 4 through 6 can be repeated. If review uncovers a problem, the user
adds redactions, re-runs step 4, re-runs step 5 on the new file, and then
proceeds. The utility will refuse to publish any file it did not see
attested.

## Design commitments

Any tool in this class must uphold the following:

- **No network I/O without explicit user action.** Discovery, extraction,
  and local review read only from the user's own disk.
- **No destructive edits to source data.** The source conversation stores
  are read-only inputs.
- **Idempotent, restartable flow.** The user can close their terminal
  between any two stages; the utility recovers state from its persistent
  configuration store.
- **Layered redaction.** Pattern-based credential detection, configurable
  literal-string replacement, and identity anonymization all run;
  none is treated as sufficient by itself.
- **A mandatory human review gate.** No automated check is trusted enough
  to bypass user inspection; publication requires an explicit, validated
  attestation from the operator.
- **Portable output.** The normalized record shape is a self-describing
  JSON schema that does not depend on the utility being installed to
  consume.

## Relationship of this document to the rest of the specification

This overview fixes vocabulary and establishes the top-level flow. The
remaining documents specify the individual pieces:

- Command-surface behaviors the utility must expose
  (see `01-command-surface.md`).
- Persistent settings and their bounds (see `02-configuration-model.md`).
- Output record shape (see `03-output-format.md`).
- Per-origin-category extraction behavior
  (see files under `04-input-categories/`).
- Sensitive-content detection and handling
  (see `05-sensitive-content-handling.md`).
- End-to-end workflow walkthroughs (see `06-workflow-flows.md`).
- Failure handling and recovery (see `07-failure-and-recovery.md`).
- Defined vocabulary used throughout these specs
  (see `08-vocabulary.md`).
