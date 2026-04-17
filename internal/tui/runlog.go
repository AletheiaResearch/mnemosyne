package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/tui/common"
)

type runlogResult struct {
	Phase                  string                     `json:"phase"`
	LastExtract            *config.LastExtract        `json:"last_extract"`
	ReviewerStatements     *config.ReviewerStatements `json:"reviewer_statements"`
	VerificationRecord     *config.VerificationRecord `json:"verification_record"`
	LastAttest             *config.LastAttest         `json:"last_attest"`
	PublicationAttestation string                     `json:"publication_attestation"`
}

type runlogScreen struct {
	configPath string
	width      int
	height     int
	running    bool
	loaded     bool
	result     runlogResult
	warnings   string
	err        string
	raw        string
	viewport   viewport.Model
	vpReady    bool
}

func newRunlogScreen(configPath string) *runlogScreen {
	return &runlogScreen{configPath: configPath}
}

func (s *runlogScreen) Init() tea.Cmd {
	s.running = true
	return runCommand(s.configPath, "runlog")
}

func (s *runlogScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.resizeViewport()
}

func (s *runlogScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandResultMsg:
		s.running = false
		s.err = errorText(msg.err)
		s.raw = msg.output
		s.warnings, _ = splitJSONPrefix(msg.output)
		body := jsonBody(msg.output)
		if body != "" {
			dec := json.NewDecoder(strings.NewReader(body))
			var result runlogResult
			if err := dec.Decode(&result); err == nil {
				s.result = result
				s.loaded = true
			} else if s.err == "" {
				s.err = "parse runlog output: " + err.Error()
			}
		}
		s.refreshContent()
		return s, nil
	case tea.KeyMsg:
		if msg.String() == "r" && !s.running {
			s.running = true
			s.err = ""
			s.loaded = false
			return s, runCommand(s.configPath, "runlog")
		}
	}
	if s.vpReady {
		if wheel, ok := msg.(tea.MouseMsg); ok {
			if (wheel.Button == tea.MouseButtonWheelUp && s.viewport.AtTop()) ||
				(wheel.Button == tea.MouseButtonWheelDown && s.viewport.AtBottom()) {
				return s, nil
			}
		}
		var cmd tea.Cmd
		s.viewport, cmd = s.viewport.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *runlogScreen) View() string {
	header := s.renderHeader()
	if s.err != "" {
		body := common.ErrorStyle.Render("Error: ") + s.err
		if strings.TrimSpace(s.raw) != "" {
			body += "\n\n" + common.MutedStyle.Render(s.raw)
		}
		return joinBlocks(header, body)
	}
	if !s.loaded {
		var body string
		if s.running {
			body = common.AccentStyle.Render("Loading runlog…")
		} else {
			body = common.MutedStyle.Render("No runlog data.")
		}
		return joinBlocks(header, body)
	}
	s.layoutViewport(header)
	return joinBlocks(header, s.viewport.View())
}

func (s *runlogScreen) FooterHints() string {
	return common.HintLine("↑↓ scroll", "r reload", "esc back")
}

func (s *runlogScreen) FooterStatus() string {
	if s.running {
		return "running…"
	}
	if s.err != "" {
		return "error"
	}
	return ""
}

func (s *runlogScreen) contentWidth() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

func (s *runlogScreen) renderHeader() string {
	pill := phasePill(s.result.Phase)
	summary := common.LabelStyle.Render("workflow state")
	return lipgloss.JoinHorizontal(lipgloss.Center, pill, "  ", summary)
}

func (s *runlogScreen) renderBody() string {
	blocks := []string{}
	if s.warnings != "" {
		blocks = append(blocks, common.WarnStyle.Render("Warnings")+"\n"+common.MutedStyle.Render(indent(s.warnings, 2)))
	}
	blocks = append(blocks, common.Panel("Last extract", s.renderExtract(), s.contentWidth()))
	blocks = append(blocks, common.Panel("Reviewer statements", s.renderStatements(), s.contentWidth()))
	blocks = append(blocks, common.Panel("Verification record", s.renderVerification(), s.contentWidth()))
	blocks = append(blocks, common.Panel("Last attest", s.renderAttest(), s.contentWidth()))
	blocks = append(blocks, common.Panel("Publication attestation", s.renderPublication(), s.contentWidth()))
	return joinBlocks(blocks...)
}

