package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/l10n"
	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/playtest"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

// RightRailMode aliases RightRailTab for callers selecting which panel occupies the editor's right rail.
type RightRailMode = RightRailTab

// MentionGroup classifies items in the composer's @-mention picker (Threads.dc.html).
type MentionGroup string

const (
	MentionGroupAll       MentionGroup = "all"
	MentionGroupObjects   MentionGroup = "objects"
	MentionGroupFiles     MentionGroup = "files"
	MentionGroupPrefabs   MentionGroup = "prefabs"
	MentionGroupAssets    MentionGroup = "assets"
	MentionGroupProblems  MentionGroup = "problems"
	MentionGroupPlaytests MentionGroup = "playtests"
	MentionGroupHistory   MentionGroup = "history"
	MentionGroupRules     MentionGroup = "rules"
)

// mentionGroupOrder lists all picker tabs in the exact order shown in Threads.dc.html.
var mentionGroupOrder = []MentionGroup{
	MentionGroupAll,
	MentionGroupObjects,
	MentionGroupFiles,
	MentionGroupPrefabs,
	MentionGroupAssets,
	MentionGroupProblems,
	MentionGroupPlaytests,
	MentionGroupHistory,
	MentionGroupRules,
}

// mentionSectionOrder lists the display order of groups inside the "All" view.
var mentionSectionOrder = []MentionGroup{
	MentionGroupObjects,
	MentionGroupFiles,
	MentionGroupPrefabs,
	MentionGroupAssets,
	MentionGroupProblems,
	MentionGroupPlaytests,
	MentionGroupHistory,
	MentionGroupRules,
}

// MentionIconKind identifies the visual icon rendered for a picker row or chip.
type MentionIconKind string

const (
	MentionIconObject   MentionIconKind = "obj"
	MentionIconFile     MentionIconKind = "file"
	MentionIconPrefab   MentionIconKind = "prefab"
	MentionIconImage    MentionIconKind = "thumb"
	MentionIconSound    MentionIconKind = "sound"
	MentionIconProblem  MentionIconKind = "problem"
	MentionIconPlaytest MentionIconKind = "run"
	MentionIconHistory  MentionIconKind = "history"
	MentionIconRule     MentionIconKind = "rule"
	MentionIconScene    MentionIconKind = "scene"
	MentionIconViewport MentionIconKind = "viewport"
)

// ContextChipKind distinguishes what kind of context item a chip represents.
type ContextChipKind string

const (
	ChipKindObject        ContextChipKind = "object"
	ChipKindWholeScene    ContextChipKind = "scene"
	ChipKindFile          ContextChipKind = "file"
	ChipKindPrefab        ContextChipKind = "prefab"
	ChipKindAsset         ContextChipKind = "asset"
	ChipKindProblem       ContextChipKind = "problem"
	ChipKindPlaytestRun   ContextChipKind = "playtest"
	ChipKindHistoryStep   ContextChipKind = "history"
	ChipKindRule          ContextChipKind = "rule"
	ChipKindAlways        ContextChipKind = "always"
	ChipKindViewportImage ContextChipKind = "viewport_image"
)

// MentionItem is one selectable entry in the composer's @-mention picker.
type MentionItem struct {
	ID       string
	Group    MentionGroup
	ChipKind ContextChipKind
	Icon     MentionIconKind
	Name     string
	Meta     string
	// Target references the underlying entity: object ID, project-relative file path, problem key, etc.
	Target string
	// SceneFile is the project-relative scene path for object mentions.
	SceneFile string
	// Layer is the scene layer name for object mentions.
	Layer string
	// Line is the 1-based line number when known (such as a problem or object start line).
	Line int
	// Detail holds structured summary text for non-file entities (problems, playtests, history steps).
	Detail string
	// ColorSwatch is an optional hex tint for object/asset dots (using Spec tokens when 0).
	ColorRole string
}

// AgentContextChip is one chip shown above the composer.
type AgentContextChip struct {
	ID        string
	Kind      ContextChipKind
	Name      string
	Target    string
	SceneFile string
	Layer     string
	Line      int
	Detail    string
	Icon      MentionIconKind
	ColorRole string
	Always    bool
	Suggested bool
}

// AgentChipPayload is the exact content shown on chip hover AND assembled into the request sent to the agent.
// Both the hover preview and AssembleAgentRequest resolve every chip through ResolveChipPayload so they can never diverge.
type AgentChipPayload struct {
	Chip      AgentContextChip
	Header    string
	Text      string
	ImagePNG  []byte
	ImageSrc  core.ImageSource
	ImageW    int
	ImageH    int
	StartLine int
	EndLine   int
	Tokens    int
}

// Formatted returns the exact text payload sent to the agent and shown on hover.
func (p AgentChipPayload) Formatted() string {
	switch {
	case p.Header != "" && p.Text != "":
		return p.Header + "\n" + p.Text
	case p.Text != "":
		return p.Text
	default:
		return p.Header
	}
}

// AgentTokenUsage reports token usage across the 6 context breakdown categories shown in Threads.dc.html.
type AgentTokenUsage struct {
	Limit         int
	Conversation  int
	OpenScene     int
	Attached      int
	Always        int
	ToolsAndInstr int
	ViewportImage int
}

// Total returns the sum of used tokens across all categories.
func (u AgentTokenUsage) Total() int {
	return max(0, u.Conversation) + max(0, u.OpenScene) + max(0, u.Attached) + max(0, u.Always) + max(0, u.ToolsAndInstr) + max(0, u.ViewportImage)
}

// AgentResponse is returned by Editor.OnSendAgentComposerRequest when a scripted or connected agent handles a request.
type AgentResponse struct {
	Reply string
	Usage *AgentTokenUsage
}

// AgentThreadTurn records one request/response turn in the active agent thread.
type AgentThreadTurn struct {
	ID        string
	Prompt    string
	Reply     string
	Chips     []string
	Tokens    int
	Timestamp time.Time
	Outcome   string // e.g. "kept", "reverted", "sent"
}

// ComposerPlaytestRun is a lightweight playtest run entry surfaced in the @-mention Playtest runs group.
type ComposerPlaytestRun struct {
	ID      string
	Name    string
	Summary string
	Detail  string
}

// ContextMeterLevel classifies the context ring visual state (Threads.dc.html).
type ContextMeterLevel int

const (
	// ContextMeterNormal is used at <= 70% context usage (muted ring, strong text).
	ContextMeterNormal ContextMeterLevel = iota
	// ContextMeterWarning is used at 71–89% context usage (changed/amber ring and percentage).
	ContextMeterWarning
	// ContextMeterCritical is used at >= 90% context usage ("compact now", warning fill/edge/fg).
	ContextMeterCritical
)

// ContextBreakdownRow is one bar in the context meter breakdown popover.
type ContextBreakdownRow struct {
	Key       string
	Label     string
	Tokens    int
	Percent   int
	Highlight bool // true when Percent > 30 (Threads.dc.html)
}

// ContextMeterSnapshot is the computed state for the context ring and breakdown popover.
type ContextMeterSnapshot struct {
	UsedTokens          int
	LimitTokens         int
	Percent             int
	Level               ContextMeterLevel
	Rows                []ContextBreakdownRow
	CompactableRequests int
	HasCompactedSummary bool
	CompactedSummary    string
}

const (
	defaultContextLimitTokens   = 200_000
	defaultToolsInstrTokens     = 10_000
	defaultViewportImgTokens    = 6_000
	maxComposerReadBytes        = 1 << 20
	maxComposerFirstLineBytes   = 8 << 10
	maxComposerImageHeaderBytes = 64 << 10
	maxComposerImageDim         = 4096
	maxComposerImagePixels      = 16_000_000
	maxComposerViewportDim      = 1280
	maxAttachedChips            = 32
	maxAssembledContextBytes    = 2 << 20
)

type agentComposerState struct {
	pickerOpen         bool
	pickerOpenedByAt   bool
	pickerGroup        MentionGroup
	pickerQuery        string
	pickerTriggerStart int
	pickerActiveIndex  int
	attached           []AgentContextChip
	viewportImageOn    bool
	hoveredChipID      string
	breakdownOpen      bool
	threadID           string
	threadSeq          int
	threadRev          uint64
	turns              []AgentThreadTurn
	compactedSummary   string
	summarySeed        string
	reportedUsage      *AgentTokenUsage
	extraPlaytestRuns  []ComposerPlaytestRun
	lastRequest        *AgentRequest
	composerFocusSeq   uint64
	cachedViewportPNG  []byte
	cachedViewportSrc  core.ImageSource
	cachedViewportW    int
	cachedViewportH    int
	cachedViewportRev  uint64
	metaCacheRoot      string
	metaCacheFP        string
	ruleSummaryCache   map[string]string
	imageDimsCache     map[string][2]int
	imageDimsKnown     map[string]bool
}

func (e *Editor) initAgentComposer() {
	if e == nil {
		return
	}
	if e.agentComposer.pickerGroup == "" {
		e.agentComposer.pickerGroup = MentionGroupAll
	}
	if e.agentComposer.threadID == "" {
		e.agentComposer.threadSeq = 1
		e.agentComposer.threadID = "thread-1"
	}
	e.agent.threadID = e.agentComposer.threadID
	e.agentComposer.viewportImageOn = true
}

func (e *Editor) resetAgentComposerForProject() {
	if e == nil {
		return
	}
	e.agentComposer = agentComposerState{
		pickerGroup:     MentionGroupAll,
		viewportImageOn: true,
		threadSeq:       1,
		threadID:        "thread-1",
	}
	e.agent.threadID = "thread-1"
	e.composerDraft = ""
}

// closeComposerPopovers closes transient composer popovers (mention picker, breakdown, hover preview)
// without altering the draft or attached chips.
func (e *Editor) closeComposerPopovers() {
	if e == nil {
		return
	}
	changed := e.agentComposer.pickerOpen || e.agentComposer.breakdownOpen || e.agentComposer.hoveredChipID != ""
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	if changed {
		e.notify()
	}
}

// onSceneChangedForComposer closes stale scene-bound popovers and invalidates the cached viewport image
// whenever the active scene tab changes (SelectTab, OpenScene, NewScene, CloseTab).
func (e *Editor) onSceneChangedForComposer() {
	if e == nil {
		return
	}
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	e.agentComposer.cachedViewportPNG = nil
	e.agentComposer.cachedViewportSrc = core.ImageSource{}
	e.agentComposer.cachedViewportW = 0
	e.agentComposer.cachedViewportH = 0
	e.agentComposer.cachedViewportRev = 0
}

// composerKeysBlocked reports whether higher-priority modal dialogs, full-screen modes,
// Play-in-editor, shortcut capture, or an inactive Agent rail should suppress composer keybindings.
func (e *Editor) composerKeysBlocked() bool {
	return e == nil || e.playing || e.shortcutCaptureActive ||
		e.RightRailTab() != RightTabAgent ||
		e.AgentMode() == AgentModeNone ||
		e.AgentSwitcherOpen() ||
		e.AskInPlaceOpen() || e.contextMenu.open ||
		e.PermissionsDraftOpen() ||
		e.StartScreenOpen() || e.MergeConflictOpen() || e.PlaytestReportOpen() ||
		e.UsagesScreenOpen() || e.ChangesOpen() || e.CodeReviewOpen() || e.HUDLayoutOpen() ||
		e.SettingsOpen() || e.ShortcutsSheetOpen() || e.BuildReleaseScreenOpen() ||
		e.ConnectDialogOpen() || e.AssetGeneratorOpen() || e.CommandPaletteOpen() ||
		e.PendingTurnPromptOpen()
}

