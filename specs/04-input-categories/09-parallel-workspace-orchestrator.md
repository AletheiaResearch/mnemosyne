# Input Category 9: Parallel-Workspace Orchestration Database

## Category profile

This category covers an orchestration utility that does not itself
run a language model. Instead, it provides workspace-management
infrastructure on top of one or more other coding-assistant products
the user already has installed. The orchestrator's job is to manage
parallel coding tasks against one or more git repositories, where
each task gets its own isolated workspace (typically a separate git
worktree, but any equivalent isolation mechanism is allowed by this
class), and each workspace runs a coding-assistant agent of the
user's choice.

Defining traits of products in this class:

- An orchestration utility that manages parallel coding tasks,
  each with its own git worktree (or equivalent isolated
  workspace), against one or more repositories the user has
  registered with it.
- For each task, the orchestrator launches a coding assistant —
  which may be any of several supported agent types, the user
  picks per-task — against the isolated workspace.
- The orchestrator persists its own state in a single embedded
  relational database (a single-file SQL store), typically
  located in the per-user application-data directory for the
  orchestrator product.
- The orchestrator's database tracks its own notion of a session
  (one conversation with the agent from within a specific
  workspace), and carries a pointer into the external agent's
  own session storage. The pointer is the agent's session
  identifier from whichever input category that agent belongs to.

This makes the orchestrator's database structurally a metadata-only
or hybrid source per the discussion in
`04-input-categories/00-common.md`: extractors reading the
orchestrator must decide whether to read the orchestrator's own
stored copy of the conversation messages, to treat the orchestrator
as metadata-only and follow the pointer into the external store, or
to combine both.

## Storage location

A single-file embedded relational database (SQL) under a per-user
application-data directory following the host operating system's
conventions for such data — typically an application-data
directory named after the orchestrator product. The exact directory
location varies by host. The implementer consults the platform's
conventional roots and descends into the orchestrator's subtree.

## Schema shape

The database carries at least four tables relevant to the
extractor. The structural elements below are described abstractly;
the implementer's chosen names for these tables and columns are not
constrained by this specification.

### Repositories table

Each row represents one repository the orchestrator manages.
Columns of interest to the extractor:

- A unique repository identifier.
- A remote-origin URL (a git-style URL referencing a code-hosting
  service).
- A short display name, typically the repository's leaf directory
  name.
- A default-branch label.
- An on-disk path to the repository's canonical checkout.
- Creation and last-updated timestamps.

Other columns present in such databases store orchestrator-specific
product features (notification settings, integration tokens, UI
state). These are not of interest to the extractor.

### Workspaces table

Each row represents one parallel task-workspace within a repository.
Columns of interest:

- A unique workspace identifier.
- A foreign key to the repositories table.
- A short directory-name label used both for the worktree directory
  and for display. In older versions of products in this class, this
  label may be a whimsical codename chosen at creation time —
  city names, animal names, and the like. Newer versions tend to
  use branch-derived or user-specified labels. The extractor should
  be tolerant of either form.
- A branch name (the branch the workspace is working on).
- A state label indicating whether the workspace is active,
  archived, or in some other lifecycle state.
- Creation and last-updated timestamps.
- In some versions of such products, a **deprecated codename
  column** may coexist with the newer label column. When both
  exist, the extractor prefers the newer column but falls back
  to the deprecated one when it is the only one populated.

Other columns store orchestrator-specific state such as pull-request
metadata, setup-script log paths, and unread-notification flags;
these are not of interest to the extractor.

### Sessions table

Each row represents one conversation with an agent, conducted from
within one workspace. Columns of interest:

