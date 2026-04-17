# Sensitive Content Handling

The utility processes data that a user actively copy-pasted, commanded,
or instructed an assistant about. That corpus is dense with credentials,
personal identifiers, and internal strings. This document specifies the
utility's obligations around detecting, redacting, anonymizing, and
reporting on such content, and the guarantees — and limits of those
guarantees — the utility offers the user.

## Design philosophy

The utility applies defence in depth:

1. **Pattern-based credential redaction** runs over every string
   written to the output. This is automatic and unconditional.
2. **Identity anonymization** rewrites user-identifying strings into
   stable pseudonymous tokens. This is automatic and unconditional.
3. **User-supplied literal-string redaction** lets the user add
   arbitrary substrings to be replaced. This is configurable.
4. **A user-driven review gate** blocks publication until the user
   attests to having manually scanned a sample and interviewed
   (or self-interviewed) about sensitive entities the utility
   cannot see on its own. This is non-bypassable.

No layer is considered sufficient on its own. The utility's final
act before publication is always a human attestation; the automated
layers exist to make that attestation easy to complete without
finding something, not to make it unnecessary.

## Layer 1: credential and identifier pattern redaction

### Scope

Every textual value the utility writes to the output passes through
this layer. In nested structures the layer recurses into every
string value. In attached-content blocks, text values are redacted
but base64-encoded binary payloads are left intact.

### Categories detected

The implementation must detect — at minimum — strings matching the
following credential and identifier categories. For each category,
the implementation maintains a pattern or detector; patterns are an
implementation choice, but the categories are required.

| Category | Description |
|---|---|
| Standard bearer/JWT tokens | Multi-segment base64url payloads used as bearer tokens, including truncated forms carrying only the header. |
| Database connection strings | URLs for database-style protocols that embed a username and password in the URL. |
| Cloud-provider API keys | Provider-specific key formats issued by major model-hosting, cloud-computing, storage, and messaging platforms. The implementer selects which providers to cover; at minimum, covering the most common formats is expected. |
| Code-hosting platform tokens | Personal-access tokens, fine-grained tokens, and OAuth tokens issued by code-hosting platforms. |
| Package-registry tokens | Access tokens issued by common package registries. |
| Private-key blocks | Multi-line PEM-encoded private keys of any common algorithm. |
| CLI secret flags | Command-line invocations passing a secret as the value of a flag whose name resembles `--token`, `--api-key`, `--password`, or a close variant. |
| Environment-variable assignments | Assignments of the form `NAME=value` where `NAME` resembles a secret-like variable name. |
| Generic secret assignments | Key/value assignments (in JSON-like or language syntax) where the key name resembles a known secret key. |
| Password-preceded values | A value appearing after a "password"-like keyword and a separator, on the same line or the immediately-following line. |
| URL query-parameter secrets | URL query parameters named like `token`, `key`, `secret`, or a close variant. |
| Bearer-style authorization values | An `Authorization: Bearer <value>` pattern. |
| Chat-platform webhook URLs | Webhook URLs whose path carries an embedded secret. |
| Crypto-wallet private keys | Hex-prefixed fixed-length private keys for common blockchain wallet formats. |
| Non-private IPv4 addresses | Dotted-quad IPv4 addresses, excluding private ranges, loopback, broadcast, and well-known public DNS resolvers. |
| Email addresses | Any RFC-shaped email address. |
| High-entropy quoted strings | Quoted strings of sufficient length whose Shannon entropy exceeds a threshold and which mix upper/lower/digit characters. Used as a last-resort catch for anything the specific detectors missed. |

### Allowlist

A category detector may match against a benign string by accident —
for example, an email placeholder or a regex-pattern literal in
source code. The utility must maintain an allowlist that exempts
known-benign matches from redaction. Required allowlist entries at
a minimum include:

- Placeholder email addresses (e.g., `noreply@...`, `@example.com`,
  `@localhost`, and the email domain of the user's own assistant
  vendor when that domain carries reserved example accounts).
- Private-network IPv4 ranges (10.0.0.0/8, 172.16.0.0/12,
  192.168.0.0/16), loopback, broadcast, and widely-published
  public DNS resolver addresses.
- Pattern-source fragments the detectors themselves match against
  (a detector looking for `AKIA...` should not redact a regex
  literal containing `AKIA[`).
- Decorator-style annotations that look superficially like
  email addresses but are in fact language syntax (for
  example, test-framework fixture decorators).

The allowlist is an implementation-level artifact; users do not
edit it through the settings file. Users wanting to force-redact
a matching string add it to the literal-string redaction list.

### Replacement behavior

A matched range is replaced in place with a single, fixed
placeholder marker string. The marker is constant across the
entire utility and is distinctive enough to be recognized by
users reviewing the output. Implementers choose the exact
marker; this specification refers to it abstractly as "the
placeholder marker."

Overlapping matches are resolved by keeping the later-starting
match and dropping any earlier match that overlaps with it.
Replacements are applied from the end of the string to the
beginning so that positional indexes do not shift mid-rewrite.

### Opt-out for large binary payloads

If a string is over a size threshold (typical default 4096
bytes) and, after stripping ANSI control sequences, consists
entirely of base64-looking characters with no low-byte control
characters, the redactor leaves it unchanged. The same rule
applies to `data:` URLs with a `base64,` body. This prevents
rewriting the middle of an inline image or a large binary
blob and producing corrupt output.

### Counting

The redactor counts every replacement it makes and returns
the count to the extractor, which accumulates it into the
per-run summary.

