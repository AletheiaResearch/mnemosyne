package orchestrator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	"github.com/AletheiaResearch/mnemosyne/internal/source/claudecode"
	"github.com/AletheiaResearch/mnemosyne/internal/source/codex"
	"github.com/AletheiaResearch/mnemosyne/internal/source/cursor"
	"github.com/AletheiaResearch/mnemosyne/internal/source/gemini"
	"github.com/AletheiaResearch/mnemosyne/internal/source/kimi"
	"github.com/AletheiaResearch/mnemosyne/internal/source/openclaw"
	"github.com/AletheiaResearch/mnemosyne/internal/source/opencode"
)

type Source struct {
	dbPath  string
	lookups map[string]source.SessionLookup
}

// dbSchema is the resolved column-and-table map for a specific orchestrator
// SQLite file. The orchestrator schema has drifted across releases — tables
// get renamed (repos → repositories), columns get aliased (name →
// display_name), and some columns only appear in newer versions. detectSchema
// populates this struct once per Open so the rest of the extractor can read
// fields without caring which historical variant it is touching.
type dbSchema struct {
	reposTable           string
	workspacesTable      string
	sessionsTable        string
	sessionMessagesTable string

	repoID     string
	repoName   string
	repoRemote string
	repoPath   string

	workspaceID       string
	workspaceRepoID   string
	workspaceLabel    string
	workspaceCodename string
	workspaceBranch   string

	sessionID          string
	sessionWorkspaceID string
	sessionAgentType   string
	sessionExternalID  string
	sessionModel       string
	sessionTitle       string

	messageSessionID string
	messageRole      string
	messageContent   string
	messagePayload   string
	messageModel     string
	messageSentAt    string
	messageCreatedAt string
}

func New(dbPath string) *Source {
	return newSource(dbPath, nil)
}

func newSource(dbPath string, lookups map[string]source.SessionLookup) *Source {
	if dbPath == "" {
		dbPath = detectDefaultDB()
	}
	if lookups == nil {
		lookups = defaultLookups()
	}
	return &Source{dbPath: dbPath, lookups: lookups}
}

func (s *Source) Name() string {
	return "orchestrator"
}

