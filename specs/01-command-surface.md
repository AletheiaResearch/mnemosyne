# Command Surface

This document describes the distinct operations a user can perform with the
utility, specified by capability rather than by command-line invocation.
Implementers may expose these operations through any combination of
subcommands, flags, or interactive prompts; what matters is that each
capability is reachable and that the capability's contract matches this
specification.

The names used below (`install-helper`, `survey`, `inspect`, `configure`,
`extract`, `attest`, `publish`) are chosen by this specification for
readability. Implementers should choose their own names.

## Summary table

| Capability | Purpose | Requires prior stage | Writes persistent state | Writes output file | Performs network I/O |
|---|---|---|---|---|---|
| install-helper | Install agent-integration assets into a caller-chosen project | none | no | yes (agent-integration asset) | may fetch integration assets |
| survey | Discover available origin groupings, check credential state, produce a machine-readable progress summary | none | yes (stage marker) | no | may query dataset platform for credential presence |
| inspect | Enumerate groupings for a chosen origin category | none | no | no | no |
| configure | Read or modify persistent settings | none | yes (settings file) | no | no |
| extract | Produce one normalized output file from local source data | scope confirmed | yes (stage marker, last-extract record) | yes (normalized records) | no |
| attest | Validate a prior output file, record review attestations, unlock outbound transmission | extract complete | yes (stage marker, attestation record) | no | no |
| publish | Transmit an attested output file to a dataset platform | attest complete | yes (stage marker, last-publish record) | no | yes (upload to platform) |

## install-helper

**Purpose.** Install a set of instructions or integration assets into the
user's current working project so that a conversational coding assistant can
drive the rest of the workflow on the user's behalf.

**Inputs.**
- An identifier selecting which coding-assistant integration to install.
  A tool of this class typically supports one or more known assistant
  products. The implementer decides the supported list.

**Outputs.**
- One or more files written under a project-local convention that the
  target assistant uses to discover instructions. The exact filename,
  encoding, and location are dictated by the target assistant's own
  convention.
- A confirmation message naming the location(s) written and the
  recommended next capability to invoke.

**Behavior.**
1. The utility attempts to fetch the latest version of its integration
   text from a network source it controls.
2. If the network fetch fails, the utility falls back to a copy of the
   integration text bundled inside its own distribution.
3. If neither is available, the operation fails with a message pointing
   the user at the project's distribution source.

**Failure modes.** Unsupported integration target; network fetch failure
with no bundled fallback; filesystem write error.

## survey

**Purpose.** Produce a machine-readable snapshot of where the user is in
the overall workflow, covering: which origin categories have detectable
data on this machine, whether the user has authenticated against a
publication platform (if configured to use one), whether scope decisions
have been recorded, what progress marker the persistent settings record.

**Inputs.**
- Optional: a hint indicating which origin category the user cares about.
  If absent, the utility auto-detects based on which origin categories
  have data present on this machine.

**Outputs.**
- A structured document (JSON or equivalent) describing: the current stage
  (see `02-configuration-model.md` for the stage enumeration), the
  detected destination-platform identity (if any), the detected
  groupings, configured redactions (secrets are masked for display),
  excluded groupings, and a human-readable list of next steps keyed to
  the current stage.

**Behavior.**
1. Load persistent settings.
2. Attempt a read-only lookup of destination-platform credential state
   using that platform's own client library, without mutating the
   credential.
3. Walk each supported origin category's storage location. For every
   origin category whose storage is present, enumerate the groupings.
4. Compute the stage by combining credential presence, recorded scope
   decisions, and recorded attestation state from the settings.
5. Persist the freshly computed stage back to the settings.
6. Print the structured snapshot.

**Partial-state safety.** The operation is read-only with respect to user
data and only writes the current-stage field in the settings.

## inspect

**Purpose.** Enumerate the groupings currently detectable under a selected
origin category, without touching persistent state or preparing for any
later step. This is the capability a user invokes before deciding which
groupings to exclude.

**Inputs.**
- Optional: which origin category to inspect. If absent, defaults to the
  same auto-detection rule used by `survey`.