// RightRailMode returns which panel currently occupies the right rail.
func (e *Editor) RightRailMode() RightRailMode {
	if e == nil {
		return RightRailInspector
	}
	return e.RightRailTab()
}

// SetRightRailMode switches the right rail tab (RightRailInspector, RightRailAgent, or RightRailHistory).
func (e *Editor) SetRightRailMode(mode RightRailMode) {
	if e == nil {
		return
	}
	e.SetRightRailTab(mode)
}

// FocusComposer switches the right rail to the Agent panel and requests keyboard focus for the composer input.
func (e *Editor) FocusComposer() {
	if e == nil {
		return
	}
	e.SetRightRailTab(RightTabAgent)
	e.agentComposer.composerFocusSeq++
	e.notify()
}

// TriggerAskAgentShortcut implements ⌘L for the panel composer (Spec.dc.html & Threads.dc.html):
//   - When an object is selected, attaches that object as a chip, focuses the agent composer, and updates AskAgentAboutObject.
//   - When nothing is selected, attaches the whole open scene as a chip and focuses the agent composer.
func (e *Editor) TriggerAskAgentShortcut() error {
	if e == nil || e.Doc == nil {
		return errors.New("editor: no scene open")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	e.SetRightRailTab(RightTabAgent)
	e.agentComposer.composerFocusSeq++
	if e.SelectedID != "" {
		if _, err := e.AttachObjectChip(e.SelectedID); err != nil {
			return err
		}
		_, err := e.AskAgentAboutObject(e.SelectedID)
		return err
	}
	if _, err := e.AttachWholeSceneChip(); err != nil {
		return err
	}
	e.Status = e.T("composer.attachedSceneStatus")
	e.notify()
	return nil
}

// --- Draft & @-mention trigger detection ---

// detectMentionTrigger checks if draft ends with an active "@<query>" mention token
// (preceded by start-of-string or whitespace, containing no whitespace).
func detectMentionTrigger(draft string) (start int, query string, ok bool) {
	if draft == "" {
		return 0, "", false
	}
	for i := len(draft); i > 0; {
		r, size := utf8.DecodeLastRuneInString(draft[:i])
		i -= size
		if unicode.IsSpace(r) {
			return 0, "", false
		}
		if r == '@' {
			if i == 0 {
				return 0, draft[1:], true
			}
			prev, _ := utf8.DecodeLastRuneInString(draft[:i])
			if unicode.IsSpace(prev) {
				return i, draft[i+1:], true
			}
			return 0, "", false
		}
	}
	return 0, "", false
}

// updateComposerDraftFromInput updates the composer text and live @-mention picker state.
func (e *Editor) updateComposerDraftFromInput(text string) {
	if e == nil {
		return
	}
	e.composerDraft = text
	if start, query, ok := detectMentionTrigger(text); ok {
		prevQuery := e.agentComposer.pickerQuery
		e.agentComposer.pickerOpen = true
		e.agentComposer.pickerOpenedByAt = true
		e.agentComposer.pickerTriggerStart = start
		e.agentComposer.pickerQuery = query
		e.agentComposer.breakdownOpen = false
		if query != prevQuery {
			e.agentComposer.pickerActiveIndex = 0
		}
		e.clampMentionSelection()
	} else if e.agentComposer.pickerOpen && e.agentComposer.pickerOpenedByAt {
		e.agentComposer.pickerOpen = false
		e.agentComposer.pickerOpenedByAt = false
		e.agentComposer.pickerQuery = ""
		e.agentComposer.pickerActiveIndex = 0
	}
	e.notify()
}

// MentionPickerOpen reports whether the @-mention picker popover is currently open.
func (e *Editor) MentionPickerOpen() bool {
	return e != nil && e.agentComposer.pickerOpen
}

// MentionPickerQuery returns the current @-mention filter query (without the leading '@').
func (e *Editor) MentionPickerQuery() string {
	if e == nil {
		return ""
	}
	return e.agentComposer.pickerQuery
}

// MentionPickerGroup returns the active group tab in the @-mention picker.
func (e *Editor) MentionPickerGroup() MentionGroup {
	if e == nil || e.agentComposer.pickerGroup == "" {
		return MentionGroupAll
	}
	return e.agentComposer.pickerGroup
}

// MentionPickerActiveIndex returns the 0-based index of the highlighted item in MentionPickerItems().
func (e *Editor) MentionPickerActiveIndex() int {
	if e == nil {
		return 0
	}
	return e.agentComposer.pickerActiveIndex
}

// OpenMentionPicker opens the @-mention picker (with optional initial query).
func (e *Editor) OpenMentionPicker(query string) {
	if e == nil {
		return
	}
	e.agentComposer.pickerOpen = true
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerQuery = query
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	e.clampMentionSelection()
	e.notify()
}

// ToggleMentionPicker toggles the @-mention picker open or closed (clicking the @ button).
func (e *Editor) ToggleMentionPicker() {
	if e == nil {
		return
	}
	if e.agentComposer.pickerOpen {
		e.CloseMentionPicker()
		return
	}
	_, q, ok := detectMentionTrigger(e.composerDraft)
	if !ok {
		q = ""
	}
	e.agentComposer.pickerOpen = true
	e.agentComposer.pickerOpenedByAt = ok
	e.agentComposer.pickerQuery = q
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	e.clampMentionSelection()
	e.notify()
}

// CloseMentionPicker closes the @-mention picker without altering the composer draft.
func (e *Editor) CloseMentionPicker() {
	if e == nil || !e.agentComposer.pickerOpen {
		return
	}
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerActiveIndex = 0
	e.notify()
}

// SetMentionPickerGroup switches the active tab of the @-mention picker.
func (e *Editor) SetMentionPickerGroup(group MentionGroup) {
	if e == nil {
		return
	}
	if !slices.Contains(mentionGroupOrder, group) {
		group = MentionGroupAll
	}
	e.agentComposer.pickerGroup = group
	e.agentComposer.pickerActiveIndex = 0
	e.clampMentionSelection()
	e.notify()
}

// CycleMentionPickerGroup advances (+1) or rewinds (-1) the active group tab (Tab / Shift+Tab).
func (e *Editor) CycleMentionPickerGroup(delta int) MentionGroup {
	if e == nil {
		return MentionGroupAll
	}
	cur := e.MentionPickerGroup()
	idx := slices.Index(mentionGroupOrder, cur)
	if idx < 0 {
		idx = 0
	}
	n := len(mentionGroupOrder)
	next := ((idx+delta)%n + n) % n
	e.agentComposer.pickerGroup = mentionGroupOrder[next]
	e.agentComposer.pickerActiveIndex = 0
	e.clampMentionSelection()
	e.notify()
	return e.agentComposer.pickerGroup
}

// MoveMentionSelection moves the highlighted row in the @-mention picker by delta (+1 down, -1 up).
func (e *Editor) MoveMentionSelection(delta int) int {
	if e == nil || !e.agentComposer.pickerOpen {
		return 0
	}
	items := e.MentionPickerItems()
	if len(items) == 0 {
		e.agentComposer.pickerActiveIndex = 0
		return 0
	}
	n := len(items)
	next := ((e.agentComposer.pickerActiveIndex+delta)%n + n) % n
	e.agentComposer.pickerActiveIndex = next
	e.notify()
	return next
}

func (e *Editor) clampMentionSelection() {
	items := e.MentionPickerItems()
	if len(items) == 0 {
		e.agentComposer.pickerActiveIndex = 0
		return
	}
	if e.agentComposer.pickerActiveIndex < 0 {
		e.agentComposer.pickerActiveIndex = 0
	}
	if e.agentComposer.pickerActiveIndex >= len(items) {
		e.agentComposer.pickerActiveIndex = len(items) - 1
	}
}

// AcceptActiveMention adds the currently highlighted picker item as a context chip, removes the
// trailing "@<query>" trigger from the composer draft, and closes the picker.
func (e *Editor) AcceptActiveMention() (AgentContextChip, bool) {
	if e == nil || !e.agentComposer.pickerOpen {
		return AgentContextChip{}, false
	}
	items := e.MentionPickerItems()
	if len(items) == 0 {
		e.CloseMentionPicker()
		return AgentContextChip{}, false
	}
	idx := e.agentComposer.pickerActiveIndex
	if idx < 0 || idx >= len(items) {
		idx = 0
	}
	return e.AcceptMentionItem(items[idx].ID)
}

// AcceptMentionItem adds the picker item with id as an attached chip, strips any active "@<query>"
// trigger from the draft, and closes the picker.
func (e *Editor) AcceptMentionItem(id string) (AgentContextChip, bool) {
	if e == nil || id == "" {
		return AgentContextChip{}, false
	}
	var found *MentionItem
	for _, item := range e.collectMentionItems(e.agentComposer.pickerQuery, MentionGroupAll) {
		if item.ID == id {
			c := item
			found = &c
			break
		}
	}
	if found == nil {
		for _, item := range e.collectMentionItems("", MentionGroupAll) {
			if item.ID == id {
				c := item
				found = &c
				break
			}
		}
	}
	if found == nil {
		return AgentContextChip{}, false
	}
	chip := mentionItemToChip(*found)
	if !e.addAttachedChip(chip) {
		e.Status = e.T("composer.chipLimitStatus", l10n.Param("max", maxAttachedChips))
		e.notify()
		return AgentContextChip{}, false
	}
	if start, _, ok := detectMentionTrigger(e.composerDraft); ok {
		e.composerDraft = e.composerDraft[:start]
	}
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerQuery = ""
	e.agentComposer.pickerActiveIndex = 0
	e.notify()
	return chip, true
}

func mentionItemToChip(m MentionItem) AgentContextChip {
	return AgentContextChip{
		ID:        m.ID,
		Kind:      m.ChipKind,
		Name:      m.Name,
		Target:    m.Target,
		SceneFile: m.SceneFile,
		Layer:     m.Layer,
		Line:      m.Line,
		Detail:    m.Detail,
		Icon:      m.Icon,
		ColorRole: m.ColorRole,
	}
}

// MentionPickerItems returns the filtered, ranked items currently shown in the @-mention picker
// for the active query and group tab, excluding items already attached as chips.
func (e *Editor) MentionPickerItems() []MentionItem {
	if e == nil {
		return nil
	}
	return e.collectMentionItems(e.agentComposer.pickerQuery, e.MentionPickerGroup())
}

// SearchMentions queries the @-mention index for query and group without mutating picker UI state.
func (e *Editor) SearchMentions(query string, group MentionGroup) []MentionItem {
	if e == nil {
		return nil
	}
	if group == "" {
		group = MentionGroupAll
	}
	return e.collectMentionItems(query, group)
}

func (e *Editor) collectMentionItems(query string, group MentionGroup) []MentionItem {
	attachedSet := make(map[string]bool, len(e.agentComposer.attached))
	for _, c := range e.agentComposer.attached {
		attachedSet[c.ID] = true
	}

	terms := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	type rankedMention struct {
		groupOrder int
		rank       int
		item       MentionItem
	}
	var out []rankedMention

	groupIdx := func(g MentionGroup) int {
		if i := slices.Index(mentionSectionOrder, g); i >= 0 {
			return i
		}
		return len(mentionSectionOrder)
	}

	wantsGroup := func(g MentionGroup) bool {
		if group == MentionGroupAll || group == "" {
			return true
		}
		if group == MentionGroupFiles && g == MentionGroupPrefabs {
			return true
		}
		return group == g
	}

	addIfMatch := func(item MentionItem, haystacks ...string) {
		if attachedSet[item.ID] {
			return
		}
		if !wantsGroup(item.Group) {
			return
		}
		rank := 2
		if len(terms) > 0 {
			lows := make([]string, 0, len(haystacks)+2)
			lows = append(lows, strings.ToLower(item.Name), strings.ToLower(item.Meta))
			for _, h := range haystacks {
				if h != "" {
					lows = append(lows, strings.ToLower(h))
				}
			}
			for _, t := range terms {
				matched := false
				for i, h := range lows {
					if strings.Contains(h, t) {
						matched = true
						r := matchRank(h, t)
						if i > 0 {
							r++
						}
						if r < rank {
							rank = r
						}
					}
				}
				if !matched {
					return
				}
			}
		} else {
			rank = 0
		}
		out = append(out, rankedMention{
			groupOrder: groupIdx(item.Group),
			rank:       rank,
			item:       item,
		})
	}

	// 1. Objects: live objects in the open scene (plus other scenes from search.go index).
	openRel := e.openSceneRelPath()
	seenObjects := make(map[string]bool)
	if e.Doc != nil && wantsGroup(MentionGroupObjects) {
		for li := range e.Doc.Layers {
			layer := &e.Doc.Layers[li]
			for oi := range layer.Objects {
				sceneformat.WalkObjects(layer.Objects[oi:oi+1], func(o, _ *sceneformat.ObjectDoc) bool {
					if o == nil || o.ID == "" {
						return true
					}
					key := openRel + "#" + o.ID
					if seenObjects[key] {
						return true
					}
					seenObjects[key] = true
					disp := contextObjectDisplayName(o)
					meta := fmt.Sprintf("%s · x %g, y %g", layer.Name, o.X, o.Y)
					if o.Prefab != "" {
						meta = fmt.Sprintf("%s · %s · x %g", layer.Name, path.Base(o.Prefab), o.X)
					}
					addIfMatch(MentionItem{
						ID:        "obj:" + openRel + ":" + o.ID,
						Group:     MentionGroupObjects,
						ChipKind:  ChipKindObject,
						Icon:      MentionIconObject,
						Name:      disp,
						Meta:      meta,
						Target:    o.ID,
						SceneFile: openRel,
						Layer:     layer.Name,
						ColorRole: objectColorRole(o),
					}, o.ID, o.Name, o.Prefab, layer.Name)
					return true
				})
			}
		}
	}

	// 2. Project search index (search.go): Files, Prefabs, Assets, Rules, and objects in other scenes.
	var ix *projectSearchIndex
	if e.Project != nil && e.ProjectPath != "" {
		if loaded, err := e.ensureProjectIndex(false); err == nil {
			ix = loaded
		}
	}
	prefabUseCount := make(map[string]int)
	if ix != nil && ix.scan != nil {
		for _, r := range ix.scan.Refs {
			if r.Target != "" {
				prefabUseCount[r.Target]++
			}
		}
	}

	if ix != nil {
		// Objects in non-open scenes from search index.
		if wantsGroup(MentionGroupObjects) {
			for _, f := range ix.files {
				if !ix.sceneFiles[f.path] || f.path == openRel {
					continue
				}
				sc := ix.scenes[f.path]
				for _, o := range sc.objects {
					if o.id == "" {
						continue
					}
					key := f.path + "#" + o.id
					if seenObjects[key] {
						continue
					}
					seenObjects[key] = true
					disp := o.name
					if disp == "" {
						disp = o.id
					}
					meta := fmt.Sprintf("%s · %s", path.Base(f.path), o.layer)
					addIfMatch(MentionItem{
						ID:        "obj:" + f.path + ":" + o.id,
						Group:     MentionGroupObjects,
						ChipKind:  ChipKindObject,
						Icon:      MentionIconObject,
						Name:      disp,
						Meta:      meta,
						Target:    o.id,
						SceneFile: f.path,
						Layer:     o.layer,
						ColorRole: "accent",
					}, o.id, o.name, o.prefab, f.path, o.layer)
				}
			}
		}

		// Classify project files into Prefabs, Assets, Rules, and Files.
		e.syncComposerMetaCache(ix.rootDir, ix.fingerprint)
		for _, f := range ix.files {
			p := f.path
			low := strings.ToLower(p)
			base := path.Base(p)
			dir := path.Dir(p)
			dirMeta := ""
			if dir != "." {
				dirMeta = dir + "/"
			}
			switch {
			case isRuleProjectFile(p):
				meta := dirMeta
				if summary := e.cachedRuleFirstLine(ix.rootDir, p); summary != "" {
					meta = summary
				}
				addIfMatch(MentionItem{
					ID:        "rule:" + p,
					Group:     MentionGroupRules,
					ChipKind:  ChipKindRule,
					Icon:      MentionIconRule,
					Name:      p,
					Meta:      meta,
					Target:    p,
					ColorRole: "muted",
				}, p, base, meta)
			case strings.HasSuffix(low, ".prefab.yaml") || strings.HasSuffix(low, ".prefab.yml"):
				meta := dirMeta
				if n := prefabUseCount[p]; n > 0 {
					if meta != "" {
						meta += " · "
					}
					meta += fmt.Sprintf("used %d times", n)
				}
				addIfMatch(MentionItem{
					ID:        "prefab:" + p,
					Group:     MentionGroupPrefabs,
					ChipKind:  ChipKindPrefab,
					Icon:      MentionIconPrefab,
					Name:      base,
					Meta:      meta,
					Target:    p,
					ColorRole: "agent",
				}, p, base)
			case isImageAssetPath(low):
				meta := dirMeta
				if w, h, ok := e.cachedImageDims(ix.rootDir, p); ok {
					if meta != "" {
						meta += " · "
					}
					meta += fmt.Sprintf("%d × %d", w, h)
				}
				addIfMatch(MentionItem{
					ID:        "asset:" + p,
					Group:     MentionGroupAssets,
					ChipKind:  ChipKindAsset,
					Icon:      MentionIconImage,
					Name:      base,
					Meta:      meta,
					Target:    p,
					ColorRole: "added",
				}, p, base)
			case isAudioAssetPath(low):
				addIfMatch(MentionItem{
					ID:        "asset:" + p,
					Group:     MentionGroupAssets,
					ChipKind:  ChipKindAsset,
					Icon:      MentionIconSound,
					Name:      base,
					Meta:      dirMeta,
					Target:    p,
					ColorRole: "muted",
				}, p, base)
			case isOtherAssetPath(low):
				addIfMatch(MentionItem{
					ID:        "asset:" + p,
					Group:     MentionGroupAssets,
					ChipKind:  ChipKindAsset,
					Icon:      MentionIconFile,
					Name:      base,
					Meta:      dirMeta,
					Target:    p,
					ColorRole: "muted",
				}, p, base)
			default:
				meta := dirMeta
				if f.sceneName != "" {
					if meta != "" {
						meta += " · "
					}
					meta += "scene " + f.sceneName
				}
				if meta == "" {
					meta = p
				}
				addIfMatch(MentionItem{
					ID:        "file:" + p,
					Group:     MentionGroupFiles,
					ChipKind:  ChipKindFile,
					Icon:      MentionIconFile,
					Name:      base,
					Meta:      meta,
					Target:    p,
					ColorRole: "accent",
				}, p, base, f.sceneName, f.uid)
			}
		}
	}

	// Registered in-memory prefabs not backed by a file on disk.
	if wantsGroup(MentionGroupPrefabs) && len(e.Prefabs) > 0 {
		names := make([]string, 0, len(e.Prefabs))
		for name := range e.Prefabs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			id := "prefab:" + name
			addIfMatch(MentionItem{
				ID:        id,
				Group:     MentionGroupPrefabs,
				ChipKind:  ChipKindPrefab,
				Icon:      MentionIconPrefab,
				Name:      name,
				Meta:      "registered prefab",
				Target:    name,
				ColorRole: "agent",
			}, name)
		}
	}

	// 3. Problems (live from Editor.Problems).
	if wantsGroup(MentionGroupProblems) {
		for i, prob := range e.Problems() {
			meta := prob.File
			if prob.Line > 0 {
				meta = fmt.Sprintf("%s:%d", prob.File, prob.Line)
			}
			addIfMatch(MentionItem{
				ID:        fmt.Sprintf("problem:%d:%s:%d", i, prob.File, prob.Line),
				Group:     MentionGroupProblems,
				ChipKind:  ChipKindProblem,
				Icon:      MentionIconProblem,
				Name:      prob.Message,
				Meta:      meta,
				Target:    prob.File,
				Line:      prob.Line,
				Detail:    prob.String(),
				ColorRole: "warning",
			}, prob.Message, prob.File, meta)
		}
	}

	// 4. Playtest runs (from PlaytestSession + registered extra runs).
	if wantsGroup(MentionGroupPlaytests) {
		if sess := e.PlaytestSession(); sess != nil {
			for i, entry := range sess.Entries {
				if entry == nil || entry.Report == nil {
					continue
				}
				rep := entry.Report
				name := fmt.Sprintf("Run %d · %s", i+1, rep.Bot.Profile)
				if rep.Bot.Profile == "" {
					name = fmt.Sprintf("Playtest run %d", i+1)
				}
				meta := fmt.Sprintf("%d runs · %s", len(rep.Runs), entry.ReportPath)
				detail := formatPlaytestRunSummary(rep)
				addIfMatch(MentionItem{
					ID:        fmt.Sprintf("playtest:session:%d", i),
					Group:     MentionGroupPlaytests,
					ChipKind:  ChipKindPlaytestRun,
					Icon:      MentionIconPlaytest,
					Name:      name,
					Meta:      meta,
					Target:    entry.ReportPath,
					Detail:    detail,
					ColorRole: "muted",
				}, name, meta, detail)
			}
		}
		for _, run := range e.agentComposer.extraPlaytestRuns {
			addIfMatch(MentionItem{
				ID:        "playtest:" + run.ID,
				Group:     MentionGroupPlaytests,
				ChipKind:  ChipKindPlaytestRun,
				Icon:      MentionIconPlaytest,
				Name:      run.Name,
				Meta:      run.Summary,
				Target:    run.ID,
				Detail:    run.Detail,
				ColorRole: "muted",
			}, run.Name, run.Summary, run.Detail)
		}
	}

	// 5. History steps (applied undo steps in History + CommitMilestones).
	if wantsGroup(MentionGroupHistory) && e.History != nil {
		applied := min(e.History.Index(), len(e.History.commands))
		for i := applied - 1; i >= 0; i-- {
			cmd := e.History.commands[i]
			if cmd == nil {
				continue
			}
			lbl := cmd.Label()
			if lbl == "" {
				lbl = fmt.Sprintf("Step %d", i+1)
			}
			meta := fmt.Sprintf("step %d of %d", i+1, applied)
			addIfMatch(MentionItem{
				ID:        fmt.Sprintf("history:cmd:%d", i+1),
				Group:     MentionGroupHistory,
				ChipKind:  ChipKindHistoryStep,
				Icon:      MentionIconHistory,
				Name:      lbl,
				Meta:      meta,
				Target:    strconv.Itoa(i + 1),
				Detail:    fmt.Sprintf("History step %d: %s", i+1, lbl),
				ColorRole: "muted",
			}, lbl, meta)
		}
		for i, m := range e.History.Milestones() {
			meta := m.SHA
			if m.Branch != "" {
				meta = m.Branch + " · " + m.SHA
			}
			addIfMatch(MentionItem{
				ID:        fmt.Sprintf("history:milestone:%d:%s", i, m.SHA),
				Group:     MentionGroupHistory,
				ChipKind:  ChipKindHistoryStep,
				Icon:      MentionIconHistory,
				Name:      m.Summary,
				Meta:      meta,
				Target:    m.SHA,
				Detail:    fmt.Sprintf("Commit %s (%s): %s", m.SHA, m.Branch, m.Summary),
				ColorRole: "muted",
			}, m.Summary, m.SHA, m.Branch)
		}
	}

	slices.SortFunc(out, func(a, b rankedMention) int {
		return cmp.Or(
			cmp.Compare(a.groupOrder, b.groupOrder),
			cmp.Compare(a.rank, b.rank),
			cmp.Compare(strings.ToLower(a.item.Name), strings.ToLower(b.item.Name)),
			cmp.Compare(a.item.ID, b.item.ID),
		)
	})

	items := make([]MentionItem, len(out))
	for i := range out {
		items[i] = out[i].item
	}
	return items
}

