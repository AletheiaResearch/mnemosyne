package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/AletheiaResearch/mnemosyne/internal/tui/common"
)

type surveyGrouping struct {
	ID               string `json:"id"`
	DisplayLabel     string `json:"display_label"`
	Origin           string `json:"origin"`
	EstimatedRecords int    `json:"estimated_records"`
	EstimatedBytes   int64  `json:"estimated_bytes"`
	Excluded         bool   `json:"excluded,omitempty"`
}

type surveyResult struct {
	Phase              string           `json:"phase"`
	PlatformIdentity   string           `json:"platform_identity"`
	DestinationRepo    string           `json:"destination_repo"`
	OriginScope        string           `json:"origin_scope"`
	GroupingExclusions []string         `json:"grouping_exclusions"`
	CustomRedactions   []string         `json:"custom_redactions"`
	CustomHandles      []string         `json:"custom_handles"`
	ScopeConfirmed     bool             `json:"scope_confirmed"`
	NextSteps          []string         `json:"next_steps"`
	Groupings          []surveyGrouping `json:"groupings"`
}

type surveyScreen struct {
	configPath string
	width      int
	height     int
	running    bool
	loaded     bool
	result     surveyResult
	warnings   string
	err        string
	raw        string
	viewport   viewport.Model
	vpReady    bool
}

func newSurveyScreen(configPath string) *surveyScreen {
	return &surveyScreen{configPath: configPath}
}

func (s *surveyScreen) Init() tea.Cmd {
	s.running = true
	return runCommand(s.configPath, "survey")
}

func (s *surveyScreen) SetSize(width, height int) {
	s.width = width
	s.height = height
	s.resizeViewport()
}

func (s *surveyScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case commandResultMsg:
		s.running = false
		s.err = errorText(msg.err)
		s.raw = msg.output
		s.warnings, _ = splitJSONPrefix(msg.output)
		body := jsonBody(msg.output)
		if body != "" {
			dec := json.NewDecoder(strings.NewReader(body))
			var result surveyResult
			if err := dec.Decode(&result); err == nil {
				s.result = result
				s.loaded = true
			} else if s.err == "" {
				s.err = "parse survey output: " + err.Error()
			}
		}
		s.refreshGroupings()
		return s, nil
	case tea.KeyMsg:
		if msg.String() == "r" && !s.running {
			s.running = true
			s.err = ""
			s.loaded = false
			return s, runCommand(s.configPath, "survey")
		}
	}
	if s.vpReady {
		var cmd tea.Cmd
		s.viewport, cmd = s.viewport.Update(msg)
		return s, cmd
	}
	return s, nil
}

func (s *surveyScreen) View() string {
	head := s.renderHeader()
	warnings := ""
	if s.warnings != "" {
		warnings = common.WarnStyle.Render("Warnings") + "\n" +
			common.MutedStyle.Render(indent(s.warnings, 2))
	}

	if s.err != "" {
		body := common.ErrorStyle.Render("Error: ") + s.err
		if strings.TrimSpace(s.raw) != "" {
			body += "\n\n" + common.MutedStyle.Render(s.raw)
		}
		return joinBlocks(head, warnings, body)
	}

	if !s.loaded {
		var body string
		if s.running {
			body = common.AccentStyle.Render("Running survey…")
		} else {
			body = common.MutedStyle.Render("No survey data.")
		}
		return joinBlocks(head, warnings, body)
	}

	above := joinBlocks(
		head,
		warnings,
		common.Panel("Scope", s.renderScope(), s.contentWidth()),
		common.Panel("Next steps", s.renderNextSteps(), s.contentWidth()),
	)
	groupings := s.renderGroupingsPanel(above)
	return joinBlocks(above, groupings)
}

func (s *surveyScreen) FooterHints() string {
	return common.HintLine("↑↓ scroll", "r reload", "esc back")
}

func (s *surveyScreen) FooterStatus() string {
	if s.running {
		return "running…"
	}
	if s.err != "" {
		return "error"
	}
	if s.loaded {
		return fmt.Sprintf("%d groupings", len(s.result.Groupings))
	}
	return ""
}

func (s *surveyScreen) contentWidth() int {
	if s.width <= 0 {
		return 80
	}
	return s.width
}

func (s *surveyScreen) renderHeader() string {
	pill := phasePill(s.result.Phase)
	identity := s.result.PlatformIdentity
	if identity == "" {
		identity = common.MutedStyle.Render("not detected")
	} else {
		identity = common.ValueStyle.Render(identity)
	}
	repo := s.result.DestinationRepo
	if repo == "" {
		repo = common.MutedStyle.Render("not set")
	} else {
		repo = common.ValueStyle.Render(repo)
	}
	sep := common.SubtleStyle.Render(" │ ")
	return lipgloss.JoinHorizontal(lipgloss.Center,
		pill,
		"  ",
		common.LabelStyle.Render("identity "), identity,
		sep,
		common.LabelStyle.Render("repo "), repo,
	)
}

func (s *surveyScreen) renderScope() string {
	rows := []string{
		common.KV("origin scope", valueOrNone(s.result.OriginScope), 14),
		common.KV("confirmed", yesNo(s.result.ScopeConfirmed), 14),
		common.KV("exclusions", listOrNone(s.result.GroupingExclusions), 14),
		common.KV("redactions", listOrNone(s.result.CustomRedactions), 14),
		common.KV("handles", listOrNone(s.result.CustomHandles), 14),
	}
	return strings.Join(rows, "\n")
}