**Outputs.**
- A structured list. Each entry represents one grouping and carries:
  a display label for the grouping, the origin category that produced
  it, an estimated record count, an estimated on-disk size, and a flag
  indicating whether the grouping is currently excluded in the settings.

**Behavior.** This capability is purely a read; it does not mutate
settings.

## configure

**Purpose.** Read or modify the persistent settings store.

**Inputs (any subset; all optional).**
- A destination-platform repository identifier.
- An explicit origin-category scope decision (one specific category, or
  a pseudo-value meaning "all categories").
- A set of grouping labels to exclude.
- A set of literal strings to always redact.
- A set of username-like handles to always anonymize (for example,
  display handles from code-hosting or chat platforms not present in
  the local account identity).
- A flag marking the grouping-selection step as confirmed.

**Outputs.**
- When invoked with no changes: the current settings, printed for
  display, with any entry in the literal-redaction list masked.
- When invoked with changes: the merged settings after applying the
  inputs, printed for display, with literal redactions masked.

**Behavior.**
1. Load current settings.
2. For each list-valued input (excluded groupings, literal redactions,
   username-like handles), merge with the existing value by set union
   and sorted canonicalization. Configure never removes entries from
   these lists implicitly; list removal is an implementation decision
   for the utility's maintainers.
3. For scalar inputs, overwrite.
4. Persist. Behavior when the settings file cannot be written is an
   implementation decision, but must surface a clear error.

## extract

**Purpose.** Produce one normalized output artefact from selected local
sources, with anonymization and secret redaction applied, without any
network I/O.

**Inputs.**
- Optional: output file path. If absent, the utility chooses a default
  filename rooted in the current working directory.
- Optional: origin-category scope. If absent, uses the value from the
  persistent settings, or rejects the operation if the settings do not
  record a confirmed scope.
- Optional: a flag overriding the grouping-exclusion list and including
  every grouping in the chosen scope.
- Optional: a flag suppressing the reasoning-trace field of assistant
  turns in the output.
- Optional: a publication-approval attestation (used by `publish`; see
  below).

**Preconditions.**
- The persistent settings must record either an explicit origin-category
  scope or the override flag must be present.
- Unless the override flag is present, the settings must record the
  grouping-selection-confirmed marker.
- For publication-path invocations (see `publish`), additional
  preconditions apply.

**Outputs.**
- A newline-delimited JSON file, one record per line, conforming to
  `03-output-format.md`. The file is written with UTF-8 encoding and
  without a byte-order mark.
- A textual summary of counts (record count, skipped-record count,
  redaction count) and a breakdown by model identifier.
- Settings update recording the timestamp, record count, and scope of
  the most recent extraction.
- Stage marker advance.

**Behavior.**
1. Resolve the effective scope and grouping list.
2. Construct an anonymization session keyed to the local account
   identity and any configured username-like handles.
3. For each included grouping, dispatch to the per-origin-category
   extractor (see files under `04-input-categories/`).
4. For every candidate record:
   a. If the record lacks a model identifier, or is marked as abandoned
      by the source, skip it.
   b. If the origin category warrants deduplication (see the relevant
      input-category file for the rule), fingerprint the record and
      drop duplicates.
   c. Apply secret redaction to every textual field reachable from the
      record, recursing into nested structures.
   d. Apply literal-string redaction for each user-configured literal.
   e. Write the record as one line of JSON.
5. Accumulate the per-grouping and per-model breakdown, the redaction
   count, and the skipped-record count.
6. Persist the accumulated summary and advance the stage marker.

This capability must not upload, notify, or otherwise communicate with
any remote system. All network activity is confined to `publish`.

## attest

**Purpose.** Record that the user (or their agent) has performed a
specific, named set of review steps on a previously produced output
artefact. Without this record, the utility refuses to publish.

**Inputs.**
- A path to the output artefact to review. If absent, the utility
  searches the current working directory for a single matching file.
- Either:
  - A full name the user agrees can be scanned for verbatim, or
  - A flag indicating the user declined to share a name for this
    check.