func objectColorRole(o *sceneformat.ObjectDoc) string {
	if o == nil {
		return "accent"
	}
	switch {
	case o.Prefab != "":
		return "added"
	case o.Sprite != nil:
		return "changed"
	case o.Light != nil:
		return "warning"
	default:
		return "accent"
	}
}

func isRuleProjectFile(rel string) bool {
	low := strings.ToLower(rel)
	if low == "agents.md" || low == "brief.yaml" {
		return true
	}
	if strings.HasPrefix(low, "rules/") && (strings.HasSuffix(low, ".md") || strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml") || strings.HasSuffix(low, ".txt")) {
		return true
	}
	return false
}

func isImageAssetPath(low string) bool {
	return strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".jpg") || strings.HasSuffix(low, ".jpeg") || strings.HasSuffix(low, ".gif") || strings.HasSuffix(low, ".webp")
}

func isAudioAssetPath(low string) bool {
	return strings.HasSuffix(low, ".wav") || strings.HasSuffix(low, ".ogg") || strings.HasSuffix(low, ".mp3") || strings.HasSuffix(low, ".flac")
}

func isOtherAssetPath(low string) bool {
	return strings.HasSuffix(low, ".ttf") || strings.HasSuffix(low, ".otf") || strings.HasSuffix(low, ".glb") || strings.HasSuffix(low, ".gltf")
}

