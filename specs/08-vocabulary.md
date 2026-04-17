# Vocabulary

This document defines the terms the specification uses with specific
meaning. Every term here is invented by this specification for
clarity; implementers pick their own names in their own product.

## Structural terms

**Record.** One conversation exported from one source. The unit that
the normalized output emits, one per line.

**Turn.** One message within a record. Has a role (user or
assistant) and optional text, reasoning, tool invocations, and
attached content.

**Tool invocation.** An assistant-initiated call to a tool,
containing a tool name, input arguments, an optional paired
output, and an optional status indicator.

**Attached content.** Structured non-text content attached to a
user turn — typically images or documents — expressed as a list of
entries each carrying a media type and a source (inline or URL).

**Reasoning trace.** An optional free-form string on an assistant
turn reflecting the model's private deliberation. Emitted when the
source supplies it and the user has not opted out.

**Usage tally.** The per-record aggregate counters: user-turn count,
assistant-turn count, tool-invocation count, input-token count,
output-token count.

**Metadata manifest.** A companion document to the records file,
produced at publication time, containing the per-record aggregate
totals and the breakdowns by model identifier and grouping.

**Description document.** The human-readable Markdown document
produced at publication time for display on the destination
platform's repository page.

## Process terms

**Discovery.** The act of walking an origin category's storage to
enumerate groupings and estimate counts and sizes.

**Extraction.** The act of reading one origin category's records,
normalizing them, anonymizing and redacting textual content, and
writing normalized records to the local output file.

**Review gate.** The set of checks that must pass before the
utility will transmit data to a remote destination. The gate is
composed of attestation-text validation, built-in scan
presentation, and preservation of the attested file.

**Attestation.** A short free-form text statement from the user
(or their agent) describing that a specific review action has
been performed. Three are required before publication; a fourth
(publication approval) is required at publication time.

**Scope.** The combination of origin-category selection plus the
list of included and excluded groupings. Fixed by the user via
configure.

**Publication.** The act of transmitting the attested local
output file plus its manifest and description to a remote
destination platform.

## Entity terms

**Origin category.** One kind of coding-assistant product that can
produce source data. Each supported origin category has a dedicated
extractor per `04-input-categories/`.

**Grouping.** One workspace- or project-level organizational unit
within an origin category. A discovery walk emits one entry per
grouping.

**Display label.** The human-readable string the utility shows the
user for a grouping. May be derived from the grouping's source
identifier (directory name, hashed path, working-directory column,
etc.).

**Placeholder marker.** The fixed string used to replace any text
matched by the secret-detection or literal-redaction layers. A
single marker is used throughout the utility; it is chosen to be
distinctive and unlikely to collide with real content.

**Pseudonymous token.** The deterministic replacement for a
user-identifying string produced by the anonymizer. Same input
always yields same output; not reversible.

**Destination platform.** Any public or private dataset hosting
platform to which the utility can publish. A tool of this class
supports one or more; each has its own credential mechanism
accessed via its own client library.

## State terms

**Stage marker.** The persisted record of how far the user has
progressed in the overall workflow. Values: initial, preparing,
pending-review, cleared, finalized.

**Settings store.** The single file in the per-user configuration
directory that persists the settings listed in
`02-configuration-model.md` across invocations.

**Last-extract record.** The persisted summary of the most recent
extract: timestamp, record count, scope.

**Last-attest record.** The persisted summary of the most recent
attest: timestamp, attested file path, full name used or skipped,
full-name match count, parsed manual-sample count, scan findings
flag.

**Review-verification record.** The machine-parsed corollaries of
the user's attestations: the full name that was provided (or
skip flag), the full-name scan result, the parsed sample count.

**Publication-approval attestation.** The final text attestation
supplied at publication time, declaring that the user approved
outbound transmission.

## Quantitative terms

**Minimum attestation length.** The minimum character count a
free-form attestation must meet to pass validation. Default: 20
characters.

**Minimum manual-sample count.** The minimum integer that must
appear in the manual-review attestation text. Default: 15
records.

**Large-binary threshold.** The minimum string size, in bytes,
above which the redactor and anonymizer check whether the string
is a binary payload and leave it unchanged if so. Default: 4096
bytes.

**Custom-redaction minimum length.** The minimum length of a
literal-redaction entry to be honored. Default: 3 characters.

**Handle minimum length.** The minimum length of a
configured-handle entry to be honored by the anonymizer. Default:
4 characters.

**Scan-result display cap.** The maximum number of high-entropy
quoted-string findings shown in the attest report. Default: 15
entries.

**Email-finding display cap.** The maximum number of email matches
shown per category during the built-in scan. Default: 20 entries.

## Flag-style terms

**Include-all override.** A flag passed to extract that forces
inclusion of every grouping regardless of the exclusion list.

**Reasoning opt-out.** A flag passed to extract that suppresses
the reasoning field of assistant turns in the output.

**Name-scan skip.** A flag passed to attest indicating the user
declined to share a full name; the attestation text for the
full-name check must acknowledge the skip in order to pass
validation.

**Local-only mode.** A flag passed to extract that ensures the
capability stops after writing the local file and does not attempt
publication; this is the default mode of operation.