- Three free-form text attestations describing, in the user's or
  agent's own words, the three required review actions:
    (i) the full-name scan or the reason for declining it,
   (ii) the interview about sensitive entities (company, client,
        internal project, private URL, and so on) and its outcome,
  (iii) the manual sample review of records and the number of records
        reviewed.

**Preconditions.**
- A prior `extract` must have completed and its output file must be
  present at the path the user supplies.

**Outputs.**
- A structured report of: record count, per-grouping count, per-model
  count, the results of the full-name scan (match count and example
  line excerpts), and the results of the built-in sensitive-content
  scan.
- Settings update recording: the three attestation strings, the
  outcome of the full-name check, the sample count the user claims to
  have reviewed, the path of the file attested, the full name used
  (or the fact that the user declined), a timestamp, and an advance
  of the stage marker.

**Validation rules applied to the attestation inputs.**
The utility must reject the operation unless all of the following hold:

- Each of the three attestation texts is at least a configurable
  minimum number of characters long. The default minimum is 20
  characters. This forces a substantive description rather than a
  token word.
- The full-name attestation, when a name was provided, must mention
  both (a) asking the user for the name and (b) having scanned the
  export for it, and must itself contain every word of the supplied
  name. When a name was not provided, the attestation must mention
  that the user declined or skipped the check.
- The sensitive-entity attestation must mention asking the user about
  at least one of the named sensitive-entity categories and must
  mention the outcome (for example: "none found" or "added to
  redaction list").
- The manual-review attestation must mention a manual scan and must
  include an integer at or above a configurable sample threshold.
  The default threshold is 15 records.

On validation failure, the utility must emit a structured error listing
each failed check and must not advance the stage marker.

**Behavior.**
1. Locate the output file (using the supplied path, or the fallback
   search rule).
2. Validate the attestation inputs per the rules above.
3. Execute the full-name scan (see `05-sensitive-content-handling.md`).
4. Execute the built-in sensitive-content scan.
5. Re-tally record, grouping, and model counts from the file on disk
   (the utility trusts the file, not any remembered summary).
6. Persist the attestation record and advance the stage marker.
7. Emit a structured report with the counts, scan results, and
   rendered next-step guidance.

## publish

**Purpose.** Upload a previously attested output file to a remote dataset
platform identified by a repository-style identifier.

**Inputs.**
- Optional: a repository identifier overriding the one stored in
  settings. If absent and settings do not have one, the utility
  attempts to derive a default using the user's detected platform
  identity.
- A publication-approval attestation: free-form text in which the
  operator states that the user explicitly approved publishing.

**Preconditions.**
- The stage marker must indicate that an attested export is on disk.
- The attestation record must carry valid values for all required
  review actions (re-validated here, independent of `attest`).
- The publication-approval attestation itself must satisfy:
  - Length at or above the configurable minimum (default 20
    characters).
  - Contains at least one word from a set indicating approval
    (for example "approved").
  - Contains at least one word from a set indicating transmission
    (for example "publish" or "upload").
- The file referenced in the attestation record must still exist.

**Outputs.**
- Remote mutations: the creation of the target dataset repository
  (if not present), the upload of the normalized records file, the
  upload of a metadata manifest, and the upload of a generated
  human-readable description.
- Local settings update: the stage marker advances to the published
  state, and the publication-approval attestation is recorded.
- Terminal output showing the remote URL of the resulting dataset.

**Behavior.**
1. Re-validate every precondition from scratch. Do not rely on a
   cached assumption that, because `attest` succeeded, the file is
   still valid.
2. Authenticate with the platform using that platform's own
   credential mechanism. Do not prompt for credentials in this
   operation; require that they were set up ahead of time.
3. Upload the file.
4. Upload a metadata manifest (see `03-output-format.md`).
5. Generate and upload a human-readable description. The template is
   an implementation decision, but must at minimum include the record
   count, aggregate usage statistics, per-model and per-grouping
   tables, and a short loading snippet.
6. Persist stage advance.
7. Print the resulting URL.

**Re-execution.** A user may re-run the full flow at any time. A
re-publication replaces the previously uploaded file in place
(using the platform's own update semantics); it does not create a
new repository unless the user specifies a different identifier.