func (e *Editor) syncComposerMetaCache(rootDir, fingerprint string) {
	if e.agentComposer.metaCacheRoot == rootDir && e.agentComposer.metaCacheFP == fingerprint && e.agentComposer.ruleSummaryCache != nil {
		return
	}
	e.agentComposer.metaCacheRoot = rootDir
	e.agentComposer.metaCacheFP = fingerprint
	e.agentComposer.ruleSummaryCache = make(map[string]string)
	e.agentComposer.imageDimsCache = make(map[string][2]int)
	e.agentComposer.imageDimsKnown = make(map[string]bool)
}

func (e *Editor) cachedRuleFirstLine(rootDir, rel string) string {
	if s, ok := e.agentComposer.ruleSummaryCache[rel]; ok {
		return s
	}
	s := firstNonEmptyLineSafe(rootDir, rel)
	e.agentComposer.ruleSummaryCache[rel] = s
	return s
}

func (e *Editor) cachedImageDims(rootDir, rel string) (int, int, bool) {
	if known, ok := e.agentComposer.imageDimsKnown[rel]; ok {
		if !known {
			return 0, 0, false
		}
		d := e.agentComposer.imageDimsCache[rel]
		return d[0], d[1], true
	}
	w, h, ok := readImageDimsSafe(rootDir, rel)
	e.agentComposer.imageDimsKnown[rel] = ok
	if ok {
		e.agentComposer.imageDimsCache[rel] = [2]int{w, h}
	}
	return w, h, ok
}

func firstNonEmptyLineSafe(rootDir, rel string) string {
	data, err := readProjectPrefixSafe(rootDir, rel, maxComposerFirstLineBytes)
	if err != nil || len(data) == 0 || bytes.IndexByte(data, 0) >= 0 {
		return ""
	}
	// Strip an incomplete trailing UTF-8 sequence if truncated mid-rune.
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	if len(data) == 0 {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimLeft(line, "# "))
		if trimmed != "" {
			return truncateRunes(trimmed, 72)
		}
	}
	return ""
}

func readImageDimsSafe(rootDir, rel string) (int, int, bool) {
	data, err := readProjectPrefixSafe(rootDir, rel, maxComposerImageHeaderBytes)
	if err != nil || len(data) == 0 {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

func formatPlaytestRunSummary(rep *playtest.Report) string {
	if rep == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "profile: %s\nruns: %d", rep.Bot.Profile, len(rep.Runs))
	for _, m := range rep.Metrics {
		fmt.Fprintf(&b, "\n%s: %g", m.Name, m.Value)
		if m.Unit != "" {
			b.WriteByte(' ')
			b.WriteString(m.Unit)
		}
	}
	return b.String()
}

func imageToRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

// AddComposerPlaytestRun registers a playtest run summary in the composer's @-mention Playtest runs group.
func (e *Editor) AddComposerPlaytestRun(run ComposerPlaytestRun) {
	if e == nil || strings.TrimSpace(run.ID) == "" {
		return
	}
	for i := range e.agentComposer.extraPlaytestRuns {
		if e.agentComposer.extraPlaytestRuns[i].ID == run.ID {
			e.agentComposer.extraPlaytestRuns[i] = run
			e.notify()
			return
		}
	}
	e.agentComposer.extraPlaytestRuns = append(e.agentComposer.extraPlaytestRuns, run)
	e.notify()
}

// --- Context Chips ---

func (e *Editor) addAttachedChip(chip AgentContextChip) bool {
	for i := range e.agentComposer.attached {
		if e.agentComposer.attached[i].ID == chip.ID {
			e.agentComposer.attached[i] = chip
			return true
		}
	}
	if len(e.agentComposer.attached) >= maxAttachedChips {
		return false
	}
	e.agentComposer.attached = append(e.agentComposer.attached, chip)
	return true
}

// ComposerChips returns the currently attached context chips (excluding suggested and always chips).
func (e *Editor) ComposerChips() []AgentContextChip {
	if e == nil || len(e.agentComposer.attached) == 0 {
		return nil
	}
	return slices.Clone(e.agentComposer.attached)
}

// AttachObjectChip explicitly attaches scene object id as a context chip.
func (e *Editor) AttachObjectChip(id string) (AgentContextChip, error) {
	if e == nil || e.Doc == nil {
		return AgentContextChip{}, errors.New("editor: no scene open")
	}
	if id == "" {
		id = e.SelectedID
	}
	if id == "" {
		return AgentContextChip{}, errors.New("editor: no object selected")
	}
	obj, layerName, _, _ := e.resolveContextObject(id)
	if obj == nil {
		return AgentContextChip{}, fmt.Errorf("editor: object %q not found", id)
	}
	openRel := e.openSceneRelPath()
	chip := AgentContextChip{
		ID:        "obj:" + openRel + ":" + obj.ID,
		Kind:      ChipKindObject,
		Name:      contextObjectDisplayName(obj),
		Target:    obj.ID,
		SceneFile: openRel,
		Layer:     layerName,
		Icon:      MentionIconObject,
		ColorRole: objectColorRole(obj),
	}
	if !e.addAttachedChip(chip) {
		return AgentContextChip{}, fmt.Errorf("editor: maximum attached chips reached (%d)", maxAttachedChips)
	}
	e.notify()
	return chip, nil
}

// AttachWholeSceneChip explicitly attaches the entire open scene as a context chip (used by ⌘L with nothing selected).
func (e *Editor) AttachWholeSceneChip() (AgentContextChip, error) {
	if e == nil || e.Doc == nil {
		return AgentContextChip{}, errors.New("editor: no scene open")
	}
	sceneRel := e.openSceneRelPath()
	sceneShort := e.openSceneShortName()
	wholeLabel := e.T("composer.wholeScene")
	if wholeLabel == "" || wholeLabel == "composer.wholeScene" {
		wholeLabel = "whole scene"
	}
	chip := AgentContextChip{
		ID:        "scene:" + sceneRel,
		Kind:      ChipKindWholeScene,
		Name:      sceneShort + " · " + wholeLabel,
		Target:    sceneRel,
		SceneFile: sceneRel,
		Icon:      MentionIconScene,
		ColorRole: "accent",
	}
	if !e.addAttachedChip(chip) {
		return AgentContextChip{}, fmt.Errorf("editor: maximum attached chips reached (%d)", maxAttachedChips)
	}
	e.notify()
	return chip, nil
}

// AttachFileChip explicitly attaches a project-relative file as a context chip.
func (e *Editor) AttachFileChip(relPath string) (AgentContextChip, error) {
	if e == nil {
		return AgentContextChip{}, errors.New("editor: nil editor")
	}
	root := ""
	if e.Project != nil && e.ProjectPath != "" {
		root = filepath.Dir(e.ProjectPath)
	}
	clean, err := sanitizeComposerProjectRelPath(root, relPath)
	if err != nil {
		return AgentContextChip{}, err
	}
	if root != "" {
		if _, err := resolveSafeProjectRegularPath(root, clean); err != nil {
			return AgentContextChip{}, err
		}
	}
	chip := AgentContextChip{
		ID:        "file:" + clean,
		Kind:      ChipKindFile,
		Name:      path.Base(clean),
		Target:    clean,
		Icon:      MentionIconFile,
		ColorRole: "accent",
	}
	if !e.addAttachedChip(chip) {
		return AgentContextChip{}, fmt.Errorf("editor: maximum attached chips reached (%d)", maxAttachedChips)
	}
	e.notify()
	return chip, nil
}

// RemoveComposerChip removes an attached chip by ID. Returns true if a chip was removed.
func (e *Editor) RemoveComposerChip(id string) bool {
	if e == nil || id == "" {
		return false
	}
	for i := range e.agentComposer.attached {
		if e.agentComposer.attached[i].ID == id {
			e.agentComposer.attached = append(e.agentComposer.attached[:i], e.agentComposer.attached[i+1:]...)
			if e.agentComposer.hoveredChipID == id {
				e.agentComposer.hoveredChipID = ""
			}
			e.notify()
			return true
		}
	}
	return false
}

// RemoveLastComposerChip removes the rightmost attached chip (triggered by Backspace in an empty composer).
func (e *Editor) RemoveLastComposerChip() bool {
	if e == nil || len(e.agentComposer.attached) == 0 {
		return false
	}
	last := e.agentComposer.attached[len(e.agentComposer.attached)-1]
	e.agentComposer.attached = e.agentComposer.attached[:len(e.agentComposer.attached)-1]
	if e.agentComposer.hoveredChipID == last.ID {
		e.agentComposer.hoveredChipID = ""
	}
	e.notify()
	return true
}

// SuggestedChip returns a suggested chip for the currently selected object when one is selected
// and not yet attached, or (zero, false). Selecting an object ONLY suggests a chip; nothing joins
// the request unless the person explicitly adds it.
func (e *Editor) SuggestedChip() (AgentContextChip, bool) {
	if e == nil || e.Doc == nil || e.SelectedID == "" {
		return AgentContextChip{}, false
	}
	obj, layerName, _, _ := e.resolveContextObject(e.SelectedID)
	if obj == nil {
		return AgentContextChip{}, false
	}
	openRel := e.openSceneRelPath()
	id := "obj:" + openRel + ":" + obj.ID
	for _, c := range e.agentComposer.attached {
		if c.ID == id {
			return AgentContextChip{}, false
		}
	}
	return AgentContextChip{
		ID:        id,
		Kind:      ChipKindObject,
		Name:      contextObjectDisplayName(obj),
		Target:    obj.ID,
		SceneFile: openRel,
		Layer:     layerName,
		Icon:      MentionIconObject,
		ColorRole: objectColorRole(obj),
		Suggested: true,
	}, true
}

// AcceptSuggestedChip promotes the currently suggested selection chip into an attached chip.
func (e *Editor) AcceptSuggestedChip() (AgentContextChip, bool) {
	s, ok := e.SuggestedChip()
	if !ok {
		return AgentContextChip{}, false
	}
	s.Suggested = false
	if !e.addAttachedChip(s) {
		e.Status = e.T("composer.chipLimitStatus", l10n.Param("max", maxAttachedChips))
		e.notify()
		return AgentContextChip{}, false
	}
	e.notify()
	return s, true
}

// AlwaysChips returns the always-included project files (brief.yaml, AGENTS.md) present in the open project.
func (e *Editor) AlwaysChips() []AgentContextChip {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil
	}
	root := filepath.Dir(e.ProjectPath)
	var out []AgentContextChip
	for _, rel := range []string{project.BriefFileName, ProjectRulesFileName} {
		present := false
		switch rel {
		case project.BriefFileName:
			present = e.BriefExists()
		case ProjectRulesFileName:
			present = e.ProjectRulesExists()
		}
		if !present {
			if _, err := readProjectTextFileSafe(root, rel); err == nil {
				present = true
			}
		}
		if present {
			out = append(out, AgentContextChip{
				ID:        "always:" + rel,
				Kind:      ChipKindAlways,
				Name:      rel,
				Target:    rel,
				Icon:      MentionIconRule,
				ColorRole: "muted",
				Always:    true,
			})
		}
	}
	return out
}

