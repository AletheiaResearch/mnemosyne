// Package cursor extracts Cursor coding-assistant conversations from Cursor's
// on-disk stores. Cursor keeps the same conversation in two places:
//
//   - agent-transcripts JSONL:  ~/.cursor/projects/<project-dir>/agent-transcripts/<sid>/<sid>.jsonl
//     (also flat <sid>.jsonl). The <project-dir> name decodes to the workspace path.
//     Transcripts carry assistant prose and tool-call inputs but no reasoning, tool
//     results, tokens, model, or timestamps. Sub-agent conversations live alongside
//     under <parent-sid>/subagents/<sub-sid>.jsonl.
//   - global state:             ~/Library/Application Support/Cursor/User/globalStorage/state.vscdb
//     table cursorDiskKV, keys composerData:<sid> (header order + model/title/createdAt,
//     plus subagentInfo on sub-agent sessions) and bubbleId:<sid>:<bid> (per-turn text,
//     thinking, toolFormerData with results, and tokenCount). This store is the richer
//     of the two.
//
// Where a session exists in both stores (the common case), the records are merged
// into one: the global-state bubbles are the content/metrics backbone (they alone
// carry reasoning, tool results, tokens, model, and timestamps) and the transcript
// supplies the authoritative workspace path. Sessions present in only one store are
// read from whichever store has them. A session is never emitted twice. Sub-agent
// conversations are emitted as their own records, tagged via Provenance.SourceOrigin
// ("cursor-subagent") with a link back to the parent session.
package cursor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	dbPath       string // state.vscdb (global state)
	projectsRoot string // ~/.cursor/projects (agent-transcripts)

	mu    sync.Mutex
	cache map[string]*sessionEntry // memoized session index for one extraction pass
}

func New(dbPath string) *Source {
	if dbPath == "" {
		dbPath = defaultDBPath()
	}
	return &Source{
		dbPath:       dbPath,
		projectsRoot: defaultProjectsRoot(),
	}
}

func (s *Source) Name() string {
	return "cursor"
}

// sessionEntry is one logical Cursor conversation, unified across both stores.
type sessionEntry struct {
	id             string
	workspace      string // resolved filesystem path, or "" when unattributed
	transcriptPath string // set when an agent-transcripts JSONL exists
	hasComposer    bool   // set when a non-empty composerData:<id> exists
	isSubagent     bool   // set when the session is a Cursor sub-agent conversation
	parentID       string // parent session id for a sub-agent, when known
}