func defaultLookups() map[string]source.SessionLookup {
	return map[string]source.SessionLookup{
		"claudecode": claudecode.New(""),
		"codex":      codex.New("", ""),
		"cursor":     cursor.New(""),
		"gemini":     gemini.New(""),
		"kimi":       kimi.New(""),
		"openclaw":   openclaw.New(""),
		"opencode":   opencode.New(""),
	}
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	db, schemaInfo, err := s.open()
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	query := `select r.%s, coalesce(nullif(r.%s, ''), r.%s), count(s.%s)
from %s r
join %s w on w.%s = r.%s
join %s s on s.%s = w.%s
group by r.%s, r.%s, r.%s`
	rows, err := db.Query(sprintf(query,
		schemaInfo.repoID, schemaInfo.repoName, schemaInfo.repoRemote, schemaInfo.sessionID,
		schemaInfo.reposTable,
		schemaInfo.workspacesTable, schemaInfo.workspaceRepoID, schemaInfo.repoID,
		schemaInfo.sessionsTable, schemaInfo.sessionWorkspaceID, schemaInfo.workspaceID,
		schemaInfo.repoID, schemaInfo.repoName, schemaInfo.repoRemote,
	))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var fileSize int64
	if info, err := os.Stat(s.dbPath); err == nil {
		fileSize = info.Size()
	}
	totalSessions := 0
	type row struct {
		id    string
		label string
		count int
	}
	rowsOut := make([]row, 0)
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.label, &item.count); err != nil {
			continue
		}
		if item.count == 0 {
			continue
		}
		item.label = repoDisplayName(item.label)
		rowsOut = append(rowsOut, item)
		totalSessions += item.count
	}

	groupings := make([]source.Grouping, 0, len(rowsOut))
	for _, item := range rowsOut {
		size := int64(0)
		if totalSessions > 0 {
			size = fileSize / int64(totalSessions) * int64(item.count)
		}
		groupings = append(groupings, source.Grouping{
			ID:               item.id,
			DisplayLabel:     "orchestrator:" + item.label,
			Origin:           s.Name(),
			EstimatedRecords: item.count,
			EstimatedBytes:   size,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	db, schemaInfo, err := s.open()
	if err != nil {
		return nil
	}
	defer db.Close()

	query := `select s.%s, s.%s, s.%s, s.%s, s.%s, w.%s, w.%s, w.%s, r.%s, coalesce(nullif(r.%s, ''), r.%s)
from %s s
join %s w on s.%s = w.%s
join %s r on w.%s = r.%s
where r.%s = ?`
	rows, err := db.Query(sprintf(query,
		schemaInfo.sessionID, schemaInfo.sessionAgentType, schemaInfo.sessionExternalID, schemaInfo.sessionModel, schemaInfo.sessionTitle,
		schemaInfo.workspaceLabel, schemaInfo.workspaceCodename, schemaInfo.workspaceBranch,
		schemaInfo.repoPath, schemaInfo.repoName, schemaInfo.repoRemote,
		schemaInfo.sessionsTable,
		schemaInfo.workspacesTable, schemaInfo.sessionWorkspaceID, schemaInfo.workspaceID,
		schemaInfo.reposTable, schemaInfo.workspaceRepoID, schemaInfo.repoID,
		schemaInfo.repoID,
	), grouping.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var sessionID, agentType, externalID, model, title, workspaceLabel, codename, branch, repoPath, repoLabel string
		if err := rows.Scan(&sessionID, &agentType, &externalID, &model, &title, &workspaceLabel, &codename, &branch, &repoPath, &repoLabel); err != nil {
			continue
		}
		label := firstNonEmpty(workspaceLabel, codename)
		record, err := s.extractSession(ctx, db, schemaInfo, sessionID, repoLabel, repoPath, label, branch, model, title, agentType, externalID)
		if err != nil || len(record.Turns) == 0 {
			continue
		}
		record.Grouping = grouping.DisplayLabel
		if err := emit(record); err != nil {
			return err
		}
	}
	return nil
}

func (s *Source) extractSession(ctx context.Context, db *sql.DB, schemaInfo dbSchema, sessionID, repoLabel, repoPath, workspaceLabel, branch, model, title, agentType, externalID string) (schema.Record, error) {
	workingDir := derivedWorkingDir(repoPath, workspaceLabel)
	lookup := s.lookups[normalizeAgentType(agentType)]
	externalStoreAvailable := lookup != nil && strings.TrimSpace(externalID) != ""
	if externalStoreAvailable {
		externalRecord, found, err := lookup.LookupSession(ctx, externalID)
		if err == nil && found && len(externalRecord.Turns) > 0 {
			return preferExternalRecord(sessionID, repoLabel, workingDir, branch, model, title, agentType, externalID, externalRecord), nil
		}
	}

	sentAtColumn := firstNonEmpty(schemaInfo.messageSentAt, schemaInfo.messageCreatedAt)
	createdAtColumn := firstNonEmpty(schemaInfo.messageCreatedAt, schemaInfo.messageSentAt)
	query := `select %s, %s, %s, %s, %s, %s from %s where %s = ? order by coalesce(%s, %s), %s`
	rows, err := db.Query(sprintf(query,
		schemaInfo.messageRole, schemaInfo.messageContent, schemaInfo.messagePayload, schemaInfo.messageModel, sentAtColumn, createdAtColumn,
		schemaInfo.sessionMessagesTable, schemaInfo.messageSessionID, sentAtColumn, createdAtColumn, createdAtColumn,
	), sessionID)
	if err != nil {
		return schema.Record{}, err
	}
	defer rows.Close()

	record := schema.Record{
		RecordID: sessionID,
		Origin:   s.Name(),
		Model:    strings.TrimSpace(model),
		Branch:   branch,
		Title:    title,
		Turns:    make([]schema.Turn, 0),
		Extensions: map[string]any{
			"orchestrator": map[string]any{
				"agent_type":               agentType,
				"orchestrator_session_id":  sessionID,
				"external_session_id":      externalID,
				"content_source":           "orchestrator",
				"external_store_available": externalStoreAvailable,
				"external_store_preferred": false,
			},
		},
	}
	record.WorkingDir = workingDir

	for rows.Next() {
		var role, content string
		var payloadRaw sql.NullString
		var messageModel, sentAt, createdAt sql.NullString
		if err := rows.Scan(&role, &content, &payloadRaw, &messageModel, &sentAt, &createdAt); err != nil {
			continue
		}
		turn := schema.Turn{
			Role:      normalizeRole(role),
			Timestamp: source.NormalizeTimestamp(firstNonEmpty(sentAt.String, createdAt.String)),
			Text:      content,
		}
		if messageModel.Valid && messageModel.String != "" && record.Model == "" {
			record.Model = messageModel.String
		}
		if payloadRaw.Valid && payloadRaw.String != "" {
			decodeOrchestratorPayload(payloadRaw.String, &turn)
		}
		if turn.Role == "" || (turn.Text == "" && turn.Reasoning == "" && len(turn.ToolCalls) == 0 && len(turn.Attachments) == 0) {
			continue
		}
		record.Turns = append(record.Turns, turn)
		record.StartedAt = source.EarliestTimestamp(record.StartedAt, turn.Timestamp)
		record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
	}

	record.Usage = schema.Usage{
		UserTurns:      source.CountTurns(record.Turns, "user"),
		AssistantTurns: source.CountTurns(record.Turns, "assistant"),
		ToolCalls:      source.CountToolCalls(record.Turns),
	}
	record.Model = firstNonEmpty(record.Model, "orchestrator/unknown")
	record.Grouping = "orchestrator:" + repoDisplayName(repoLabel)
	return record, nil
}

func (s *Source) open() (*sql.DB, dbSchema, error) {
	db, err := source.OpenSQLite(s.dbPath)
	if err != nil {
		return nil, dbSchema{}, err
	}
	schemaInfo, err := detectSchema(db)
	if err != nil {
		db.Close()
		return nil, dbSchema{}, err
	}
	return db, schemaInfo, nil
}

// detectSchema introspects the SQLite catalog to resolve actual table and
// column names against the set of historically-used aliases. It returns
// os.ErrNotExist when any of the four core tables (repos, workspaces,
// sessions, session_messages) are missing so callers can distinguish
// "unsupported database" from a genuine IO failure. Individual column names
// may come back empty when the field is optional in a given schema version.
func detectSchema(db *sql.DB) (dbSchema, error) {
	tables, err := tableColumns(db)
	if err != nil {
		return dbSchema{}, err
	}
	info := dbSchema{
		reposTable:           findTable(tables, "repositories", "repos", "repository"),
		workspacesTable:      findTable(tables, "workspaces", "workspace"),
		sessionsTable:        findTable(tables, "sessions", "session"),
		sessionMessagesTable: findTable(tables, "session_messages", "sessionMessages", "messages"),
	}
	if info.reposTable == "" || info.workspacesTable == "" || info.sessionsTable == "" || info.sessionMessagesTable == "" {
		return dbSchema{}, os.ErrNotExist
	}

	repoCols := tables[info.reposTable]
	workspaceCols := tables[info.workspacesTable]
	sessionCols := tables[info.sessionsTable]
	messageCols := tables[info.sessionMessagesTable]

	info.repoID = findColumn(repoCols, "id")
	info.repoName = findColumn(repoCols, "display_name", "name", "repo_name")
	info.repoRemote = findColumn(repoCols, "remote_origin", "remote_url", "origin_url", "git_url")
	info.repoPath = findColumn(repoCols, "path", "root_path", "directory")

	info.workspaceID = findColumn(workspaceCols, "id")
	info.workspaceRepoID = findColumn(workspaceCols, "repository_id", "repo_id")
	info.workspaceLabel = findColumn(workspaceCols, "label", "directory_name", "name")
	info.workspaceCodename = findColumn(workspaceCols, "codename", "nickname")
	info.workspaceBranch = findColumn(workspaceCols, "branch", "branch_name")

	info.sessionID = findColumn(sessionCols, "id")
	info.sessionWorkspaceID = findColumn(sessionCols, "workspace_id")
	info.sessionAgentType = findColumn(sessionCols, "agent_type", "agent")
	info.sessionExternalID = findColumn(sessionCols, "external_session_id", "claude_session_id", "agent_session_id")
	info.sessionModel = findColumn(sessionCols, "model", "model_id")
	info.sessionTitle = findColumn(sessionCols, "title", "name")

	info.messageSessionID = findColumn(messageCols, "session_id")
	info.messageRole = findColumn(messageCols, "role")
	info.messageContent = findColumn(messageCols, "content", "text")
	info.messagePayload = findColumn(messageCols, "payload", "rich_payload", "data")
	info.messageModel = findColumn(messageCols, "model", "model_id")
	info.messageSentAt = findColumn(messageCols, "sent_at", "timestamp")
	info.messageCreatedAt = findColumn(messageCols, "created_at", "created_at_ms", "time_created")
	if info.messageSentAt == "" {
		info.messageSentAt = info.messageCreatedAt
	}
	if info.messageCreatedAt == "" {
		info.messageCreatedAt = info.messageSentAt
	}

	return info, nil
}

func tableColumns(db *sql.DB) (map[string][]string, error) {
	rows, err := db.Query(`select name from sqlite_master where type = 'table'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make(map[string][]string)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		colRows, err := db.Query(`pragma table_info("` + name + `")`)
		if err != nil {
			continue
		}
		cols := make([]string, 0)
		for colRows.Next() {
			var cid int
			var colName, colType string
			var notNull, pk int
			var defaultValue any
			if err := colRows.Scan(&cid, &colName, &colType, &notNull, &defaultValue, &pk); err != nil {
				continue
			}
			cols = append(cols, colName)
		}
		colRows.Close()
		sort.Strings(cols)
		tables[name] = cols
	}
	return tables, nil
}

func findTable(tables map[string][]string, candidates ...string) string {
	for _, candidate := range candidates {
		for name := range tables {
			if strings.EqualFold(name, candidate) {
				return name
			}
		}
	}
	for _, candidate := range candidates {
		for name := range tables {
			if strings.Contains(strings.ToLower(name), strings.ToLower(candidate)) {
				return name
			}
		}
	}
	return ""
}

func findColumn(columns []string, candidates ...string) string {
	for _, candidate := range candidates {
		for _, column := range columns {
			if strings.EqualFold(column, candidate) {
				return column
			}
		}
	}
	for _, candidate := range candidates {
		for _, column := range columns {
			if strings.Contains(strings.ToLower(column), strings.ToLower(candidate)) {
				return column
			}
		}
	}
	return ""
}

func normalizeRole(role string) string {
	switch strings.ToLower(role) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return ""
	}
}

func decodeOrchestratorPayload(raw string, turn *schema.Turn) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return
	}
	turn.Text = firstNonEmpty(turn.Text, source.ExtractString(payload, "text", "content"))
	turn.Reasoning = firstNonEmpty(turn.Reasoning, source.ExtractString(payload, "reasoning"))
	for _, item := range source.ExtractSlice(payload, "tool_calls") {
		callMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		call := schema.ToolCall{
			Tool:   source.ExtractString(callMap, "tool", "name"),
			Input:  callMap["input"],
			Status: source.ExtractString(callMap, "status"),
		}
		if output := callMap["output"]; output != nil {
			call.Output = &schema.ToolOutput{Raw: output}
			if text, ok := output.(string); ok {
				call.Output.Text = text
			}
		}
		turn.ToolCalls = append(turn.ToolCalls, call)
	}
}

func repoDisplayName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, ".git")
	value = strings.TrimSuffix(value, "/")
	if strings.Contains(value, "/") {
		parts := strings.Split(value, "/")
		return parts[len(parts)-1]
	}
	return value
}

func detectDefaultDB() string {
	home := source.HomeDir()
	candidates := []string{
		filepath.Join(home, "Library", "Application Support", "Conductor", "conductor.db"),
		filepath.Join(home, "Library", "Application Support", "Conductor", "db.sqlite"),
		filepath.Join(home, ".local", "share", "conductor", "conductor.db"),
		filepath.Join(home, ".config", "conductor", "conductor.db"),
	}
	for _, candidate := range candidates {
		if source.FileExists(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

func sprintf(format string, parts ...string) string {
	args := make([]any, 0, len(parts))
	for _, part := range parts {
		args = append(args, `"`+part+`"`)
	}
	return fmt.Sprintf(format, args...)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func derivedWorkingDir(repoPath, workspaceLabel string) string {
	if repoPath == "" || workspaceLabel == "" {
		return ""
	}
	return filepath.Join(repoPath, workspaceLabel)
}

// preferExternalRecord merges an externally-sourced record (from claudecode,
// codex, gemini, ...) with the orchestrator's own metadata. The external
// record's turns and tool calls are preserved verbatim — they are richer than
// the orchestrator mirror — while the orchestrator overrides origin/grouping,
// working dir, branch, title, and model, and stuffs the originating record's
// identifiers into Extensions["orchestrator"] so consumers can trace
// provenance back to the native store.
func preferExternalRecord(sessionID, repoLabel, workingDir, branch, model, title, agentType, externalID string, external schema.Record) schema.Record {
	externalOrigin := external.Origin
	externalGrouping := external.Grouping
	externalRecordID := external.RecordID

	record := external
	record.RecordID = firstNonEmpty(externalID, external.RecordID, sessionID)
	record.Origin = "orchestrator"
	record.Grouping = "orchestrator:" + repoDisplayName(repoLabel)
	record.WorkingDir = firstNonEmpty(workingDir, record.WorkingDir)
	record.Branch = firstNonEmpty(branch, record.Branch)
	record.Title = firstNonEmpty(title, record.Title)
	record.Model = firstNonEmpty(model, record.Model, "orchestrator/unknown")
	if record.Extensions == nil {
		record.Extensions = make(map[string]any)
	}

	orchestratorMeta := map[string]any{
		"agent_type":               agentType,
		"orchestrator_session_id":  sessionID,
		"external_session_id":      externalID,
		"content_source":           externalOrigin,
		"external_store_available": true,
		"external_store_preferred": true,
	}
	if externalOrigin != "" {
		orchestratorMeta["external_origin"] = externalOrigin
	}
	if externalGrouping != "" {
		orchestratorMeta["external_grouping"] = externalGrouping
	}
	if externalRecordID != "" {
		orchestratorMeta["external_record_id"] = externalRecordID
	}
	record.Extensions["orchestrator"] = orchestratorMeta
	return record
}

// normalizeAgentType folds the orchestrator's free-form agent_type column
// into the canonical slugs used for dispatch (claudecode, gemini, kimi, ...).
// Non-alphanumeric characters are stripped and ASCII letters lowercased so
// that "Claude Code", "claude-code", and "CLAUDECODE" all collapse to
// "claudecode". Unrecognised values fall through as their stripped/lowercased
// form so downstream lookups can still match a source registered under that
// exact name.
func normalizeAgentType(agentType string) string {
	key := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			return unicode.ToLower(r)
		default:
			return -1
		}
	}, agentType)

	switch key {
	case "claude", "claudecode", "claudecodecli", "anthropic":
		return "claudecode"
	case "gemini", "geminicli":
		return "gemini"
	case "kimi", "kimicli":
		return "kimi"
	default:
		return key
	}
}
