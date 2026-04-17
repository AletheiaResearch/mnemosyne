# Workflow Flows

This document walks each major user-facing flow end to end. Each flow
is described as a sequence of behaviors with decision points and
failure branches called out inline. Where a flow triggers a capability
described in `01-command-surface.md`, it refers to the capability by
its abstract name rather than any specific command invocation.

## Overall state machine

Every flow operates against a single persisted state machine whose
stages were named in `02-configuration-model.md`:

```
  (A) initial ──► (B) preparing ──► (C) pending-review ──► (D) cleared ──► (E) finalized
     ▲               │                     │                   │                  │
     │               └─── re-run at any time (returning to B or re-extracting) ───┘
     └── any stage can repeat
```

Stage transitions happen exclusively inside capabilities. A flow
description below says things like "the utility advances the stage
marker"; this is always implemented by the capability, never by the
flow itself.

## Flow 1: First-time onboarding

**Trigger.** The user installs the utility for the first time and
runs it, or runs it with no arguments in a working directory that
has never been configured.

**Steps.**

1. The utility computes the initial stage by inspecting: whether a
   platform credential can be detected via the platform's own
   client library; whether the persistent settings file exists and
   what it contains.
2. With no credential present and no saved settings, the utility
   determines it is at stage (A) initial.
3. The utility prints a next-steps list for stage (A):
   a. Ask the user for a platform credential (or inform them that
      credential setup is needed if they intend to publish).
   b. Invoke the platform's own credential-storage tool with the
      user's token.
   c. Register the token itself as a literal-redaction entry so
      that any appearance of the token inside conversation data
      is redacted at extraction time.
   d. Re-run the survey capability to confirm stage progression.
4. If the user intends never to publish (only to produce a local
   archive), they may skip credential setup. The downstream
   `extract` capability runs without requiring credentials; only
   `publish` does.

**Branches.**

- If credentials are present, the utility jumps directly to the
  stage (B) preparing flow below.
- If the user has an existing configuration file from a previous
  installation, the utility loads it and computes the stage it
  implies.

**Failure modes.**

- The platform client library is not installed: credential
  detection reports absent, and the flow degrades to local-only.
  The utility still functions.

## Flow 2: Scope configuration

**Trigger.** Stage (B) preparing. Either the user explicitly
triggers the survey capability, or the utility returns here
because scope has not yet been fixed.

**Steps.**

1. The utility walks each supported origin-category storage
   location and enumerates the detectable groupings. Each
   grouping is described by display label, origin category,
   estimated record count, estimated size, and an "excluded"
   flag from the current settings.
2. The utility displays the full grouping list.
3. The utility asks the user to choose an origin scope. If the
   user chooses a specific origin category, the pseudo-value
   for "all" is not used; if they want everything, they choose
   "all." The choice is persisted via the configure capability.
4. The user reviews the grouping list and names the groupings
   they want to exclude. The utility persists these as an
   exclusion list via the configure capability. Setting any
   exclusion implicitly marks the grouping selection as
   confirmed.
5. The user registers any literal strings to always redact
   (for example, internal project names, client names, private
   URLs) and any username-like handles to anonymize (for
   example, handles used on chat platforms that differ from
   the local account identifier). These are persisted via the
   configure capability.
6. When the user has made at least the scope and grouping
   choices, the utility reports stage (B) is satisfied and
   the next expected capability is `extract`.

**Branches.**

- If the user explicitly confirms the grouping list with no
  exclusions, that choice is persisted by a dedicated flag
  rather than by a non-empty exclusion list.
- If the user adds new handles or literals after an
  extraction has already run, they should re-run `extract`
  before proceeding; the previously extracted file is stale.

**Failure modes.**

- None of the supported origin categories have storage
  present on this machine: the utility reports the
  condition and exits non-zero, with guidance on where
  each origin category's storage would be expected to
  live.
- The detected grouping list is empty even for an origin
  category whose storage is present: the utility reports
  zero groupings for that origin and does not advance the
  stage.

## Flow 3: Local extraction

**Trigger.** The user invokes the extract capability with a
scope and an output path. Stage must be (B) preparing or later,
and scope must be confirmed.

**Steps.**

1. The utility verifies preconditions:
   a. Either the origin-scope setting is an explicit value,
      or the caller passed an include-all override flag.
   b. Either the grouping-selection-confirmed flag is true,
      or the caller passed an include-all override flag.
   c. The storage for the chosen origin category is present.
   d. At least one grouping is included after exclusions.
2. The utility opens the output file for writing. If opening
   fails, an error is reported and the flow stops.
3. The utility constructs the anonymizer with the configured
   handle list.
4. For each included grouping (in whatever order discovery
   returned), the utility dispatches to the per-origin
   extractor, anonymizes and redacts every record as it
   passes through, and writes each surviving record as one
   line of JSON.
5. As records are written, the utility maintains counters:
   total records, skipped records (no model identifier or
   marker-only model), total redaction count, per-model
   breakdown, per-grouping breakdown, input-token and
   output-token totals.
6. On completion, the utility prints a summary of the
   counters, prints guidance for the review step, records
   a last-extract entry in the settings with timestamp,
   record count, and scope, and advances the stage marker
   to (C) pending-review.
7. If the caller passed a publish-now flag, the flow
   continues into Flow 5 (publication) rather than
   stopping at (C).

**Branches.**

- Publish-now path: requires the preconditions of Flow 5
  (a prior attested export) and rejects otherwise.
- Local-only path (default when the user wants to inspect
  locally first): stops at stage (C) and emits a
  next-steps list pointing at Flow 4 (review and
  attestation).