- A unique session identifier (the orchestrator's own).
- A foreign key to the workspaces table.
- An agent-type label naming which external agent ran this
  session — typically one of several supported command-line coding
  assistants the orchestrator integrates with.
- An **external session identifier** column that points into the
  external agent's own session storage. Despite the orchestrator
  supporting multiple agent types, this column typically carries a
  single generic name historically derived from the
  first-supported agent's vocabulary. **The extractor must not
  assume the column name implies which agent's store it points
  into.** The column is used generically across all agent types;
  routing of lookups must be based on the agent-type column, not
  on the column name.
- A model identifier string.
- A short title, often a user-authored description of the task.
- Creation, last-updated, and last-user-message timestamps.

Other columns record orchestrator-specific controls (agent
personality, thinking-level setting, permission mode); not of
interest to the extractor.

### Session-messages table

Each row represents one turn within a session. Columns of interest:

- A unique message identifier.
- A foreign key to the sessions table.
- A role label (user or assistant).
- A textual content column carrying the message body in the
  orchestrator's preferred representation. This may be plain
  Markdown, plain prose, or an agent-specific serialization of the
  message; the orchestrator does not necessarily preserve the
  external agent's full structural representation.

Optional columns that may be present in some versions:

- A column carrying the full rich-message payload (for example a
  serialized version of the agent's structured turn). When
  present, this is more faithful than the textual content column
  alone.
- A per-message model identifier distinct from the session-level
  one (some agents change models mid-conversation).
- A timestamp distinct from the row's creation timestamp — for
  example a "sent at" timestamp recording when the message was
  actually exchanged versus when it was stored.
- A column tying the message to the external agent's own message
  identifier, useful for joining against the external store.

Not all optional columns are populated in every deployment. The
extractor must tolerate any of these being empty.

## Discovery behavior

1. Open the database in read-only mode.
2. Enumerate the rows of the repositories table.
3. For each repository, the canonical grouping is the **repository
   itself**. The grouping's canonical identifier is the repository
   identifier from the repositories table; the display label is the
   repository's display-name column, falling back to the leaf of the
   remote-origin URL when the display name is absent.
4. Count sessions per grouping by joining through the workspaces
   table to the sessions table. Size-per-grouping is estimated
   proportionally from the database file size divided by total
   session count, or using whatever weighting the implementer finds
   useful (the database does not expose per-row byte sizes
   directly).

> **Critical property: one grouping per repository.** Even when a
> repository has many parallel workspaces, all of those workspaces'
> sessions roll up under the single repository grouping. This is the
> key semantic difference between this category and extractors that
> operate against raw agent-store data: agent-store extractors see
> each workspace directory as its own grouping (because they have no
> notion that the workspaces are sibling worktrees of one
> repository), which over-fragments the dataset. The orchestrator
> extractor correctly sees that multiple workspaces all belong to
> the same logical project.

## Extraction behavior

The extractor must choose one of three strategies. The implementer
may support multiple strategies and let the user pick at extract
time; this specification requires the implementer to support **at
least Strategy A** as a baseline. Recommended default is Strategy C
when external agent storage is available; fall back to Strategy A
otherwise.

### Strategy A: metadata enrichment

The extractor produces no conversation records of its own. Instead,
it produces a mapping from external-session-identifier to enriched
metadata: repository name, workspace label, task title, and any
other useful project-level context.

A separate extractor pass — running against the appropriate
external agent's own storage (one of the other input categories) —
then consults this mapping. For each record whose source-side
session identifier matches an entry in the mapping, the extractor
overrides the grouping label on the record with the repository
name and may attach the task title as additional metadata.

Effectively, the orchestrator's database is used purely as a
project-labeling lookup. The advantage: the records themselves
remain at the fidelity the external agent stored them, which is
typically richer than the orchestrator's flattened copy. The
disadvantage: this strategy only works if the external agent's
storage is available and accessible.

### Strategy B: direct extraction from orchestrator tables

The extractor reads the session-messages table and assembles
conversation records directly from the orchestrator's own copy of
each turn. The grouping is naturally repository-level (because the
orchestrator knows which workspace each session belongs to), but
the message content is whatever the orchestrator chose to store —
which is typically a flattened version of what the external agent
actually exchanged with the user.

The advantage: works without reaching into any other input
category's storage at all. The disadvantage: lossy compared with
the agent's authoritative store.

### Strategy C: join-and-prefer

Combines Strategies A and B. The orchestrator's tables are used
to supply repository-level grouping and task-title metadata, and
to enumerate the universe of sessions to extract. For each session,
the extractor first attempts to read the message content from the
external agent's own storage (indexed via the external session
identifier from the orchestrator's sessions table). When the
external store has the referenced session, that content wins. When
it does not — for example because the external store has been
pruned, the agent type is one whose store the extractor does not
support, or the external session identifier is missing — the
extractor falls back to the orchestrator's own session-messages
table for that record.

This strategy yields the highest fidelity overall. It requires the
extractor to be able to reach into other input categories'
storage, which makes implementation more involved than either A
or B alone.

### Recommendation

- Default to Strategy C when external agent storage is
  available, since it combines the best content with the best
  grouping.
- Fall back to Strategy A as the minimum viable implementation
  for a release that focuses on correct grouping but does not
  yet pull from the orchestrator's content tables.
- Use Strategy B when the user has deleted or otherwise made
  inaccessible the external agent's own storage but still has
  the orchestrator's database.

## Field derivation

| Field | Derivation |
|---|---|
| Record identity | Prefer the orchestrator's session identifier under Strategy B; prefer the external session identifier under Strategies A and C, so that downstream deduplication can detect the same conversation surfacing through different paths. |
| Origin category | Under Strategies B and C, the orchestrator's own category label. Under Strategy A (metadata-only enrichment of records produced elsewhere), the origin category may remain that of the external agent and only the grouping is enriched. The implementer decides which convention to use; whichever is chosen must be applied consistently. |
| Grouping label | The repository's display name (never the workspace label). This is the key semantic difference from agent-store extractors and the principal value-add of this category. |
| Model | The sessions table's model column, falling back to the per-message model column from the session-messages table when the session-level one is empty. |
| Branch | The workspaces table's branch column. |
| Working directory | Not exposed directly by the orchestrator. Can be derived as the concatenation `<repository-root-path>/<workspace-directory-name>` when both are present, then anonymized via the path rule. If the repository root path is absent, the working-directory field is absent in the record. |
| Start / end timestamps | Earliest and latest timestamp from the session-messages table for this session, using whichever timestamp column is populated, in this order of preference: sent-at, then created-at. |
| Turns | Strategy A: no turns (this is the metadata-only case; the enrichment applies to records extracted by other input categories). Strategy B: assembled from session-messages rows in order, role-dispatched. Strategy C: turns taken from the external agent's storage when possible; fallen back to session-messages rows when the external store does not have the session. |
| Usage tally | Computed per the normal rules in `03-output-format.md`. In strategies that read from external storage, the external store's usage counters are authoritative because they reflect the real exchange the agent observed. |

## Deduplication

In Strategy C, the same external session may appear in both the
orchestrator's database and the external agent's own storage. If
both extractor passes produce a record for it, two records will
exist in the output for the same conversation.

The extractor must deduplicate such records so that no two records
share the same external session identifier. Where a tiebreak is
needed, prefer the record produced via the orchestrator-backed path
because it carries the richer project grouping (repository name)
even when the message content was sourced from the external store.

## Handling of the generic-name external-identifier column

As noted under "Sessions table" above, the external-session-
identifier column may be named in a way that suggests it
corresponds to one specific agent type — historical naming from
when the orchestrator supported only that one agent. In the wild,
this same column is used generically for all agent types the
orchestrator now supports.

The extractor must route lookups based on the sessions table's
agent-type column, not on the name of the external-identifier
column. A misled extractor that always treats the identifier as
belonging to the historically-first agent type will mis-route
lookups whenever the user has run a non-default agent.

## Inherent limitations

- Repositories that exist in the orchestrator's database but have
  zero sessions (registered but never used) produce groupings with
  empty member lists. The extractor should skip such groupings
  silently rather than emitting empty grouping entries.
- Some session rows may have no external session identifier at all
  — typically because the session was created in the orchestrator's
  UI but no actual conversation took place. Such sessions are
  skipped.
- The orchestrator's own message content is often a truncated or
  reformatted version of the external agent's rich representation.
  Strategy B records will therefore be lower-fidelity than records
  for the same sessions extracted directly from the external store.
- Workspace-level metadata (feature-branch identity, pull-request
  titles, setup logs, and similar) is available in the
  orchestrator's workspaces table but is not preserved in the
  normalized output's standard fields. Implementers may choose to
  surface such metadata as optional supplementary fields; this
  specification does not require it.
- Some deployments have schema-version drift: older databases lack
  newer optional columns; newer databases may have columns the
  extractor does not yet know about. The extractor must tolerate
  both directions per the universal rules in
  `04-input-categories/00-common.md`.
