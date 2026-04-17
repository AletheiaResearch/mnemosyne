# Input Category 4: User-Supplied Pre-Normalized Pass-Through

## Category profile

This category exists for conversation data that is not produced by any
of the built-in origin categories the utility knows natively — for
example, records produced by a user's own recording scripts or by a
coding assistant whose native format the utility does not recognize.
To be consumed, such data must already be in the utility's normalized
output shape (see `03-output-format.md`), with only text redaction
and identity anonymization still to be applied.

Storage location: under a subdirectory of the utility's own per-user
configuration root, dedicated to user-supplied data. Inside that
directory, each immediate subdirectory is treated as a grouping
named for the subdirectory itself.

File shape: one or more newline-delimited JSON files per grouping,
each line a complete pre-normalized record.

## Discovery behavior

1. Walk the user-supplied-data root. For each direct child that is a
   directory and that contains one or more newline-delimited JSON
   files:
   - Count records by counting non-empty lines across all files.
   - Measure size as the sum of file byte sizes.
   - The grouping's canonical identifier is the subdirectory name.
   - The display label is an origin-prefixed form of the
     subdirectory name.

## Extraction behavior

For each line in each file:

1. Parse the JSON object.
2. Reject the record (with a warning) if any of the following
   minimal fields are absent: a record identifier, a model
   identifier, or a turns list. These three are non-negotiable
   because downstream consumers cannot use a record that lacks
   them.
3. Stamp the grouping label and origin category onto the record.
4. Walk every turn's text content and apply secret redaction
   followed by text anonymization. The remainder of the record
   is trusted as-is.

Because this category exists to ingest already-normalized records,
the extractor does not attempt to reshape or re-key anything
beyond the minimum listed above.

## Field derivation

All fields are taken verbatim from the record, with text
anonymization and secret redaction applied to text content only.

## Deduplication

No cross-record deduplication.

## Inherent limitations

- The extractor trusts that the user-supplied records conform to
  the normalized shape. If they do not, downstream consumers may
  fail.
- Only textual message content passes through redaction; complex
  tool-invocation payloads in user-supplied records are passed
  through as-is unless the user also placed them under keys that
  the redactor walks.
- This category has no schema-validation step beyond the
  minimum-fields check. Validation of the richer record shape is
  an implementation decision.