**Failure modes.**

- The output path cannot be written: the utility reports
  the OS error and exits non-zero.
- A malformed record in a source file: the utility logs a
  warning and continues with the next record; the user
  sees nonzero skipped-record count.

## Flow 4: Review and attestation

**Trigger.** Stage (C) pending-review. A prior extract has
written a file to disk.

**Steps.**

1. The user inspects the output file using any combination of:
   a. The scan-and-summary output from the utility's
      attest capability (see steps below).
   b. Their own tools (grep, a text editor, etc.).
2. The user runs the attest capability, supplying:
   a. The path of the file being attested (explicit or
      implied via a search of the current working
      directory).
   b. Either their full name (for verbatim scanning) or a
      flag declining the full-name check.
   c. Three text attestations describing the three
      required review actions.
3. The utility validates the attestation inputs per
   `01-command-surface.md` and `05-sensitive-content-handling.md`.
   On any failure, it emits a structured error listing each
   failed check and stops.
4. With valid inputs, the utility:
   a. Runs the full-name scan against the file, if one was
      supplied.
   b. Runs the built-in sensitive-content scan against the
      file.
   c. Re-tallies record, per-grouping, and per-model
      counts by reading the file.
   d. Persists the attestation text, the verification
      record (full name, scan skip flag, match count,
      parsed sample count), the last-attest record (path,
      timestamp, full name, scan findings flag), and
      advances the stage marker to (D) cleared.
5. The utility emits a structured report combining the
   scan outputs, the re-tallied counts, and a rendered
   next-steps list pointing at Flow 5 (publication).

**Branches.**

- **Findings path.** If the user's own inspection or the
  built-in scan surfaces real sensitive content, the user
  registers additional literal redactions via Flow 2's
  configure step and re-runs Flow 3 on the same output
  path (or a different one). Re-running Flow 3 does not
  automatically invalidate the stored attestations — if
  the user re-extracted under the same terms, the file
  and the attestations still agree — but the user should
  re-run Flow 4 if the corpus has changed materially.
- **Skip-full-name path.** If the user declines to share
  a name, the flow runs with only two text attestations
  that carry sensitive-entity and manual-review
  information, plus a skip-attestation for the full-name
  check explaining the decline.

**Failure modes.**

- The file referenced does not exist: the utility emits
  a blocking error pointing the user back to Flow 3.
- The attestations fail validation: the utility emits a
  structured error listing each failed check and does
  not advance the stage marker.
- The full-name scan returns high match counts: not a
  blocking error, but surfaces prominently in the report
  so the user may choose to add redactions and re-run.

## Flow 5: Publication

**Trigger.** Stage (D) cleared, plus the user explicitly invoking
the utility with publication intent (the local-only flag is
omitted) and supplying a publication-approval attestation.

**Steps.**

1. The utility re-validates every precondition:
   a. The stage marker is (D) cleared.
   b. The previously-stored review attestations are still
      valid under the current rules (including that the
      parsed manual-sample count meets the threshold and
      that the full-name scan was either performed or
      explicitly skipped with a valid skip-attestation).
   c. The publication-approval attestation passes its own
      length, approval-word, and transmission-word
      validators.
   d. The last-attest record names a file path, and that
      file still exists on disk.
2. The utility persists the publication-approval
   attestation into settings.
3. The utility authenticates against the destination
   platform using the platform's own client library. If
   the library reports that no credential is present,
   publication stops with guidance on how to set up the
   credential (Flow 1).
4. The utility resolves the destination repository
   identifier: explicit value from the invocation if any,
   else the value from settings, else a default derived
   from the detected account identity.
5. The utility creates or updates the remote repository.
6. The utility uploads the normalized records file.
7. The utility uploads the metadata manifest derived from
   the last extraction's summary.
8. The utility generates and uploads the human-readable
   description document (template per
   `03-output-format.md`).
9. The utility advances the stage marker to (E) finalized
   and prints the resulting remote URL.

**Branches.**

- Re-publication: re-running the flow overwrites the
  uploaded files in place, using the platform's own
  update semantics. The previous published version may
  or may not be retained depending on the platform.
- Different destination: the user may specify a new
  repository identifier to produce a fresh upload target;
  this creates a new remote repository while leaving the
  previous one intact.

**Failure modes.**

- Any precondition fails: the utility emits a structured
  error naming the failed precondition and pointing the
  user at the capability that should be run to recover.
- The platform client library is not installed: the
  utility exits with guidance to install it.
- Network failure during upload: the utility reports the
  error and exits non-zero. The local file and the
  settings state remain intact; the user re-runs Flow 5.

## Flow 6: Iteration after publication

**Trigger.** Stage (E) finalized; the user has new conversations
since the last publication and wants to refresh the published
dataset.

**Steps.**

1. The user invokes the survey capability. The utility
   recomputes the stage and reports that the most recent
   step is publication.
2. To refresh, the user re-enters Flow 3 (extraction).
   Re-running the extract capability resets the stage
   marker back to (C) pending-review.
3. Flow 4 (review and attestation) must be re-run on the
   new file. The utility does not trust stale
   attestations that refer to a different file path or a
   different extract timestamp.
4. Flow 5 (publication) runs as above. The destination
   platform receives the new file; the utility does not
   attempt any delta upload — the whole file is
   re-uploaded.

**Branches.**

- If the user wants to change scope between publications,
  they interleave Flow 2 (scope configuration) with the
  above.

**Failure modes.**

- Same as the underlying flows.