## Layer 2: identity anonymization

### Scope

Every textual value the extractor writes passes through the
anonymizer. Path-shaped values (whose containing key signals a
path, or whose prefix or shape is a filesystem path) are
anonymized with extra care so that home-directory prefixes are
detected reliably.

### Targets

The anonymizer detects and replaces:

- **The local account identifier.** At construction time, the
  anonymizer determines the user's home directory path and
  extracts the basename as the account identifier. Occurrences
  of this identifier in text are replaced with a stable
  pseudonymous token.
- **The home directory path itself.** If the home directory
  follows a conventional shape (such as `/Users/name`,
  `/home/name`, or `C:\Users\name`), the anonymizer recognizes
  that shape and replaces the name portion; if the home
  directory is unconventional, the anonymizer builds a more
  specific pattern from the home directory itself and detects
  occurrences of that path prefix (including variants in which
  the path separator has been encoded as a different character
  by upstream tooling).
- **User-configured handles.** Any string in the
  `custom-handles` list is detected and replaced with its own
  stable pseudonymous token.

### Token shape

A replacement token is the concatenation of a short fixed
prefix (identifying this as a pseudonymous token) and a short
prefix of a cryptographic digest of the underlying identifier.
The token is deterministic: the same input always yields the
same output, so a consumer can join anonymous records from the
same user. It is not reversible: a consumer cannot recover
the original identifier from the token alone.

### Matching rules

- For identifiers four or more characters long, the matcher
  detects them at word boundaries that also exclude
  underscore-adjacent positions (because `\b`-style word
  boundaries are not ideal for this case), case-insensitively.
- For identifiers shorter than four characters — which would
  cause too many false positives if matched as bare substrings —
  the matcher only replaces occurrences inside recognized
  home-path prefixes.
- The anonymizer caches its compiled matchers per identifier.

### Literal-string list

The `custom-redactions` list (see `02-configuration-model.md`)
is applied by the anonymizer as a separate pass, replacing any
occurrence of each literal with the placeholder marker used by
the secret detector. Literals shorter than three characters
are ignored.

## Layer 3: user-driven review gate

### What the gate enforces

Publication is blocked until:

1. A local extract exists and its file is still present on disk.
2. A full-name scan has been run (or the user has explicitly
   declined to share a name for scanning) and the result has
   been recorded.
3. The user has supplied three free-form text attestations:
   (i) describing the full-name scan or the reason for
   declining, (ii) describing the sensitive-entity
   interview and its outcome, and (iii) describing the
   manual sample review, including a sample-count number.
4. Each attestation passes validation against the rules in
   `01-command-surface.md`.
5. A separate publication-approval attestation is supplied
   at publication time and passes its own validation.

These checks are re-run from scratch at publication time. An
extract attested in one run cannot be pushed by a different
run that relied only on a cached "attestation complete"
flag: the rules are re-validated against the attestation text
and the referenced file.

### Full-name scan

When the user supplies a name, the utility reads the export
file line-by-line and counts occurrences of the name in a
case-insensitive substring match. It records the count and a
small set of example lines (with an excerpt truncated to a
reasonable length) for the user's review. A non-zero match
count is not an automatic block: the utility surfaces the
count and the examples, and the user's attestation is what
confirms they reviewed and accepted the matches (or
re-ran with additional redactions).

### Manual sample count

The manual-review attestation must include an integer at or
above a configurable threshold. The default threshold is 15
records. The threshold exists to ensure the user has actually
spot-checked enough material to notice systemic issues; a
perfunctory "looked at one record" cannot pass. The threshold
may be raised by an implementer but is not expected to be
lowered.

### Sensitive-entity interview

The sensitive-entity attestation must mention at least one of
the named sensitive-entity categories (company name, client
name, internal project codename, private URL, custom domain,
internal tool name, third-party name) and must mention an
outcome — either that nothing was found, or that specific
redactions were added.

## Built-in scan reports

The `attest` capability runs a built-in scan over the export
file before asking for attestations. The scan produces:

- Occurrence counts for fixed patterns (emails, JWT-shaped
  tokens, code-hosting-platform token prefixes, IPv4-style
  addresses). These are shown to the user.
- A list of high-entropy quoted strings that look like leaked
  secrets, each with a small context excerpt so the user
  can assess whether to add it to the literal-redaction
  list. This list is kept short (typical limit of fifteen
  entries).

## Guarantees and non-guarantees

### Guarantees

- Every string written to the output has been through both
  the pattern-based redactor and the anonymizer.
- The local account identifier never appears in the output
  in its original form.
- The placeholder marker is distinctive and greppable, so
  users and downstream tooling can audit the density of
  redactions in any record.
- The review gate cannot be bypassed through settings
  edits alone: the attestation-text validators re-run at
  publication time, and failing them re-blocks the push.

### Non-guarantees

- No automated detector catches every secret. Secrets
  encoded in formats the detectors do not know, or
  embedded in larger prose, will pass through unmodified.
- The anonymizer catches the local account identifier but
  cannot know about every handle the user has used
  elsewhere. The user's configured-handle list is the only
  way to extend it.
- The utility does not detect general personally identifying
  information beyond emails, IP addresses, and the user-
  supplied full name. Names of third parties, internal
  project codenames, client names, physical addresses, and
  phone numbers are not detected; the user is responsible
  for adding them to the literal-redaction list.
- The review gate enforces only that the user made
  statements; it cannot verify that the statements are
  true. The user carries final responsibility for the
  contents of any publication.
