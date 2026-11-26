package editor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// AgentStuckFailureThreshold is the number of consecutive failures of the same tool that pauses a request.
const AgentStuckFailureThreshold = 3

const (
	maxTurnMutations          = 2048
	maxFailedCallSummaryRunes = 200
	maxFailedCallErrorRunes   = 512
)

func lowerFirstRune(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[size:]
}

func upperFirstRune(s string) string {
	if s == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// AgentPartialState holds the panel state when only part of a request could be completed.
type AgentPartialState struct {
	DoneCount     int
	TotalCount    int
	Target        string
	Title         string
	Reason        string
	PointPrompt   string
	Pointing      bool
	HasPoint      bool
	PointedX      float32
	PointedY      float32
	Kept          bool
	TryingAnother bool
}

// EffectiveTitle returns the card heading, defaulting to "Done N of M · couldn't place <target>".
func (p AgentPartialState) EffectiveTitle() string {
	if strings.TrimSpace(p.Title) != "" {
		return p.Title
	}
	if p.TotalCount > 0 && p.Target != "" {
		return fmt.Sprintf("Done %d of %d · couldn't place %s", p.DoneCount, p.TotalCount, p.Target)
	}
	if p.TotalCount > 0 {
		return fmt.Sprintf("Done %d of %d", p.DoneCount, p.TotalCount)
	}
	if p.Target != "" {
		return fmt.Sprintf("Couldn't complete %s", p.Target)
	}
	return "Completed partially"
}

// EffectivePointPrompt returns the inline viewport pointing instruction.
func (p AgentPartialState) EffectivePointPrompt() string {
	if strings.TrimSpace(p.PointPrompt) != "" {
		return p.PointPrompt
	}
	if p.Target != "" {
		return fmt.Sprintf("Click where %s should go", p.Target)
	}
	return "Click in the viewport where it should go"
}

// AgentFailedCall records one refused tool call listed in the Stuck card.
type AgentFailedCall struct {
	ToolName string
	Call     string
	Error    string
}

// AgentStuckResolution tracks whether a paused Stuck card is waiting, continued with a hint, or stopped.
type AgentStuckResolution string

const (
	AgentStuckWaiting   AgentStuckResolution = ""
	AgentStuckContinued AgentStuckResolution = "continued"
	AgentStuckStopped   AgentStuckResolution = "stopped"
)

// AgentStuckState holds the state when the same tool fails three times in a row and pauses the request.
type AgentStuckState struct {
	ToolName       string
	Failures       []AgentFailedCall
	HintDraft      string
	SubmittedHint  string
	Resolution     AgentStuckResolution
	NothingChanged bool
}

// AgentLostKind classifies whether a request was interrupted by a lost MCP connection or a model API error.
type AgentLostKind string

const (
	AgentLostConnection AgentLostKind = "connection"
	AgentLostModelError AgentLostKind = "model_error"
)

// AgentLostState holds the state when the connection or model fails mid-request.
type AgentLostState struct {
	Kind             AgentLostKind
	AgentName        string
	LostAt           time.Time
	AfterStep        int
	DuringStep       int
	Title            string
	Detail           string
	RetryAttempt     int
	RetryTotal       int
	RetryIn          time.Duration
	FallbackAgent    string
	Reconnected      bool
	AskResumePending bool
	Resumed          bool
}

// EffectiveTitle returns the Lost card heading.
func (l AgentLostState) EffectiveTitle() string {
	if strings.TrimSpace(l.Title) != "" {
		return l.Title
	}
	if l.Kind == AgentLostModelError {
		return "Model request failed"
	}
	if l.AfterStep > 0 {
		return fmt.Sprintf("Connection lost after step %d", l.AfterStep)
	}
	return "Connection lost"
}

// EffectiveBody returns the Lost card description with timestamp and interrupted step.
func (l AgentLostState) EffectiveBody() string {
	if strings.TrimSpace(l.Detail) != "" {
		return l.Detail
	}
	if l.Kind == AgentLostModelError {
		var parts []string
		if l.RetryTotal > 0 {
			if l.RetryIn > 0 {
				parts = append(parts, fmt.Sprintf("Retrying in %d s · try %d of %d.", int(math.Round(l.RetryIn.Seconds())), max(1, l.RetryAttempt), l.RetryTotal))
			} else {
				parts = append(parts, fmt.Sprintf("Try %d of %d.", max(1, l.RetryAttempt), l.RetryTotal))
			}
		}
		if l.FallbackAgent != "" {
			parts = append(parts, fmt.Sprintf("You can switch to %s for now.", l.FallbackAgent))
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
		return "The model stopped answering. What was done is in a review; nothing is half-written."
	}
	name := strings.TrimSpace(l.AgentName)
	if name == "" {
		name = "The agent"
	}
	when := ""
	if !l.LostAt.IsZero() {
		when = " at " + l.LostAt.Format("15:04")
	}
	step := l.DuringStep
	if step <= 0 {
		step = l.AfterStep + 1
	}
	return fmt.Sprintf("%s stopped answering%s during step %d. What was done is in a review; nothing is half-written.", name, when, step)
}

// AgentStoppedState holds the summary of a user-stopped turn and whether Resume has picked it up.
type AgentStoppedState struct {
	AtStep         int
	TotalSteps     int
	Resumed        bool
	ResumedAtTitle string
}

// EffectiveTitle returns the Stopped card heading.
func (s AgentStoppedState) EffectiveTitle() string {
	if s.Resumed {
		if s.ResumedAtTitle != "" {
			return fmt.Sprintf("Resumed at %s", lowerFirstRune(s.ResumedAtTitle))
		}
		return "Resumed"
	}
	if s.TotalSteps > 0 {
		return fmt.Sprintf("Stopped at step %d of %d", s.AtStep, s.TotalSteps)
	}
	if s.AtStep > 0 {
		return fmt.Sprintf("Stopped at step %d", s.AtStep)
	}
	return "Stopped"
}

// AgentMismatchRow is one comparison row in the Summary-vs-Review mismatch table.
type AgentMismatchRow struct {
	What      string
	Summary   string
	Review    string
	Disagrees bool
}

// AgentMismatchReport describes a disagreement between an agent's summary text and the actual diff in its review.
type AgentMismatchReport struct {
	Title       string
	ClaimedText string
	Rows        []AgentMismatchRow
	Reason      string
	AskedWhy    bool
}

// AgentTurnReview captures the actual scene diff produced during one agent turn.
// The review, never the agent's prose summary, is what Keep applies.
type AgentTurnReview struct {
	TurnID       string
	File         string
	Added        []SceneObjectChange
	Changed      []SceneObjectChange
	Removed      []SceneObjectChange
	PlusLines    int
	MinusLines   int
	HistoryStart int
	HistoryEnd   int
	Kept         bool
	Discarded    bool
}

// Count returns the total number of added, changed, and removed scene objects in the review.
func (r *AgentTurnReview) Count() int {
	if r == nil {
		return 0
	}
	return len(r.Added) + len(r.Changed) + len(r.Removed)
}

// Clone returns a shallow-slice copy of r safe to inspect outside Editor state.
func (r *AgentTurnReview) Clone() *AgentTurnReview {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Added = slices.Clone(r.Added)
	cp.Changed = slices.Clone(r.Changed)
	cp.Removed = slices.Clone(r.Removed)
	return &cp
}

// AgentToolCallRecord carries structured metadata from an MCP tool execution into the Agent panel.
type AgentToolCallRecord struct {
	ClientName  string
	ToolName    string
	StepTitle   string
	CallSummary string
	ErrorText   string
	IsError     bool
	File        string
}

type agentObjectMutation struct {
	ObjectID  string
	Op        string // "add", "remove", "change"
	StepIndex int
}

// snapshotSceneForTurn clones the currently open scene document and serialized YAML so turn diffs are exact.
func (e *Editor) snapshotSceneForTurn() (*sceneformat.SceneDoc, string, string, int) {
	if e == nil {
		return nil, "", "", 0
	}
	histIdx := 0
	if e.History != nil {
		histIdx = e.History.Index()
	}
	file := e.agentOpenSceneName()
	if e.Doc == nil {
		return nil, "", file, histIdx
	}
	raw, err := e.SaveYAML()
	if err != nil {
		return nil, "", file, histIdx
	}
	parsed, err := sceneformat.ParseWith(raw, sceneformat.ParseOptions{Components: e.ComponentRegistry()})
	if err != nil {
		return nil, string(raw), file, histIdx
	}
	return parsed, string(raw), file, histIdx
}

func (e *Editor) agentOpenSceneName() string {
	if e == nil {
		return ""
	}
	if e.ScenePath != "" {
		return e.ScenePath
	}
	if e.FilePath != "" {
		return e.FilePath
	}
	if e.Doc != nil && e.Doc.Name != "" {
		return e.Doc.Name + ".scene.yaml"
	}
	return "scene.yaml"
}

func (e *Editor) initTurnBaseline(turn *AgentTurn) {
	if turn == nil || turn.baselineDoc != nil {
		return
	}
	doc, raw, file, histIdx := e.snapshotSceneForTurn()
	turn.baselineDoc = doc
	turn.baselineYAML = raw
	turn.baselineFile = file
	turn.historyStart = histIdx
	if doc != nil {
		objs, _ := collectFlatSceneObjects(doc)
		turn.lastSeenIDs = make(map[string]struct{}, len(objs))
		for id := range objs {
			turn.lastSeenIDs[id] = struct{}{}
		}
	}
}

func (e *Editor) observeTurnSceneMutations(turn *AgentTurn, stepIndex int) {
	if e == nil || turn == nil || e.Doc == nil {
		return
	}
	if turn.baselineDoc == nil {
		e.initTurnBaseline(turn)
		return
	}
	if turn.baselineFile != "" && e.agentOpenSceneName() != turn.baselineFile {
		return
	}
	nowObjs, nowOrder := collectFlatSceneObjects(e.Doc)
	if turn.lastSeenIDs == nil {
		turn.lastSeenIDs = map[string]struct{}{}
	}
	for _, id := range nowOrder {
		if _, existed := turn.lastSeenIDs[id]; !existed && len(turn.mutations) < maxTurnMutations {
			turn.mutations = append(turn.mutations, agentObjectMutation{
				ObjectID:  id,
				Op:        "add",
				StepIndex: stepIndex,
			})
		}
	}
	var removedIDs []string
	for id := range turn.lastSeenIDs {
		if _, still := nowObjs[id]; !still {
			removedIDs = append(removedIDs, id)
		}
	}
	slices.Sort(removedIDs)
	for _, id := range removedIDs {
		if len(turn.mutations) >= maxTurnMutations {
			break
		}
		turn.mutations = append(turn.mutations, agentObjectMutation{
			ObjectID:  id,
			Op:        "remove",
			StepIndex: stepIndex,
		})
	}
	next := make(map[string]struct{}, len(nowObjs))
	for _, id := range nowOrder {
		next[id] = struct{}{}
	}
	turn.lastSeenIDs = next
}

func (e *Editor) refreshTurnReview(turn *AgentTurn) *AgentTurnReview {
	if e == nil || turn == nil {
		return nil
	}
	if turn.baselineDoc == nil || e.Doc == nil {
		return turn.Review
	}
	if turn.baselineFile != "" && e.agentOpenSceneName() != turn.baselineFile {
		return turn.Review
	}
	comp := &SceneComparison{
		HeadDoc: turn.baselineDoc,
		NowDoc:  e.Doc,
	}
	diffSceneDocs(comp)

	plus, minus := computeYAMLLineDelta(turn.baselineYAML, e.currentSceneYAML())
	histEnd := turn.historyStart
	if e.History != nil {
		histEnd = e.History.Index()
	}

	if len(comp.Added) == 0 && len(comp.Changed) == 0 && len(comp.Removed) == 0 {
		if turn.Review != nil && !turn.Review.Kept && !turn.Review.Discarded {
			turn.Review.Added = nil
			turn.Review.Changed = nil
			turn.Review.Removed = nil
			turn.Review.PlusLines = plus
			turn.Review.MinusLines = minus
			turn.Review.HistoryEnd = histEnd
		}
		return turn.Review
	}

	kept := false
	discarded := false
	if turn.Review != nil {
		kept = turn.Review.Kept
		discarded = turn.Review.Discarded
	}
	file := turn.baselineFile
	if file == "" {
		file = e.agentOpenSceneName()
	}
	turn.Review = &AgentTurnReview{
		TurnID:       turn.ID,
		File:         file,
		Added:        comp.Added,
		Changed:      comp.Changed,
		Removed:      comp.Removed,
		PlusLines:    plus,
		MinusLines:   minus,
		HistoryStart: turn.historyStart,
		HistoryEnd:   histEnd,
		Kept:         kept,
		Discarded:    discarded,
	}
	return turn.Review
}

func (e *Editor) currentSceneYAML() string {
	if e == nil || e.Doc == nil {
		return ""
	}
	raw, err := e.SaveYAML()
	if err != nil {
		return ""
	}
	return string(raw)
}

func computeYAMLLineDelta(before, after string) (plus, minus int) {
	if before == after {
		return 0, 0
	}
	a := strings.Split(strings.TrimRight(before, "\n"), "\n")
	b := strings.Split(strings.TrimRight(after, "\n"), "\n")
	if before == "" {
		a = nil
	}
	if after == "" {
		b = nil
	}
	counts := make(map[string]int, len(a))
	for _, line := range a {
		counts[line]++
	}
	for _, line := range b {
		if counts[line] > 0 {
			counts[line]--
		} else {
			plus++
		}
	}
	for _, rem := range counts {
		minus += rem
	}
	return plus, minus
}

// HasUnkeptAgentTurnReview reports whether an active agent run, an open inline turn review drawer,
// or an unkept review from a partial, lost, stopped, or mismatched turn should pause automatic saving.
func (e *Editor) HasUnkeptAgentTurnReview() bool {
	if e == nil {
		return false
	}
	if e.agent.openReviewTurnID != "" || e.agent.activeRunCancel != nil {
		return true
	}
	for i := range e.agent.turns {
		t := &e.agent.turns[i]
		if t.Review == nil || t.Review.Kept || t.Review.Discarded || t.Review.Count() == 0 {
			continue
		}
		if (t.Partial != nil && !t.Partial.Kept) || t.Lost != nil || t.StoppedState != nil || t.Mismatch != nil {
			return true
		}
	}
	return false
}

// RecordAgentToolCall records one MCP tool call into the Agent panel, tracking consecutive failures
// of the same tool and pausing the request automatically after AgentStuckFailureThreshold (3)
// consecutive failures.
func (e *Editor) RecordAgentToolCall(rec AgentToolCallRecord) {
	if e == nil || e.agent.mode == AgentModeNone {
		return
	}
	now := e.nowTime()
	toolName := normalizeMCPToolName(rec.ToolName)
	title := strings.TrimSpace(rec.StepTitle)
	if title == "" {
		title = formatAgentToolStep(toolName)
	}
	callSummary := truncateRunes(strings.TrimSpace(rec.CallSummary), maxFailedCallSummaryRunes)
	if callSummary == "" {
		callSummary = truncateRunes(title, maxFailedCallSummaryRunes)
	}
	errText := truncateRunes(strings.TrimSpace(rec.ErrorText), maxFailedCallErrorRunes)

	var turn *AgentTurn
	activeComposer := false
	if (e.agent.activeRunCancel != nil || e.agent.liveRequestActive) && len(e.agent.turns) > 0 && !e.agent.turns[len(e.agent.turns)-1].Done {
		// Active composer or live MCP request owns the latest turn.
		turn = &e.agent.turns[len(e.agent.turns)-1]
		activeComposer = true
	} else {
		// Terminal / scripted MCP session.
		if n := len(e.agent.turns); n > 0 && e.agent.turns[n-1].External &&
			e.agent.turns[n-1].Lost == nil &&
			e.agent.turns[n-1].Stuck == nil &&
			now.Sub(e.agent.turns[n-1].FinishedAt) <= 60*time.Second {
			turn = &e.agent.turns[n-1]
		} else {
			e.agent.turnSeq++
			e.agent.turnChangeSeq++
			id := fmt.Sprintf("turn-%d", e.agent.turnSeq)
			name := FriendlyAgentName(rec.ClientName)
			e.agent.turns = append(e.agent.turns, AgentTurn{
				ID:        id,
				Source:    "external",
				Status:    "done",
				External:  true,
				AgentName: name,
				CreatedAt: now,
				StartedAt: now,
			})
			e.RecordAgentRequest(1)
			e.trimAgentTurnAttachments()
			turn = &e.agent.turns[len(e.agent.turns)-1]
			// Note: because the tool call already ran before RecordAgentToolCall when the turn is brand new,
			// baselineDoc is initialized lazily on first call if not already prepared via PrepareAgentTurnBaseline.
			if e.agent.externalBaselineDoc != nil {
				turn.baselineDoc = e.agent.externalBaselineDoc
				turn.baselineYAML = e.agent.externalBaselineYAML
				turn.baselineFile = e.agent.externalBaselineFile
				turn.historyStart = e.agent.externalHistoryStart
				turn.lastSeenIDs = e.agent.externalBaselineIDs
				e.agent.externalBaselineDoc = nil
				e.agent.externalBaselineYAML = ""
				e.agent.externalBaselineFile = ""
				e.agent.externalBaselineIDs = nil
			}
		}
		stepStatus := AgentStepDone
		if rec.IsError {
			stepStatus = AgentStepFailed
		}
		turn.Steps = append(turn.Steps, AgentStep{
			ID:       fmt.Sprintf("%s-step-%d", turn.ID, len(turn.Steps)+1),
			Title:    title,
			Detail:   errText,
			ToolName: toolName,
			Status:   stepStatus,
		})
		turn.Status = "done"
		turn.FinishedAt = now
		turn.Done = true
	}

	stepNum := max(1, len(turn.Steps))
	e.observeTurnSceneMutations(turn, stepNum)
	e.refreshTurnReview(turn)

	if rec.IsError && toolName != "" {
		if activeComposer {
			turn.lastMCPFailTool = toolName
			turn.lastMCPFailAt = now
		}
		e.recordConsecutiveToolFailure(turn, toolName, callSummary, errText)
	} else if !rec.IsError {
		turn.consecutiveFailTool = ""
		turn.consecutiveFails = nil
		turn.lastMCPFailTool = ""
	}
	e.agent.turnChangeSeq++
	e.notify()
}

// PrepareExternalAgentBaseline snapshots the scene before an external MCP tool mutates it so the turn's
// review diff includes the very first edit of a terminal/scripted MCP session.
func (e *Editor) PrepareExternalAgentBaseline() {
	if e == nil || e.agent.mode == AgentModeNone {
		return
	}
	if e.agent.activeRunCancel != nil && len(e.agent.turns) > 0 && !e.agent.turns[len(e.agent.turns)-1].Done {
		e.initTurnBaseline(&e.agent.turns[len(e.agent.turns)-1])
		return
	}
	now := e.nowTime()
	if n := len(e.agent.turns); n > 0 && e.agent.turns[n-1].External &&
		e.agent.turns[n-1].Lost == nil &&
		e.agent.turns[n-1].Stuck == nil &&
		now.Sub(e.agent.turns[n-1].FinishedAt) <= 60*time.Second {
		if e.agent.turns[n-1].baselineDoc != nil {
			return
		}
	}
	if e.agent.externalBaselineDoc != nil {
		return
	}
	doc, raw, file, histIdx := e.snapshotSceneForTurn()
	e.agent.externalBaselineDoc = doc
	e.agent.externalBaselineYAML = raw
	e.agent.externalBaselineFile = file
	e.agent.externalHistoryStart = histIdx
	if doc != nil {
		objs, _ := collectFlatSceneObjects(doc)
		ids := make(map[string]struct{}, len(objs))
		for id := range objs {
			ids[id] = struct{}{}
		}
		e.agent.externalBaselineIDs = ids
	}
}

func normalizeMCPToolName(name string) string {
	n := strings.TrimSpace(name)
	if idx := strings.LastIndex(n, "__"); idx >= 0 && idx+2 < len(n) {
		n = n[idx+2:]
	}
	return n
}

func (e *Editor) recordConsecutiveToolFailure(turn *AgentTurn, toolName, callSummary, errText string) bool {
	if turn == nil || toolName == "" {
		return false
	}
	if turn.consecutiveFailTool != toolName {
		turn.consecutiveFailTool = toolName
		turn.consecutiveFails = nil
	}
	if errText == "" {
		errText = "refused"
	}
	turn.consecutiveFails = append(turn.consecutiveFails, AgentFailedCall{
		ToolName: toolName,
		Call:     truncateRunes(callSummary, maxFailedCallSummaryRunes),
		Error:    truncateRunes(errText, maxFailedCallErrorRunes),
	})
	if len(turn.consecutiveFails) < AgentStuckFailureThreshold {
		return false
	}
	// Three consecutive failures of the same tool -> pause the request instead of guessing again.
	failures := slices.Clone(turn.consecutiveFails[len(turn.consecutiveFails)-AgentStuckFailureThreshold:])
	nothingChanged := turn.Review == nil || turn.Review.Count() == 0

	// Replace trailing individual failure rows of the same tool with a single clear summary step.
	summaryTitle := fmt.Sprintf("%s failed %d times in a row", toolName, AgentStuckFailureThreshold)
	if !hasStepWithTitle(turn.Steps, summaryTitle) {
		trimmed := turn.Steps
		for len(trimmed) > 0 && trimmed[len(trimmed)-1].Status == AgentStepFailed && normalizeMCPToolName(trimmed[len(trimmed)-1].ToolName) == toolName {
			trimmed = trimmed[:len(trimmed)-1]
		}
		turn.Steps = append(trimmed, AgentStep{
			ID:       fmt.Sprintf("%s-stuck-%d", turn.ID, len(trimmed)+1),
			Title:    summaryTitle,
			ToolName: toolName,
			Status:   AgentStepFailed,
		})
	}

	turn.Stuck = &AgentStuckState{
		ToolName:       toolName,
		Failures:       failures,
		Resolution:     AgentStuckWaiting,
		NothingChanged: nothingChanged,
	}
	turn.Done = true
	turn.Status = "done"
	turn.Stopped = false
	turn.FinishedAt = e.nowTime()
	e.agent.turnChangeSeq++

	// Cancel any active composer runner or live MCP request so the agent stops guessing immediately.
	if e.agent.activeRunCancel != nil {
		e.agent.activeRunID++
		cancel := e.agent.activeRunCancel
		e.agent.activeRunCancel = nil
		cancel()
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = false
	e.agent.liveActivity = ""
	e.agent.workingStep = 0
	e.agent.workingTotal = 0
	e.SetAgentWaitingForYou(AgentOpenRequest{
		ID:    turn.ID + "-stuck",
		Title: "Paused instead of guessing again",
		Body:  fmt.Sprintf("%s failed %d times in a row. Add a hint to continue or stop here.", toolName, AgentStuckFailureThreshold),
	})
	return true
}

func hasStepWithTitle(steps []AgentStep, title string) bool {
	for _, s := range steps {
		if s.Title == title {
			return true
		}
	}
	return false
}

// --- Partial state actions ---

// MarkAgentTurnPartial attaches a partial completion card to turnID (or the latest turn when turnID is empty).
func (e *Editor) MarkAgentTurnPartial(turnID string, partial AgentPartialState) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil {
		return
	}
	cp := partial
	turn.Partial = &cp
	e.refreshTurnReview(turn)
	if e.HasUnkeptAgentTurnReview() {
		e.cancelAutoSaveTimer()
	}
	e.agent.turnChangeSeq++
	e.notify()
}

// KeepAgentPartial keeps the partial turn's changes as undoable edits and dismisses the partial prompt card.
func (e *Editor) KeepAgentPartial(turnID string) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Partial == nil {
		return
	}
	turn.Partial.Pointing = false
	turn.Partial.Kept = true
	if e.agent.pointingTurnID == turn.ID {
		e.agent.pointingTurnID = ""
	}
	if turn.Review != nil {
		turn.Review.Kept = true
		turn.Review.Discarded = false
	}
	if turn.PendingReview != nil {
		turn.PendingReview.Kept = true
		turn.PendingReview.Reverted = false
	}
	e.agent.turnChangeSeq++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
}

// ToggleAgentPointing enters or exits viewport pointing mode ("Let me point") for turnID.
func (e *Editor) ToggleAgentPointing(turnID string) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Partial == nil || turn.Partial.Kept {
		return
	}
	next := !turn.Partial.Pointing
	// Clear pointing on any other turn.
	for i := range e.agent.turns {
		if e.agent.turns[i].Partial != nil {
			e.agent.turns[i].Partial.Pointing = false
		}
	}
	turn.Partial.Pointing = next
	if next {
		e.agent.pointingTurnID = turn.ID
	} else {
		e.agent.pointingTurnID = ""
	}
	e.notify()
}

