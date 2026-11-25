package editor

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/replay"
	"github.com/nsgo-dev/nsgo/core"
)

// PlaytestChartKind identifies which single chart visualization the Playtest Report screen renders for a report.
type PlaytestChartKind string

const (
	// PlaytestChartHeatMap renders a spatial heat map (deaths or time per tile) over the level.
	PlaytestChartHeatMap PlaytestChartKind = "heat"
	// PlaytestChartBars renders labeled vertical bars per wave/bucket against an optional dashed goal line.
	PlaytestChartBars PlaytestChartKind = "bars"
	// PlaytestChartLine renders a per-level line progression chart highlighting dips, spikes or flagged levels.
	PlaytestChartLine PlaytestChartKind = "line"
	// PlaytestChartMatrix renders a row-vs-column win-rate matrix with cells above the threshold (default 60%) flagged.
	PlaytestChartMatrix PlaytestChartKind = "matrix"
)

// String returns a human-readable name for the chart kind.
func (k PlaytestChartKind) String() string {
	switch k {
	case PlaytestChartHeatMap:
		return "heat"
	case PlaytestChartBars:
		return "bars"
	case PlaytestChartLine:
		return "line"
	case PlaytestChartMatrix:
		return "matrix"
	default:
		return string(k)
	}
}

// PlaytestMetricCard is one summary card rendered in the top grid of the Playtest Report screen.
type PlaytestMetricCard struct {
	Name      string
	Label     string
	ValueText string
	Foot      string
	GoalText  string
	Source    string
	Status    string // "met", "missed", or "info"
	Badge     string // "Met", "Missed", or "Info"
	Met       bool
	Missed    bool
}

// PlaytestHeatCluster is a clustered density hotspot drawn as a radial heat blob on the level heat map.
type PlaytestHeatCluster struct {
	X       float64
	Y       float64
	Count   int
	Share   float64 // 0..1 share of total points
	Tag     string
	Problem bool
}

// PlaytestHeatChip is a concise pill annotation placed over a notable cluster on the level heat map.
type PlaytestHeatChip struct {
	X       float64
	Y       float64
	Text    string
	Problem bool
}

// PlaytestHeatChart holds the spatial data rendered by PlaytestChartHeatMap.
type PlaytestHeatChart struct {
	Metric        string
	Event         string
	IsTimePerTile bool
	Points        []playtest.HeatPoint
	Clusters      []PlaytestHeatCluster
	Chips         []PlaytestHeatChip
	MinX, MaxX    float64
	MinY, MaxY    float64
}

// PlaytestSeriesChart holds the bucketed data rendered by PlaytestChartBars or PlaytestChartLine.
type PlaytestSeriesChart struct {
	Name       string
	Unit       string
	Entries    []playtest.SeriesEntry
	TargetMax  *float64
	TargetText string
	MaxVal     float64
	Decimals   int
}

// PlaytestMatrixChart holds the grid data rendered by PlaytestChartMatrix.
type PlaytestMatrixChart struct {
	Name      string
	Unit      string
	FlagAbove float64
	Labels    []string
	Cells     [][]*float64
	Caption   string
}

// PlaytestChartSpec is the single chart selected for a playtest report.
type PlaytestChartSpec struct {
	Kind     PlaytestChartKind
	Title    string
	Subtitle string
	HeatMap  *PlaytestHeatChart
	Bars     *PlaytestSeriesChart
	Line     *PlaytestSeriesChart
	Matrix   *PlaytestMatrixChart
}

// PlaytestSuggestion is one actionable tuning suggestion under Agent's read.
type PlaytestSuggestion struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	Requested    bool   `json:"requested,omitempty"`
	ReviewStatus string `json:"reviewStatus,omitempty"` // "", "requested", "pending_review", "applied"
}

// PlaytestProposal represents a metric proposed by the agent with playtest_define awaiting a person's review.
type PlaytestProposal struct {
	ID          string       `json:"id"`
	File        string       `json:"file"`
	MetricName  string       `json:"metricName"`
	MetricLabel string       `json:"metricLabel,omitempty"`
	Rationale   string       `json:"rationale,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	Replace     bool         `json:"replace,omitempty"`
	Before      string       `json:"before,omitempty"`
	After       string       `json:"after,omitempty"`
	Diff        string       `json:"diff,omitempty"`
	Status      string       `json:"status"` // "pending", "applied", "dismissed"
	ApplyFunc   func() error `json:"-"`
	DismissFunc func() error `json:"-"`
}

// BannerText formats the single-line summary shown in the BOT PROFILE proposal banner.
func (p PlaytestProposal) BannerText() string {
	if strings.TrimSpace(p.Summary) != "" {
		return p.Summary
	}
	name := strings.TrimSpace(p.MetricLabel)
	if name == "" {
		name = strings.TrimSpace(p.MetricName)
	}
	if name == "" {
		name = "new metric"
	}
	rat := strings.TrimSpace(p.Rationale)
	if rat == "" {
		return fmt.Sprintf("Proposed with playtest_define: %s", name)
	}
	if strings.HasPrefix(strings.ToLower(rat), "from ") {
		return fmt.Sprintf("Proposed with playtest_define: %s, %s", name, rat)
	}
	return fmt.Sprintf("Proposed with playtest_define: %s, from %q", name, rat)
}

// PlaytestPromptRequest captures a tuning or suggestion request sent from the Playtest Report screen to the agent.
type PlaytestPromptRequest struct {
	Kind            string   `json:"kind"` // "tune" or "suggestion"
	Prompt          string   `json:"prompt"`
	Project         string   `json:"project,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	ReportPath      string   `json:"reportPath,omitempty"`
	ConfigPath      string   `json:"configPath,omitempty"`
	SuggestionID    string   `json:"suggestionId,omitempty"`
	SuggestionIndex int      `json:"suggestionIndex,omitempty"`
	MissedMetrics   []string `json:"missedMetrics,omitempty"`
}

// PlaytestReportOpenConfig supplies a playtest.Report and optional presentation metadata to OpenPlaytestReport / LoadPlaytestReport.
type PlaytestReportOpenConfig struct {
	// Report is the validated playtest.Report to display (required unless Entries is non-empty).
	Report *playtest.Report
	// ReportPath is the slash-separated path to the report file relative to the project root (or absolute path).
	ReportPath string
	// ConfigPath is the slash-separated path to playtest.yaml (defaults to "playtest.yaml").
	ConfigPath string
	// BaseDir is the base directory used to resolve relative recording paths and playtest.yaml when no project is open.
	BaseDir string
	// RunsSummary optionally overrides the formatted runs summary in the header.
	RunsSummary string
	// WatchRunIndex optionally selects which 1-based run Watch run N opens (0 chooses automatically).
	WatchRunIndex int
	// WatchLabel optionally overrides the Watch button label (e.g. "Watch match 38").
	WatchLabel string
	// AgentRead optionally overrides the synthesized Agent's read summary paragraph.
	AgentRead string
	// Suggestions optionally overrides the synthesized actionable suggestions list.
	Suggestions []string
	// Proposals optionally seeds pending playtest_define metric proposals for the session.
	Proposals []PlaytestProposal
	// AdditionalEntries optionally attaches extra reports so the BOT PROFILE strip can switch between multiple reports.
	AdditionalEntries []PlaytestReportOpenConfig
}

// PlaytestReportEntry holds one loaded report and its derived UI view-model.
type PlaytestReportEntry struct {
	ID            string
	Report        *playtest.Report
	ReportPath    string
	ConfigPath    string
	BaseDir       string
	RunsSummary   string
	WatchRunIndex int
	WatchLabel    string
	AgentRead     string
	Suggestions   []PlaytestSuggestion
	Cards         []PlaytestMetricCard
	Chart         PlaytestChartSpec
}

// PlaytestReportSession holds the state of the Editor's Playtest Report screen.
type PlaytestReportSession struct {
	Entries           []*PlaytestReportEntry
	ActiveIndex       int
	Proposals         []PlaytestProposal
	ReviewProposalID  string
	ConfigPreviewOpen bool
	ConfigPreviewPath string
	ConfigPreviewText string
	ConfigPreviewErr  string
	LastPrompt        PlaytestPromptRequest
	Notice            string
}