func (e *sessionEntry) hasContent() bool {
	return e.hasComposer || e.transcriptPath != ""
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	index := s.index()

	type aggregate struct {
		records int
		bytes   int64
		label   string
	}
	grouped := make(map[string]*aggregate)
	for _, entry := range index {
		if !entry.hasContent() {
			continue
		}
		gid := groupingID(entry.workspace)
		agg := grouped[gid]
		if agg == nil {
			agg = &aggregate{label: displayLabel(entry.workspace)}
			grouped[gid] = agg
		}
		agg.records++
		if entry.transcriptPath != "" {
			if info, err := os.Stat(entry.transcriptPath); err == nil {
				agg.bytes += info.Size()
			}
		}
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for gid, agg := range grouped {
		groupings = append(groupings, source.Grouping{
			ID:               gid,
			DisplayLabel:     agg.label,
			Origin:           s.Name(),
			EstimatedRecords: agg.records,
			EstimatedBytes:   agg.bytes,
		})
	}
	sort.Slice(groupings, func(i, j int) bool { return groupings[i].ID < groupings[j].ID })
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, extractCtx source.ExtractionContext, emit func(schema.Record) error) error {
	index := s.index()

	db, dbErr := source.OpenSQLite(s.dbPath)
	if dbErr == nil {
		defer db.Close()
	}

	for _, id := range sortedIDs(index) {
		entry := index[id]
		if !entry.hasContent() || groupingID(entry.workspace) != grouping.ID {
			continue
		}
		record, ok := s.buildRecord(db, entry)
		if !ok {
			source.ReportWarning(extractCtx, "cursor: session %s in %s yielded no turns", id, grouping.DisplayLabel)
			continue
		}
		if entry.workspace == "" {
			source.ReportWarning(extractCtx, "cursor: session %s emitted with no resolved workspace", id)
		}
		// buildRecord already set Grouping to the same DisplayLabel (groupingID
		// matched above), so no re-assignment is needed here.
		if extractCtx.SuppressReasoning {
			suppressReasoning(&record)
		}
		if err := emit(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) LookupSession(_ context.Context, sessionID string) (schema.Record, bool, error) {
	entry := s.index()[sessionID]
	if entry == nil || !entry.hasContent() {
		return schema.Record{}, false, nil
	}
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return schema.Record{}, false, err
	}
	if err == nil {
		defer db.Close()
	}
	record, ok := s.buildRecord(db, entry)
	if !ok {
		return schema.Record{}, false, nil
	}
	return record, true, nil
}

// index memoizes the unified session index for the lifetime of this Source so
// that Discover, the per-grouping Extract calls, and repeated LookupSession
// calls share one scan rather than re-walking the projects tree and re-decoding
// every composerData blob each time. mnemosyne runs one extraction pass per
// process, so a snapshot taken on first use is the desired semantics.
func (s *Source) index() map[string]*sessionEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil {
		s.cache = s.buildIndex()
	}
	return s.cache
}

// buildIndex unifies every session across the transcript and global-state stores
// keyed by session id. Transcripts win on workspace attribution because their
// project-dir name decodes to a real filesystem path; bubbles only attribute a
// workspace indirectly. It is best-effort: a missing store contributes nothing.
func (s *Source) buildIndex() map[string]*sessionEntry {
	entries := make(map[string]*sessionEntry)

	for id, info := range s.scanTranscripts() {
		entries[id] = &sessionEntry{
			id:             id,
			workspace:      info.workspace,
			transcriptPath: info.path,
			isSubagent:     info.isSubagent,
			parentID:       info.parentID,
		}
	}

	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return entries
	}
	defer db.Close()

	rows, err := db.Query(`select key, value from cursorDiskKV where key like 'composerData:%'`)
	if err != nil {
		return entries
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		composer, err := source.DecodeJSONObject(value)
		if err != nil {
			continue
		}
		if len(composerHeaderIDs(composer)) == 0 {
			continue // empty draft / sub-agent shell — no real conversation
		}
		id := strings.TrimPrefix(key, "composerData:")
		entry := entries[id]
		if entry == nil {
			entry = &sessionEntry{id: id}
			entries[id] = entry
		}
		entry.hasComposer = true
		// subagentInfo is the authoritative sub-agent marker; it also covers the
		// rare id collision where a sub-agent id equals an unrelated top-level
		// transcript basename, and carries the parent composer id for sub-agents
		// that have no transcript to derive it from.
		if subagentInfo := source.ExtractMap(composer, "subagentInfo"); subagentInfo != nil {
			entry.isSubagent = true
			if entry.parentID == "" {
				entry.parentID = source.ExtractString(subagentInfo, "parentComposerId")
			}
		}
		if entry.workspace == "" {
			entry.workspace = s.composerWorkspace(db, id, composer)
		}
	}
	return entries
}

// buildRecord assembles the merged record for one session. The global-state
// bubbles are the backbone whenever present; the transcript is the fallback,
// including when a composer's bubble rows have been garbage-collected and only
// its header list survives.
func (s *Source) buildRecord(db *sql.DB, entry *sessionEntry) (schema.Record, bool) {
	var record schema.Record
	usedTranscriptFallback := false
	switch {
	case entry.hasComposer && db != nil:
		record = s.recordFromComposer(db, entry.id)
		if len(record.Turns) == 0 && entry.transcriptPath != "" {
			// Bubble rows were garbage-collected but the composer header (and the
			// title/model/createdAt it carries) survived. Recover the turns from
			// the transcript while keeping the composer-derived metadata rather
			// than discarding the whole record.
			transcript := s.recordFromTranscript(entry.id, entry.transcriptPath)
			record.Turns = transcript.Turns
			record.EndedAt = source.LatestTimestamp(record.EndedAt, transcript.EndedAt)
			if record.StartedAt == "" {
				record.StartedAt = transcript.EndedAt
			}
			finalizeUsage(&record)
			usedTranscriptFallback = true
		}
	case entry.transcriptPath != "":
		record = s.recordFromTranscript(entry.id, entry.transcriptPath)
	default:
		return schema.Record{}, false
	}
	if len(record.Turns) == 0 {
		return schema.Record{}, false
	}

	record.WorkingDir = entry.workspace
	record.Grouping = displayLabel(entry.workspace)
	// SourcePath points at the store the turns actually came from; on a fallback
	// the turns are the transcript's even though the composer supplied metadata.
	sourcePath := s.dbPath
	if !entry.hasComposer || usedTranscriptFallback {
		sourcePath = entry.transcriptPath
	}
	record.Provenance = &schema.Provenance{
		SourceID:     entry.id,
		SourceOrigin: s.Name(),
		SourcePath:   sourcePath,
	}

	extensions := make(map[string]any)
	if entry.hasComposer && entry.transcriptPath != "" {
		extensions["stores"] = []string{"global-state", "transcript"}
	}
	if entry.isSubagent {
		record.Provenance.SourceOrigin = "cursor-subagent"
		subagent := map[string]any{"is_subagent": true}
		if entry.parentID != "" {
			subagent["parent_session_id"] = entry.parentID
		}
		extensions["subagent"] = subagent
	}
	if len(extensions) > 0 {
		record.Provenance.Extensions = extensions
	}
	return record, true
}