// CancelAgentPointing exits viewport pointing mode cleanly (invoked on Esc).
func (e *Editor) CancelAgentPointing() bool {
	if e == nil || e.agent.pointingTurnID == "" {
		return false
	}
	turnID := e.agent.pointingTurnID
	e.agent.pointingTurnID = ""
	if turn := e.findAgentTurn(turnID); turn != nil && turn.Partial != nil {
		turn.Partial.Pointing = false
	}
	e.notify()
	return true
}

// AgentPointingActive reports whether the editor is currently waiting for a viewport click from "Let me point".
func (e *Editor) AgentPointingActive() bool {
	if e == nil || e.agent.pointingTurnID == "" {
		return false
	}
	turn := e.findAgentTurn(e.agent.pointingTurnID)
	return turn != nil && turn.Partial != nil && turn.Partial.Pointing
}

// AgentPointingPrompt returns the active pointing instruction shown in the card and viewport pill.
func (e *Editor) AgentPointingPrompt() string {
	if e == nil || e.agent.pointingTurnID == "" {
		return ""
	}
	turn := e.findAgentTurn(e.agent.pointingTurnID)
	if turn == nil || turn.Partial == nil || !turn.Partial.Pointing {
		return ""
	}
	return turn.Partial.EffectivePointPrompt()
}