// ViewportImageIncluded reports whether the toggleable Viewport image chip is enabled.
func (e *Editor) ViewportImageIncluded() bool {
	return e != nil && e.agentComposer.viewportImageOn
}

// SetViewportImageIncluded toggles whether the Viewport image chip is included in assembled requests.
func (e *Editor) SetViewportImageIncluded(on bool) {
	if e == nil || e.agentComposer.viewportImageOn == on {
		return
	}
	e.agentComposer.viewportImageOn = on
	if !on && e.agentComposer.hoveredChipID == viewportImageChipID {
		e.agentComposer.hoveredChipID = ""
	}
	e.notify()
}

// ToggleViewportImage flips the Viewport image chip toggle and returns the new state.
func (e *Editor) ToggleViewportImage() bool {
	if e == nil {
		return false
	}
	next := !e.agentComposer.viewportImageOn
	e.SetViewportImageIncluded(next)
	return next
}

const viewportImageChipID = "viewport:image"

// ViewportImageChip returns the chip descriptor for the Viewport image toggle.
func (e *Editor) ViewportImageChip() AgentContextChip {
	name := "Viewport image"
	if e != nil {
		if lbl := e.T("composer.viewportImage"); lbl != "" && lbl != "composer.viewportImage" {
			name = lbl
		}
	}
	return AgentContextChip{
		ID:        viewportImageChipID,
		Kind:      ChipKindViewportImage,
		Name:      name,
		Target:    "viewport",
		Icon:      MentionIconViewport,
		ColorRole: "muted",
	}
}

// HoveredChipID returns the chip ID currently showing its exact-payload hover popover, or "".
func (e *Editor) HoveredChipID() string {
	if e == nil {
		return ""
	}
	return e.agentComposer.hoveredChipID
}

// SetHoveredChip sets or clears which chip's exact-payload hover preview is open.
func (e *Editor) SetHoveredChip(chipID string) {
	if e == nil || e.agentComposer.hoveredChipID == chipID {
		return
	}
	e.agentComposer.hoveredChipID = chipID
	e.notify()
}

// HoveredChipPayload resolves the exact sent payload for the currently hovered chip.
func (e *Editor) HoveredChipPayload() (AgentChipPayload, bool) {
	if e == nil || e.agentComposer.hoveredChipID == "" {
		return AgentChipPayload{}, false
	}
	return e.ChipPayloadByID(e.agentComposer.hoveredChipID)
}

// ChipPayloadByID finds chipID among attached, always, viewport image, or suggested chips and
// resolves its exact payload via ResolveChipPayload.
func (e *Editor) ChipPayloadByID(chipID string) (AgentChipPayload, bool) {
	if e == nil || chipID == "" {
		return AgentChipPayload{}, false
	}
	for _, c := range e.agentComposer.attached {
		if c.ID == chipID {
			return e.ResolveChipPayload(c), true
		}
	}
	for _, c := range e.AlwaysChips() {
		if c.ID == chipID {
			return e.ResolveChipPayload(c), true
		}
	}
	if chipID == viewportImageChipID {
		return e.ResolveChipPayload(e.ViewportImageChip()), true
	}
	if s, ok := e.SuggestedChip(); ok && s.ID == chipID {
		return e.ResolveChipPayload(s), true
	}
	return AgentChipPayload{}, false
}

// --- Single-source-of-truth payload resolution ---

// ResolveChipPayload computes the exact header, text, and/or image bytes for chip.
// Both the hover preview card and AssembleAgentRequest call this exact function.
func (e *Editor) ResolveChipPayload(chip AgentContextChip) AgentChipPayload {
	payload := AgentChipPayload{Chip: chip}
	if e == nil {
		return payload
	}

	switch chip.Kind {
	case ChipKindObject:
		data, label := e.sceneBytesForChip(chip.SceneFile)
		start, end, block := extractSceneObjectBlock(data, chip.Target)
		if start > 0 && block != "" {
			payload.StartLine = start
			payload.EndLine = end
			payload.Header = formatLineHeader(label, start, end)
			payload.Text = block
		} else if text, _, err := e.ObjectYAML(chip.Target); err == nil {
			trimmed := strings.TrimSuffix(text, "\n")
			n := lineCount(trimmed)
			payload.StartLine = 1
			payload.EndLine = n
			payload.Header = formatLineHeader(label, 1, n)
			payload.Text = trimmed
		} else {
			payload.Header = "# " + label + " · " + chip.Target
		}
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindWholeScene:
		data, label := e.sceneBytesForChip(chip.SceneFile)
		text := strings.TrimSuffix(string(data), "\n")
		n := lineCount(text)
		payload.StartLine = 1
		payload.EndLine = n
		payload.Header = formatLineHeader(label, 1, n)
		payload.Text = text
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindFile, ChipKindPrefab, ChipKindRule, ChipKindAlways:
		text, ok := e.readFileChipText(chip.Target)
		if ok {
			trimmed := strings.TrimSuffix(text, "\n")
			n := lineCount(trimmed)
			payload.StartLine = 1
			payload.EndLine = n
			payload.Header = formatLineHeader(chip.Target, 1, n)
			payload.Text = trimmed
		} else {
			payload.Header = "# " + chip.Target
			payload.Text = chip.Detail
		}
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindAsset:
		if e.Project != nil && e.ProjectPath != "" {
			root := filepath.Dir(e.ProjectPath)
			low := strings.ToLower(chip.Target)
			if isImageAssetPath(low) {
				if raw, err := readProjectBinaryFileSafe(root, chip.Target); err == nil {
					if cfg, _, cfgErr := image.DecodeConfig(bytes.NewReader(raw)); cfgErr == nil && cfg.Width > 0 && cfg.Height > 0 {
						payload.ImageW = cfg.Width
						payload.ImageH = cfg.Height
						payload.Header = fmt.Sprintf("# %s · %d × %d", chip.Target, cfg.Width, cfg.Height)
						if cfg.Width <= maxComposerImageDim && cfg.Height <= maxComposerImageDim &&
							int64(cfg.Width)*int64(cfg.Height) <= maxComposerImagePixels {
							if img, _, decErr := image.Decode(bytes.NewReader(raw)); decErr == nil {
								b := img.Bounds()
								payload.ImageW = b.Dx()
								payload.ImageH = b.Dy()
								payload.ImageSrc = core.ImageSource{Pixels: imageToRGBA(img)}
								var buf bytes.Buffer
								if png.Encode(&buf, img) == nil {
									payload.ImagePNG = buf.Bytes()
								}
								payload.Header = fmt.Sprintf("# %s · %d × %d", chip.Target, payload.ImageW, payload.ImageH)
								payload.Tokens = estimateImageTokens(payload.ImageW, payload.ImageH)
								return payload
							}
						}
						payload.Tokens = estimateTextTokens(payload.Header)
						return payload
					}
				}
			}
		}
		payload.Header = "# " + chip.Target
		payload.Text = chip.Detail
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindProblem:
		if chip.Line > 0 {
			payload.Header = fmt.Sprintf("# Problem · %s:%d", chip.Target, chip.Line)
		} else {
			payload.Header = "# Problem · " + chip.Target
		}
		payload.Text = chip.Detail
		if payload.Text == "" {
			payload.Text = chip.Name
		}
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindPlaytestRun:
		payload.Header = "# Playtest · " + chip.Name
		payload.Text = chip.Detail
		if payload.Text == "" {
			payload.Text = chip.Name
		}
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindHistoryStep:
		payload.Header = "# History · " + chip.Name
		payload.Text = chip.Detail
		if payload.Text == "" {
			payload.Text = chip.Name
		}
		payload.Tokens = estimateTextTokens(payload.Formatted())

	case ChipKindViewportImage:
		pngBytes, imgSrc, w, h := e.captureComposerViewportImage()
		payload.ImagePNG = pngBytes
		payload.ImageSrc = imgSrc
		payload.ImageW = w
		payload.ImageH = h
		payload.Header = fmt.Sprintf("# Viewport image · %d × %d", w, h)
		payload.Tokens = estimateImageTokens(w, h)
	}

	return payload
}

func (e *Editor) openSceneShortName() string {
	if e == nil {
		return "scene"
	}
	if rel := e.openSceneRelPath(); rel != "" {
		base := path.Base(rel)
		base = strings.TrimSuffix(base, ".scene.yaml")
		base = strings.TrimSuffix(base, ".scene.yml")
		return base
	}
	if e.Doc != nil && e.Doc.Name != "" {
		return e.Doc.Name
	}
	return "scene"
}

func (e *Editor) openSceneLabel() string {
	if e == nil {
		return "scene.yaml"
	}
	if rel := e.openSceneRelPath(); rel != "" {
		return path.Base(rel)
	}
	if e.FilePath != "" {
		return filepath.Base(e.FilePath)
	}
	if e.Doc != nil && e.Doc.Name != "" {
		return e.Doc.Name + ".scene.yaml"
	}
	return "scene.yaml"
}