// recordFromComposer assembles a record from composerData + bubbleId rows.
func (s *Source) recordFromComposer(db *sql.DB, composerID string) schema.Record {
	var value []byte
	if err := db.QueryRow(`select value from cursorDiskKV where key = ?`, "composerData:"+composerID).Scan(&value); err != nil {
		return schema.Record{}
	}
	composer, err := source.DecodeJSONObject(value)
	if err != nil {
		return schema.Record{}
	}

	record := schema.Record{
		RecordID: composerID,
		Origin:   s.Name(),
		Model:    "cursor/unknown",
		Title:    source.ExtractString(composer, "name"),
		Turns:    make([]schema.Turn, 0),
	}
	if modelConfig := source.ExtractMap(composer, "modelConfig"); modelConfig != nil {
		record.Model = source.FirstNonEmpty(
			source.ExtractString(modelConfig, "modelName"),
			selectedModelID(modelConfig),
			record.Model,
		)
	}
	record.StartedAt = source.NormalizeTimestamp(composer["createdAt"])

	for _, bubbleID := range composerHeaderIDs(composer) {
		bubble, err := s.loadBubble(db, composerID, bubbleID)
		if err != nil {
			continue
		}
		// Token snapshots accrue on every bubble, including ones whose turn is
		// dropped for having no renderable content.
		if tokenCount := source.ExtractMap(bubble, "tokenCount"); tokenCount != nil {
			record.Usage.InputTokens += source.IntNumber(tokenCount["inputTokens"])
			record.Usage.OutputTokens += source.IntNumber(tokenCount["outputTokens"])
		}
		turn, ok := bubbleToTurn(bubble)
		if !ok {
			continue
		}
		record.Turns = append(record.Turns, turn)
		record.StartedAt = source.EarliestTimestamp(record.StartedAt, turn.Timestamp)
		record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
	}
	record.EndedAt = source.LatestTimestamp(
		record.EndedAt,
		source.NormalizeTimestamp(composer["lastUpdatedAt"]),
		source.NormalizeTimestamp(composer["conversationCheckpointLastUpdatedAt"]),
	)
	// A session should never advertise an end without a start.
	if record.StartedAt == "" {
		record.StartedAt = record.EndedAt
	}

	finalizeUsage(&record)
	return record
}