// PickAgentPointWorld completes "Let me point" with the clicked world coordinate (wx, wy).
func (e *Editor) PickAgentPointWorld(wx, wy float32) bool {
	if !e.AgentPointingActive() {
		return false
	}
	turnID := e.agent.pointingTurnID
	turn := e.findAgentTurn(turnID)
	if turn == nil || turn.Partial == nil {
		e.agent.pointingTurnID = ""
		return false
	}
	turn.Partial.Pointing = false
	turn.Partial.HasPoint = true
	turn.Partial.PointedX = wx
	turn.Partial.PointedY = wy
	target := turn.Partial.Target
	e.agent.pointingTurnID = ""

	if e.OnAgentPointPicked != nil {
		e.OnAgentPointPicked(turnID, target, wx, wy)
	}
	// If an AgentRunner is attached and idle, dispatch a follow-up placement step with the pointed coordinates.
	if e.AgentRunner != nil && e.agent.activeRunCancel == nil && !e.AgentOffline() {
		prompt := fmt.Sprintf("Place %s at (%.0f, %.0f)", nonEmpty(target, "it"), wx, wy)
		stepTitle := fmt.Sprintf("Placing %s at x %.0f, y %.0f", nonEmpty(target, "object"), wx, wy)
		_ = e.continueExistingTurn(turn, prompt, stepTitle, func(req *AgentRequest) {
			pt := [2]float32{wx, wy}
			req.PointedWorld = &pt
			req.PointedTarget = target
		})
	} else {
		e.notify()
	}
	return true
}