func (e *Editor) sceneBytesForChip(sceneRel string) ([]byte, string) {
	openRel := e.openSceneRelPath()
	isOpen := sceneRel == "" || sceneRel == openRel
	if isOpen {
		label := e.openSceneLabel()
		// When the open scene has no unsaved edits and exists on disk, read disk bytes so
		// comments and line numbers match the file on disk (same rule as editormcp scene_read).
		if e.FilePath != "" && !e.SceneDirty() {
			root := ""
			if e.Project != nil && e.ProjectPath != "" {
				root = filepath.Dir(e.ProjectPath)
			}
			if data, err := readBoundedRegularFileSafe(root, e.FilePath, maxComposerReadBytes); err == nil {
				return data, label
			}
		}
		if data, err := e.SaveYAML(); err == nil {
			return data, label
		}
		return nil, label
	}
	label := path.Base(sceneRel)
	// If the referenced scene is open in an inactive tab with unsaved edits, encode its live doc.
	for i, t := range e.tabs {
		if i == e.activeTab || t == nil || t.doc == nil {
			continue
		}
		if t.scenePath == sceneRel && e.tabDirtyAt(i) {
			if data, err := sceneformat.Encode(t.doc); err == nil {
				return data, label
			}
		}
	}
	if e.Project != nil && e.ProjectPath != "" {
		if data, err := readProjectTextFileSafe(filepath.Dir(e.ProjectPath), sceneRel); err == nil {
			return data, label
		}
	}
	return nil, label
}

func (e *Editor) readFileChipText(rel string) (string, bool) {
	if rel == "" {
		return "", false
	}
	if rel == project.FileName && e.Project != nil && e.ProjectDirty() {
		if data, err := project.Encode(e.Project); err == nil {
			return string(data), true
		}
	}
	if rel == project.BriefFileName && e.BriefExists() && e.BriefDirty() {
		if data, err := e.BriefYAML(); err == nil && len(data) > 0 {
			return string(data), true
		}
	}
	if rel == ProjectRulesFileName && e.ProjectRulesExists() && e.ProjectRulesDirty() {
		return e.ProjectRules(), true
	}
	if e.Project != nil && e.ProjectPath != "" {
		if data, err := readProjectTextFileSafe(filepath.Dir(e.ProjectPath), rel); err == nil {
			return string(data), true
		}
	}
	if rel == project.BriefFileName && e.BriefExists() {
		if data, err := e.BriefYAML(); err == nil && len(data) > 0 {
			return string(data), true
		}
	}
	if rel == ProjectRulesFileName && e.ProjectRulesExists() {
		return e.ProjectRules(), true
	}
	if p, ok := e.Prefabs[rel]; ok {
		obj := sceneformat.ObjectDoc{
			ID:         rel,
			Name:       p.Name,
			Sprite:     p.Sprite,
			Components: p.Components,
			Properties: p.Properties,
		}
		doc := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers:  []sceneformat.LayerDoc{{Name: "prefab", Objects: []sceneformat.ObjectDoc{obj}}},
		}
		if data, err := sceneformat.Encode(doc); err == nil {
			return string(data), true
		}
	}
	return "", false
}

