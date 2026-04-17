# Configuration

Configuration is stored as JSON in the per-user config directory.

- `destination_repo`: target dataset repository identifier.
- `origin_scope`: one source category or `all`.
- `excluded_groupings`: exact grouping labels to skip.
- `custom_redactions`: literal strings replaced by the placeholder marker.
- `custom_handles`: short identity-bearing handles to anonymize.
- `scope_confirmed`: whether the user approved the grouping list.
- `phase_marker`: one of `initial`, `preparing`, `pending-review`, `cleared`, `finalized`.
- `last_extract`: timestamp, record count, scope, and last output path.
- `reviewer_statements`: saved attestation text for identity scan, entity interview, and manual review.
- `verification_record`: parsed full-name-scan and manual-sample data.
- `last_attest`: summary of the last attested file.
- `publication_attestation`: latest publish-approval text.

Unknown keys are preserved on save.