func (e *Editor) validateAgentContinuationPreconditions() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	if e.AgentOffline() {
		return errors.New("editor: offline")
	}
	if e.agent.activeRunCancel != nil {
		return errors.New("editor: an agent request is already running")
	}
	return nil
}

// TryAnotherWayAgentTurn asks the agent to try an alternative placement/strategy for a partial turn.
func (e *Editor) TryAnotherWayAgentTurn(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Partial == nil {
		return errors.New("editor: no partial turn found")
	}
	turn.Partial.Pointing = false
	turn.Partial.TryingAnother = true
	if e.agent.pointingTurnID == turn.ID {
		e.agent.pointingTurnID = ""
	}
	target := nonEmpty(turn.Partial.Target, "the remaining step")
	stepTitle := fmt.Sprintf("Trying another way for %s", target)
	prompt := fmt.Sprintf("Try another way to complete %s: %s", target, turn.Partial.Reason)

	if e.AgentRunner == nil && len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
		// Still update UI cleanly into working step for interactive preview/tests without a runner.
		turn.Steps = append(turn.Steps, AgentStep{
			ID:     fmt.Sprintf("%s-another-%d", turn.ID, len(turn.Steps)+1),
			Title:  stepTitle,
			Status: AgentStepRunning,
		})
		e.agent.status = AgentStatusWorking
		e.agent.workingStep = len(turn.Steps)
		e.agent.workingTotal = 0
		e.agent.turnChangeSeq++
		e.updateAgentAnnouncement()
		e.notify()
		return nil
	}
	return e.continueExistingTurn(turn, prompt, stepTitle, func(req *AgentRequest) {
		req.TryAnotherWay = true
	})
}

// --- Stuck state actions ---

// SetAgentStuckHint updates the draft hint text inside a Stuck card.
func (e *Editor) SetAgentStuckHint(turnID, hint string) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Stuck == nil {
		return
	}
	if turn.Stuck.HintDraft == hint {
		return
	}
	turn.Stuck.HintDraft = hint
	e.notify()
}

// ContinueAgentWithHint resumes a paused Stuck turn with the user's hint.
func (e *Editor) ContinueAgentWithHint(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Stuck == nil {
		return errors.New("editor: no stuck turn found")
	}
	hint := strings.TrimSpace(turn.Stuck.HintDraft)
	turn.Stuck.SubmittedHint = hint
	turn.Stuck.Resolution = AgentStuckContinued
	turn.consecutiveFailTool = ""
	turn.consecutiveFails = nil
	turn.lastMCPFailTool = ""
	if e.agent.openRequest != nil && e.agent.openRequest.ID == turn.ID+"-stuck" {
		e.agent.openRequest = nil
	}

	stepTitle := "Continuing with your hint"
	if hint != "" {
		stepTitle = fmt.Sprintf("Continuing with hint: %s", hint)
	}
	prompt := turn.Prompt
	if hint != "" {
		if prompt != "" {
			prompt += "\nHint: " + hint
		} else {
			prompt = hint
		}
	}

	if e.AgentRunner == nil && len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
		turn.Steps = append(turn.Steps, AgentStep{
			ID:     fmt.Sprintf("%s-hint-%d", turn.ID, len(turn.Steps)+1),
			Title:  stepTitle,
			Status: AgentStepRunning,
		})
		e.agent.status = AgentStatusWorking
		e.agent.workingStep = len(turn.Steps)
		e.agent.workingTotal = 0
		e.agent.turnChangeSeq++
		e.updateAgentAnnouncement()
		e.notify()
		return nil
	}
	return e.continueExistingTurn(turn, prompt, stepTitle, func(req *AgentRequest) {
		req.Hint = hint
	})
}

// StopStuckAgentTurn stops a paused Stuck turn cleanly without modifying any file.
func (e *Editor) StopStuckAgentTurn(turnID string) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Stuck == nil {
		return
	}
	turn.Stuck.Resolution = AgentStuckStopped
	turn.consecutiveFailTool = ""
	turn.consecutiveFails = nil
	turn.lastMCPFailTool = ""
	turn.Done = true
	turn.Status = "done"
	turn.FinishedAt = e.nowTime()
	e.agent.turnChangeSeq++
	if e.agent.openRequest != nil && e.agent.openRequest.ID == turn.ID+"-stuck" {
		e.agent.openRequest = nil
	}
	e.restoreIdleAgentStatus()
	e.notify()
}

