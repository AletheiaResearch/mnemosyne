# Configuration Model

The utility persists a small amount of state between invocations so that the
multi-stage workflow is interruptible and recoverable. This document
specifies the semantic content of that state. Implementers choose the
storage file format, the exact key names, and the on-disk layout — subject
only to the constraints below.

> **Note on key names.** Every key name used below in backticks
> (`repo-id`, `origin-scope`, `grouping-exclusions`, `custom-redactions`,
> `custom-handles`, `scope-confirmed`, `phase-marker`, `last-extract`,
> `reviewer-statements`, `verification-record`, `last-attest`,
> `publication-attestation`) is an abstract label introduced by this
> specification for the purpose of referring to the corresponding
> settings entry within these documents. They are not mandated identifiers
> for the on-disk file. Implementers choose whatever concrete identifiers
> suit their language conventions, file-format conventions, and project
> style. What matters is that each settings entry described here has a
> corresponding identifier in the implementation, used consistently.

## Storage location

The persistent settings live in a single file inside a per-user
application-data directory, following the conventions of the host
operating system. On a Unix-like host this is typically a hidden
subdirectory of the user's home directory; on other hosts, the
platform's recommended per-user configuration root. The exact file
name and directory name are an implementation decision.

The utility must:

- Create the containing directory on first write if it does not exist.
- Tolerate the settings file being absent: a missing file is equivalent
  to an empty settings object carrying all defaults.
- Tolerate the settings file being corrupt or truncated: the utility
  reports a non-fatal warning and proceeds as if the file were empty.
  Behavior on concurrent writes is an implementation decision, but
  partial-write corruption must not block all future invocations.

## File format

The storage encoding must be a format that survives a round-trip through
the host's standard text-editing tools: for example, JSON with a
conventional indentation. Binary or opaque formats are explicitly
disallowed because the user may need to inspect or hand-edit the file
to recover from mistakes.

## Settings entries

The following entries are the semantic content of the settings file.
Implementers choose their key names; the names in parentheses below are
the abstract labels this specification uses to refer to each entry.

### Destination repository identifier (`repo-id`)

- **Type.** String or absent.
- **Meaning.** Opaque identifier naming the remote dataset repository
  that `publish` targets. Typical shape on a typical platform is
  `namespace/name`, but the utility treats it as an opaque string and
  does not parse it.
- **Default.** Absent.
- **When read.** During `publish`, and during `survey` to decide
  whether the default repository name should be derived from the
  detected account identity.
- **When written.** By `configure` when the user supplies one; and
  opportunistically during `publish` if none was set and the utility
  derived one from the platform identity.
- **Invalidation.** If the platform rejects the identifier during
  `publish`, the utility reports the error and does not clear the
  stored value automatically.

### Origin-category scope (`origin-scope`)

- **Type.** One of a finite enumeration: each supported origin
  category name, plus a pseudo-value that means "all origin
  categories." Absent is permitted.
- **Meaning.** Which origin category (or all) the user has
  confirmed as the scope of the next extraction.
- **Default.** Absent. An absent scope is treated as not-yet-
  confirmed, and `extract` refuses to run unless the scope is
  supplied inline.
- **When read.** Every capability that dispatches to per-origin
  extractors.
- **When written.** By `configure`.

### Excluded groupings (`grouping-exclusions`)

- **Type.** Sorted list of strings.
- **Meaning.** Display labels of groupings the user has explicitly
  removed from the extraction scope. Membership is by exact
  canonical display label.
- **Default.** Empty list.
- **Merge semantics.** Setting a value unions the new entries with
  the existing list and re-sorts. Removal is not offered through
  the configure surface; the user hand-edits the file (see
  "Hand-editable invariant" below).
- **When read.** By `extract` and `inspect`.
- **When written.** By `configure`.

### Literal-string redactions (`custom-redactions`)

- **Type.** Sorted list of strings.
- **Meaning.** Arbitrary literal substrings the user wants removed
  from every output record. Typical uses include internal project
  codenames, private URLs, client names, and secrets that the
  built-in detector does not catch.
- **Default.** Empty list.
- **Merge semantics.** Same as the exclusion list.
- **When displayed.** Every entry is masked for display (for
  example, only the first and last four characters visible, the
  middle replaced with a run of marker characters, and short
  entries replaced entirely with a placeholder). This is to
  prevent a user scanning a screen or a logging system from
  learning a secret someone else configured.
- **Minimum length.** Entries shorter than three characters are
  ignored at redaction time to prevent catastrophic over-matching.

### Handle anonymizations (`custom-handles`)

- **Type.** Sorted list of strings.
- **Meaning.** Usernames, display handles, or other identity-
  bearing short strings that must be replaced with a consistent
  pseudonymous token.