// recordFromTranscript assembles a record from an agent-transcripts JSONL file.
// Transcripts have no per-line timestamps; the file mtime is used as a coarse
// EndedAt only (StartedAt is left unknown rather than fabricating a
// zero-duration session).
func (s *Source) recordFromTranscript(sessionID, path string) schema.Record {
	record := schema.Record{
		RecordID: sessionID,
		Origin:   s.Name(),
		Model:    "cursor/unknown",
		Turns:    make([]schema.Turn, 0),
	}
	_ = source.ReadJSONLines(path, func(_ int, raw []byte) error {
		line, err := source.DecodeJSONObject(raw)
		if err != nil {
			return nil
		}
		role := normalizeTranscriptRole(source.ExtractString(line, "role"))
		if role == "" {
			return nil
		}
		message := source.ExtractMap(line, "message")
		turn := schema.Turn{Role: role}
		for _, item := range source.ExtractSlice(message, "content") {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch source.ExtractString(block, "type") {
			case "text":
				if text := source.ExtractString(block, "text"); text != "" {
					turn.Text = appendText(turn.Text, text)
				}
			case "thinking", "reasoning":
				thinking := source.FirstNonEmpty(source.ExtractString(block, "thinking"), source.ExtractString(block, "text"))
				if thinking != "" {
					turn.Reasoning = appendText(turn.Reasoning, thinking)
				}
			case "tool_use":
				turn.ToolCalls = append(turn.ToolCalls, schema.ToolCall{
					Tool:  normalizeCursorToolName(source.ExtractString(block, "name")),
					Input: block["input"],
				})
			}
		}
		if turn.Text == "" && turn.Reasoning == "" && len(turn.ToolCalls) == 0 {
			return nil
		}
		record.Turns = append(record.Turns, turn)
		return nil
	})

	record.EndedAt = fileTimestamp(path)
	finalizeUsage(&record)
	return record
}

// bubbleToTurn converts a single bubbleId payload into a turn. Cursor splits one
// logical assistant message across several bubbles (one per thinking block, prose
// chunk, or tool call), so each bubble maps to at most one turn.
func bubbleToTurn(bubble map[string]any) (schema.Turn, bool) {
	role := bubbleRole(source.IntNumber(bubble["type"]))
	if role == "" {
		return schema.Turn{}, false
	}
	turn := schema.Turn{
		Role:      role,
		Timestamp: source.NormalizeTimestamp(bubble["createdAt"]),
		Text:      source.ExtractString(bubble, "text"),
	}
	if role == "assistant" {
		if thinking := source.ExtractMap(bubble, "thinking"); thinking != nil {
			turn.Reasoning = source.ExtractString(thinking, "text")
		}
		if tool := source.ExtractMap(bubble, "toolFormerData"); tool != nil {
			turn.ToolCalls = append(turn.ToolCalls, toolCallFromFormer(tool))
		}
	}
	if turn.Text == "" && turn.Reasoning == "" && len(turn.ToolCalls) == 0 {
		return schema.Turn{}, false
	}
	return turn, true
}

func toolCallFromFormer(tool map[string]any) schema.ToolCall {
	params := normalizeCursorPayload(tool["params"])
	if nested, ok := params.(map[string]any); ok {
		params = unwrapCursorToolParameters(nested)
	}
	result := normalizeCursorPayload(tool["result"])
	call := schema.ToolCall{
		Tool:   normalizeCursorToolName(source.ExtractString(tool, "name")),
		Input:  params,
		Status: normalizeCursorStatus(tool["status"]),
	}
	if result != nil {
		call.Output = &schema.ToolOutput{Raw: result}
		if text, ok := result.(string); ok {
			call.Output.Text = text
		}
	}
	return call
}

func (s *Source) loadBubble(db *sql.DB, composerID, bubbleID string) (map[string]any, error) {
	key := fmt.Sprintf("bubbleId:%s:%s", composerID, bubbleID)
	var raw []byte
	if err := db.QueryRow(`select value from cursorDiskKV where key = ?`, key).Scan(&raw); err != nil {
		return nil, err
	}
	return source.DecodeJSONObject(raw)
}