// --- Lost state (connection drop & model error) ---

func (e *Editor) markActiveTurnLost(reason string) {
	if e == nil || len(e.agent.turns) == 0 {
		return
	}
	turn := &e.agent.turns[len(e.agent.turns)-1]
	now := e.nowTime()

	// Only mark Lost if a composer/live run is actively running OR an external MCP turn was active recently with steps.
	activeComposer := (e.agent.activeRunCancel != nil || e.agent.liveRequestActive) && !turn.Done
	activeExternal := turn.External && len(turn.Steps) > 0 && turn.Lost == nil && turn.Stuck == nil && now.Sub(turn.FinishedAt) <= 60*time.Second
	if !activeComposer && !activeExternal {
		return
	}

	if e.agent.activeRunCancel != nil {
		e.agent.activeRunID++
		cancel := e.agent.activeRunCancel
		e.agent.activeRunCancel = nil
		cancel()
	}
	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = false
	e.agent.liveActivity = ""

	doneCount := 0
	duringStep := 0
	for i := range turn.Steps {
		switch turn.Steps[i].Status {
		case AgentStepDone:
			doneCount++
		case AgentStepRunning:
			turn.Steps[i].Status = AgentStepInterrupted
			if !strings.Contains(strings.ToLower(turn.Steps[i].Detail), "nothing written") {
				turn.Steps[i].Detail = "cut off, nothing written"
			}
			if duringStep == 0 {
				duringStep = i + 1
			}
		}
	}
	if duringStep == 0 {
		duringStep = doneCount + 1
	}

	// Ensure any planned steps not yet added remain visible as pending.
	e.appendRemainingPlannedSteps(turn)

	agentName := turn.AgentName
	if agentName == "" {
		if c, ok := e.AgentPrimaryClient(); ok {
			agentName = c.DisplayName
		}
	}
	if agentName == "" {
		agentName = "MCP client"
	}

	turn.Lost = &AgentLostState{
		Kind:       AgentLostConnection,
		AgentName:  agentName,
		LostAt:     now,
		AfterStep:  doneCount,
		DuringStep: duringStep,
	}
	if strings.TrimSpace(reason) != "" && !strings.Contains(strings.ToLower(reason), "stopped answering over mcp") {
		turn.Lost.Detail = reason
	}
	turn.Done = true
	turn.Status = "done"
	turn.FinishedAt = now
	e.refreshTurnReview(turn)
	e.cancelAutoSaveTimer()
	e.agent.turnChangeSeq++
}

func (e *Editor) markLostTurnReconnected() {
	if e == nil || len(e.agent.turns) == 0 {
		return
	}
	turn := &e.agent.turns[len(e.agent.turns)-1]
	if turn.Lost == nil || turn.Lost.Kind != AgentLostConnection || turn.Lost.Resumed || turn.Lost.Reconnected {
		return
	}
	turn.Lost.Reconnected = true
	turn.Lost.AskResumePending = true
	e.agent.turnChangeSeq++
	nextStep := turn.Lost.DuringStep
	if nextStep <= 0 {
		nextStep = turn.Lost.AfterStep + 1
	}
	e.SetAgentWaitingForYou(AgentOpenRequest{
		ID:    turn.ID + "-reconnect-ask",
		Title: fmt.Sprintf("Pick up at step %d?", nextStep),
		Body:  fmt.Sprintf("%s reconnected. Continue from step %d without redoing earlier steps?", nonEmpty(turn.Lost.AgentName, "The agent"), nextStep),
	})
}

// ConfirmResumeLostAgentTurn resumes a reconnected Lost turn starting at its interrupted/next step.
func (e *Editor) ConfirmResumeLostAgentTurn(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Lost == nil {
		return errors.New("editor: no lost turn found")
	}
	turn.Lost.AskResumePending = false
	turn.Lost.Resumed = true
	if e.agent.openRequest != nil && e.agent.openRequest.ID == turn.ID+"-reconnect-ask" {
		e.agent.openRequest = nil
	}
	return e.resumeTurnFromFirstNotDone(turn)
}

// DismissResumeLostAgentTurn declines resuming a reconnected Lost turn while keeping its review intact.
func (e *Editor) DismissResumeLostAgentTurn(turnID string) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Lost == nil {
		return
	}
	turn.Lost.AskResumePending = false
	e.agent.turnChangeSeq++
	if e.agent.openRequest != nil && e.agent.openRequest.ID == turn.ID+"-reconnect-ask" {
		e.agent.openRequest = nil
	}
	e.restoreIdleAgentStatus()
	e.notify()
}

// ReportAgentModelError attaches a model error card (such as Anthropic API 529 overloaded) to turnID.
func (e *Editor) ReportAgentModelError(turnID string, state AgentLostState) {
	if e == nil {
		return
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil {
		return
	}
	state.Kind = AgentLostModelError
	if state.LostAt.IsZero() {
		state.LostAt = e.nowTime()
	}
	cp := state
	turn.Lost = &cp
	turn.Done = true
	turn.Status = "done"
	turn.FinishedAt = e.nowTime()
	e.refreshTurnReview(turn)
	e.cancelAutoSaveTimer()
	e.agent.turnChangeSeq++
	e.notify()
}

// RetryAgentModelErrorNow immediately retries a turn that failed with a model error.
func (e *Editor) RetryAgentModelErrorNow(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Lost == nil {
		return errors.New("editor: no model error turn found")
	}
	turn.Lost.RetryIn = 0
	turn.Lost.Resumed = true
	return e.resumeTurnFromFirstNotDone(turn)
}

// SwitchAgentFallback switches a model-error turn to its fallback agent (e.g. Claude Code) and retries.
func (e *Editor) SwitchAgentFallback(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Lost == nil {
		return errors.New("editor: no model error turn found")
	}
	if turn.Lost.FallbackAgent != "" {
		turn.AgentName = turn.Lost.FallbackAgent
	}
	turn.Lost.Resumed = true
	return e.resumeTurnFromFirstNotDone(turn)
}

// --- Stopped & Resume ---

func (e *Editor) appendRemainingPlannedSteps(turn *AgentTurn) {
	if turn == nil || len(turn.PlannedSteps) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(turn.Steps))
	for _, s := range turn.Steps {
		existing[strings.ToLower(strings.TrimSpace(s.Title))] = struct{}{}
	}
	for _, title := range turn.PlannedSteps {
		key := strings.ToLower(strings.TrimSpace(title))
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		turn.Steps = append(turn.Steps, AgentStep{
			ID:     fmt.Sprintf("%s-plan-%d", turn.ID, len(turn.Steps)+1),
			Title:  title,
			Status: AgentStepPending,
		})
		existing[key] = struct{}{}
	}
}

func (e *Editor) populateStoppedTurnState(turn *AgentTurn) {
	if turn == nil {
		return
	}
	doneCount := 0
	for i := range turn.Steps {
		switch turn.Steps[i].Status {
		case AgentStepDone:
			doneCount++
		case AgentStepRunning:
			// A running step canceled by Stop becomes not-done (pending) so Resume starts right here.
			turn.Steps[i].Status = AgentStepPending
		}
	}
	e.appendRemainingPlannedSteps(turn)
	total := max(len(turn.Steps), e.agent.workingTotal)
	turn.StoppedState = &AgentStoppedState{
		AtStep:     doneCount,
		TotalSteps: total,
		Resumed:    false,
	}
	e.refreshTurnReview(turn)
	e.cancelAutoSaveTimer()
	e.agent.turnChangeSeq++
}

// ResumeAgentTurn resumes a stopped turn starting strictly at the first step not done.
// Steps already marked AgentStepDone are skipped and never redone.
func (e *Editor) ResumeAgentTurn(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil {
		return errors.New("editor: no turn to resume")
	}
	return e.resumeTurnFromFirstNotDone(turn)
}