- **Default.** Empty list.
- **Merge semantics.** Same as the exclusion list.
- **Minimum length.** Entries shorter than four characters are
  ignored to avoid over-matching short tokens.

### Grouping-selection confirmation (`scope-confirmed`)

- **Type.** Boolean.
- **Meaning.** A latch indicating the user has reviewed the
  grouping list (including any exclusions) and approved it.
  `extract` refuses to run while this is false unless the caller
  passes the include-all override.
- **Default.** False.
- **When written.** By `configure`, either explicitly via a
  dedicated flag or implicitly when the user sets exclusions
  (setting an exclusion implies the user has now seen the list).

### Stage marker (`phase-marker`)

- **Type.** One of a small enumeration. This specification names
  the stages; implementers pick their own identifiers.

  | Stage | Meaning |
  |---|---|
  | (A) initial | No credentials detected; no progress yet. |
  | (B) preparing | Credentials are present; scope not yet committed; no extract on disk. |
  | (C) pending-review | A local extract exists and is awaiting attestation. |
  | (D) cleared | The local extract has been attested; publication is unlocked. |
  | (E) finalized | A publication has completed for the current extract. |

- **Default.** The stage computed from other settings at load time
  (no separate persisted value is strictly required, but the
  implementation may cache it).
- **Stage transitions.** Each capability advances the stage
  deterministically. Stage cannot regress except through re-running
  an earlier capability.
- **Computed components.** The stage is derived from:
  - Presence of platform credentials (computed live at read time,
    not persisted).
  - Presence of a last-extract record.
  - Presence of a valid attestation record.
  - Presence of a last-publish record.

### Last-extraction record (`last-extract`)

- **Type.** Object.
- **Meaning.** The summary of the most recent `extract` run:
  - Timestamp of the run (ISO-8601 UTC).
  - Record count.
  - Scope that was used.
- **Default.** Absent.

### Review-attestation record (`reviewer-statements`)

- **Type.** Object keyed by the three review categories (full-name
  scan, sensitive-entity interview, manual sample review). Each
  value is the attestation text the user supplied.
- **Default.** Absent.

### Review-verification record (`verification-record`)

- **Type.** Object.
- **Meaning.** The machine-verified corollaries of the attestation:
  - The full name the user provided (or a flag indicating the user
    declined).
  - Whether the full-name scan was explicitly skipped.
  - The match count from the full-name scan.
  - The sample count the utility parsed out of the manual-review
    attestation.
- **Default.** Absent.

### Last-attestation record (`last-attest`)

- **Type.** Object.
- **Meaning.** A summary of the most recent `attest` run:
  - Timestamp.
  - Absolute path of the file attested.
  - Whether the built-in sensitive-content scan found anything.
  - Everything from the review-verification record, duplicated for
    audit convenience.
- **Default.** Absent.

### Publication-approval attestation (`publication-attestation`)

- **Type.** String.
- **Meaning.** The most recent text statement in which the
  operator declared the user had approved outbound transmission.
- **Default.** Absent.

## Defaults

The following table summarizes the defaults applied when the settings
file is absent or silent on a particular entry.

| Setting | Default |
|---|---|
| `repo-id` | absent |
| `origin-scope` | absent |
| `grouping-exclusions` | empty list |
| `custom-redactions` | empty list |
| `custom-handles` | empty list |
| `scope-confirmed` | false |
| `phase-marker` | initial |
| `last-extract` | absent |
| `reviewer-statements` | absent |
| `verification-record` | absent |
| `last-attest` | absent |
| `publication-attestation` | absent |

## Hand-editable invariant

A user must be able to open the settings file in a text editor and
- remove entries from any list,
- reset the stage marker to a prior stage,
- clear attestation state to force a re-run,
- introduce or retain keys the current version of the utility does
  not recognize — for example because they were written by a newer
  version, or because the user is preparing to upgrade and wants
  in-progress edits to survive the transition,

without corrupting the file's schema. The implementation must treat
unknown keys as non-fatal (forward compatibility) and must not rewrite
the file in a way that erases user edits unrelated to the operation
in flight. A user upgrading from an older version of the utility must
not lose their settings in the upgrade.

## Display masking

Wherever settings are shown back to the user (for example by
`configure` with no arguments or by `survey`), entries of the
`custom-redactions` list must be masked such that the first and last
few characters of each entry are preserved and the middle is
replaced with a marker run. Strings below a minimum length are
replaced entirely with a placeholder. Implementers choose the mask
template.

## Concurrency

The utility does not guarantee safety under concurrent invocations
against the same settings file. Implementers may add file locking;
absent a lock, the last writer wins and the user may need to re-run
the most recent capability. This is an implementation decision.