func sanitizeComposerProjectRelPath(root, rel string) (string, error) {
	raw := strings.TrimSpace(rel)
	if raw == "" || strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("editor: invalid project path %q", rel)
	}
	// Reject Windows drive-letter prefixes (e.g. C:\...) on all platforms.
	if len(raw) >= 2 && raw[1] == ':' && ((raw[0] >= 'a' && raw[0] <= 'z') || (raw[0] >= 'A' && raw[0] <= 'Z')) {
		if root == "" {
			return "", fmt.Errorf("editor: path leaves project %q", rel)
		}
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) {
		if root == "" {
			return "", fmt.Errorf("editor: path leaves project %q", rel)
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		relFromRoot, err := filepath.Rel(absRoot, filepath.Clean(raw))
		if err != nil || relFromRoot == "." || relFromRoot == ".." || strings.HasPrefix(relFromRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relFromRoot) {
			return "", fmt.Errorf("editor: path leaves project %q", rel)
		}
		raw = relFromRoot
	}
	slashed := strings.ReplaceAll(raw, `\`, "/")
	clean := path.Clean(slashed)
	if path.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(clean, ':') {
		return "", fmt.Errorf("editor: path leaves project %q", rel)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") {
		return "", fmt.Errorf("editor: .git path %q is not attachable", rel)
	}
	return clean, nil
}

func resolveSafeProjectRegularPath(root, rel string) (string, error) {
	if root == "" {
		return "", fs.ErrNotExist
	}
	clean, err := sanitizeComposerProjectRelPath(root, rel)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(absRoot, filepath.FromSlash(clean))
	lfi, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("editor: symlink file refused")
	}
	if !lfi.Mode().IsRegular() {
		return "", errors.New("editor: not a regular file")
	}
	if err := verifyFileWithinRoot(absRoot, abs, clean); err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return realPath, nil
}

func readBoundedRegularFileSafe(root, absPath string, maxBytes int64) ([]byte, error) {
	if absPath == "" {
		return nil, fs.ErrNotExist
	}
	lfi, err := os.Lstat(absPath)
	if err != nil {
		return nil, err
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("editor: symlink file refused")
	}
	if !lfi.Mode().IsRegular() {
		return nil, errors.New("editor: not a regular file")
	}
	if root != "" {
		if err := verifyFileWithinRoot(root, absPath, absPath); err != nil {
			return nil, err
		}
	}
	return readBoundedOpenFile(absPath, maxBytes, false)
}

func readBoundedOpenFile(path string, maxBytes int64, allowTruncate bool) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("editor: not a regular file")
	}
	if !allowTruncate && st.Size() > maxBytes {
		return nil, errors.New("editor: file exceeds size limit")
	}
	limit := maxBytes
	if !allowTruncate {
		limit = maxBytes + 1
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}
	if !allowTruncate && int64(len(data)) > maxBytes {
		return nil, errors.New("editor: file exceeds size limit")
	}
	return data, nil
}

func readProjectPrefixSafe(root, rel string, maxBytes int64) ([]byte, error) {
	realPath, err := resolveSafeProjectRegularPath(root, rel)
	if err != nil {
		return nil, err
	}
	return readBoundedOpenFile(realPath, maxBytes, true)
}

func readProjectTextFileSafe(root, rel string) ([]byte, error) {
	data, err := readProjectBinaryFileSafe(root, rel)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("editor: not a UTF-8 text file")
	}
	return data, nil
}

func readProjectBinaryFileSafe(root, rel string) ([]byte, error) {
	realPath, err := resolveSafeProjectRegularPath(root, rel)
	if err != nil {
		return nil, err
	}
	return readBoundedOpenFile(realPath, maxComposerReadBytes, false)
}

// extractSceneObjectBlock locates object id inside YAML scene bytes data, returning its
// 1-based start/end lines and the exact YAML lines dedented to column 0.
func extractSceneObjectBlock(data []byte, id string) (startLine, endLine int, block string) {
	if len(data) == 0 || id == "" {
		return 0, 0, ""
	}
	rawLines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(rawLines) == 0 {
		return 0, 0, ""
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err == nil && root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		doc := yamlDeref(root.Content[0])
		if doc != nil && doc.Kind == yaml.MappingNode {
			if objNode := findYAMLObjectNode(doc, id); objNode != nil {
				minL, maxL := yamlLineBounds(objNode)
				if minL >= 1 && maxL >= minL && maxL <= len(rawLines) {
					return minL, maxL, dedentYAMLLines(rawLines[minL-1 : maxL])
				}
			}
		}
	}

	// Fallback line scan for scenes with comments/formatting.
	for i, line := range rawLines {
		trimmed := strings.TrimLeft(line, " ")
		v, ok := strings.CutPrefix(trimmed, "- id:")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if unq, err := strconv.Unquote(v); err == nil {
			v = unq
		} else if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
			v = strings.ReplaceAll(v[1:len(v)-1], "''", "'")
		}
		if v != id {
			continue
		}
		indent := len(line) - len(trimmed)
		end := i + 1
		for end < len(rawLines) {
			next := rawLines[end]
			rest := strings.TrimLeft(next, " ")
			if rest != "" && len(next)-len(rest) <= indent {
				break
			}
			end++
		}
		for end > i+1 && strings.TrimSpace(rawLines[end-1]) == "" {
			end--
		}
		return i + 1, end, dedentYAMLLines(rawLines[i:end])
	}
	return 0, 0, ""
}

func findYAMLObjectNode(doc *yaml.Node, id string) *yaml.Node {
	layers := yamlField(doc, "layers")
	if layers == nil || layers.Kind != yaml.SequenceNode {
		return nil
	}
	for _, ln := range layers.Content {
		ln = yamlDeref(ln)
		if ln == nil || ln.Kind != yaml.MappingNode {
			continue
		}
		if found := findYAMLObjectInSeq(yamlField(ln, "objects"), id, 0); found != nil {
			return found
		}
	}
	return nil
}

func findYAMLObjectInSeq(seq *yaml.Node, id string, depth int) *yaml.Node {
	if seq == nil || seq.Kind != yaml.SequenceNode || depth > 128 {
		return nil
	}
	for _, item := range seq.Content {
		item = yamlDeref(item)
		if item == nil || item.Kind != yaml.MappingNode {
			continue
		}
		if yamlFieldString(item, "id") == id {
			return item
		}
		if found := findYAMLObjectInSeq(yamlField(item, "children"), id, depth+1); found != nil {
			return found
		}
	}
	return nil
}

func dedentYAMLLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	first := lines[0]
	trimFirst := strings.TrimLeft(first, " ")
	strip := len(first) - len(trimFirst)
	// Keep the leading "- " aligned at column 0 when the object is a sequence item.
	if strip > 0 && !strings.HasPrefix(trimFirst, "- ") {
		if idx := strings.Index(first, "- "); idx >= 0 && idx < strip {
			strip = idx
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if len(l) >= strip && strings.TrimSpace(l[:strip]) == "" {
			out[i] = l[strip:]
		} else {
			out[i] = strings.TrimLeft(l, " ")
		}
	}
	return strings.Join(out, "\n")
}

func formatLineHeader(label string, start, end int) string {
	if start <= 0 || end <= 0 {
		return "# " + label
	}
	if start == end {
		return fmt.Sprintf("# %s · line %d", label, start)
	}
	return fmt.Sprintf("# %s · lines %d–%d", label, start, end)
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func (e *Editor) captureComposerViewportImage() ([]byte, core.ImageSource, int, int) {
	if e == nil {
		return nil, core.ImageSource{}, 0, 0
	}
	if e.agentComposer.cachedViewportRev == e.revision && len(e.agentComposer.cachedViewportPNG) > 0 {
		return slices.Clone(e.agentComposer.cachedViewportPNG), e.agentComposer.cachedViewportSrc, e.agentComposer.cachedViewportW, e.agentComposer.cachedViewportH
	}
	w, h := e.CanvasSize()
	if w <= 0 || h <= 0 {
		w, h = 640, 360
		if e.Doc != nil && e.Doc.VirtualWidth > 0 && e.Doc.VirtualHeight > 0 {
			w, h = float32(e.Doc.VirtualWidth), float32(e.Doc.VirtualHeight)
		}
	}
	var d core.Drawing
	e.DrawViewport(&d, core.Size{Width: core.Points(w), Height: core.Points(h)})
	pw, ph := max(1, int(math.Round(float64(w)))), max(1, int(math.Round(float64(h))))
	if pw > maxComposerViewportDim || ph > maxComposerViewportDim {
		scale := float64(maxComposerViewportDim) / float64(max(pw, ph))
		pw = max(1, int(math.Round(float64(pw)*scale)))
		ph = max(1, int(math.Round(float64(ph)*scale)))
	}
	res, err := gamemcp.RenderPNG(d.Ops, pw, ph, 0)
	if err != nil || len(res.PNG) == 0 {
		return nil, core.ImageSource{}, pw, ph
	}
	var src core.ImageSource
	if img, _, decErr := image.Decode(bytes.NewReader(res.PNG)); decErr == nil {
		src = core.ImageSource{Pixels: imageToRGBA(img)}
	}
	e.agentComposer.cachedViewportPNG = slices.Clone(res.PNG)
	e.agentComposer.cachedViewportSrc = src
	e.agentComposer.cachedViewportW = res.Width
	e.agentComposer.cachedViewportH = res.Height
	e.agentComposer.cachedViewportRev = e.revision
	return res.PNG, src, res.Width, res.Height
}

// --- Request Assembly & Sending ---

// AssembleAgentRequest assembles the exact request sent to the agent from the active chips
// (Always chips + explicitly attached chips + Viewport image if toggled on) and NOTHING else.
// Suggested selection chips are never included unless explicitly added.
func (e *Editor) AssembleAgentRequest(prompt string) AgentRequest {
	req := AgentRequest{
		ThreadID:    e.AgentThreadID(),
		Prompt:      strings.TrimSpace(prompt),
		SummarySeed: e.agentComposer.summarySeed,
	}
	if e == nil {
		return req
	}

	var chips []AgentContextChip
	chips = append(chips, e.AlwaysChips()...)
	chips = append(chips, e.agentComposer.attached...)
	if e.agentComposer.viewportImageOn {
		chips = append(chips, e.ViewportImageChip())
	}

	var blocks []string
	if e.agentComposer.compactedSummary != "" {
		blocks = append(blocks, "# Compacted earlier requests\n"+e.agentComposer.compactedSummary)
	} else if e.agentComposer.summarySeed != "" {
		blocks = append(blocks, "# Thread summary\n"+e.agentComposer.summarySeed)
	}

	req.Attachments = make([]AgentChipPayload, 0, len(chips))
	for _, c := range chips {
		payload := e.ResolveChipPayload(c)
		if len(payload.ImagePNG) > 0 {
			payload.ImagePNG = slices.Clone(payload.ImagePNG)
		}
		req.Attachments = append(req.Attachments, payload)
		if c.Kind == ChipKindViewportImage {
			req.HasViewportImg = true
			req.ViewportImage = slices.Clone(payload.ImagePNG)
			req.ViewportWidth = payload.ImageW
			req.ViewportHeight = payload.ImageH
			if payload.Header != "" {
				blocks = append(blocks, payload.Header)
			}
			continue
		}
		if formatted := payload.Formatted(); formatted != "" {
			blocks = append(blocks, formatted)
		}
	}

	req.AssembledContext = joinContextBlocksBounded(blocks, maxAssembledContextBytes)
	return req
}

func joinContextBlocksBounded(blocks []string, maxBytes int) string {
	if len(blocks) == 0 || maxBytes <= 0 {
		return ""
	}
	const truncMarker = "\n… (truncated to context budget)"
	var b strings.Builder
	for i, blk := range blocks {
		sep := ""
		if i > 0 {
			sep = "\n\n"
		}
		if b.Len()+len(sep)+len(blk) <= maxBytes {
			b.WriteString(sep)
			b.WriteString(blk)
			continue
		}
		remain := maxBytes - b.Len() - len(sep) - len(truncMarker)
		if remain > 0 {
			b.WriteString(sep)
			// Truncate cleanly on a UTF-8 rune boundary.
			cut := remain
			for cut > 0 && !utf8.RuneStart(blk[cut]) {
				cut--
			}
			b.WriteString(blk[:cut])
		}
		b.WriteString(truncMarker)
		break
	}
	return b.String()
}

func cloneAgentRequest(req AgentRequest) AgentRequest {
	out := req
	if len(req.ViewportImage) > 0 {
		out.ViewportImage = slices.Clone(req.ViewportImage)
	}
	if len(req.Attachments) > 0 {
		out.Attachments = make([]AgentChipPayload, len(req.Attachments))
		for i, a := range req.Attachments {
			if len(a.ImagePNG) > 0 {
				a.ImagePNG = slices.Clone(a.ImagePNG)
			}
			out.Attachments[i] = a
		}
	}
	return out
}

// SendComposerPrompt assembles the request from the current draft and active chips (and nothing else),
// delivers it to Editor.OnSendAgentComposerRequest when set, records the turn in the active thread, updates
// token usage from the agent's reported usage (or estimates it), and clears the draft.
func (e *Editor) SendComposerPrompt() (AgentRequest, error) {
	if e == nil {
		return AgentRequest{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return AgentRequest{}, ErrPlaying
	}
	prompt := strings.TrimSpace(e.composerDraft)
	if prompt == "" {
		return AgentRequest{}, errors.New("editor: composer prompt is empty")
	}
	req := e.AssembleAgentRequest(prompt)

	var (
		resp AgentResponse
		err  error
	)
	if e.OnSendAgentComposerRequest != nil {
		resp, err = e.OnSendAgentComposerRequest(req)
		if err != nil {
			e.Status = err.Error()
			e.notify()
			return req, err
		}
	}

	chipNames := make([]string, 0, len(req.Attachments))
	for _, a := range req.Attachments {
		chipNames = append(chipNames, a.Chip.Name)
	}
	turnTokens := estimateTextTokens(req.Prompt) + estimateTextTokens(resp.Reply)
	for _, a := range req.Attachments {
		turnTokens += a.Tokens
	}
	if turnTokens < 64 {
		turnTokens = 64
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	turn := AgentThreadTurn{
		ID:        fmt.Sprintf("turn-%d", len(e.agentComposer.turns)+1),
		Prompt:    req.Prompt,
		Reply:     resp.Reply,
		Chips:     chipNames,
		Tokens:    turnTokens,
		Timestamp: now,
		Outcome:   "sent",
	}
	e.agentComposer.turns = append(e.agentComposer.turns, turn)
	e.agentComposer.threadRev++
	reqCopy := cloneAgentRequest(req)
	e.agentComposer.lastRequest = &reqCopy

	if resp.Usage != nil {
		u := *resp.Usage
		if u.Limit <= 0 {
			u.Limit = defaultContextLimitTokens
		}
		e.agentComposer.reportedUsage = &u
	} else if e.agentComposer.reportedUsage != nil {
		e.agentComposer.reportedUsage.Conversation += turnTokens
	}

	e.composerDraft = ""
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerQuery = ""
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.hoveredChipID = ""
	e.Status = e.T("composer.sentStatus")
	e.notify()
	return req, nil
}

// submitComposerFromUI routes UI submission (Enter / Mod+Enter / Send button) through
// OnSendAgentComposerRequest when configured, through AgentRunner / connected MCP agent when active,
// or through SendComposerPrompt otherwise so both scripted tests and live external runners work seamlessly.
func (e *Editor) submitComposerFromUI() {
	if e == nil || strings.TrimSpace(e.composerDraft) == "" {
		return
	}
	if e.AgentOffline() {
		e.QueueAgentRequest("")
		return
	}
	if e.OnSendAgentComposerRequest != nil {
		if _, err := e.SendComposerPrompt(); err != nil {
			e.report(err)
		}
		return
	}
	if e.AgentRunner != nil || len(e.agent.clients) > 0 || e.agent.mode == AgentModeLocal {
		e.report(e.SubmitAgentPrompt(""))
		return
	}
	if _, err := e.SendComposerPrompt(); err != nil {
		e.report(err)
	}
}

// LastSentAgentRequest returns a copy of the last request assembled and sent by SendComposerPrompt, or nil.
func (e *Editor) LastSentAgentRequest() *AgentRequest {
	if e == nil || e.agentComposer.lastRequest == nil {
		return nil
	}
	c := cloneAgentRequest(*e.agentComposer.lastRequest)
	return &c
}

// RecallLastComposerPrompt restores the most recently sent prompt into an empty composer (Spec: ↑ in empty composer).
func (e *Editor) RecallLastComposerPrompt() bool {
	if e == nil || strings.TrimSpace(e.composerDraft) != "" {
		return false
	}
	if len(e.agentComposer.turns) > 0 {
		e.composerDraft = e.agentComposer.turns[len(e.agentComposer.turns)-1].Prompt
		e.notify()
		return true
	}
	for i := len(e.agent.turns) - 1; i >= 0; i-- {
		if !e.agent.turns[i].External && strings.TrimSpace(e.agent.turns[i].Prompt) != "" {
			e.composerDraft = e.agent.turns[i].Prompt
			e.notify()
			return true
		}
	}
	return false
}

// --- Context Meter, Breakdown, Compact & Start Fresh ---

// AgentThreadID returns the active agent thread identifier.
func (e *Editor) AgentThreadID() string {
	if e == nil {
		return ""
	}
	if e.agentComposer.threadID == "" {
		e.agentComposer.threadSeq = 1
		e.agentComposer.threadID = "thread-1"
	}
	return e.agentComposer.threadID
}

// AgentThreadTurns returns a copy of the turns currently held in the active thread.
func (e *Editor) AgentThreadTurns() []AgentThreadTurn {
	if e == nil || len(e.agentComposer.turns) == 0 {
		return nil
	}
	return slices.Clone(e.agentComposer.turns)
}

// AddAgentThreadTurn appends a turn to the active thread (useful for hydrating thread state or tests).
func (e *Editor) AddAgentThreadTurn(turn AgentThreadTurn) {
	if e == nil {
		return
	}
	if turn.ID == "" {
		turn.ID = fmt.Sprintf("turn-%d", len(e.agentComposer.turns)+1)
	}
	if turn.Tokens <= 0 {
		turn.Tokens = max(64, estimateTextTokens(turn.Prompt)+estimateTextTokens(turn.Reply))
	}
	e.agentComposer.turns = append(e.agentComposer.turns, turn)
	e.agentComposer.threadRev++
	e.notify()
}

// AgentThreadSummary returns the compacted summary or fresh-thread summary seed, if any.
func (e *Editor) AgentThreadSummary() string {
	if e == nil {
		return ""
	}
	if e.agentComposer.compactedSummary != "" {
		return e.agentComposer.compactedSummary
	}
	return e.agentComposer.summarySeed
}

// SetAgentTokenUsage updates the context meter from the agent's reported token usage.
func (e *Editor) SetAgentTokenUsage(usage AgentTokenUsage) {
	if e == nil {
		return
	}
	if usage.Limit <= 0 {
		usage.Limit = defaultContextLimitTokens
	}
	u := usage
	e.agentComposer.reportedUsage = &u
	e.notify()
}

// ContextBreakdownOpen reports whether the context ring breakdown popover is open.
func (e *Editor) ContextBreakdownOpen() bool {
	return e != nil && e.agentComposer.breakdownOpen
}

// SetContextBreakdownOpen opens or closes the context ring breakdown popover.
func (e *Editor) SetContextBreakdownOpen(open bool) {
	if e == nil || e.agentComposer.breakdownOpen == open {
		return
	}
	e.agentComposer.breakdownOpen = open
	if open {
		e.agentComposer.pickerOpen = false
		e.agentComposer.hoveredChipID = ""
	}
	e.notify()
}

// ToggleContextBreakdown toggles the context ring breakdown popover.
func (e *Editor) ToggleContextBreakdown() bool {
	if e == nil {
		return false
	}
	next := !e.agentComposer.breakdownOpen
	e.SetContextBreakdownOpen(next)
	return next
}

// ContextMeter computes the live context ring percentage, warning level, and 6-category breakdown.
func (e *Editor) ContextMeter() ContextMeterSnapshot {
	if e == nil {
		return ContextMeterSnapshot{LimitTokens: defaultContextLimitTokens}
	}

	usage := e.effectiveTokenUsage()
	limit := usage.Limit
	if limit <= 0 {
		limit = defaultContextLimitTokens
	}
	used := usage.Total()
	pct := 0
	if limit > 0 {
		pct = int(math.Round(float64(used) * 100 / float64(limit)))
	}
	pct = max(0, min(100, pct))

	level := ContextMeterNormal
	switch {
	case pct >= 90:
		level = ContextMeterCritical
	case pct > 70:
		level = ContextMeterWarning
	}

	rowPct := func(tok int) int {
		if limit <= 0 || tok <= 0 {
			return 0
		}
		p := int(math.Round(float64(tok) * 100 / float64(limit)))
		if p == 0 && tok > 0 {
			p = 1
		}
		return min(100, p)
	}

	convLabel := e.formatConversationBucketLabel()
	sceneLabel := e.T("composer.bucket.openScene", l10n.Param("scene", e.openSceneShortName()))
	attachedLabel := e.formatAttachedBucketLabel()
	alwaysLabel := e.formatAlwaysBucketLabel()

	rawRows := []struct {
		key    string
		label  string
		tokens int
	}{
		{"conversation", convLabel, usage.Conversation},
		{"scene", sceneLabel, usage.OpenScene},
		{"attached", attachedLabel, usage.Attached},
		{"always", alwaysLabel, usage.Always},
		{"tools", e.T("composer.bucket.toolsAndInstr"), usage.ToolsAndInstr},
		{"viewport", e.T("composer.bucket.viewportImage"), usage.ViewportImage},
	}

	rows := make([]ContextBreakdownRow, len(rawRows))
	for i, r := range rawRows {
		rp := rowPct(r.tokens)
		rows[i] = ContextBreakdownRow{
			Key:       r.key,
			Label:     r.label,
			Tokens:    r.tokens,
			Percent:   rp,
			Highlight: rp > 30,
		}
	}

	compactable := 0
	if len(e.agentComposer.turns) > 1 {
		compactable = len(e.agentComposer.turns) - 1
	}
	return ContextMeterSnapshot{
		UsedTokens:          used,
		LimitTokens:         limit,
		Percent:             pct,
		Level:               level,
		Rows:                rows,
		CompactableRequests: compactable,
		HasCompactedSummary: e.agentComposer.compactedSummary != "",
		CompactedSummary:    e.agentComposer.compactedSummary,
	}
}

func (e *Editor) effectiveTokenUsage() AgentTokenUsage {
	if e.agentComposer.reportedUsage != nil {
		u := *e.agentComposer.reportedUsage
		if !e.agentComposer.viewportImageOn {
			u.ViewportImage = 0
		}
		return u
	}

	// Estimate live token usage from conversation turns + active chips when no external agent has reported yet.
	conv := 0
	if e.agentComposer.compactedSummary != "" {
		conv += estimateTextTokens(e.agentComposer.compactedSummary)
	} else if e.agentComposer.summarySeed != "" {
		conv += estimateTextTokens(e.agentComposer.summarySeed)
	}
	for _, t := range e.agentComposer.turns {
		conv += t.Tokens
	}

	openSceneTokens := 0
	attachedTokens := 0
	for _, c := range e.agentComposer.attached {
		p := e.ResolveChipPayload(c)
		if c.Kind == ChipKindWholeScene {
			openSceneTokens += p.Tokens
		} else {
			attachedTokens += p.Tokens
		}
	}
	alwaysTokens := 0
	for _, c := range e.AlwaysChips() {
		alwaysTokens += e.ResolveChipPayload(c).Tokens
	}
	vpTokens := 0
	if e.agentComposer.viewportImageOn {
		vpTokens = defaultViewportImgTokens
	}

	return AgentTokenUsage{
		Limit:         defaultContextLimitTokens,
		Conversation:  conv,
		OpenScene:     openSceneTokens,
		Attached:      attachedTokens,
		Always:        alwaysTokens,
		ToolsAndInstr: defaultToolsInstrTokens,
		ViewportImage: vpTokens,
	}
}

func (e *Editor) formatConversationBucketLabel() string {
	n := len(e.agentComposer.turns)
	if e.agentComposer.compactedSummary != "" {
		if n == 1 {
			return e.T("composer.bucket.conversationSummaryOne")
		}
		return e.T("composer.bucket.conversationSummaryMany", l10n.Param("count", n))
	}
	if n == 1 {
		return e.T("composer.bucket.conversationOne")
	}
	return e.T("composer.bucket.conversationMany", l10n.Param("count", n))
}

func (e *Editor) formatAttachedBucketLabel() string {
	if len(e.agentComposer.attached) == 0 {
		return e.T("composer.bucket.attachedNone")
	}
	names := make([]string, 0, len(e.agentComposer.attached))
	for _, c := range e.agentComposer.attached {
		names = append(names, c.Name)
	}
	if len(names) > 3 {
		return e.T("composer.bucket.attachedMore",
			l10n.Param("items", strings.Join(names[:3], ", ")),
			l10n.Param("more", len(names)-3),
		)
	}
	return e.T("composer.bucket.attachedList", l10n.Param("items", strings.Join(names, ", ")))
}

func (e *Editor) formatAlwaysBucketLabel() string {
	always := e.AlwaysChips()
	if len(always) == 0 {
		return e.T("composer.bucket.alwaysNone")
	}
	names := make([]string, 0, len(always))
	for _, c := range always {
		names = append(names, c.Name)
	}
	return e.T("composer.bucket.alwaysList", l10n.Param("items", strings.Join(names, ", ")))
}

// CompactAgentThread folds earlier requests into a concise summary while keeping the last request
// whole, and drops conversation token usage so the context ring reflects the compaction.
func (e *Editor) CompactAgentThread() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	turns := e.agentComposer.turns
	hasReportedConv := e.agentComposer.reportedUsage != nil && e.agentComposer.reportedUsage.Conversation > 0
	if len(turns) < 2 && !hasReportedConv {
		return "", errors.New("editor: nothing earlier to compact")
	}

	var earlier []AgentThreadTurn
	var last []AgentThreadTurn
	if len(turns) >= 2 {
		earlier = turns[:len(turns)-1]
		last = turns[len(turns)-1:]
	} else {
		last = turns
	}

	summary := buildThreadSummary(e.agentComposer.compactedSummary, earlier)
	var customUsage *AgentTokenUsage
	if e.OnCompactAgentThread != nil {
		s, u, err := e.OnCompactAgentThread(slices.Clone(earlier), slices.Clone(last))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(s) != "" {
			summary = strings.TrimSpace(s)
		}
		customUsage = u
	}

	e.agentComposer.compactedSummary = summary
	if len(last) > 0 {
		e.agentComposer.turns = slices.Clone(last)
	}
	e.agentComposer.threadRev++

	if customUsage != nil {
		u := *customUsage
		if u.Limit <= 0 {
			u.Limit = defaultContextLimitTokens
		}
		e.agentComposer.reportedUsage = &u
	} else if e.agentComposer.reportedUsage != nil {
		// Drop conversation tokens to the summary cost plus the kept last request share.
		prevConv := e.agentComposer.reportedUsage.Conversation
		summaryTokens := max(256, estimateTextTokens(summary))
		lastShare := 0
		if len(turns) >= 2 {
			lastShare = prevConv / len(turns)
		} else {
			lastShare = prevConv / 4
		}
		newConv := summaryTokens + lastShare
		if newConv >= prevConv && prevConv > summaryTokens {
			newConv = max(summaryTokens, prevConv/3)
		}
		e.agentComposer.reportedUsage.Conversation = newConv
	}

	e.Status = e.T("composer.compactedStatus")
	e.notify()
	return summary, nil
}

// StartFreshAgentThread starts a new thread seeded with a summary of the current thread,
// clears the old turns and transient attached chips, and drops context usage.
func (e *Editor) StartFreshAgentThread() (string, error) {
	if e == nil {
		return "", errors.New("editor: nil editor")
	}
	summary := buildThreadSummary(e.agentComposer.compactedSummary, e.agentComposer.turns)
	if summary == "" {
		summary = e.agentComposer.summarySeed
	}
	if summary == "" {
		summary = fmt.Sprintf("Fresh thread on %s.", e.openSceneShortName())
	}

	if e.OnStartFreshAgentThread != nil {
		if err := e.OnStartFreshAgentThread(summary); err != nil {
			return "", err
		}
	}

	e.StopAgentRun()
	e.agentComposer.threadSeq++
	e.agentComposer.threadID = fmt.Sprintf("thread-%d", e.agentComposer.threadSeq)
	e.agentComposer.threadRev++
	e.agent.threadID = e.agentComposer.threadID
	e.agent.sessionID = ""
	e.agent.turns = nil
	e.agent.turnChangeSeq++
	e.agentComposer.turns = nil
	e.agentComposer.compactedSummary = ""
	e.agentComposer.summarySeed = summary
	e.agentComposer.attached = nil
	e.agentComposer.hoveredChipID = ""
	e.agentComposer.breakdownOpen = false
	e.agentComposer.pickerOpen = false

	seedTokens := max(256, estimateTextTokens(summary))
	if e.agentComposer.reportedUsage != nil {
		e.agentComposer.reportedUsage.Conversation = seedTokens
		e.agentComposer.reportedUsage.Attached = 0
	}

	e.Status = e.T("composer.startedFreshStatus")
	e.notify()
	return summary, nil
}

func buildThreadSummary(existing string, turns []AgentThreadTurn) string {
	var parts []string
	if strings.TrimSpace(existing) != "" {
		parts = append(parts, strings.TrimSpace(existing))
	}
	for _, t := range turns {
		p := strings.TrimSpace(t.Prompt)
		if p == "" {
			continue
		}
		p = truncateRunes(p, 100)
		line := "- " + p
		if r := strings.TrimSpace(t.Reply); r != "" {
			r = truncateRunes(r, 80)
			line += " → " + r
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, "\n")
}

func estimateTextTokens(s string) int {
	if s == "" {
		return 0
	}
	// Standard ~4 chars per token approximation, minimum 1 for non-empty text.
	return max(1, (len(s)+3)/4)
}

func estimateImageTokens(w, h int) int {
	if w <= 0 || h <= 0 {
		return defaultViewportImgTokens
	}
	// Tile-based estimate capped reasonably.
	tiles := max(1, ((w+511)/512)*((h+511)/512))
	return tiles * 1500
}

func formatTokenCountShort(n int) string {
	if n >= 1000 {
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return strconv.Itoa(n)
}