func (e *Editor) resumeTurnFromFirstNotDone(turn *AgentTurn) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	var completed []string
	var remaining []string
	firstNotDoneIdx := -1
	for i := range turn.Steps {
		if turn.Steps[i].Status == AgentStepDone {
			completed = append(completed, turn.Steps[i].Title)
			continue
		}
		if firstNotDoneIdx < 0 {
			firstNotDoneIdx = i
			turn.Steps[i].Status = AgentStepRunning
			turn.Steps[i].Detail = ""
		} else if turn.Steps[i].Status == AgentStepInterrupted {
			turn.Steps[i].Status = AgentStepPending
			turn.Steps[i].Detail = ""
		}
		remaining = append(remaining, turn.Steps[i].Title)
	}
	if firstNotDoneIdx < 0 && turn.Lost == nil {
		// All steps are already done; nothing left to redo.
		return nil
	}
	resumeFrom := len(completed) + 1
	resumedTitle := ""
	if firstNotDoneIdx >= 0 {
		resumedTitle = turn.Steps[firstNotDoneIdx].Title
	}
	if turn.StoppedState != nil {
		turn.StoppedState.Resumed = true
		turn.StoppedState.ResumedAtTitle = resumedTitle
	}
	turn.Done = false
	turn.Stopped = false
	turn.Status = "running"
	e.agent.turnChangeSeq++

	total := len(turn.Steps)
	if turn.StoppedState != nil && turn.StoppedState.TotalSteps > total {
		total = turn.StoppedState.TotalSteps
	}
	e.agent.workingStep = resumeFrom
	e.agent.workingTotal = total

	runner := e.AgentRunner
	if runner == nil {
		if len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
			// Preview/offline-free UI transition without spawning a subprocess.
			e.agent.status = AgentStatusWorking
			e.updateAgentAnnouncement()
			e.notify()
			return nil
		}
		runner = defaultCLIAgentRunner()
	}

	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = true
	e.agent.liveStopped = false
	e.agent.liveStopCh = make(chan struct{})
	e.agent.liveDeniedKeys = make(map[string]bool)
	e.agent.liveQueuedFollowUps = nil
	e.agent.liveActivity = "Agent is working · manual edits stay available"

	e.agent.activeRunID++
	runID := e.agent.activeRunID
	ctx, cancel := context.WithCancel(context.Background())
	e.agent.activeRunCancel = cancel
	e.agent.status = AgentStatusWorking
	e.cancelAutoSaveTimer()
	e.updateAgentAnnouncement()
	e.notify()

	agentKind := ExternalAgentAuto
	if c, ok := e.AgentPrimaryClient(); ok {
		agentKind = DetectExternalAgentKind(c.Name)
	}
	if turn.AgentName != "" {
		if k := DetectExternalAgentKind(turn.AgentName); k != ExternalAgentAuto {
			agentKind = k
		}
	}

	req := AgentRequest{
		ThreadID:       e.agent.threadID,
		SessionID:      e.agent.sessionID,
		Prompt:         turn.Prompt,
		ProjectRoot:    e.agentProjectRoot(),
		ScenePath:      e.ScenePath,
		SelectedID:     e.SelectedID,
		AgentKind:      agentKind,
		Permissions:    e.agent.permissions,
		ResumeFromStep: resumeFrom,
		CompletedSteps: completed,
		RemainingSteps: remaining,
	}

	turnID := turn.ID
	go func() {
		err := runner.Run(ctx, req, func(ev AgentEvent) {
			e.postOrRun(func() {
				e.applyAgentEventForRun(runID, turnID, ev)
			})
		})
		e.postOrRun(func() {
			e.finishAgentRun(runID, turnID, err, ctx.Err() != nil)
		})
	}()
	return nil
}

func (e *Editor) continueExistingTurn(turn *AgentTurn, prompt, stepTitle string, configure func(*AgentRequest)) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	runner := e.AgentRunner
	if runner == nil {
		runner = defaultCLIAgentRunner()
	}
	turn.Done = false
	turn.Stopped = false
	turn.Status = "running"
	turn.Steps = append(turn.Steps, AgentStep{
		ID:     fmt.Sprintf("%s-cont-%d", turn.ID, len(turn.Steps)+1),
		Title:  stepTitle,
		Status: AgentStepRunning,
	})
	e.agent.turnChangeSeq++

	e.resolvePendingLiveAskLocked(PermissionOutcomeStopped)
	e.closeLiveStopChLocked()
	e.agent.liveRequestActive = true
	e.agent.liveStopped = false
	e.agent.liveStopCh = make(chan struct{})
	e.agent.liveDeniedKeys = make(map[string]bool)
	e.agent.liveQueuedFollowUps = nil
	e.agent.liveActivity = "Agent is working · manual edits stay available"

	e.agent.activeRunID++
	runID := e.agent.activeRunID
	ctx, cancel := context.WithCancel(context.Background())
	e.agent.activeRunCancel = cancel
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = len(turn.Steps)
	e.agent.workingTotal = 0
	e.cancelAutoSaveTimer()
	e.updateAgentAnnouncement()
	e.notify()

	agentKind := ExternalAgentAuto
	if c, ok := e.AgentPrimaryClient(); ok {
		agentKind = DetectExternalAgentKind(c.Name)
	}
	req := AgentRequest{
		ThreadID:    e.agent.threadID,
		SessionID:   e.agent.sessionID,
		Prompt:      prompt,
		ProjectRoot: e.agentProjectRoot(),
		ScenePath:   e.ScenePath,
		SelectedID:  e.SelectedID,
		AgentKind:   agentKind,
		Permissions: e.agent.permissions,
	}
	if configure != nil {
		configure(&req)
	}
	turnID := turn.ID
	go func() {
		err := runner.Run(ctx, req, func(ev AgentEvent) {
			e.postOrRun(func() {
				e.applyAgentEventForRun(runID, turnID, ev)
			})
		})
		e.postOrRun(func() {
			e.finishAgentRun(runID, turnID, err, ctx.Err() != nil)
		})
	}()
	return nil
}

// --- Mismatch verification (summary vs actual diff) ---

var summaryCountClaimRe = regexp.MustCompile(`(?i)\b(zero|no|none|one|two|three|four|five|six|seven|eight|nine|ten|\d+)\s+(?:(?:new|added|placed|patrolling)\s+)*([a-z][a-z0-9_-]{1,30})\b`)
var summaryUntouchedRe = regexp.MustCompile(`(?i)\b(?:left|kept|leave|keep)\s+(?:the\s+)?([a-z][a-z0-9_-]{1,30})\s+untouched\b|\b([a-z][a-z0-9_-]{1,30})\s+(?:is|was|remains|stayed)\s+untouched\b`)

var numberWords = map[string]int{
	"zero":  0,
	"no":    0,
	"none":  0,
	"one":   1,
	"two":   2,
	"three": 3,
	"four":  4,
	"five":  5,
	"six":   6,
	"seven": 7,
	"eight": 8,
	"nine":  9,
	"ten":   10,
}

func parseSummaryNumber(tok string) (int, bool) {
	lower := strings.ToLower(strings.TrimSpace(tok))
	if n, ok := numberWords[lower]; ok {
		return n, true
	}
	n, err := strconv.Atoi(lower)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func singularStem(word string) string {
	w := strings.ToLower(strings.TrimSpace(word))
	if strings.HasSuffix(w, "ies") && len(w) > 4 {
		return w[:len(w)-3] + "y"
	}
	if strings.HasSuffix(w, "es") && len(w) > 4 && (strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "xes") || strings.HasSuffix(w, "zes")) {
		return w[:len(w)-2]
	}
	if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 2 {
		return w[:len(w)-1]
	}
	return w
}

func titleCaseWord(w string) string {
	return upperFirstRune(w)
}

func objectMatchesStem(obj SceneObjectChange, stem string) bool {
	if stem == "" {
		return false
	}
	if stem == "object" || stem == "item" || stem == "entity" || stem == "node" || stem == "change" {
		return true
	}
	id := strings.ToLower(obj.ID)
	name := strings.ToLower(obj.Name)
	return strings.HasPrefix(id, stem) || strings.Contains(id, "_"+stem) || strings.HasPrefix(name, stem) || strings.Contains(name, " "+stem)
}