// composerWorkspace attributes a workspace to a composer that has no transcript.
// It prefers the composer's own workspaceIdentifier, then falls back to the
// workspaceUris stamped on its bubbles. Unlike the previous implementation it
// scans every header bubble rather than capping at the first five.
func (s *Source) composerWorkspace(db *sql.DB, composerID string, composer map[string]any) string {
	if wi := source.ExtractMap(composer, "workspaceIdentifier"); wi != nil {
		if uri := source.ExtractString(wi, "uri"); uri != "" {
			return uriToPath(uri)
		}
	}
	for _, bubbleID := range composerHeaderIDs(composer) {
		bubble, err := s.loadBubble(db, composerID, bubbleID)
		if err != nil {
			continue
		}
		for _, item := range source.ExtractSlice(bubble, "workspaceUris") {
			if uri, ok := item.(string); ok && uri != "" {
				return uriToPath(uri)
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Transcript discovery + project-dir decoding
// ---------------------------------------------------------------------------

type transcriptInfo struct {
	path       string
	workspace  string
	isSubagent bool
	parentID   string
}

func (s *Source) scanTranscripts() map[string]transcriptInfo {
	out := make(map[string]transcriptInfo)
	if !source.DirExists(s.projectsRoot) {
		return out
	}
	projectDirs, err := os.ReadDir(s.projectsRoot)
	if err != nil {
		return out
	}
	for _, projectDir := range projectDirs {
		if !projectDir.IsDir() {
			continue
		}
		transcriptsDir := filepath.Join(s.projectsRoot, projectDir.Name(), "agent-transcripts")
		if !source.DirExists(transcriptsDir) {
			continue
		}
		workspace := decodeProjectDir(projectDir.Name())
		for id, raw := range collectTranscripts(transcriptsDir) {
			// Same non-sub-agent preference as collectTranscripts, applied across
			// project dirs for the rare cross-project basename collision.
			if existing, ok := out[id]; ok && !(existing.isSubagent && !raw.isSubagent) {
				continue
			}
			out[id] = transcriptInfo{
				path:       raw.path,
				workspace:  workspace,
				isSubagent: raw.isSubagent,
				parentID:   raw.parentID,
			}
		}
	}
	return out
}

type rawTranscript struct {
	path       string
	isSubagent bool
	parentID   string
}

// collectTranscripts returns id->transcript for every transcript JSONL under an
// agent-transcripts directory, keyed by the file's own basename (the id that
// joins composerData). Cursor uses several layouts — flat (<id>.jsonl), nested
// (<id>/<id>.jsonl), and sub-agent conversations at
// <parent>/subagents/<sub>.jsonl — so the tree is walked recursively and each
// file is classified by whether a "subagents" segment appears in its path.
func collectTranscripts(transcriptsDir string) map[string]rawTranscript {
	out := make(map[string]rawTranscript)
	files, err := source.CollectFiles(transcriptsDir, func(path string, _ fs.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl"
	})
	if err != nil {
		return out
	}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		parentID, isSubagent := classifySubagentTranscript(transcriptsDir, path)
		// On a basename collision prefer a top-level transcript over a sub-agent
		// one so a real top-level session is never mis-tagged as a sub-agent.
		if existing, ok := out[id]; ok && !(existing.isSubagent && !isSubagent) {
			continue
		}
		out[id] = rawTranscript{path: path, isSubagent: isSubagent, parentID: parentID}
	}
	return out
}

// classifySubagentTranscript reports whether a transcript path is a sub-agent
// conversation (lives under a "subagents" directory) and, if so, the parent
// session id (the path segment immediately above "subagents").
func classifySubagentTranscript(transcriptsDir, path string) (string, bool) {
	rel, err := filepath.Rel(transcriptsDir, path)
	if err != nil {
		return "", false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		if parts[i] == "subagents" {
			return parts[i-1], true
		}
	}
	return "", false
}

// decodeProjectDir turns Cursor's dash-encoded project-dir name back into a
// filesystem path. Directory names can legitimately contain dashes (e.g.
// "vibe-replay"), so the segments are resolved greedily against the real
// filesystem; when no on-disk match exists every dash becomes a separator.
// Non-path window sentinels (e.g. "empty-window", numeric window ids) decode to
// the empty string so they bucket under cursor:unknown instead of a bogus path.
func decodeProjectDir(encoded string) string {
	if encoded == "" {
		return ""
	}
	parts := strings.Split(encoded, "-")
	start := 0
	if parts[0] == "" {
		start = 1
	}
	if resolved := resolveEncodedParts(parts, start, string(filepath.Separator)); resolved != "" {
		return resolved
	}
	// On-disk resolution failed. Non-path window sentinels (empty-window, numeric
	// window ids) have no real directory, so decode to "" — they bucket under
	// cursor:unknown instead of a fabricated path. The check runs only after
	// resolution so a real directory whose name happens to be all-numeric still
	// decodes correctly.
	if isSentinelProjectDir(encoded) {
		return ""
	}
	return string(filepath.Separator) + strings.ReplaceAll(strings.TrimPrefix(encoded, "-"), "-", string(filepath.Separator))
}

// isSentinelProjectDir matches Cursor project-dir names that are not paths: the
// "empty-window" no-folder sentinel and purely numeric window ids.
func isSentinelProjectDir(name string) bool {
	if name == "empty-window" {
		return true
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return name != ""
}

func resolveEncodedParts(parts []string, idx int, current string) string {
	if idx >= len(parts) {
		if source.DirExists(current) {
			return current
		}
		return ""
	}
	entries, err := os.ReadDir(current)
	if err != nil || len(entries) == 0 {
		return ""
	}
	dirNames := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			dirNames[entry.Name()] = true
		}
	}
	for end := idx + 1; end <= len(parts); end++ {
		candidate := strings.Join(parts[idx:end], "-")
		if !dirNames[candidate] {
			continue
		}
		if resolved := resolveEncodedParts(parts, end, filepath.Join(current, candidate)); resolved != "" {
			return resolved
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func composerHeaderIDs(composer map[string]any) []string {
	for _, key := range []string{"fullConversationHeadersOnly", "conversation"} {
		items := source.ExtractSlice(composer, key)
		if len(items) == 0 {
			continue
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			header, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if bubbleID := source.ExtractString(header, "bubbleId"); bubbleID != "" {
				out = append(out, bubbleID)
			}
		}
		return out
	}
	return nil
}

func selectedModelID(modelConfig map[string]any) string {
	for _, item := range source.ExtractSlice(modelConfig, "selectedModels") {
		if model, ok := item.(map[string]any); ok {
			if id := source.ExtractString(model, "modelId"); id != "" {
				return id
			}
		}
	}
	return ""
}

func bubbleRole(bubbleType int) string {
	switch bubbleType {
	case 1:
		return "user"
	case 2:
		return "assistant"
	default:
		return ""
	}
}

func normalizeTranscriptRole(role string) string {
	switch role {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return ""
	}
}

func appendText(existing, addition string) string {
	if existing == "" {
		return addition
	}
	return existing + "\n" + addition
}

func finalizeUsage(record *schema.Record) {
	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)
}

func suppressReasoning(record *schema.Record) {
	for i := range record.Turns {
		record.Turns[i].Reasoning = ""
	}
}

func sortedIDs(index map[string]*sessionEntry) []string {
	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func uriToPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

func fileTimestamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return source.NormalizeTimestamp(float64(info.ModTime().UnixMilli()))
}

// groupingID is the stable Grouping.ID for a workspace — the raw path, or the
// shared "cursor:unknown" bucket when a workspace could not be resolved.
func groupingID(workspace string) string {
	if workspace == "" {
		return source.EstimateUnknownLabel("cursor")
	}
	return workspace
}

// displayLabel is the human-readable Grouping.DisplayLabel. It applies the
// "cursor:" prefix exactly once (the previous implementation double-prefixed
// unknown groupings as "cursor:cursor:unknown").
func displayLabel(workspace string) string {
	if workspace == "" {
		return source.EstimateUnknownLabel("cursor")
	}
	return "cursor:" + source.DisplayLabelFromPath(workspace)
}

func normalizeCursorPayload(value any) any {
	switch typed := value.(type) {
	case string:
		return source.JSONString(typed)
	default:
		return typed
	}
}

func unwrapCursorToolParameters(payload map[string]any) any {
	tools := source.ExtractSlice(payload, "tools")
	if len(tools) == 0 {
		return payload
	}
	first, ok := tools[0].(map[string]any)
	if !ok {
		return payload
	}
	parameters := first["parameters"]
	if parameters == nil {
		return payload
	}
	return parameters
}

func normalizeCursorToolName(name string) string {
	switch {
	case strings.HasPrefix(name, "mcp_"):
		parts := strings.Split(name, "_")
		return parts[len(parts)-1]
	case strings.HasPrefix(name, "mcp-"):
		parts := strings.Split(name, "-")
		return parts[len(parts)-1]
	default:
		return name
	}
}

func normalizeCursorStatus(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		return source.ExtractString(typed, "status", "state")
	default:
		return ""
	}
}

func defaultProjectsRoot() string {
	return source.Expand("~/.cursor/projects")
}

func defaultDBPath() string {
	home := source.HomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User", "globalStorage", "state.vscdb")
	default:
		return filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "state.vscdb")
	}
}