// ActiveEntry returns the currently displayed report entry, or nil when none is loaded.
func (s *PlaytestReportSession) ActiveEntry() *PlaytestReportEntry {
	if s == nil || len(s.Entries) == 0 {
		return nil
	}
	if s.ActiveIndex < 0 || s.ActiveIndex >= len(s.Entries) {
		return s.Entries[0]
	}
	return s.Entries[s.ActiveIndex]
}

// FirstPendingProposal returns the first pending playtest_define proposal, if any.
func (s *PlaytestReportSession) FirstPendingProposal() *PlaytestProposal {
	if s == nil {
		return nil
	}
	for i := range s.Proposals {
		if s.Proposals[i].Status == "pending" {
			return &s.Proposals[i]
		}
	}
	return nil
}

// ProposalByID returns the proposal with ID id, or nil.
func (s *PlaytestReportSession) ProposalByID(id string) *PlaytestProposal {
	if s == nil || id == "" {
		return nil
	}
	for i := range s.Proposals {
		if s.Proposals[i].ID == id {
			return &s.Proposals[i]
		}
	}
	return nil
}

type playtestReportState struct {
	open       bool
	rev        uint64
	nextPropID int
	session    *PlaytestReportSession
}

func isPlaytestReportFile(relPath string) bool {
	lower := strings.ToLower(strings.ReplaceAll(relPath, `\`, "/"))
	return strings.HasSuffix(lower, ".report.yaml") || strings.HasSuffix(lower, ".report.yml")
}

// PlaytestReportOpen reports whether the full-window Playtest Report screen is currently active.
func (e *Editor) PlaytestReportOpen() bool {
	return e != nil && e.playtest.open
}

// PlaytestSession returns the active PlaytestReportSession (even while the screen is closed), or nil if no report has been loaded.
func (e *Editor) PlaytestSession() *PlaytestReportSession {
	if e == nil {
		return nil
	}
	return e.playtest.session
}

// ActivePlaytestReport returns the currently active playtest.Report, or nil when none is loaded.
func (e *Editor) ActivePlaytestReport() *playtest.Report {
	if e == nil || e.playtest.session == nil {
		return nil
	}
	if entry := e.playtest.session.ActiveEntry(); entry != nil {
		return entry.Report
	}
	return nil
}

func (e *Editor) playtestStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.playtest.rev
}

func (e *Editor) playtestProjectDir() string {
	if e == nil || strings.TrimSpace(e.ProjectPath) == "" {
		return ""
	}
	return filepath.Clean(filepath.Dir(e.ProjectPath))
}

// SetPlaytestReportOpen opens or closes the Playtest Report screen.
// Opening while Play mode is active is ignored. If no report is loaded yet and an open project contains playtest.report.yaml, it loads it automatically.
func (e *Editor) SetPlaytestReportOpen(open bool) {
	if e == nil {
		return
	}
	if open && e.playing {
		return
	}
	if open && e.playtest.session == nil {
		if dir := e.playtestProjectDir(); dir != "" {
			defaultPath := filepath.Join(dir, playtest.ReportFileName)
			if _, err := os.Stat(defaultPath); err == nil {
				_ = e.OpenPlaytestReportFile(playtest.ReportFileName)
				return
			}
		}
	}
	if e.playtest.open == open {
		return
	}
	if open {
		e.startScreen.open = false
		e.merge.open = false
		e.hud.open = false
	}
	e.playtest.open = open
	e.playtest.rev++
	e.notify()
}

// ClosePlaytestReport closes the Playtest Report screen and returns to the scene editor.
func (e *Editor) ClosePlaytestReport() {
	e.SetPlaytestReportOpen(false)
}

func (e *Editor) resetPlaytestForProject() {
	if e == nil {
		return
	}
	e.playtest.open = false
	e.playtest.session = nil
	e.playtest.rev++
}

// LoadPlaytestReport loads a playtest report into the editor without forcing the Playtest Report screen open.
func (e *Editor) LoadPlaytestReport(cfg PlaytestReportOpenConfig) error {
	return e.setPlaytestReport(cfg, false)
}

// OpenPlaytestReport loads a playtest report into the editor and opens the full-window Playtest Report screen.
func (e *Editor) OpenPlaytestReport(cfg PlaytestReportOpenConfig) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	return e.setPlaytestReport(cfg, true)
}

func (e *Editor) setPlaytestReport(cfg PlaytestReportOpenConfig, openScreen bool) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if cfg.Report == nil {
		return errors.New("editor: playtest report is nil")
	}
	if err := playtest.ValidateReport(cfg.Report); err != nil {
		return fmt.Errorf("editor: invalid playtest report: %w", err)
	}

	entries := make([]*PlaytestReportEntry, 0, 1+len(cfg.AdditionalEntries))
	primary, err := e.buildPlaytestEntry(cfg, 0)
	if err != nil {
		return err
	}
	entries = append(entries, primary)
	for i, extra := range cfg.AdditionalEntries {
		if extra.Report == nil {
			continue
		}
		if err := playtest.ValidateReport(extra.Report); err != nil {
			return fmt.Errorf("editor: invalid additional playtest report %d: %w", i, err)
		}
		ent, err := e.buildPlaytestEntry(extra, i+1)
		if err != nil {
			return err
		}
		entries = append(entries, ent)
	}

	var existingProposals []PlaytestProposal
	if e.playtest.session != nil && len(cfg.Proposals) == 0 {
		existingProposals = slices.Clone(e.playtest.session.Proposals)
	}
	sess := &PlaytestReportSession{
		Entries:     entries,
		ActiveIndex: 0,
		Proposals:   existingProposals,
	}
	for _, p := range cfg.Proposals {
		if p.ID == "" {
			e.playtest.nextPropID++
			p.ID = fmt.Sprintf("playtest-proposal-%d", e.playtest.nextPropID)
		}
		if p.Status == "" {
			p.Status = "pending"
		}
		if p.File == "" {
			p.File = playtest.FileName
		}
		sess.Proposals = append(sess.Proposals, p)
	}

	e.playtest.session = sess
	if openScreen {
		e.startScreen.open = false
		e.merge.open = false
		e.hud.open = false
		e.playtest.open = true
	}
	e.playtest.rev++
	e.Status = fmt.Sprintf("Loaded playtest report · %s", primary.Report.Scope.Scene)
	e.notify()
	return nil
}

func (e *Editor) buildPlaytestEntry(cfg PlaytestReportOpenConfig, idx int) (*PlaytestReportEntry, error) {
	rep := cfg.Report
	configPath := strings.TrimSpace(cfg.ConfigPath)
	if configPath == "" {
		configPath = playtest.FileName
	}
	baseDir := strings.TrimSpace(cfg.BaseDir)
	if baseDir == "" {
		baseDir = e.playtestProjectDir()
	}
	if baseDir == "" && filepath.IsAbs(cfg.ReportPath) {
		baseDir = filepath.Dir(cfg.ReportPath)
	}

	runsSummary := strings.TrimSpace(cfg.RunsSummary)
	if runsSummary == "" {
		runsSummary = FormatPlaytestRunsSummary(rep)
	}

	watchIdx := cfg.WatchRunIndex
	if watchIdx <= 0 {
		watchIdx = DefaultWatchRunIndex(rep)
	}
	watchLabel := strings.TrimSpace(cfg.WatchLabel)
	if watchLabel == "" {
		if watchIdx > 0 {
			watchLabel = fmt.Sprintf("Watch run %d", watchIdx)
		} else {
			watchLabel = "Watch run"
		}
	}

	defaultRead, defaultSuggestions := DefaultPlaytestAgentRead(rep)
	agentRead := strings.TrimSpace(cfg.AgentRead)
	if agentRead == "" {
		agentRead = defaultRead
	}
	var suggestions []PlaytestSuggestion
	if len(cfg.Suggestions) > 0 {
		for i, text := range cfg.Suggestions {
			if strings.TrimSpace(text) == "" {
				continue
			}
			suggestions = append(suggestions, PlaytestSuggestion{
				ID:   fmt.Sprintf("suggestion-%d", i+1),
				Text: strings.TrimSpace(text),
			})
		}
	} else {
		suggestions = defaultSuggestions
	}

	id := fmt.Sprintf("%s-%d", rep.Bot.Profile, idx+1)
	return &PlaytestReportEntry{
		ID:            id,
		Report:        rep,
		ReportPath:    filepath.ToSlash(strings.TrimSpace(cfg.ReportPath)),
		ConfigPath:    filepath.ToSlash(configPath),
		BaseDir:       baseDir,
		RunsSummary:   runsSummary,
		WatchRunIndex: watchIdx,
		WatchLabel:    watchLabel,
		AgentRead:     agentRead,
		Suggestions:   suggestions,
		Cards:         BuildPlaytestMetricCards(rep),
		Chart:         SelectPlaytestChart(rep),
	}, nil
}

// OpenPlaytestReportFile reads a YAML playtest report from disk and opens it in the Playtest Report screen.
// Relative paths are resolved against the open project's root directory and guarded against path traversal.
func (e *Editor) OpenPlaytestReportFile(reportPath string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	raw := strings.TrimSpace(reportPath)
	if raw == "" {
		raw = playtest.ReportFileName
	}
	absPath, relPath, baseDir, err := e.resolvePlaytestPath(raw, "", true)
	if err != nil {
		return err
	}
	rep, err := playtest.LoadReportFile(absPath)
	if err != nil {
		return fmt.Errorf("editor: open playtest report: %w", err)
	}
	return e.OpenPlaytestReport(PlaytestReportOpenConfig{
		Report:     rep,
		ReportPath: relPath,
		ConfigPath: playtest.FileName,
		BaseDir:    baseDir,
	})
}

func isWithinPlaytestDir(base, target string) (string, bool) {
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// evalPlaytestPathSymlinks resolves symlinks in target, walking up to the nearest existing
// ancestor when target does not exist yet so symlinked parent directories cannot escape baseDir.
func evalPlaytestPathSymlinks(target string) (string, error) {
	clean := filepath.Clean(target)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		return resolved, nil
	}
	var tail []string
	cur := clean
	for {
		parent := filepath.Dir(cur)
		if parent == cur {
			return clean, nil
		}
		tail = append(tail, filepath.Base(cur))
		cur = parent
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			parts := make([]string, 0, 1+len(tail))
			parts = append(parts, resolved)
			for i := len(tail) - 1; i >= 0; i-- {
				parts = append(parts, tail[i])
			}
			return filepath.Join(parts...), nil
		}
	}
}

func (e *Editor) resolvePlaytestPath(p, fallbackBase string, allowStandaloneAbs bool) (absPath, relPath, baseDir string, err error) {
	baseDir = strings.TrimSpace(fallbackBase)
	if baseDir == "" {
		baseDir = e.playtestProjectDir()
	}
	if filepath.IsAbs(p) {
		clean := filepath.Clean(p)
		if baseDir != "" {
			resolvedBase, berr := evalPlaytestPathSymlinks(baseDir)
			if berr != nil {
				return "", "", "", fmt.Errorf("editor: resolve base dir %q: %w", baseDir, berr)
			}
			resolvedTarget, terr := evalPlaytestPathSymlinks(clean)
			if terr != nil {
				return "", "", "", fmt.Errorf("editor: resolve path %q: %w", p, terr)
			}
			rel, ok := isWithinPlaytestDir(resolvedBase, resolvedTarget)
			if !ok {
				return "", "", "", fmt.Errorf("editor: path %q leaves the project directory", p)
			}
			return clean, filepath.ToSlash(rel), baseDir, nil
		}
		if !allowStandaloneAbs {
			return "", "", "", fmt.Errorf("editor: absolute path %q is not allowed without a project base directory", p)
		}
		return clean, filepath.ToSlash(clean), filepath.Dir(clean), nil
	}
	if baseDir == "" {
		return "", "", "", fmt.Errorf("editor: cannot resolve relative path %q without an open project", p)
	}
	slashed := path.Clean(strings.ReplaceAll(p, `\`, "/"))
	if slashed == "." || slashed == ".." || strings.HasPrefix(slashed, "../") || strings.HasPrefix(slashed, "/") {
		return "", "", "", fmt.Errorf("editor: path %q leaves the project directory", p)
	}
	joined := filepath.Join(baseDir, filepath.FromSlash(slashed))
	// Reject symlinks escaping baseDir for both existing files and non-existent files under symlinked directories.
	resolvedBase, berr := evalPlaytestPathSymlinks(baseDir)
	if berr != nil {
		return "", "", "", fmt.Errorf("editor: resolve base dir %q: %w", baseDir, berr)
	}
	resolvedTarget, terr := evalPlaytestPathSymlinks(joined)
	if terr != nil {
		return "", "", "", fmt.Errorf("editor: resolve path %q: %w", p, terr)
	}
	if _, ok := isWithinPlaytestDir(resolvedBase, resolvedTarget); !ok {
		return "", "", "", fmt.Errorf("editor: path %q escapes the project directory via symlink", p)
	}
	return joined, slashed, baseDir, nil
}

// SelectPlaytestReportIndex switches the active entry in the Playtest Report session (used by the BOT PROFILE switcher when multiple reports are loaded).
func (e *Editor) SelectPlaytestReportIndex(idx int) {
	if e == nil || e.playtest.session == nil {
		return
	}
	if idx < 0 || idx >= len(e.playtest.session.Entries) || e.playtest.session.ActiveIndex == idx {
		return
	}
	e.playtest.session.ActiveIndex = idx
	e.playtest.session.ConfigPreviewOpen = false
	e.playtest.rev++
	e.notify()
}

// SetPlaytestAgentRead replaces the Agent's read text and actionable suggestions on the active playtest report entry.
func (e *Editor) SetPlaytestAgentRead(read string, suggestions []string) error {
	if e == nil || e.playtest.session == nil || e.playtest.session.ActiveEntry() == nil {
		return errors.New("editor: no playtest report is loaded")
	}
	entry := e.playtest.session.ActiveEntry()
	if strings.TrimSpace(read) != "" {
		entry.AgentRead = strings.TrimSpace(read)
	}
	if suggestions != nil {
		out := make([]PlaytestSuggestion, 0, len(suggestions))
		for i, text := range suggestions {
			if strings.TrimSpace(text) == "" {
				continue
			}
			out = append(out, PlaytestSuggestion{
				ID:   fmt.Sprintf("suggestion-%d", i+1),
				Text: strings.TrimSpace(text),
			})
		}
		entry.Suggestions = out
	}
	e.playtest.rev++
	e.notify()
	return nil
}

// AddPlaytestProposal adds (or updates by ID) a pending metric proposal from playtest_define so it appears as a banner with Review on the Playtest Report screen.
func (e *Editor) AddPlaytestProposal(p PlaytestProposal) string {
	if e == nil {
		return ""
	}
	if e.playtest.session == nil {
		e.playtest.session = &PlaytestReportSession{}
	}
	if p.ID == "" {
		e.playtest.nextPropID++
		p.ID = fmt.Sprintf("playtest-proposal-%d", e.playtest.nextPropID)
	}
	if p.Status == "" {
		p.Status = "pending"
	}
	if p.File == "" {
		p.File = playtest.FileName
	}
	for i := range e.playtest.session.Proposals {
		if e.playtest.session.Proposals[i].ID == p.ID {
			e.playtest.session.Proposals[i] = p
			e.playtest.rev++
			e.notify()
			return p.ID
		}
	}
	e.playtest.session.Proposals = append(e.playtest.session.Proposals, p)
	e.playtest.rev++
	e.notify()
	return p.ID
}

// OpenPlaytestProposalReview opens the inline review panel for the proposed playtest_define metric with ID id.
func (e *Editor) OpenPlaytestProposalReview(id string) {
	if e == nil || e.playtest.session == nil {
		return
	}
	if p := e.playtest.session.ProposalByID(id); p != nil {
		e.playtest.session.ReviewProposalID = p.ID
		e.playtest.session.ConfigPreviewOpen = false
		e.playtest.rev++
		e.notify()
	}
}

// ClosePlaytestProposalReview closes the inline proposal review drawer.
func (e *Editor) ClosePlaytestProposalReview() {
	if e == nil || e.playtest.session == nil || e.playtest.session.ReviewProposalID == "" {
		return
	}
	e.playtest.session.ReviewProposalID = ""
	e.playtest.rev++
	e.notify()
}

// ApplyPlaytestProposal applies a pending playtest_define proposal (invoking ApplyFunc if provided, or writing After to playtest.yaml after verifying Before matches disk).
func (e *Editor) ApplyPlaytestProposal(id string) error {
	if e == nil || e.playtest.session == nil {
		return errors.New("editor: no playtest session")
	}
	if e.playing {
		return ErrPlaying
	}
	p := e.playtest.session.ProposalByID(id)
	if p == nil {
		return fmt.Errorf("editor: unknown playtest proposal %q", id)
	}
	if p.Status != "pending" {
		return fmt.Errorf("editor: playtest proposal %q is %s, not pending", id, p.Status)
	}
	if p.ApplyFunc != nil {
		if err := p.ApplyFunc(); err != nil {
			return err
		}
	} else if p.After != "" {
		base := ""
		if ent := e.playtest.session.ActiveEntry(); ent != nil {
			base = ent.BaseDir
		}
		absPath, _, _, err := e.resolvePlaytestPath(p.File, base, false)
		if err != nil {
			return err
		}
		if p.Before != "" {
			cur, rerr := os.ReadFile(absPath)
			if rerr != nil {
				return fmt.Errorf("editor: read %s: %w", p.File, rerr)
			}
			if string(cur) != p.Before {
				return fmt.Errorf("editor: %s changed on disk since proposal %s was created", p.File, id)
			}
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("editor: prepare dir for %s: %w", p.File, err)
		}
		if err := writeFileAtomic(absPath, []byte(p.After)); err != nil {
			return fmt.Errorf("editor: write %s: %w", p.File, err)
		}
	}
	p.Status = "applied"
	if e.playtest.session.ReviewProposalID == id {
		e.playtest.session.ReviewProposalID = ""
	}
	e.playtest.session.Notice = fmt.Sprintf("Applied metric %q to %s", p.MetricName, p.File)
	e.Status = e.playtest.session.Notice
	e.playtest.rev++
	e.notify()
	return nil
}

// DismissPlaytestProposal dismisses a pending playtest_define proposal without modifying playtest.yaml.
func (e *Editor) DismissPlaytestProposal(id string) error {
	if e == nil || e.playtest.session == nil {
		return errors.New("editor: no playtest session")
	}
	if e.playing {
		return ErrPlaying
	}
	p := e.playtest.session.ProposalByID(id)
	if p == nil {
		return fmt.Errorf("editor: unknown playtest proposal %q", id)
	}
	if p.Status != "pending" {
		return fmt.Errorf("editor: playtest proposal %q is %s, not pending", id, p.Status)
	}
	if p.DismissFunc != nil {
		if err := p.DismissFunc(); err != nil {
			return err
		}
	}
	p.Status = "dismissed"
	if e.playtest.session.ReviewProposalID == id {
		e.playtest.session.ReviewProposalID = ""
	}
	e.playtest.session.Notice = fmt.Sprintf("Dismissed proposed metric %q", p.MetricName)
	e.Status = e.playtest.session.Notice
	e.playtest.rev++
	e.notify()
	return nil
}

// OpenPlaytestConfigPreview invokes Editor.OnOpenPlaytestConfig if set, or loads the active report's playtest.yaml into the inline preview drawer.
func (e *Editor) OpenPlaytestConfigPreview() error {
	if e == nil || e.playtest.session == nil || e.playtest.session.ActiveEntry() == nil {
		return errors.New("editor: no playtest report is loaded")
	}
	entry := e.playtest.session.ActiveEntry()
	cfgFile := entry.ConfigPath
	if cfgFile == "" {
		cfgFile = playtest.FileName
	}
	if e.OnOpenPlaytestConfig != nil {
		return e.OnOpenPlaytestConfig(cfgFile)
	}
	if e.Project != nil {
		e.SelectFile(cfgFile)
	}
	e.playtest.session.ReviewProposalID = ""
	e.playtest.session.ConfigPreviewOpen = true
	e.playtest.session.ConfigPreviewPath = cfgFile
	absPath, _, _, err := e.resolvePlaytestPath(cfgFile, entry.BaseDir, false)
	if err != nil {
		e.playtest.session.ConfigPreviewText = ""
		e.playtest.session.ConfigPreviewErr = err.Error()
		e.playtest.rev++
		e.notify()
		return nil
	}
	data, rerr := os.ReadFile(absPath)
	if rerr != nil {
		e.playtest.session.ConfigPreviewText = ""
		e.playtest.session.ConfigPreviewErr = fmt.Sprintf("Could not read %s: %v", cfgFile, rerr)
	} else {
		e.playtest.session.ConfigPreviewText = string(data)
		e.playtest.session.ConfigPreviewErr = ""
	}
	e.playtest.rev++
	e.notify()
	return nil
}

// ClosePlaytestConfigPreview closes the inline playtest.yaml viewer drawer.
func (e *Editor) ClosePlaytestConfigPreview() {
	if e == nil || e.playtest.session == nil || !e.playtest.session.ConfigPreviewOpen {
		return
	}
	e.playtest.session.ConfigPreviewOpen = false
	e.playtest.rev++
	e.notify()
}

// AskAgentToTunePlaytest creates and dispatches a tuning request for the active playtest report's missed goals.
func (e *Editor) AskAgentToTunePlaytest() PlaytestPromptRequest {
	if e == nil || e.playtest.session == nil || e.playtest.session.ActiveEntry() == nil {
		return PlaytestPromptRequest{}
	}
	entry := e.playtest.session.ActiveEntry()
	rep := entry.Report
	var missedNames []string
	var missedDesc []string
	for _, m := range rep.MissedMetrics() {
		name := m.Label
		if name == "" {
			name = m.Name
		}
		missedNames = append(missedNames, m.Name)
		if m.Goal != nil {
			missedDesc = append(missedDesc, fmt.Sprintf("%s (%s, goal %s)", name, formatMetricValue(m, rep.SeedCount), m.Goal.Describe(m.Unit)))
		} else {
			missedDesc = append(missedDesc, fmt.Sprintf("%s (%s)", name, formatMetricValue(m, rep.SeedCount)))
		}
	}
	prompt := fmt.Sprintf("Tune %s based on the latest playtest report", rep.Scope.Scene)
	if len(missedDesc) > 0 {
		prompt = fmt.Sprintf("Tune %s to meet missed playtest goals: %s. Return changes as a review.", rep.Scope.Scene, strings.Join(missedDesc, "; "))
	}
	req := PlaytestPromptRequest{
		Kind:          "tune",
		Prompt:        prompt,
		Project:       rep.Scope.Project,
		Scope:         rep.Scope.Scene,
		ReportPath:    entry.ReportPath,
		ConfigPath:    entry.ConfigPath,
		MissedMetrics: missedNames,
	}
	e.playtest.session.LastPrompt = req
	e.playtest.session.Notice = "Asked the agent to tune it · changes will arrive as a review"
	e.Status = e.playtest.session.Notice
	if e.OnPlaytestPrompt != nil {
		e.OnPlaytestPrompt(req)
	}
	e.playtest.rev++
	e.notify()
	return req
}

// RequestPlaytestSuggestion sends suggestion index i from Agent's read as an agent request that comes back as a review.
func (e *Editor) RequestPlaytestSuggestion(index int) (PlaytestPromptRequest, error) {
	if e == nil || e.playtest.session == nil || e.playtest.session.ActiveEntry() == nil {
		return PlaytestPromptRequest{}, errors.New("editor: no playtest report is loaded")
	}
	entry := e.playtest.session.ActiveEntry()
	if index < 0 || index >= len(entry.Suggestions) {
		return PlaytestPromptRequest{}, fmt.Errorf("editor: playtest suggestion index %d out of range", index)
	}
	sg := &entry.Suggestions[index]
	rep := entry.Report
	var missed []string
	for _, m := range rep.MissedMetrics() {
		missed = append(missed, m.Name)
	}
	req := PlaytestPromptRequest{
		Kind:            "suggestion",
		Prompt:          fmt.Sprintf("Apply playtest suggestion on %s: %s. Return changes as a review.", rep.Scope.Scene, sg.Text),
		Project:         rep.Scope.Project,
		Scope:           rep.Scope.Scene,
		ReportPath:      entry.ReportPath,
		ConfigPath:      entry.ConfigPath,
		SuggestionID:    sg.ID,
		SuggestionIndex: index,
		MissedMetrics:   missed,
	}
	sg.Requested = true
	sg.ReviewStatus = "pending_review"
	e.playtest.session.LastPrompt = req
	e.playtest.session.Notice = fmt.Sprintf("Requested review: %s", sg.Text)
	e.Status = e.playtest.session.Notice
	if e.OnPlaytestPrompt != nil {
		e.OnPlaytestPrompt(req)
	}
	e.playtest.rev++
	e.notify()
	return req, nil
}

// WatchPlaytestRun opens the specified 1-based run's .replay.yaml recording in Play mode using the editor's attached core.Scope.
// When runIndex <= 0, the active entry's default WatchRunIndex is used.
func (e *Editor) WatchPlaytestRun(runIndex int) error {
	return e.WatchPlaytestRunIn(e.scope, runIndex)
}

// WatchPlaytestRunIn opens the specified 1-based run's .replay.yaml recording in Play mode on cx.
func (e *Editor) WatchPlaytestRunIn(cx *core.Scope, runIndex int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playtest.session == nil || e.playtest.session.ActiveEntry() == nil {
		return errors.New("editor: no playtest report is loaded")
	}
	entry := e.playtest.session.ActiveEntry()
	if runIndex <= 0 {
		runIndex = entry.WatchRunIndex
	}
	run, ok := findPlaytestRun(entry.Report, runIndex)
	if !ok {
		return fmt.Errorf("editor: playtest report has no run %d", runIndex)
	}
	if strings.TrimSpace(run.Recording) == "" {
		return fmt.Errorf("editor: run %d has no saved replay recording", run.Index)
	}
	absPath, relPath, _, err := e.resolvePlaytestPath(run.Recording, entry.BaseDir, false)
	if err != nil {
		return fmt.Errorf("editor: resolve run %d recording: %w", run.Index, err)
	}
	rec, err := replay.LoadFile(absPath)
	if err != nil {
		return fmt.Errorf("editor: load run %d recording %q: %w", run.Index, run.Recording, err)
	}
	return e.PlayReplayIn(cx, rec, relPath, run.Index)
}

func findPlaytestRun(rep *playtest.Report, runIndex int) (playtest.RunReport, bool) {
	if rep == nil || len(rep.Runs) == 0 {
		return playtest.RunReport{}, false
	}
	for _, r := range rep.Runs {
		if r.Index == runIndex {
			return r, true
		}
	}
	if runIndex >= 1 && runIndex <= len(rep.Runs) {
		return rep.Runs[runIndex-1], true
	}
	return playtest.RunReport{}, false
}

// DefaultWatchRunIndex picks the most informative 1-based run index for the "Watch run N" button:
// first non-finished run with a saved recording, then first run with a recording, then first non-finished run, then the first run.
func DefaultWatchRunIndex(rep *playtest.Report) int {
	if rep == nil || len(rep.Runs) == 0 {
		return 0
	}
	for _, r := range rep.Runs {
		if r.Outcome != playtest.OutcomeFinished && strings.TrimSpace(r.Recording) != "" {
			return r.Index
		}
	}
	for _, r := range rep.Runs {
		if strings.TrimSpace(r.Recording) != "" {
			return r.Index
		}
	}
	for _, r := range rep.Runs {
		if r.Outcome != playtest.OutcomeFinished {
			return r.Index
		}
	}
	return rep.Runs[0].Index
}

// BotProfileDisplayName maps a registered playtest bot profile ID to its human-readable label matching Playtest.dc.html.
func BotProfileDisplayName(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "platformer-runner", "platformer":
		return "Platformer runner"
	case "topdown-explorer", "explorer":
		return "Top-down explorer"
	case "shooter":
		return "Shooter"
	case "puzzle-solver", "puzzle":
		return "Puzzle solver"
	case "card-ai", "cards":
		return "Card AI"
	case "":
		return "Custom bot"
	default:
		parts := strings.FieldsFunc(profile, func(r rune) bool { return r == '-' || r == '_' })
		for i, p := range parts {
			if len(p) > 0 {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, " ")
	}
}

// BotProfileDefaultStrategy returns a concise one-sentence strategy fallback when report.Bot.Strategy is empty.
func BotProfileDefaultStrategy(bot playtest.BotConfig) string {
	if s := strings.TrimSpace(bot.Strategy); s != "" {
		return s
	}
	switch strings.ToLower(strings.TrimSpace(bot.Profile)) {
	case "platformer-runner":
		jitter := formatReactionRange(bot.ReactionMin, bot.ReactionMax)
		if jitter != "" {
			return fmt.Sprintf("Greedy route to the door, jumps at ledges and gaps, %s human reaction jitter", jitter)
		}
		return "Greedy route to the door, jumps at ledges and gaps across seeded runs"
	case "topdown-explorer":
		return "Walks to the nearest unseen tile, talks to every NPC once, opens every chest"
	case "shooter":
		jitter := formatReactionRange(bot.ReactionMin, bot.ReactionMax)
		if jitter != "" {
			return fmt.Sprintf("Dodges the nearest bullet with %s reaction, fires constantly, collects reachable power-ups", jitter)
		}
		return "Dodges the nearest bullet, fires constantly, and collects nearby power-ups"
	case "puzzle-solver":
		return "Breadth-first search for the shortest solution plus a human-like player that undoes after bad moves"
	case "card-ai":
		return "Two greedy AIs that look one turn ahead play each deck pair, taking turns to go first"
	default:
		return fmt.Sprintf("Headless bot running profile %q across deterministic seeds", bot.Profile)
	}
}

func formatReactionRange(minD, maxD time.Duration) string {
	if minD <= 0 && maxD <= 0 {
		return ""
	}
	if maxD <= 0 || minD == maxD {
		return fmt.Sprintf("%d ms", minD.Milliseconds())
	}
	return fmt.Sprintf("%d–%d ms", minD.Milliseconds(), maxD.Milliseconds())
}

// FormatPlaytestRunsSummary builds the header runs summary such as "20 headless runs by the agent · 1 m 40 s".
func FormatPlaytestRunsSummary(rep *playtest.Report) string {
	if rep == nil {
		return ""
	}
	count := rep.SeedCount
	if count == 0 {
		count = len(rep.Runs)
	}
	var totalSec float64
	for _, r := range rep.Runs {
		totalSec += max(0.0, r.DurationSeconds)
	}
	noun := "runs"
	if count == 1 {
		noun = "run"
	}
	head := fmt.Sprintf("%d headless %s by the agent", count, noun)
	if rep.Speed > 1 {
		head = fmt.Sprintf("%d headless %s at %s× speed", count, noun, strconv.FormatFloat(rep.Speed, 'f', -1, 64))
	}
	if totalSec > 0 {
		return head + " · " + formatCompactDuration(totalSec)
	}
	return head
}

func formatCompactDuration(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(math.Round(sec))
	if total >= 60 {
		m := total / 60
		s := total % 60
		return fmt.Sprintf("%d m %02d s", m, s)
	}
	if sec < 10 && math.Abs(sec-math.Round(sec)) >= 0.05 {
		return fmt.Sprintf("%.1f s", sec)
	}
	return fmt.Sprintf("%d s", total)
}

// BuildPlaytestMetricCards converts report.Metrics into display-ready metric cards with Met/Missed badges, foot lines, goals, and source files.
func BuildPlaytestMetricCards(rep *playtest.Report) []PlaytestMetricCard {
	if rep == nil || len(rep.Metrics) == 0 {
		return nil
	}
	cards := make([]PlaytestMetricCard, 0, len(rep.Metrics))
	for _, m := range rep.Metrics {
		// Skip pure heat-map event counters when there are already 4 goal-bearing metrics, so the 4-card top row stays focused on goal cards like Playtest.dc.html.
		if m.Goal == nil && m.Status == playtest.StatusInfo && len(rep.Metrics) > 4 && hasHeatMapForMetric(rep, m.Name) {
			continue
		}
		label := strings.TrimSpace(m.Label)
		if label == "" {
			label = m.Name
		}
		badge := "Info"
		met := false
		missed := false
		switch m.Status {
		case playtest.StatusMet:
			badge = "Met"
			met = true
		case playtest.StatusMissed:
			badge = "Missed"
			missed = true
		}
		goalText := "No goal set"
		source := playtest.FileName
		if m.Goal != nil {
			goalText = "Goal · " + m.Goal.Describe(m.Unit)
			source = m.Goal.Source
		}
		cards = append(cards, PlaytestMetricCard{
			Name:      m.Name,
			Label:     label,
			ValueText: formatMetricValue(m, rep.SeedCount),
			Foot:      formatMetricFoot(m, rep),
			GoalText:  goalText,
			Source:    source,
			Status:    m.Status,
			Badge:     badge,
			Met:       met,
			Missed:    missed,
		})
	}
	return cards
}

func hasHeatMapForMetric(rep *playtest.Report, metricName string) bool {
	for _, hm := range rep.HeatMaps {
		if hm.Metric == metricName {
			return true
		}
	}
	return false
}

func formatMetricValue(m playtest.MetricReport, seedCount int) string {
	unit := strings.TrimSpace(m.Unit)
	if unit == "" && m.Aggregate == playtest.AggCount && seedCount > 0 && m.Value >= 0 && m.Value <= float64(seedCount) && isOutcomeMetricName(m.Name) {
		return fmt.Sprintf("%s / %d", formatCleanNumber(m.Value, 0), seedCount)
	}
	if unit == "%" {
		return formatCleanNumber(m.Value, 1) + "%"
	}
	if unit == "s" {
		if m.Value >= 60 {
			return formatCompactDuration(m.Value)
		}
		return formatCleanNumber(m.Value, 1) + " s"
	}
	if unit == "ms" {
		return formatCleanNumber(m.Value, 1) + " ms"
	}
	if unit != "" {
		return formatCleanNumber(m.Value, 1) + " " + unit
	}
	return formatCleanNumber(m.Value, 1)
}

func isOutcomeMetricName(name string) bool {
	low := strings.ToLower(name)
	return low == "finished" || low == "cleared" || low == "solvable" || strings.Contains(low, "reach") || strings.Contains(low, "clear")
}

func formatMetricFoot(m playtest.MetricReport, rep *playtest.Report) string {
	if rep == nil {
		return ""
	}
	if m.Aggregate == playtest.AggCount && isOutcomeMetricName(m.Name) && len(rep.Runs) > 0 {
		failed := 0
		timedOut := 0
		reasonCount := map[string]int{}
		for _, r := range rep.Runs {
			if r.Outcome == playtest.OutcomeFailed {
				failed++
				if r.Reason != "" {
					reasonCount[r.Reason]++
				}
			} else if r.Outcome == playtest.OutcomeTimeout {
				timedOut++
			}
		}
		if failed > 0 {
			topReason := mostFrequentKey(reasonCount)
			if topReason != "" {
				return fmt.Sprintf("%d failed · %s", failed, topReason)
			}
			return fmt.Sprintf("%d failed · %d finished", failed, int(math.Round(m.Value)))
		}
		if timedOut > 0 {
			return fmt.Sprintf("%d timed out", timedOut)
		}
		return fmt.Sprintf("all %d runs succeeded", len(rep.Runs))
	}
	if m.Unit == "ms" {
		var p99Max, allocMax float64
		hasPerf := false
		for _, r := range rep.Runs {
			if r.Perf != nil {
				hasPerf = true
				if r.Perf.P99FrameMs > p99Max {
					p99Max = r.Perf.P99FrameMs
				}
				if r.Perf.AllocsPerFrame > allocMax {
					allocMax = r.Perf.AllocsPerFrame
				}
			}
		}
		if hasPerf && p99Max > 0 {
			return fmt.Sprintf("p99 %s ms · %s allocs/frame", formatCleanNumber(p99Max, 1), formatCleanNumber(allocMax, 1))
		}
	}
	if m.Unit == "s" && (m.Min > 0 || m.Max > 0) && m.Max >= m.Min {
		return fmt.Sprintf("fastest %s · slowest %s", formatCompactDuration(m.Min), formatCompactDuration(m.Max))
	}
	if m.Min != m.Max && (m.Min != 0 || m.Max != 0) {
		unitSuffix := ""
		if m.Unit == "%" {
			unitSuffix = "%"
		} else if m.Unit != "" {
			unitSuffix = " " + m.Unit
		}
		return fmt.Sprintf("min %s%s · max %s%s", formatCleanNumber(m.Min, 1), unitSuffix, formatCleanNumber(m.Max, 1), unitSuffix)
	}
	n := max(1, rep.SeedCount)
	if n == 1 {
		return fmt.Sprintf("%s across 1 run", m.Aggregate)
	}
	return fmt.Sprintf("%s across %d runs", m.Aggregate, n)
}

func formatCleanNumber(v float64, maxDec int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	if v == 0 {
		return "0"
	}
	if maxDec <= 0 || math.Abs(v-math.Round(v)) < 1e-6 {
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', maxDec, 64), "0"), ".")
}

func mostFrequentKey(counts map[string]int) string {
	bestKey := ""
	bestVal := 0
	for k, v := range counts {
		if v > bestVal || (v == bestVal && (bestKey == "" || k < bestKey)) {
			bestKey = k
			bestVal = v
		}
	}
	return bestKey
}

// SelectPlaytestChart chooses the single best chart visualization for rep from its metrics and data:
//  1. Win-rate matrix (PlaytestChartMatrix) when Matrices is non-empty,
//  2. Spatial level heat map (PlaytestChartHeatMap) when HeatMaps is non-empty,
//  3. Bars per wave against a goal line (PlaytestChartBars) or line per level (PlaytestChartLine) when Series is non-empty,
//  4. Fallback to positioned run events (heat map) or per-run progression (line).
func SelectPlaytestChart(rep *playtest.Report) PlaytestChartSpec {
	if rep == nil {
		return PlaytestChartSpec{
			Kind:     PlaytestChartBars,
			Title:    "No playtest report loaded",
			Subtitle: "Run a playtest or open playtest.report.yaml",
		}
	}
	if len(rep.Matrices) > 0 {
		return buildMatrixChartSpec(rep, rep.Matrices[0])
	}
	if len(rep.HeatMaps) > 0 {
		return buildHeatMapChartSpec(rep, rep.HeatMaps[0])
	}
	if len(rep.Series) > 0 {
		s := rep.Series[0]
		if shouldRenderSeriesAsLine(s, rep) {
			return buildLineChartSpec(rep, s)
		}
		return buildBarChartSpec(rep, s)
	}

	// Synthesize heat map from positioned run events if present.
	var pts []playtest.HeatPoint
	eventKind := ""
	for _, run := range rep.Runs {
		for _, ev := range run.Events {
			if eventKind == "" {
				eventKind = ev.Kind
			}
			if ev.Kind == eventKind {
				pts = append(pts, playtest.HeatPoint{
					Seed: run.Seed,
					Step: ev.Step,
					X:    ev.X,
					Y:    ev.Y,
					Tag:  ev.Tag,
				})
			}
		}
	}
	if len(pts) > 0 {
		return buildHeatMapChartSpec(rep, playtest.HeatMap{
			Metric: eventKind,
			Event:  eventKind,
			Points: pts,
		})
	}

	// Fallback: line chart of per-run durations so every report still renders a meaningful chart.
	entries := make([]playtest.SeriesEntry, 0, len(rep.Runs))
	for _, run := range rep.Runs {
		flag := ""
		if run.Outcome != playtest.OutcomeFinished {
			flag = playtest.FlagProblem
		}
		entries = append(entries, playtest.SeriesEntry{
			Label: fmt.Sprintf("#%d", run.Index),
			Value: run.DurationSeconds,
			Flag:  flag,
			Note:  run.Reason,
		})
	}
	return buildLineChartSpec(rep, playtest.SeriesChart{
		Name:    "run_duration",
		Label:   "Run duration per seed",
		Unit:    "s",
		Entries: entries,
	})
}

func shouldRenderSeriesAsLine(s playtest.SeriesChart, rep *playtest.Report) bool {
	if s.TargetMax != nil {
		// Bars against a goal line (e.g. hits per wave with goal <= 1).
		return false
	}
	lowName := strings.ToLower(s.Name + " " + s.Label)
	if strings.Contains(lowName, "wave") {
		return false
	}
	if strings.Contains(lowName, "level") || strings.Contains(lowName, "solution") || strings.Contains(lowName, "difficulty") || strings.Contains(lowName, "curve") || strings.Contains(lowName, "line") || strings.Contains(lowName, "progression") {
		return true
	}
	if rep != nil && strings.ToLower(strings.TrimSpace(rep.Bot.Profile)) == "puzzle-solver" {
		return true
	}
	if len(s.Entries) > 0 {
		first := strings.ToLower(strings.TrimSpace(s.Entries[0].Label))
		if strings.HasPrefix(first, "level") || strings.HasPrefix(first, "lvl") || first == "01" || first == "1" {
			return true
		}
	}
	return false
}

func buildHeatMapChartSpec(rep *playtest.Report, hm playtest.HeatMap) PlaytestChartSpec {
	evLow := strings.ToLower(hm.Event + " " + hm.Metric)
	isTime := strings.Contains(evLow, "tile") || strings.Contains(evLow, "time") || strings.Contains(evLow, "visit")

	title := "Where players died"
	sub := fmt.Sprintf("heat over the level · %d runs", max(1, rep.SeedCount))
	if isTime {
		title = "Where the explorer spent its time"
		sub = fmt.Sprintf("time per tile · %d runs", max(1, rep.SeedCount))
	} else if hm.Event != "" && !strings.Contains(evLow, "death") {
		title = fmt.Sprintf("Heat map · %s", hm.Event)
	}

	pts := make([]playtest.HeatPoint, 0, len(hm.Points))
	for _, pt := range hm.Points {
		if isFinite64(pt.X) && isFinite64(pt.Y) {
			pts = append(pts, pt)
		}
	}

	minX, maxX := 0.0, 640.0
	minY, maxY := 0.0, 360.0
	if len(pts) > 0 {
		minX, maxX = pts[0].X, pts[0].X
		minY, maxY = pts[0].Y, pts[0].Y
		for _, pt := range pts[1:] {
			if pt.X < minX {
				minX = pt.X
			}
			if pt.X > maxX {
				maxX = pt.X
			}
			if pt.Y < minY {
				minY = pt.Y
			}
			if pt.Y > maxY {
				maxY = pt.Y
			}
		}
		padX := max(48.0, (maxX-minX)*0.22)
		padY := max(36.0, (maxY-minY)*0.28)
		minX = max(0.0, minX-padX)
		maxX += padX
		minY = max(0.0, minY-padY)
		maxY += padY
		if maxX-minX < 160 {
			mid := (minX + maxX) / 2
			minX, maxX = max(0.0, mid-160), mid+160
		}
		if maxY-minY < 100 {
			mid := (minY + maxY) / 2
			minY, maxY = max(0.0, mid-90), mid+90
		}
	}

	clusters := clusterHeatPoints(pts, maxX-minX, maxY-minY)
	chips := make([]PlaytestHeatChip, 0, min(3, len(clusters)))
	for i := 0; i < len(clusters) && i < 3; i++ {
		cl := clusters[i]
		var text string
		if isTime {
			pct := int(math.Round(cl.Share * 100))
			if cl.Tag != "" {
				text = fmt.Sprintf("%s · %d%% of visits", cl.Tag, pct)
			} else {
				text = fmt.Sprintf("%d%% of tile visits", pct)
			}
		} else {
			noun := hm.Event
			if noun == "" {
				noun = "event"
			}
			if cl.Count != 1 {
				noun += "s"
			}
			if cl.Tag != "" {
				text = fmt.Sprintf("%d %s · %s", cl.Count, noun, cl.Tag)
			} else {
				text = fmt.Sprintf("%d %s at (%.0f, %.0f)", cl.Count, noun, cl.X, cl.Y)
			}
		}
		chips = append(chips, PlaytestHeatChip{
			X:       cl.X,
			Y:       cl.Y,
			Text:    text,
			Problem: cl.Problem,
		})
	}

	return PlaytestChartSpec{
		Kind:     PlaytestChartHeatMap,
		Title:    title,
		Subtitle: sub,
		HeatMap: &PlaytestHeatChart{
			Metric:        hm.Metric,
			Event:         hm.Event,
			IsTimePerTile: isTime,
			Points:        pts,
			Clusters:      clusters,
			Chips:         chips,
			MinX:          minX,
			MaxX:          maxX,
			MinY:          minY,
			MaxY:          maxY,
		},
	}
}

func clusterHeatPoints(pts []playtest.HeatPoint, spanX, spanY float64) []PlaytestHeatCluster {
	if len(pts) == 0 {
		return nil
	}
	if !isFinite64(spanX) || spanX < 0 {
		spanX = 0
	}
	if !isFinite64(spanY) || spanY < 0 {
		spanY = 0
	}
	radius := max(24.0, math.Hypot(spanX, spanY)*0.09)
	type accum struct {
		sumX, sumY float64
		count      int
		tags       map[string]int
	}
	var groups []*accum
	validCount := 0
	for _, pt := range pts {
		if !isFinite64(pt.X) || !isFinite64(pt.Y) {
			continue
		}
		validCount++
		var best *accum
		bestDist := radius
		for _, g := range groups {
			cx := g.sumX / float64(g.count)
			cy := g.sumY / float64(g.count)
			if d := math.Hypot(pt.X-cx, pt.Y-cy); d <= bestDist {
				best = g
				bestDist = d
			}
		}
		if best == nil {
			best = &accum{tags: map[string]int{}}
			groups = append(groups, best)
		}
		best.sumX += pt.X
		best.sumY += pt.Y
		best.count++
		if pt.Tag != "" {
			best.tags[pt.Tag]++
		}
	}
	if validCount == 0 {
		return nil
	}

	total := float64(validCount)
	out := make([]PlaytestHeatCluster, 0, len(groups))
	for _, g := range groups {
		share := float64(g.count) / total
		out = append(out, PlaytestHeatCluster{
			X:     g.sumX / float64(g.count),
			Y:     g.sumY / float64(g.count),
			Count: g.count,
			Share: share,
			Tag:   mostFrequentKey(g.tags),
		})
	}
	slices.SortFunc(out, func(a, b PlaytestHeatCluster) int {
		if a.Count != b.Count {
			if a.Count > b.Count {
				return -1
			}
			return 1
		}
		if a.X < b.X {
			return -1
		}
		if a.X > b.X {
			return 1
		}
		return 0
	})
	if len(out) > 0 {
		out[0].Problem = true
		for i := 1; i < len(out); i++ {
			if out[i].Share >= 0.35 && out[i].Count >= 3 {
				out[i].Problem = true
			}
		}
	}
	return out
}

func buildBarChartSpec(rep *playtest.Report, s playtest.SeriesChart) PlaytestChartSpec {
	title := strings.TrimSpace(s.Label)
	if title == "" {
		title = s.Name
	}
	agg := s.Aggregate
	if agg == "" {
		agg = "median"
	}
	sub := fmt.Sprintf("%s per run · %d runs", agg, max(1, rep.SeedCount))

	entries := slices.Clone(s.Entries)
	maxVal := 0.0
	hasFractional := false
	for i := range entries {
		v := entries[i].Value
		if !isFinite64(v) {
			entries[i].Value = 0
			continue
		}
		if v > maxVal {
			maxVal = v
		}
		if math.Abs(v-math.Round(v)) >= 0.01 {
			hasFractional = true
		}
	}
	var targetMax *float64
	var targetText string
	if s.TargetMax != nil && isFinite64(*s.TargetMax) {
		tmVal := *s.TargetMax
		targetMax = &tmVal
		if tmVal > maxVal {
			maxVal = tmVal
		}
		targetText = fmt.Sprintf("goal <= %s", formatCleanNumber(tmVal, 1))
		if s.Unit != "" {
			targetText += " " + s.Unit
		}
		// Append source if any metric references brief.yaml/playtest.yaml.
		for _, m := range rep.Metrics {
			if m.Goal != nil && m.Goal.Source != "" {
				targetText += " · " + m.Goal.Source
				break
			}
		}
	}
	if !isFinite64(maxVal) || maxVal <= 0 {
		maxVal = 1
	} else {
		maxVal *= 1.15
		if !isFinite64(maxVal) {
			maxVal = 1
		}
	}
	dec := 0
	if hasFractional {
		dec = 1
	}
	return PlaytestChartSpec{
		Kind:     PlaytestChartBars,
		Title:    title,
		Subtitle: sub,
		Bars: &PlaytestSeriesChart{
			Name:       s.Name,
			Unit:       s.Unit,
			Entries:    entries,
			TargetMax:  targetMax,
			TargetText: targetText,
			MaxVal:     maxVal,
			Decimals:   dec,
		},
	}
}

func buildLineChartSpec(rep *playtest.Report, s playtest.SeriesChart) PlaytestChartSpec {
	title := strings.TrimSpace(s.Label)
	if title == "" {
		title = s.Name
	}
	sub := fmt.Sprintf("per level · %d entries", len(s.Entries))
	if s.Unit != "" {
		sub = fmt.Sprintf("%s · per level", s.Unit)
	}

	entries := slices.Clone(s.Entries)
	maxVal := 0.0
	hasFractional := false
	for i := range entries {
		v := entries[i].Value
		if !isFinite64(v) {
			entries[i].Value = 0
			continue
		}
		if v > maxVal {
			maxVal = v
		}
		if math.Abs(v-math.Round(v)) >= 0.01 {
			hasFractional = true
		}
	}
	var targetMax *float64
	var targetText string
	if s.TargetMax != nil && isFinite64(*s.TargetMax) {
		tmVal := *s.TargetMax
		targetMax = &tmVal
		if tmVal > maxVal {
			maxVal = tmVal
		}
		targetText = fmt.Sprintf("goal <= %s", formatCleanNumber(tmVal, 1))
	}
	if !isFinite64(maxVal) || maxVal <= 0 {
		maxVal = 1
	} else {
		maxVal *= 1.15
		if !isFinite64(maxVal) {
			maxVal = 1
		}
	}
	dec := 0
	if hasFractional {
		dec = 1
	}
	return PlaytestChartSpec{
		Kind:     PlaytestChartLine,
		Title:    title,
		Subtitle: sub,
		Line: &PlaytestSeriesChart{
			Name:       s.Name,
			Unit:       s.Unit,
			Entries:    entries,
			TargetMax:  targetMax,
			TargetText: targetText,
			MaxVal:     maxVal,
			Decimals:   dec,
		},
	}
}

func buildMatrixChartSpec(rep *playtest.Report, m playtest.MatrixChart) PlaytestChartSpec {
	title := strings.TrimSpace(m.Label)
	if title == "" {
		title = "Win rate by deck pair"
	}
	flagAbove := 60.0
	if m.FlagAbove != nil && isFinite64(*m.FlagAbove) {
		flagAbove = *m.FlagAbove
	}
	unit := m.Unit
	if unit == "" {
		unit = "%"
	}
	sub := fmt.Sprintf("%d matchups", len(m.Labels)*(max(0, len(m.Labels)-1)))
	if v, ok := rep.Bot.Params["matchesPerPair"]; ok {
		sub = fmt.Sprintf("%v matches per pair", v)
	}
	caption := fmt.Sprintf("Row deck's win rate against the column deck · above %s%s is flagged", formatCleanNumber(flagAbove, 0), unit)

	cells := make([][]*float64, len(m.Cells))
	for i, row := range m.Cells {
		cloned := make([]*float64, len(row))
		for j, cell := range row {
			if cell != nil && isFinite64(*cell) {
				v := *cell
				cloned[j] = &v
			}
		}
		cells[i] = cloned
	}
	return PlaytestChartSpec{
		Kind:     PlaytestChartMatrix,
		Title:    title,
		Subtitle: sub,
		Matrix: &PlaytestMatrixChart{
			Name:      m.Name,
			Unit:      unit,
			FlagAbove: flagAbove,
			Labels:    slices.Clone(m.Labels),
			Cells:     cells,
			Caption:   caption,
		},
	}
}

// DefaultPlaytestAgentRead synthesizes a concise multi-sentence diagnostic read and actionable suggestions from rep.
func DefaultPlaytestAgentRead(rep *playtest.Report) (string, []PlaytestSuggestion) {
	if rep == nil {
		return "No playtest report is loaded yet.", nil
	}
	missed := rep.MissedMetrics()
	chart := SelectPlaytestChart(rep)

	var sentences []string
	var suggestions []PlaytestSuggestion
	addSuggestion := func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		for _, existing := range suggestions {
			if existing.Text == text {
				return
			}
		}
		suggestions = append(suggestions, PlaytestSuggestion{
			ID:   fmt.Sprintf("suggestion-%d", len(suggestions)+1),
			Text: text,
		})
	}

	if len(missed) == 0 {
		sentences = append(sentences, fmt.Sprintf("All configured goals in %s were met across %d headless runs.", rep.Scope.Scene, max(1, rep.SeedCount)))
	} else {
		m0 := missed[0]
		name := m0.Label
		if name == "" {
			name = m0.Name
		}
		if m0.Goal != nil {
			sentences = append(sentences, fmt.Sprintf("%s missed its %s goal (%s vs %s).", name, m0.Goal.Source, formatMetricValue(m0, rep.SeedCount), m0.Goal.Describe(m0.Unit)))
		} else {
			sentences = append(sentences, fmt.Sprintf("%s missed its target at %s.", name, formatMetricValue(m0, rep.SeedCount)))
		}
	}

	switch chart.Kind {
	case PlaytestChartHeatMap:
		if chart.HeatMap != nil && len(chart.HeatMap.Clusters) > 0 {
			top := chart.HeatMap.Clusters[0]
			where := fmt.Sprintf("(%.0f, %.0f)", top.X, top.Y)
			if top.Tag != "" {
				where = top.Tag
			}
			if chart.HeatMap.IsTimePerTile {
				sentences = append(sentences, fmt.Sprintf("The explorer spent %d%% of recorded tile visits around %s, leaving other areas under-explored.", int(math.Round(top.Share*100)), where))
				addSuggestion(fmt.Sprintf("Open a clearer landmark or route leading away from %s", where))
			} else {
				sentences = append(sentences, fmt.Sprintf("%d of %d %s events cluster at %s, creating a sharp choke point.", top.Count, len(chart.HeatMap.Points), chart.HeatMap.Event, where))
				addSuggestion(fmt.Sprintf("Ease hazard timing or spacing around %s", where))
			}
		}
	case PlaytestChartBars:
		if chart.Bars != nil {
			for _, e := range chart.Bars.Entries {
				over := e.Flag == playtest.FlagProblem || (chart.Bars.TargetMax != nil && e.Value > *chart.Bars.TargetMax)
				if over {
					sentences = append(sentences, fmt.Sprintf("%s spikes to %s%s, well above the target line.", e.Label, formatCleanNumber(e.Value, chart.Bars.Decimals), unitWithSpace(chart.Bars.Unit)))
					addSuggestion(fmt.Sprintf("Stagger spawns or reduce hazard density in %s", e.Label))
					break
				}
			}
		}
	case PlaytestChartLine:
		if chart.Line != nil {
			for _, e := range chart.Line.Entries {
				if e.Flag == playtest.FlagProblem || e.Flag == playtest.FlagWarning {
					note := e.Note
					if note == "" {
						note = fmt.Sprintf("stands out at %s%s", formatCleanNumber(e.Value, chart.Line.Decimals), unitWithSpace(chart.Line.Unit))
					}
					sentences = append(sentences, fmt.Sprintf("%s %s, breaking the intended difficulty curve.", e.Label, note))
					addSuggestion(fmt.Sprintf("Rebalance or reorder %s to smooth the progression curve", e.Label))
					break
				}
			}
		}
	case PlaytestChartMatrix:
		if chart.Matrix != nil {
			bestVal := -1.0
			bestRow, bestCol := "", ""
			for rIdx, row := range chart.Matrix.Cells {
				for cIdx, cell := range row {
					if cell != nil && *cell > chart.Matrix.FlagAbove && *cell > bestVal {
						bestVal = *cell
						bestRow = chart.Matrix.Labels[rIdx]
						bestCol = chart.Matrix.Labels[cIdx]
					}
				}
			}
			if bestVal >= 0 {
				sentences = append(sentences, fmt.Sprintf("%s beats %s %s%s of the time, exceeding the %s%s balance ceiling.",
					bestRow, bestCol, formatCleanNumber(bestVal, 0), chart.Matrix.Unit, formatCleanNumber(chart.Matrix.FlagAbove, 0), chart.Matrix.Unit))
				addSuggestion(fmt.Sprintf("Nerf %s's early tempo or buff %s's counterplay", bestRow, bestCol))
			}
		}
	}

	for _, m := range missed {
		if len(suggestions) >= 2 {
			break
		}
		name := m.Label
		if name == "" {
			name = m.Name
		}
		addSuggestion(fmt.Sprintf("Tune %s so %s meets %s", rep.Scope.Scene, name, m.Goal.Describe(m.Unit)))
	}
	if len(suggestions) == 0 {
		addSuggestion(fmt.Sprintf("Tighten difficulty or add a harder seed sweep in %s", playtest.FileName))
	}

	return strings.Join(sentences, " "), suggestions
}

func unitWithSpace(unit string) string {
	if unit == "" {
		return ""
	}
	if unit == "%" {
		return "%"
	}
	return " " + unit
}