// CheckAgentSummaryAgainstDiff compares every numeric and untouched claim in summary against the actual
// scene diff in review. When any claim disagrees with review, it returns a populated AgentMismatchReport
// (including the known reason when an object was added and later removed during the turn).
func CheckAgentSummaryAgainstDiff(summary string, review *AgentTurnReview, mutations []agentObjectMutation) *AgentMismatchReport {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" || (review == nil && len(mutations) == 0) {
		return nil
	}
	if review == nil {
		review = &AgentTurnReview{}
	}

	var rows []AgentMismatchRow
	seenRow := map[string]bool{}
	firstDisagreeSum := ""
	firstDisagreeRev := ""
	firstDisagreeClaim := ""
	disagreedStems := map[string]bool{}

	// 1. Check numeric count claims (e.g. "Three slimes patrol...", "added 3 objects", "3 coins").
	matches := summaryCountClaimRe.FindAllStringSubmatchIndex(trimmed, -1)
	for _, m := range matches {
		if len(m) < 6 {
			continue
		}
		numStr := trimmed[m[2]:m[3]]
		noun := trimmed[m[4]:m[5]]
		claimed, ok := parseSummaryNumber(numStr)
		if !ok {
			continue
		}
		stem := singularStem(noun)
		// Skip non-object time/unit/step nouns.
		switch stem {
		case "step", "second", "minute", "hour", "file", "line", "px", "pixel", "time", "try", "attempt", "retry", "floor", "way", "percent":
			continue
		}

		// Determine if this claim refers to added objects, generic objects, or existing diff groups.
		generic := stem == "object" || stem == "item" || stem == "entity" || stem == "node"
		actualAdded := 0
		for _, a := range review.Added {
			if objectMatchesStem(a, stem) {
				actualAdded++
			}
		}
		// Only include a noun if it is generic ("objects") OR appears in review.Added OR appears in mutations (e.g. added then removed).
		seenInMutations := false
		for _, mut := range mutations {
			if strings.HasPrefix(strings.ToLower(mut.ObjectID), stem) {
				seenInMutations = true
				break
			}
		}
		if !generic && actualAdded == 0 && !seenInMutations {
			continue
		}

		labelNoun := titleCaseWord(stem) + "s"
		if generic {
			labelNoun = "Objects"
		}
		what := labelNoun + " added"
		if seenRow[what] {
			continue
		}
		seenRow[what] = true
		disagrees := claimed != actualAdded
		rows = append(rows, AgentMismatchRow{
			What:      what,
			Summary:   strconv.Itoa(claimed),
			Review:    strconv.Itoa(actualAdded),
			Disagrees: disagrees,
		})
		if disagrees {
			disagreedStems[stem] = true
			if firstDisagreeSum == "" {
				firstDisagreeSum = strconv.Itoa(claimed)
				firstDisagreeRev = strconv.Itoa(actualAdded)
				firstDisagreeClaim = extractClause(trimmed, m[0], m[1])
			}
		}
	}

	// 2. Check untouched claims (e.g. "I left the door untouched").
	untouchedMatches := summaryUntouchedRe.FindAllStringSubmatch(trimmed, -1)
	for _, m := range untouchedMatches {
		target := m[1]
		if target == "" {
			target = m[2]
		}
		stem := singularStem(target)
		if stem == "" {
			continue
		}
		what := titleCaseWord(stem)
		if seenRow[what] {
			continue
		}
		seenRow[what] = true
		reviewState := "untouched"
		disagrees := false
		for _, c := range review.Changed {
			if objectMatchesStem(c, stem) {
				reviewState = "changed"
				disagrees = true
				break
			}
		}
		if !disagrees {
			for _, r := range review.Removed {
				if objectMatchesStem(r, stem) {
					reviewState = "removed"
					disagrees = true
					break
				}
			}
		}
		rows = append(rows, AgentMismatchRow{
			What:      what,
			Summary:   "untouched",
			Review:    reviewState,
			Disagrees: disagrees,
		})
		if disagrees && firstDisagreeSum == "" {
			firstDisagreeSum = "untouched"
			firstDisagreeRev = reviewState
			firstDisagreeClaim = m[0]
		}
	}

	// 3. Also check if other added groups in the review weren't explicitly numbered in summary (e.g. "and the coins lead to the door").
	if firstDisagreeSum != "" {
		groupCounts := map[string]int{}
		var groupOrder []string
		for _, a := range review.Added {
			stem := objectGroupStem(a.ID)
			if stem == "" {
				continue
			}
			if _, ok := groupCounts[stem]; !ok {
				groupOrder = append(groupOrder, stem)
			}
			groupCounts[stem]++
		}
		lowerSummary := strings.ToLower(trimmed)
		for _, stem := range groupOrder {
			what := titleCaseWord(stem) + "s added"
			if seenRow[what] {
				continue
			}
			if strings.Contains(lowerSummary, stem) {
				seenRow[what] = true
				cnt := strconv.Itoa(groupCounts[stem])
				rows = append(rows, AgentMismatchRow{
					What:      what,
					Summary:   cnt,
					Review:    cnt,
					Disagrees: false,
				})
			}
		}
	}

	if firstDisagreeSum == "" {
		return nil
	}

	title := fmt.Sprintf("Summary says %s, review has %s", firstDisagreeSum, firstDisagreeRev)
	reason := explainMismatchFromMutations(mutations, review, disagreedStems)
	return &AgentMismatchReport{
		Title:       title,
		ClaimedText: firstDisagreeClaim,
		Rows:        rows,
		Reason:      reason,
	}
}

func extractClause(s string, start, end int) string {
	// Expand forward to the end of the clause (comma, period, " and ", semicolon) for clean underline display.
	lower := strings.ToLower(s)
	stop := len(s)
	for i := end; i < len(s); i++ {
		switch s[i] {
		case '.', ',', ';', '\n':
			stop = i
			goto done
		}
		if strings.HasPrefix(lower[i:], " and ") {
			stop = i
			goto done
		}
	}
done:
	return strings.TrimSpace(s[start:stop])
}

func objectGroupStem(id string) string {
	lower := strings.ToLower(strings.TrimSpace(id))
	if lower == "" {
		return ""
	}
	if idx := strings.IndexAny(lower, "_-0123456789"); idx > 0 {
		return lower[:idx]
	}
	return lower
}

