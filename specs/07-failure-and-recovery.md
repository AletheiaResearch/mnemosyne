# Failure Modes and Recovery

This document enumerates the categories of failures the utility may
encounter, what the utility must detect, what it must do in response,
what the user sees, and how the user recovers. Where a failure aligns
with a specific capability or flow, the entry names it. Where the
response is an implementation decision, the entry says so rather than
prescribing behavior.

## Source data failures

### Source storage absent

**Trigger.** The user invokes a capability that walks the storage
of a specific origin category, but that storage is not present on
this machine.

**Detection.** The origin-category discovery returns an empty list
or a specific "storage absent" signal.

**Response.**
- If the user asked for a single origin category explicitly, the
  utility exits non-zero with a message naming the expected
  storage location category (e.g., "under the per-user
  application-data directory for that assistant").
- If the user asked for "all," the utility silently skips the
  absent origin and proceeds with whichever origin categories
  have storage.

**Recovery.** The user installs or configures the origin
assistant and re-runs.

### Source storage present but empty

**Trigger.** The origin category's storage exists but contains
zero groupings.

**Detection.** Discovery returns an empty list.

**Response.** The utility reports zero groupings for that origin
and does not advance the stage marker. The user sees a clear
message.

**Recovery.** The user generates some conversation data with the
origin assistant and re-runs.

### Malformed record in source file

**Trigger.** A newline-delimited JSON file contains a line whose
JSON cannot be parsed.

**Detection.** JSON parse failure on a single line.

**Response.** The utility logs a warning naming the file and line
number, skips the line, and continues with the next line. The
overall record extraction is not aborted.

**Recovery.** None required. The user may investigate the
offending file if desired.

### File read permission denied

**Trigger.** The utility lacks read permission on a source file.

**Detection.** OS-level permission error.

**Response.** The utility logs a warning naming the file and
skips it. The per-grouping record count will be lower than the
discovery estimate.

**Recovery.** The user adjusts permissions and re-runs if they
want the skipped records included.

### Database file in use

**Trigger.** An embedded-database origin category is in use by
its owning assistant when the utility opens it.

**Detection.** The database layer opens read-only and may read a
slightly-stale snapshot.

**Response.** The utility reads whatever the snapshot presents.
A warning about the snapshot being possibly stale is an
implementation decision.

**Recovery.** The user may close the owning assistant and
re-run to capture the live state.

### Schema drift between versions

**Trigger.** A source assistant has updated its storage schema
and the utility encounters fields or shapes it does not know.

**Detection.** Per-field checks fall through to default
handling; unrecognized structural elements fall through to
pass-through paths.

**Response.** The utility extracts what it recognizes; unknown
structural elements are preserved as-is inside the output's raw
fields when a raw-fields path is available, or dropped
otherwise. A warning for completely-unrecognized records is an
implementation decision.

**Recovery.** The utility's maintainers update the per-origin
extractor. The user may update their installation.

## Sensitive-content failures

### Built-in scan reports findings

**Trigger.** The built-in sensitive-content scan run at attest
time surfaces email addresses, token-shaped strings, high-
entropy quoted strings, or IPv4 addresses.

**Detection.** The scan returns a non-empty findings object.

**Response.** The attest operation surfaces the findings in its
structured report. It does not automatically block; it is the
user's responsibility to judge findings as real or benign.

**Recovery.** The user registers additional literal redactions
via the configure capability and re-runs extract. If the
findings were false positives, the user acknowledges them in
the attestation text.

### Full-name scan reports matches

**Trigger.** The user supplied a full name during attest and the
scan found occurrences.

**Detection.** The scan's match count is non-zero.

**Response.** The attest operation surfaces match count and
example line excerpts. It does not block.

**Recovery.** The user decides whether the occurrences are
problematic. If so, they configure redactions and re-extract.

### User has not completed review

**Trigger.** The user invokes publish without having run attest,
or attest failed validation.

**Detection.** The stage marker is not (D) cleared, or the
stored attestation record is missing or invalid.

**Response.** The utility emits a structured error naming the
missing step and exits non-zero. The error message points the
user at the capability that must be run next.

**Recovery.** The user runs attest with valid attestation text.

### Attestation text fails validation

**Trigger.** One or more attestation texts are too short, do not
mention the required key phrases, or do not include the required
numeric sample count.

**Detection.** The validator for each attestation category
returns a specific error.

**Response.** The utility emits a structured error listing each
failed check individually. The stage marker does not advance.

**Recovery.** The user (or agent) supplies better attestation
text. The utility deliberately does not offer a "force-through"
bypass.

### Publication-approval attestation fails validation

**Trigger.** The text passed to publish does not meet the
length, approval-word, or transmission-word requirements.

**Detection.** The publication-attestation validator returns an
error.

**Response.** The utility emits a structured error naming the
specific failed check. Publication does not proceed.

**Recovery.** The user supplies a proper attestation.

### User attempts a deprecated bypass flag

**Trigger.** The user (or an older agent integration) passes a
flag that once existed in an earlier design but that the
utility no longer honors as a bypass path.

**Detection.** The argument parser surfaces the deprecated
flag.

**Response.** The utility emits a structured error explaining
that the old bypass no longer works and pointing to the text-
attestation replacement. The command exits non-zero.

**Recovery.** The user invokes the command with the current
text-attestation arguments.

## Configuration-state failures

### Settings file cannot be read

**Trigger.** The persistent settings file exists but the JSON is
corrupt or the file cannot be opened.

**Detection.** Parse or OS error during load.

**Response.** The utility emits a warning to standard error,
treats settings as empty (all defaults), and continues.

**Recovery.** The user hand-edits or deletes the file.

### Settings file cannot be written

**Trigger.** The utility tries to persist settings but the file
system rejects the write.

**Detection.** OS-level error on write.

**Response.** The utility emits a warning to standard error and
continues the current operation. The specific operation may
still succeed but the settings will not reflect it on next
run.

**Recovery.** The user adjusts permissions or disk space and
re-runs the operation that should have persisted.

### Settings written by an incompatible version

**Trigger.** The utility finds keys it does not recognize or
misses keys it expected.

**Detection.** Per-key lookups fall through to defaults.

**Response.** Unknown keys are ignored (forward-compatible).
Missing keys become defaults. The utility continues.

**Recovery.** No user action required; the next save will
write the current-version shape while preserving unknown
keys (an implementation decision).

### Stage marker points at a state that is no longer valid

**Trigger.** The stage marker says "cleared" but the file the
attestation refers to has been deleted.

**Detection.** At publish time, the referenced file path is
missing.

**Response.** The utility emits a structured error explaining
that the attested file is gone and pointing the user back to
extract.

**Recovery.** The user re-runs extract and attest.

## Destination-platform failures

### Credentials missing

**Trigger.** The user invokes publish without having logged in
to the destination platform.

**Detection.** The platform client library reports no
credential.

**Response.** The utility emits a structured error with
instructions on how to log in via the platform's own
credential mechanism. It does not prompt for credentials
directly.

**Recovery.** The user logs in and re-runs publish.

### Credentials rejected

**Trigger.** The platform returns an authentication error
during upload.

**Detection.** The platform's client library raises an error
indicating bad credentials.

**Response.** The utility emits a structured error quoting the
platform's message. Exits non-zero.

**Recovery.** The user refreshes their credentials and re-runs
publish.

### Destination repository creation refused

**Trigger.** The platform refuses to create the repository
(for example due to a naming conflict with another user, or
quota limits).

**Detection.** The platform's client library raises an error.

**Response.** The utility emits the error and exits non-zero.

**Recovery.** The user picks a different repository identifier
via the configure capability.

### Network interruption during upload

**Trigger.** The upload is aborted partway.

**Detection.** OS- or library-level network error.

**Response.** The utility emits the error. The stage marker is
not advanced to (E) finalized. The local file is intact.

**Recovery.** The user re-runs publish. The platform's own
semantics determine how the previously partial upload is
handled (most platforms overwrite on retry).

### Upload exceeds platform size limits

**Trigger.** The file is larger than the destination platform
will accept.

**Detection.** The platform rejects the upload with a size
error.

**Response.** The utility emits the error. No automatic
split-and-retry is performed; behavior is an implementation
decision.

**Recovery.** The user re-extracts with a narrower scope (for
example, excluding larger groupings) and re-runs the review
and publication flow.

## Operational failures

### User interrupts a long operation

**Trigger.** Keyboard interrupt (or equivalent) during
discovery, extraction, review, or publication.

**Detection.** The utility receives the signal.

**Response.** Partial state:
- During discovery: nothing was persisted; the next run
  re-discovers from scratch.
- During extraction: the output file is partially written.
  The stage marker has not advanced. The last-extract
  record has not been written.
- During review: no attestation has been persisted unless
  every validation passed before the interrupt.
- During publication: upload may be partial; see network
  interruption above.

**Recovery.** The user re-runs the interrupted capability.
Specific cleanup behavior (for example, truncating a
partially-written output file on next extract) is an
implementation decision.

### Concurrent invocations

**Trigger.** Two invocations run against the same settings file
simultaneously.

**Detection.** None required by this specification.

**Response.** The last writer wins. The user may see a stage
that reflects only one of the two invocations.

**Recovery.** The user runs the survey capability to recompute
stage and continues manually. Implementers may add file
locking; that is an implementation decision.

### Output exceeds reasonable local disk

**Trigger.** The user's corpus is large enough that the output
file fills available disk space.

**Detection.** OS-level write error.

**Response.** The utility emits the error and exits non-zero.
The partially-written file remains on disk.

**Recovery.** The user frees space or narrows scope. Automatic
cleanup of the partial file is an implementation decision.

### Agent-integration asset fetch fails

**Trigger.** The install-helper capability cannot reach its
network source and has no bundled fallback.

**Detection.** Network error plus absent local fallback.

**Response.** The utility exits non-zero with a message
pointing the user to the project's distribution source.

**Recovery.** The user installs the agent integration manually
or re-runs when connectivity returns.

## Recovery matrix

| Failure | Blocks what | Recovery |
|---|---|---|
| Source storage absent (single-origin) | extract | Install the assistant or pick a different origin |
| Source storage absent (all-origins) | none | Utility proceeds with other origins |
| Settings file corrupt | nothing | Warning; defaults used |
| Settings file unwriteable | persistence | Fix permissions; re-run |
| Stage marker mismatched to on-disk reality | publish | Re-run extract and attest |
| Missing attestations | publish | Run attest with valid text |
| Invalid attestation text | publish or attest | Supply valid text |
| Credential absent | publish | Log in via platform's mechanism |
| Credential rejected | publish | Refresh credentials |
| Network failure during upload | publish | Re-run publish |
| Output exceeds disk | extract | Narrow scope and re-run |
| Concurrent invocations | unpredictable | Re-run survey to reconcile |