func (s *runlogScreen) renderExtract() string {
	e := s.result.LastExtract
	if e == nil {
		return common.MutedStyle.Render("(no extract recorded yet)")
	}
	rows := []string{
		common.KV("timestamp", common.ValueStyle.Render(formatTimestamp(e.Timestamp)), 14),
		common.KV("scope", valueOrNone(e.Scope), 14),
		common.KV("output", valueOrNone(e.OutputPath), 14),
		common.KV("records", common.ValueStyle.Render(fmt.Sprintf("%d", e.RecordCount)), 14),
	}
	if e.SkippedRecords > 0 {
		rows = append(rows, common.KV("skipped", common.WarnStyle.Render(fmt.Sprintf("%d", e.SkippedRecords)), 14))
	}
	if e.RedactionCount > 0 {
		rows = append(rows, common.KV("redactions", common.AccentStyle.Render(fmt.Sprintf("%d", e.RedactionCount)), 14))
	}
	if e.InputTokens > 0 || e.OutputTokens > 0 {
		rows = append(rows, common.KV("tokens", common.ValueStyle.Render(fmt.Sprintf("in %d · out %d", e.InputTokens, e.OutputTokens)), 14))
	}
	if len(e.Warnings) > 0 {
		var w strings.Builder
		for i, msg := range e.Warnings {
			if i > 0 {
				w.WriteString("\n")
			}
			w.WriteString(common.WarnStyle.Render("• ") + common.ValueStyle.Render(msg))
		}
		rows = append(rows, common.KV("warnings", w.String(), 14))
	}
	return strings.Join(rows, "\n")
}

func (s *runlogScreen) renderStatements() string {
	r := s.result.ReviewerStatements
	if r == nil {
		return common.MutedStyle.Render("(no reviewer statements recorded)")
	}
	return strings.Join([]string{
		common.KV("identity", statementOrNone(r.IdentityScan), 14),
		common.KV("entities", statementOrNone(r.EntityInterview), 14),
		common.KV("manual", statementOrNone(r.ManualReview), 14),
	}, "\n")
}

func (s *runlogScreen) renderVerification() string {
	v := s.result.VerificationRecord
	if v == nil {
		return common.MutedStyle.Render("(no verification recorded)")
	}
	rows := []string{
		common.KV("full name", valueOrNone(v.FullName), 16),
		common.KV("name scan", yesNo(!v.NameScanSkipped), 16),
	}
	if v.FullNameMatchCount > 0 {
		rows = append(rows, common.KV("name matches", common.WarnStyle.Render(fmt.Sprintf("%d", v.FullNameMatchCount)), 16))
	}
	if v.ManualSampleCount > 0 {
		rows = append(rows, common.KV("manual samples", common.ValueStyle.Render(fmt.Sprintf("%d", v.ManualSampleCount)), 16))
	}
	return strings.Join(rows, "\n")
}

func (s *runlogScreen) renderAttest() string {
	a := s.result.LastAttest
	if a == nil {
		return common.MutedStyle.Render("(no attestation recorded)")
	}
	rows := []string{
		common.KV("timestamp", common.ValueStyle.Render(formatTimestamp(a.Timestamp)), 16),
		common.KV("file", valueOrNone(a.FilePath), 16),
	}
	if a.FileSize > 0 {
		rows = append(rows, common.KV("file size", common.ValueStyle.Render(humanBytes(a.FileSize)), 16))
	}
	rows = append(rows, common.KV("built-in scan", yesNo(a.BuiltInFindings), 16))
	if a.FullName != "" {
		rows = append(rows, common.KV("full name", common.ValueStyle.Render(a.FullName), 16))
	}
	rows = append(rows, common.KV("name scan", yesNo(!a.NameScanSkipped), 16))
	if a.FullNameMatches > 0 {
		rows = append(rows, common.KV("name matches", common.WarnStyle.Render(fmt.Sprintf("%d", a.FullNameMatches)), 16))
	}
	if a.ManualSampleCount > 0 {
		rows = append(rows, common.KV("manual samples", common.ValueStyle.Render(fmt.Sprintf("%d", a.ManualSampleCount)), 16))
	}
	return strings.Join(rows, "\n")
}

func (s *runlogScreen) renderPublication() string {
	if strings.TrimSpace(s.result.PublicationAttestation) == "" {
		return common.MutedStyle.Render("(not yet published)")
	}
	return common.SuccessStyle.Render("✓ ") + common.ValueStyle.Render(s.result.PublicationAttestation)
}

func (s *runlogScreen) refreshContent() {
	if !s.vpReady {
		w := s.contentWidth()
		if w < 20 {
			w = 20
		}
		s.viewport = viewport.New(w, 4)
		s.vpReady = true
	}
	s.viewport.SetContent(s.renderBody())
}

func (s *runlogScreen) resizeViewport() {
	if !s.vpReady {
		return
	}
	w := s.contentWidth()
	if w < 20 {
		w = 20
	}
	s.viewport.Width = w
	s.viewport.SetContent(s.renderBody())
}

func (s *runlogScreen) layoutViewport(header string) {
	if !s.vpReady {
		return
	}
	h := s.height - lipgloss.Height(header) - 1
	if h < 4 {
		h = 4
	}
	s.viewport.Height = h
}

func statementOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return common.MutedStyle.Render("(missing)")
	}
	return common.ValueStyle.Render(s)
}

func formatTimestamp(ts string) string {
	if ts == "" {
		return "(none)"
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.Local().Format("2006-01-02 15:04:05 MST")
	}
	return ts
}