func explainMismatchFromMutations(mutations []agentObjectMutation, review *AgentTurnReview, stems map[string]bool) string {
	if len(mutations) == 0 {
		return ""
	}
	addedInFinal := map[string]bool{}
	if review != nil {
		for _, a := range review.Added {
			addedInFinal[a.ID] = true
		}
	}
	addedAt := map[string]int{}
	removedAt := map[string]int{}
	var order []string
	for _, m := range mutations {
		if _, seen := addedAt[m.ObjectID]; !seen && m.Op == "add" {
			order = append(order, m.ObjectID)
			addedAt[m.ObjectID] = m.StepIndex
		}
		if m.Op == "remove" {
			removedAt[m.ObjectID] = m.StepIndex
		}
	}
	for _, id := range order {
		if addedInFinal[id] {
			continue
		}
		aStep := addedAt[id]
		rStep, wasRemoved := removedAt[id]
		if !wasRemoved {
			continue
		}
		if len(stems) > 0 {
			matched := false
			for st := range stems {
				if st == "object" || st == "item" || st == "entity" || st == "node" || strings.HasPrefix(strings.ToLower(id), st) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		return fmt.Sprintf("%s was added at step %d and removed again at step %d.", id, aStep, rStep)
	}
	return ""
}

// RecordAgentTurnSummary sets the summary on turnID (or latest turn) and runs the editor's automatic
// summary-vs-diff mismatch check against the turn's actual review.
func (e *Editor) RecordAgentTurnSummary(turnID, summary string) *AgentMismatchReport {
	if e == nil {
		return nil
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil {
		return nil
	}
	turn.Summary = summary
	e.refreshTurnReview(turn)
	turn.Mismatch = CheckAgentSummaryAgainstDiff(turn.Summary, turn.Review, turn.mutations)
	if turn.Mismatch != nil {
		e.cancelAutoSaveTimer()
	}
	e.agent.turnChangeSeq++
	e.notify()
	return turn.Mismatch
}

// AskAgentWhyMismatch sends a follow-up question asking why the summary disagrees with the actual review diff.
func (e *Editor) AskAgentWhyMismatch(turnID string) error {
	if err := e.validateAgentContinuationPreconditions(); err != nil {
		return err
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Mismatch == nil {
		return errors.New("editor: no mismatch on turn")
	}
	var details []string
	for _, r := range turn.Mismatch.Rows {
		if r.Disagrees {
			details = append(details, fmt.Sprintf("%s: summary says %s, review has %s", r.What, r.Summary, r.Review))
		}
	}
	prompt := fmt.Sprintf("Why does your summary disagree with the review diff (%s)?", strings.Join(details, "; "))
	if e.AgentRunner == nil && len(e.agent.clients) == 0 && e.agent.mode != AgentModeLocal {
		turn.Mismatch.AskedWhy = true
		e.agent.turnChangeSeq++
		e.notify()
		return nil
	}
	if err := e.SubmitAgentPrompt(prompt); err != nil {
		return err
	}
	// Re-fetch turn because SubmitAgentPrompt appended a new turn (may reallocate e.agent.turns).
	if orig := e.findAgentTurn(turn.ID); orig != nil && orig.Mismatch != nil {
		orig.Mismatch.AskedWhy = true
		e.agent.turnChangeSeq++
		e.notify()
	}
	return nil
}

// --- Turn Review (Open / Close / Keep / Discard) ---

// OpenAgentTurnReview opens the inline review for turnID (or latest turn with a review) so the author can
// inspect the actual diff and Keep or Discard it.
func (e *Editor) OpenAgentTurnReview(turnID string) *AgentTurnReview {
	if e == nil {
		return nil
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil {
		return nil
	}
	rev := e.refreshTurnReview(turn)
	if rev == nil {
		return nil
	}
	e.agent.openReviewTurnID = turn.ID
	e.cancelAutoSaveTimer()
	if e.OnOpenAgentReview != nil {
		e.OnOpenAgentReview(*rev.Clone())
	}
	e.notify()
	return rev.Clone()
}

// CloseAgentTurnReview closes the open inline agent turn review drawer.
func (e *Editor) CloseAgentTurnReview() {
	if e == nil || e.agent.openReviewTurnID == "" {
		return
	}
	e.agent.openReviewTurnID = ""
	e.notify()
}

// ActiveAgentReview returns a copy of the currently open agent turn review, or nil.
func (e *Editor) ActiveAgentReview() *AgentTurnReview {
	if e == nil || e.agent.openReviewTurnID == "" {
		return nil
	}
	turn := e.findAgentTurn(e.agent.openReviewTurnID)
	if turn == nil || turn.Review == nil {
		return nil
	}
	return turn.Review.Clone()
}

// KeepAgentTurnDiffReview keeps the actual diff in turnID's review (never the summary) and closes the review drawer.
func (e *Editor) KeepAgentTurnDiffReview(turnID string) bool {
	if e == nil {
		return false
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Review == nil {
		return false
	}
	turn.Review.Kept = true
	turn.Review.Discarded = false
	if turn.PendingReview != nil {
		turn.PendingReview.Kept = true
		turn.PendingReview.Reverted = false
	}
	if turn.Partial != nil {
		turn.Partial.Kept = true
		turn.Partial.Pointing = false
	}
	if e.agent.pointingTurnID == turn.ID {
		e.agent.pointingTurnID = ""
	}
	if e.agent.openReviewTurnID == turn.ID {
		e.agent.openReviewTurnID = ""
	}
	e.agent.turnChangeSeq++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return true
}

// DiscardAgentTurnReview reverts all unkept undo steps recorded since the turn began and marks the review discarded.
func (e *Editor) DiscardAgentTurnReview(turnID string) bool {
	if e == nil || e.playing {
		return false
	}
	turn := e.findOrLatestAgentTurn(turnID)
	if turn == nil || turn.Review == nil || turn.Review.Kept || turn.Review.Discarded {
		return false
	}
	// Never unwind undo history of a different scene tab than the one the turn modified.
	if turn.Review.File != "" && e.agentOpenSceneName() != turn.Review.File {
		return false
	}
	if e.History != nil && e.History.Index() >= turn.Review.HistoryStart {
		for e.History.Index() > turn.Review.HistoryStart {
			if !e.Undo() {
				break
			}
		}
	}
	turn.Review.Discarded = true
	turn.Review.Added = nil
	turn.Review.Changed = nil
	turn.Review.Removed = nil
	turn.Review.PlusLines = 0
	turn.Review.MinusLines = 0
	if turn.PendingReview != nil {
		turn.PendingReview.Kept = false
		turn.PendingReview.Reverted = true
	}
	turn.baselineDoc = nil
	turn.baselineYAML = ""
	turn.lastSeenIDs = nil
	turn.mutations = nil
	if e.agent.pointingTurnID == turn.ID {
		e.agent.pointingTurnID = ""
	}
	if e.agent.openReviewTurnID == turn.ID {
		e.agent.openReviewTurnID = ""
	}
	e.agent.turnChangeSeq++
	e.notify()
	return true
}

// --- Offline mode & request queue ---

// AgentOffline reports whether the editor's Agent panel is currently in offline mode.
func (e *Editor) AgentOffline() bool {
	return e != nil && e.agent.offline
}

// SetAgentOffline switches the Agent panel into or out of offline mode.
// Manual tools (select, move, inspector, game check, Play) keep working in offline mode.
// Returning online never auto-runs queued requests; each queued request waits to be sent explicitly.
func (e *Editor) SetAgentOffline(offline bool) {
	if e == nil || e.agent.offline == offline {
		return
	}
	e.agent.offline = offline
	if offline {
		e.cancelAgentReconnect()
		if e.agent.activeRunCancel != nil {
			e.markActiveTurnLost("Offline")
		}
		e.agent.status = AgentStatusOffline
	} else {
		e.restoreIdleAgentStatus()
	}
	e.updateAgentAnnouncement()
	e.notify()
}

// QueueAgentRequest appends a request to the queue with its timestamp without executing it.
func (e *Editor) QueueAgentRequest(prompt string) AgentQueuedRequest {
	return e.queueAgentTaskRequest(AgentTaskSceneEdit, prompt, true)
}

func (e *Editor) queueAgentTaskRequest(task AgentTaskKind, prompt string, offline bool) AgentQueuedRequest {
	if e == nil {
		return AgentQueuedRequest{}
	}
	if !task.Valid() {
		task = AgentTaskSceneEdit
	}
	fromComposer := strings.TrimSpace(prompt) == ""
	text := strings.TrimSpace(prompt)
	if fromComposer {
		text = strings.TrimSpace(e.composerDraft)
		if text == "" {
			text = strings.TrimSpace(e.agent.composerText)
		}
	}
	if text == "" {
		return AgentQueuedRequest{}
	}
	if len(e.agent.queued) >= maxQueuedAgentRequests {
		return AgentQueuedRequest{}
	}
	if fromComposer || text == strings.TrimSpace(e.composerDraft) || text == strings.TrimSpace(e.agent.composerText) {
		e.composerDraft = ""
		e.agent.composerText = ""
		e.agentComposer.pickerOpen = false
		e.agentComposer.pickerOpenedByAt = false
		e.agentComposer.pickerQuery = ""
		e.agentComposer.pickerActiveIndex = 0
		e.agentComposer.hoveredChipID = ""
	}
	e.agent.queueSeq++
	item := AgentQueuedRequest{
		ID:       fmt.Sprintf("queue-%d", e.agent.queueSeq),
		Task:     task,
		Prompt:   text,
		QueuedAt: e.nowTime(),
		Offline:  offline,
	}
	e.agent.queued = append(e.agent.queued, item)
	e.notify()
	return item
}

func (e *Editor) findOrLatestAgentTurn(turnID string) *AgentTurn {
	if e == nil || len(e.agent.turns) == 0 {
		return nil
	}
	if turnID != "" {
		return e.findAgentTurn(turnID)
	}
	return &e.agent.turns[len(e.agent.turns)-1]
}