func (s *surveyScreen) renderNextSteps() string {
	if len(s.result.NextSteps) == 0 {
		return common.MutedStyle.Render("(none)")
	}
	lines := make([]string, 0, len(s.result.NextSteps))
	for _, step := range s.result.NextSteps {
		lines = append(lines, common.AccentStyle.Render("• ")+common.ValueStyle.Render(step))
	}
	return strings.Join(lines, "\n")
}

func (s *surveyScreen) renderGroupingsPanel(above string) string {
	title := fmt.Sprintf("Groupings (%d detected)", len(s.result.Groupings))
	if !s.vpReady {
		return common.Panel(title, common.MutedStyle.Render("(not ready)"), s.contentWidth())
	}
	s.layoutGroupings(above)
	return common.Panel(title, s.viewport.View(), s.contentWidth())
}

func (s *surveyScreen) refreshGroupings() {
	if !s.vpReady {
		w := s.contentWidth() - 4
		if w < 10 {
			w = 10
		}
		s.viewport = viewport.New(w, 4)
		s.vpReady = true
	}
	s.viewport.SetContent(s.groupingsTable(s.viewport.Width))
}

func (s *surveyScreen) resizeViewport() {
	if !s.vpReady {
		return
	}
	w := s.contentWidth() - 4
	if w < 10 {
		w = 10
	}
	s.viewport.Width = w
	s.viewport.SetContent(s.groupingsTable(w))
}

func (s *surveyScreen) layoutGroupings(above string) {
	aboveH := lipgloss.Height(above)
	remaining := s.height - aboveH - 1 // spacer between above and groupings panel
	panelH := remaining - 1            // title line above the border
	vpH := panelH - 2                  // top/bottom border
	if vpH < 3 {
		vpH = 3
	}
	s.viewport.Height = vpH
}

func (s *surveyScreen) groupingsTable(width int) string {
	if len(s.result.Groupings) == 0 {
		return common.MutedStyle.Render("(no groupings detected)")
	}
	const (
		originW   = 14
		recordsW  = 8
		bytesW    = 10
		excludedW = 3
	)
	idW := width - originW - recordsW - bytesW - excludedW - 4
	if idW < 10 {
		idW = 10
	}

	header := lipgloss.JoinHorizontal(lipgloss.Top,
		common.LabelStyle.Copy().Width(originW).Render("origin"),
		common.LabelStyle.Copy().Width(idW).Render("id"),
		common.LabelStyle.Copy().Width(recordsW).Align(lipgloss.Right).Render("records"),
		common.LabelStyle.Copy().Width(bytesW).Align(lipgloss.Right).Render("size"),
		common.LabelStyle.Copy().Width(excludedW).Align(lipgloss.Right).Render(""),
	)
	rule := common.SubtleStyle.Render(strings.Repeat("─", originW+idW+recordsW+bytesW+excludedW))

	rows := []string{header, rule}
	for _, g := range s.result.Groupings {
		idStyle := common.ValueStyle
		idText := g.ID
		if g.DisplayLabel != "" && g.DisplayLabel != g.ID {
			idText = g.ID + common.MutedStyle.Render(" · "+g.DisplayLabel)
		}
		mark := ""
		if g.Excluded {
			mark = common.WarnStyle.Render("×")
			idStyle = common.MutedStyle
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
			common.AccentStyle.Copy().Width(originW).Render(truncate(g.Origin, originW-1)),
			idStyle.Copy().Width(idW).Render(truncate(idText, idW-1)),
			common.ValueStyle.Copy().Width(recordsW).Align(lipgloss.Right).Render(fmt.Sprintf("%d", g.EstimatedRecords)),
			common.ValueStyle.Copy().Width(bytesW).Align(lipgloss.Right).Render(humanBytes(g.EstimatedBytes)),
			common.WarnStyle.Copy().Width(excludedW).Align(lipgloss.Right).Render(mark),
		))
	}
	return strings.Join(rows, "\n")
}

func phasePill(phase string) string {
	label := strings.ToUpper(strings.ReplaceAll(phase, "-", " "))
	switch phase {
	case "initial":
		return common.MutedPillStyle.Render(label)
	case "preparing":
		return common.AccentPillStyle.Render(label)
	case "pending-review":
		return common.WarnPillStyle.Render(label)
	case "cleared":
		return common.SuccessPillStyle.Render(label)
	case "finalized":
		return common.BrandPillStyle.Render(label)
	default:
		if label == "" {
			label = "UNKNOWN"
		}
		return common.MutedPillStyle.Render(label)
	}
}

func valueOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return common.MutedStyle.Render("(none)")
	}
	return common.ValueStyle.Render(s)
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return common.MutedStyle.Render("(none)")
	}
	return common.ValueStyle.Render(strings.Join(items, ", "))
}

func yesNo(b bool) string {
	if b {
		return common.SuccessStyle.Render("yes")
	}
	return common.MutedStyle.Render("no")
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for i := n / unit; i >= unit; i /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func truncate(s string, limit int) string {
	if limit <= 1 {
		return "…"
	}
	if lipgloss.Width(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit-1]) + "…"
}

// splitJSONPrefix splits free-form log output before the first JSON object
// from the JSON body. It returns trimmed warnings and the json body.
func splitJSONPrefix(output string) (warnings, jsonText string) {
	idx := strings.Index(output, "{")
	if idx < 0 {
		return strings.TrimSpace(output), ""
	}
	return strings.TrimSpace(output[:idx]), output[idx:]
}

func jsonBody(output string) string {
	idx := strings.Index(output, "{")
	if idx < 0 {
		return ""
	}
	return output[idx:]
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n")
}

func joinBlocks(blocks ...string) string {
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if strings.TrimSpace(b) == "" {
			continue
		}
		parts = append(parts, b)
	}
	return strings.Join(parts, "\n\n")
}
