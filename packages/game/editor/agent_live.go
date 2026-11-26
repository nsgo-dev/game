package editor

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// ErrAgentTouchingObject is returned when a manual edit attempts to modify an object
// that the agent is currently touching during an active step.
var ErrAgentTouchingObject = errors.New("editor: object is currently being edited by the agent")

// ErrNoAgentTurn is returned when KeepAgentTurn or RevertAgentTurn is called without a pending agent turn.
var ErrNoAgentTurn = errors.New("editor: no pending agent turn")

// OverlayLegendKind identifies one toggleable row of the Spec viewport overlay legend
// (docs/design/editor/Spec.dc.html): Selection, Agent working, Added, Changed, Removed, Problem.
type OverlayLegendKind string

const (
	OverlayLegendSelection OverlayLegendKind = "selection"
	OverlayLegendAgent     OverlayLegendKind = "agent"
	OverlayLegendAdded     OverlayLegendKind = "added"
	OverlayLegendChanged   OverlayLegendKind = "changed"
	OverlayLegendRemoved   OverlayLegendKind = "removed"
	OverlayLegendProblem   OverlayLegendKind = "problem"
)

var allOverlayLegendKinds = []OverlayLegendKind{
	OverlayLegendSelection,
	OverlayLegendAgent,
	OverlayLegendAdded,
	OverlayLegendChanged,
	OverlayLegendRemoved,
	OverlayLegendProblem,
}

// AllOverlayLegendKinds returns every Spec overlay legend kind in canonical display order.
func AllOverlayLegendKinds() []OverlayLegendKind {
	return slices.Clone(allOverlayLegendKinds)
}

// Valid reports whether k is a recognized Spec overlay legend kind.
func (k OverlayLegendKind) Valid() bool {
	return slices.Contains(allOverlayLegendKinds, k)
}

// AgentState describes the agent's live state reflected in the editor header pill
// (docs/design/editor/Working.dc.html).
type AgentState string

const (
	// AgentStateIdle means no agent turn is active and no live step is running.
	AgentStateIdle AgentState = "idle"
	// AgentStateWorking means the agent is actively executing a step or running a tool.
	AgentStateWorking AgentState = "working"
	// AgentStateWaiting means the agent is paused waiting for the person (for example a permission ask).
	AgentStateWaiting AgentState = "waiting"
	// AgentStatePendingReview means the agent finished its current step and pending turn changes await Keep or Revert.
	AgentStatePendingReview AgentState = "pending_review"
)

// Valid reports whether s is a recognized AgentState.
func (s AgentState) Valid() bool {
	switch s {
	case AgentStateIdle, AgentStateWorking, AgentStateWaiting, AgentStatePendingReview:
		return true
	default:
		return false
	}
}

// AgentChangeKind classifies how an object differs in a pending agent turn.
type AgentChangeKind string

const (
	AgentChangeAdded   AgentChangeKind = "added"
	AgentChangeChanged AgentChangeKind = "changed"
	AgentChangeRemoved AgentChangeKind = "removed"
)

// AgentFieldDiff records one changed object, component, or property field in a pending agent turn.
type AgentFieldDiff struct {
	Component string `json:"component,omitempty"`
	Field     string `json:"field"`
	OldValue  string `json:"oldValue"`
	NewValue  string `json:"newValue"`
}

// AgentObjectChange describes one object added, changed, or removed during the pending agent turn.
type AgentObjectChange struct {
	ObjectID   string                 `json:"objectId"`
	Name       string                 `json:"name"`
	Layer      string                 `json:"layer"`
	ParentID   string                 `json:"parentId,omitempty"`
	Kind       AgentChangeKind        `json:"kind"`
	Detail     string                 `json:"detail"`
	Fields     []AgentFieldDiff       `json:"fields,omitempty"`
	RemovedDoc *sceneformat.ObjectDoc `json:"-"`
}

// AgentRemovedNode represents an object removed during the pending agent turn so the scene tree
// and viewport can show its removed marker and ghost until Keep or Revert.
type AgentRemovedNode struct {
	ObjectID string
	Name     string
	Layer    string
	ParentID string
	Doc      *sceneformat.ObjectDoc
}

// ViewportLiveOverlay describes one inspectable overlay drawn in the viewport for the current editor state.
type ViewportLiveOverlay struct {
	Kind     OverlayLegendKind `json:"kind"`
	ObjectID string            `json:"objectId"`
	Label    string            `json:"label"`
	Detail   string            `json:"detail,omitempty"`
	Dashed   bool              `json:"dashed"`
	OutsetPx float32           `json:"outsetPx"`
	Ghost    bool              `json:"ghost,omitempty"`
	Visible  bool              `json:"visible"`
	ReadOnly bool              `json:"readOnly,omitempty"`
}

type agentLiveTurnState struct {
	label          string
	baseDoc        *sceneformat.SceneDoc
	baseBuilt      *sceneformat.BuiltScene
	historyStart   int
	changes        []AgentObjectChange
	byID           map[string]AgentObjectChange
	removed        []AgentRemovedNode
	inKeepOrRevert bool
}

type agentLiveState struct {
	turn              *agentLiveTurnState
	state             AgentState
	stateDetail       string
	workingIDs        []string
	workingSet        map[string]bool
	workingActivity   string
	pulseStart        time.Time
	pulsePhase        float64
	pulseStop         func()
	pulseRev          uint64
	nextPulseListener uint64
	pulseListeners    map[uint64]func()
	reducedMotion     *bool
}

const (
	agentPulsePeriod   = 1600 * time.Millisecond
	agentPulseMinAlpha = 0.45
	agentPulseMaxAlpha = 1.0
)

// RunAsAgent executes fn while marking edits as originating from the agent rather than manual UI input,
// so agent-initiated MCP calls can edit objects the agent is currently touching while manual edits stay blocked.
func (e *Editor) RunAsAgent(fn func() error) error {
	if e == nil {
		return fn()
	}
	e.agentMutating++
	defer func() {
		e.agentMutating--
	}()
	return fn()
}

// AgentMutating reports whether the editor is currently executing an agent-originated mutation.
func (e *Editor) AgentMutating() bool {
	return e != nil && e.agentMutating > 0
}

// BeginAgentTurn opens a pending agent turn for the active scene if none is open yet,
// snapshotting the current scene as the structural diff baseline and recording the current history index.
// When a turn is already open, it preserves the existing baseline and updates the label if empty.
func (e *Editor) BeginAgentTurn(label string) {
	if e == nil || e.Doc == nil {
		return
	}
	if e.agentLive.turn != nil {
		if e.agentLive.turn.label == "" && strings.TrimSpace(label) != "" {
			e.agentLive.turn.label = strings.TrimSpace(label)
		}
		return
	}
	baseDoc := e.Doc.Clone()
	var baseBuilt *sceneformat.BuiltScene
	if built, err := sceneformat.Build(baseDoc, e.liveResolver()); err == nil {
		baseBuilt = built
	}
	histStart := 0
	if e.History != nil {
		histStart = e.History.Index()
	}
	e.agentLive.turn = &agentLiveTurnState{
		label:        strings.TrimSpace(label),
		baseDoc:      baseDoc,
		baseBuilt:    baseBuilt,
		historyStart: histStart,
		byID:         map[string]AgentObjectChange{},
	}
	e.refreshAgentTurnDiff()
	e.syncActiveTab()
	e.notify()
}

// AgentTurnPending reports whether the active scene currently has an open pending agent turn.
func (e *Editor) AgentTurnPending() bool {
	return e != nil && e.agentLive.turn != nil
}

// AgentTurnChanges returns a copy of the derived object changes in the pending agent turn.
func (e *Editor) AgentTurnChanges() []AgentObjectChange {
	if e == nil || e.agentLive.turn == nil || len(e.agentLive.turn.changes) == 0 {
		return nil
	}
	out := make([]AgentObjectChange, len(e.agentLive.turn.changes))
	for i, c := range e.agentLive.turn.changes {
		c.Fields = slices.Clone(c.Fields)
		out[i] = c
	}
	return out
}

// AgentObjectChange returns the pending turn change for objectID, if any.
func (e *Editor) AgentObjectChange(objectID string) (AgentObjectChange, bool) {
	if e == nil || e.agentLive.turn == nil || objectID == "" {
		return AgentObjectChange{}, false
	}
	c, ok := e.agentLive.turn.byID[objectID]
	if !ok {
		return AgentObjectChange{}, false
	}
	c.Fields = slices.Clone(c.Fields)
	return c, true
}

// AgentRemovedNodes returns pending removed nodes belonging to layer and parentID (empty parentID for top-level layer children).
func (e *Editor) AgentRemovedNodes(layer, parentID string) []AgentRemovedNode {
	if e == nil || e.agentLive.turn == nil || len(e.agentLive.turn.removed) == 0 {
		return nil
	}
	var out []AgentRemovedNode
	for _, r := range e.agentLive.turn.removed {
		if r.Layer == layer && r.ParentID == parentID {
			out = append(out, r)
		}
	}
	return out
}

// DiscardEmptyAgentTurn closes an active agent turn when it has recorded no scene changes
// (for example after a refused MCP edit).
func (e *Editor) DiscardEmptyAgentTurn() {
	if e == nil || e.agentLive.turn == nil {
		return
	}
	e.refreshAgentTurnDiff()
	if len(e.agentLive.turn.changes) > 0 {
		return
	}
	e.agentLive.turn = nil
	if len(e.agentLive.workingIDs) == 0 && e.agentLive.state == AgentStatePendingReview {
		e.agentLive.state = AgentStateIdle
		e.agentLive.stateDetail = ""
	}
	e.inspectorReviewChanges = nil
	e.syncActiveTab()
	e.notify()
}

// keepLiveAgentTurn accepts all pending changes in the open live agent turn as a single undo step,
// clearing the live viewport overlays, scene tree markers, inspector review hints, and agent working state.
func (e *Editor) keepLiveAgentTurn() error {
	if e == nil || e.agentLive.turn == nil {
		return ErrNoAgentTurn
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	turn := e.agentLive.turn
	turn.inKeepOrRevert = true
	if e.History != nil && turn.historyStart >= 0 && e.History.Index() > turn.historyStart+1 {
		// Collapse every command executed during the turn into a single undo step (Spec: one undo step ⌘Z)
		// and replace the intermediate timeline entries with one unified Agent History entry.
		start := turn.historyStart
		end := e.History.Index()
		merged := make([]Command, end-start)
		copy(merged, e.History.commands[start:end])
		label := turn.label
		if label == "" {
			label = fmt.Sprintf("Agent turn (%d changes)", len(turn.changes))
		}
		summary := strings.TrimSpace(strings.TrimPrefix(label, "Agent:"))
		if summary == "" {
			summary = label
		}
		collapsed := &groupCmd{label: label, cmds: merged}
		e.History.truncateRecordsAfterCommand(start)
		next := make([]Command, 0, start+1)
		next = append(next, e.History.commands[:start]...)
		next = append(next, collapsed)
		e.History.commands = next
		e.History.cursor = len(next)

		prevMeta := e.historyPanel.pendingMeta
		nChanges := len(turn.changes)
		if nChanges <= 0 {
			nChanges = len(merged)
		}
		e.historyPanel.pendingMeta = &HistoryMeta{
			Author:      HistoryAuthorAgent,
			Summary:     summary,
			Detail:      formatAgentTurnDetail("", nChanges, nChanges),
			ChangeCount: nChanges,
			KeptCount:   nChanges,
		}
		e.History.recordCommand(collapsed, e.History.cursor)
		e.historyPanel.pendingMeta = prevMeta
	}
	e.stopAgentPulseLoop()
	e.agentLive.turn = nil
	e.agentLive.workingIDs = nil
	e.agentLive.workingSet = nil
	e.agentLive.workingActivity = ""
	e.agentLive.state = AgentStateIdle
	e.agentLive.stateDetail = ""
	e.inspectorReviewChanges = nil
	e.Status = e.T("agent.statusKept")
	e.syncActiveTab()
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

// revertLiveAgentTurn reverts the open scene back to its exact state before the pending live agent turn,
// clearing the live viewport overlays, scene tree markers, inspector review hints, and agent working state.
func (e *Editor) revertLiveAgentTurn() error {
	if e == nil || e.agentLive.turn == nil {
		return ErrNoAgentTurn
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return ErrHistoryPreview
	}
	turn := e.agentLive.turn
	turn.inKeepOrRevert = true
	e.stopAgentPulseLoop()
	e.CancelDrag2D()
	e.CancelShapeDrag2D()
	e.CancelInspectorScrub()
	e.CancelGizmoDrag3D()
	e.CancelHandleDrag3D()
	e.CancelComponentGizmoDrag3D()

	restoredViaHistory := false
	if e.History != nil && turn.historyStart >= 0 && turn.historyStart <= e.History.Index() {
		steps := e.History.Index() - turn.historyStart
		allOK := true
		for range steps {
			ok, err := e.History.undo()
			if err != nil || !ok {
				allOK = false
				break
			}
		}
		if allOK && e.History.Index() == turn.historyStart {
			if turn.historyStart < len(e.History.commands) {
				clear(e.History.commands[turn.historyStart:])
				e.History.commands = e.History.commands[:turn.historyStart]
			}
			e.History.truncateRecordsAfterCommand(turn.historyStart)
			e.History.syncUndoFlags()
			restoredViaHistory = true
		}
	}
	if !restoredViaHistory && turn.baseDoc != nil {
		e.Doc = turn.baseDoc.Clone()
		e.Rebuild()
		e.pruneSelection()
	}

	e.agentLive.turn = nil
	e.agentLive.workingIDs = nil
	e.agentLive.workingSet = nil
	e.agentLive.workingActivity = ""
	e.agentLive.state = AgentStateIdle
	e.agentLive.stateDetail = ""
	e.inspectorReviewChanges = nil
	e.Status = e.T("agent.statusReverted")
	e.syncActiveTab()
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

// BeginAgentStep marks objectIDs as currently being touched by the agent with the human-readable
// activity pill label (for example "placing slime_02"). While marked, manual edits to those objects
// are refused in the viewport, inspector, scene tree, and direct Editor edit methods while keeping
// selection handles visible.
func (e *Editor) BeginAgentStep(objectIDs []string, activity string) {
	if e == nil {
		return
	}
	var clean []string
	set := make(map[string]bool, len(objectIDs))
	for _, id := range objectIDs {
		id = strings.TrimSpace(id)
		if id == "" || set[id] {
			continue
		}
		set[id] = true
		clean = append(clean, id)
	}
	// Cancel any in-flight manual drag or inspector scrub touching a newly locked object.
	if e.drag2D.handle != Handle2DNone {
		cancel2D := set[e.drag2D.objectID]
		if !cancel2D {
			for _, tgt := range e.drag2D.targets {
				if set[tgt.id] {
					cancel2D = true
					break
				}
			}
		}
		if cancel2D {
			e.CancelDrag2D()
		}
	}
	if e.shapeDrag.active && set[e.shapeDrag.handle.ObjectID] {
		e.CancelShapeDrag2D()
	}
	if e.inspectorScrub.active && set[e.inspectorScrub.spec.ObjectID] {
		e.CancelInspectorScrub()
	}
	if e.compGizmoDrag.active && set[e.compGizmoDrag.objectID] {
		e.CancelComponentGizmoDrag3D()
	}
	if e.drag3D.kind != drag3DNone {
		cancel3D := set[e.drag3D.objectID] || set[e.SelectedID]
		if !cancel3D {
			for _, tgt := range e.drag3D.targets {
				if set[tgt.id] {
					cancel3D = true
					break
				}
			}
		}
		if cancel3D {
			if e.drag3D.kind == drag3DHandle {
				e.CancelHandleDrag3D()
			} else {
				e.CancelGizmoDrag3D()
			}
		}
	}

	e.agentLive.workingIDs = clean
	if len(set) > 0 {
		e.agentLive.workingSet = set
	} else {
		e.agentLive.workingSet = nil
	}
	e.agentLive.workingActivity = strings.TrimSpace(activity)
	e.agentLive.state = AgentStateWorking
	e.agentLive.stateDetail = e.agentLive.workingActivity
	e.syncAgentPulseLoop()
	e.syncActiveTab()
	e.notify()
}

// EndAgentStep clears the objects currently touched by the agent so manual edits to them are allowed again,
// and moves the header state to AgentStatePendingReview when pending turn changes exist, or AgentStateIdle otherwise.
func (e *Editor) EndAgentStep() {
	if e == nil {
		return
	}
	hadWorking := len(e.agentLive.workingIDs) > 0 || e.agentLive.workingActivity != "" || e.agentLive.state == AgentStateWorking
	e.stopAgentPulseLoop()
	e.agentLive.workingIDs = nil
	e.agentLive.workingSet = nil
	e.agentLive.workingActivity = ""
	if e.agentLive.turn != nil && len(e.agentLive.turn.changes) > 0 {
		e.agentLive.state = AgentStatePendingReview
		e.agentLive.stateDetail = ""
	} else {
		e.agentLive.state = AgentStateIdle
		e.agentLive.stateDetail = ""
	}
	if hadWorking {
		e.syncActiveTab()
		e.notify()
	}
}

// SetAgentState updates the agent's live state and optional detail text shown in the header pill.
func (e *Editor) SetAgentState(state AgentState, detail string) {
	if e == nil || !state.Valid() {
		return
	}
	detail = strings.TrimSpace(detail)
	if e.agentLive.state == state && e.agentLive.stateDetail == detail {
		return
	}
	e.agentLive.state = state
	e.agentLive.stateDetail = detail
	if state == AgentStateIdle {
		e.stopAgentPulseLoop()
		e.agentLive.workingIDs = nil
		e.agentLive.workingSet = nil
		e.agentLive.workingActivity = ""
	}
	e.syncActiveTab()
	e.notify()
}

// AgentState returns the current live agent state and detail text.
func (e *Editor) AgentState() (AgentState, string) {
	if e == nil || e.agentLive.state == "" {
		return AgentStateIdle, ""
	}
	return e.agentLive.state, e.agentLive.stateDetail
}

// AgentHeaderPillText returns the localized text shown in the header pill for the current agent state,
// or "" when the agent is idle.
func (e *Editor) AgentHeaderPillText() string {
	if e == nil {
		return ""
	}
	state, detail := e.AgentState()
	switch state {
	case AgentStateWorking:
		if detail != "" {
			return e.T("agent.headerWorkingDetail", l10n.Param("activity", detail))
		}
		return e.T("agent.headerWorking")
	case AgentStateWaiting:
		if detail != "" {
			return e.T("agent.headerWaitingDetail", l10n.Param("detail", detail))
		}
		return e.T("agent.headerWaiting")
	case AgentStatePendingReview:
		n := 0
		if e.agentLive.turn != nil {
			n = len(e.agentLive.turn.changes)
		}
		if n <= 0 {
			return ""
		}
		return e.T("agent.headerPendingReview", l10n.Param("count", n))
	default:
		return ""
	}
}

// AgentTouchingObject reports whether the agent is currently touching objectID during an active step.
func (e *Editor) AgentTouchingObject(objectID string) bool {
	if e == nil || objectID == "" || len(e.agentLive.workingSet) == 0 {
		return false
	}
	return e.agentLive.workingSet[objectID]
}

// AgentTouchedObjects returns a copy of the object IDs the agent is currently touching.
func (e *Editor) AgentTouchedObjects() []string {
	if e == nil || len(e.agentLive.workingIDs) == 0 {
		return nil
	}
	return slices.Clone(e.agentLive.workingIDs)
}

// AgentWorkingActivity returns the pill text for the agent's active step (for example "placing slime_02").
func (e *Editor) AgentWorkingActivity() string {
	if e == nil {
		return ""
	}
	return e.agentLive.workingActivity
}

// SetReducedMotionOverride overrides reduced-motion detection for deterministic testing.
// Pass nil to follow core.Scope.Env().ReducedMotion.
func (e *Editor) SetReducedMotionOverride(v *bool) {
	if e == nil {
		return
	}
	if v == nil {
		e.agentLive.reducedMotion = nil
	} else {
		cp := *v
		e.agentLive.reducedMotion = &cp
	}
	e.syncAgentPulseLoop()
	e.notify()
}

// AgentPulseRevision returns a monotonically increasing counter bumped on viewport-only agent pulse ticks
// without bumping Editor.Revision().
func (e *Editor) AgentPulseRevision() uint64 {
	if e == nil {
		return 0
	}
	return e.agentLive.pulseRev
}

// SubscribeAgentPulse registers fn to run on viewport-only agent pulse ticks and returns an unsubscribe function.
func (e *Editor) SubscribeAgentPulse(fn func()) func() {
	if e == nil || fn == nil {
		return func() {}
	}
	e.agentLive.nextPulseListener++
	id := e.agentLive.nextPulseListener
	if e.agentLive.pulseListeners == nil {
		e.agentLive.pulseListeners = make(map[uint64]func())
	}
	e.agentLive.pulseListeners[id] = fn
	return func() {
		delete(e.agentLive.pulseListeners, id)
	}
}

func (e *Editor) notifyAgentPulse() {
	if e == nil {
		return
	}
	e.agentLive.pulseRev++
	for _, fn := range e.agentLive.pulseListeners {
		fn()
	}
}

func (e *Editor) syncAgentPulseLoop() {
	if e == nil {
		return
	}
	if e.playing || e.scope == nil || e.prefersReducedMotion() || len(e.agentLive.workingIDs) == 0 {
		e.stopAgentPulseLoop()
		return
	}
	e.startAgentPulseLoop()
}

func (e *Editor) prefersReducedMotion() bool {
	if e == nil {
		return false
	}
	if e.agentLive.reducedMotion != nil {
		return *e.agentLive.reducedMotion
	}
	if e.scope != nil {
		return e.scope.Env().ReduceMotion()
	}
	return false
}

// AgentPulseOpacity returns the current opacity of the Agent working outline in [0.45, 1.0].
// Under reduced motion (or when no pulse timer is running), it stays solid at 1.0 (Spec.dc.html).
func (e *Editor) AgentPulseOpacity() float32 {
	if e == nil || e.prefersReducedMotion() || e.agentLive.pulseStop == nil {
		return agentPulseMaxAlpha
	}
	// Smooth cosine oscillation between 1.0 and 0.45 over 1.6 s.
	mid := (agentPulseMaxAlpha + agentPulseMinAlpha) * 0.5
	amp := (agentPulseMaxAlpha - agentPulseMinAlpha) * 0.5
	return float32(mid + amp*math.Cos(2*math.Pi*e.agentLive.pulsePhase))
}

func (e *Editor) startAgentPulseLoop() {
	if e == nil || e.playing || e.scope == nil || e.prefersReducedMotion() || e.agentLive.pulseStop != nil {
		return
	}
	nowFn := e.Now
	if nowFn == nil {
		nowFn = time.Now
	}
	e.agentLive.pulseStart = nowFn()
	e.agentLive.pulsePhase = 0
	loop := &game.Loop{
		Rate: 30,
		Update: func(dt time.Duration) {
			if e.playing || e.agentLive.pulseStop == nil || len(e.agentLive.workingIDs) == 0 || e.prefersReducedMotion() {
				e.stopAgentPulseLoop()
				return
			}
			elapsed := nowFn().Sub(e.agentLive.pulseStart)
			e.agentLive.pulsePhase = math.Mod(float64(elapsed)/float64(agentPulsePeriod), 1.0)
			e.notifyAgentPulse()
		},
	}
	e.scope.Run(func() {
		e.agentLive.pulseStop = loop.Run(e.scope)
	})
}

func (e *Editor) stopAgentPulseLoop() {
	if e == nil {
		return
	}
	if e.agentLive.pulseStop != nil {
		stop := e.agentLive.pulseStop
		e.agentLive.pulseStop = nil
		stop()
	}
	e.agentLive.pulsePhase = 0
}

// checkAgentTouchLock refuses manual edits on objectID while the agent is touching it.
// Agent-originated edits running inside RunAsAgent are always allowed.
func (e *Editor) checkAgentTouchLock(objectID string) error {
	if e == nil || objectID == "" || e.agentMutating > 0 {
		return nil
	}
	if e.AgentTouchingObject(objectID) {
		return fmt.Errorf("%w: %q is read-only until the agent moves on", ErrAgentTouchingObject, objectID)
	}
	return nil
}

// checkAgentTouchLockSubtree refuses manual structural edits on objectID when objectID itself
// or any descendant is currently touched by the agent.
func (e *Editor) checkAgentTouchLockSubtree(objectID string) error {
	if err := e.checkAgentTouchLock(objectID); err != nil {
		return err
	}
	if e == nil || objectID == "" || e.agentMutating > 0 || len(e.agentLive.workingSet) == 0 {
		return nil
	}
	obj, _, _ := e.FindObject(objectID)
	if obj == nil {
		return nil
	}
	var lockedID string
	sceneformat.WalkObjects(obj.Children, func(o, _ *sceneformat.ObjectDoc) bool {
		if e.agentLive.workingSet[o.ID] {
			lockedID = o.ID
			return false
		}
		return true
	})
	if lockedID != "" {
		return fmt.Errorf("%w: %q is read-only until the agent moves on", ErrAgentTouchingObject, lockedID)
	}
	return nil
}

// HasAgentLiveOverlays reports whether any live agent change overlay or agent-working overlay is active.
func (e *Editor) HasAgentLiveOverlays() bool {
	if e == nil {
		return false
	}
	if len(e.agentLive.workingIDs) > 0 {
		return true
	}
	return e.agentLive.turn != nil && len(e.agentLive.turn.changes) > 0
}

// OverlayLegendKindEnabled reports whether the Spec overlay legend row kind is currently toggled on.
// All kinds default to enabled unless persisted in EditorSettings.HiddenOverlayKinds.
func (e *Editor) OverlayLegendKindEnabled(kind OverlayLegendKind) bool {
	if e == nil || !kind.Valid() {
		return true
	}
	return !slices.Contains(e.settings.HiddenOverlayKinds, string(kind))
}

// SetOverlayLegendKind turns the Spec overlay legend row kind on or off and persists the choice
// in the editor's settings file when SettingsPath is configured.
func (e *Editor) SetOverlayLegendKind(kind OverlayLegendKind, on bool) error {
	if e == nil {
		return nil
	}
	if !kind.Valid() {
		return fmt.Errorf("editor: unknown overlay legend kind %q", kind)
	}
	if e.OverlayLegendKindEnabled(kind) == on {
		return nil
	}
	cur := e.Settings()
	k := string(kind)
	if on {
		cur.HiddenOverlayKinds = slices.DeleteFunc(cur.HiddenOverlayKinds, func(s string) bool { return s == k })
		if len(cur.HiddenOverlayKinds) == 0 {
			cur.HiddenOverlayKinds = nil
		}
	} else if !slices.Contains(cur.HiddenOverlayKinds, k) {
		cur.HiddenOverlayKinds = append(cur.HiddenOverlayKinds, k)
		slices.Sort(cur.HiddenOverlayKinds)
	}
	e.settings = cur
	if e.SettingsPath != "" {
		if err := SaveEditorSettings(e.SettingsPath, &cur); err != nil {
			e.notify()
			return err
		}
	}
	e.notify()
	return nil
}

// ToggleOverlayLegendKind flips the Spec overlay legend row kind between shown and hidden
// and persists the choice in the editor's settings file when SettingsPath is configured.
func (e *Editor) ToggleOverlayLegendKind(kind OverlayLegendKind) error {
	if e == nil {
		return nil
	}
	return e.SetOverlayLegendKind(kind, !e.OverlayLegendKindEnabled(kind))
}

// ViewportLiveOverlays returns the inspectable list of every live overlay item present in the viewport
// (Selection, Added, Changed, Removed, Agent working, and Problem), each carrying its visibility
// according to the Overlay Legend toggles.
func (e *Editor) ViewportLiveOverlays() []ViewportLiveOverlay {
	if e == nil || e.playing || e.HistoryPreviewActive() {
		return nil
	}
	var out []ViewportLiveOverlay

	// 1. Selection overlays.
	selVis := e.OverlayLegendKindEnabled(OverlayLegendSelection)
	for _, id := range e.SelectedIDs() {
		obj, _ := e.FindResolvedObject(id)
		if obj == nil || e.hidden[id] {
			continue
		}
		out = append(out, ViewportLiveOverlay{
			Kind:     OverlayLegendSelection,
			ObjectID: id,
			Label:    agentDisplayName(obj),
			Dashed:   false,
			OutsetPx: 0,
			Visible:  selVis,
			ReadOnly: e.AgentTouchingObject(id),
		})
	}

	// 2. Pending turn Added / Changed / Removed overlays.
	if e.agentLive.turn != nil {
		for _, ch := range e.agentLive.turn.changes {
			switch ch.Kind {
			case AgentChangeAdded:
				if e.hidden[ch.ObjectID] {
					continue
				}
				out = append(out, ViewportLiveOverlay{
					Kind:     OverlayLegendAdded,
					ObjectID: ch.ObjectID,
					Label:    "+ " + ch.Name,
					Detail:   ch.Detail,
					Dashed:   true,
					OutsetPx: 3,
					Visible:  e.OverlayLegendKindEnabled(OverlayLegendAdded),
					ReadOnly: e.AgentTouchingObject(ch.ObjectID),
				})
			case AgentChangeChanged:
				if e.hidden[ch.ObjectID] {
					continue
				}
				out = append(out, ViewportLiveOverlay{
					Kind:     OverlayLegendChanged,
					ObjectID: ch.ObjectID,
					Label:    ch.Detail,
					Detail:   ch.Detail,
					Dashed:   true,
					OutsetPx: 4,
					Visible:  e.OverlayLegendKindEnabled(OverlayLegendChanged),
					ReadOnly: e.AgentTouchingObject(ch.ObjectID),
				})
			case AgentChangeRemoved:
				out = append(out, ViewportLiveOverlay{
					Kind:     OverlayLegendRemoved,
					ObjectID: ch.ObjectID,
					Label:    ch.Name + " removed",
					Detail:   ch.Name,
					Dashed:   false,
					OutsetPx: 4,
					Ghost:    true,
					Visible:  e.OverlayLegendKindEnabled(OverlayLegendRemoved),
				})
			}
		}
	}

	// 3. Agent working overlays (sits 3 px further out when Selection is on the same object).
	if len(e.agentLive.workingIDs) > 0 {
		agentVis := e.OverlayLegendKindEnabled(OverlayLegendAgent)
		activity := e.agentLive.workingActivity
		if activity == "" {
			activity = e.T("agent.workingHere")
		}
		for _, id := range e.agentLive.workingIDs {
			if e.hidden[id] {
				continue
			}
			if obj, _ := e.FindResolvedObject(id); obj == nil {
				continue
			}
			outset := float32(3)
			if e.IsSelected(id) {
				outset = 6 // 3 px further out than the selection outline
			}
			out = append(out, ViewportLiveOverlay{
				Kind:     OverlayLegendAgent,
				ObjectID: id,
				Label:    activity,
				Detail:   activity,
				Dashed:   false,
				OutsetPx: outset,
				Visible:  agentVis,
				ReadOnly: true,
			})
		}
	}

	// 4. Problem overlays on live objects (from explicit inspector field problems).
	if len(e.inspectorFieldProblems) > 0 {
		probVis := e.OverlayLegendKindEnabled(OverlayLegendProblem)
		seen := make(map[string]bool)
		for _, p := range e.inspectorFieldProblems {
			if p.ObjectID == "" || p.Message == "" || seen[p.ObjectID] || e.hidden[p.ObjectID] {
				continue
			}
			obj, _ := e.FindResolvedObject(p.ObjectID)
			if obj == nil {
				continue
			}
			seen[p.ObjectID] = true
			out = append(out, ViewportLiveOverlay{
				Kind:     OverlayLegendProblem,
				ObjectID: p.ObjectID,
				Label:    agentDisplayName(obj) + ": " + p.Message,
				Detail:   p.Message,
				Dashed:   false,
				OutsetPx: 3,
				Visible:  probVis,
				ReadOnly: e.AgentTouchingObject(p.ObjectID),
			})
		}
	}

	return out
}

// VisibleViewportLiveOverlays returns only the live viewport overlays whose legend kind is currently enabled.
func (e *Editor) VisibleViewportLiveOverlays() []ViewportLiveOverlay {
	all := e.ViewportLiveOverlays()
	if len(all) == 0 {
		return nil
	}
	out := make([]ViewportLiveOverlay, 0, len(all))
	for _, ov := range all {
		if ov.Visible {
			out = append(out, ov)
		}
	}
	return out
}

func (e *Editor) syncAgentTurnAfterMutate() {
	if e == nil || e.agentLive.turn == nil || e.agentLive.turn.inKeepOrRevert {
		return
	}
	e.refreshAgentTurnDiff()
}

func (e *Editor) clearAgentLiveForSceneReset() {
	if e == nil {
		return
	}
	e.stopAgentPulseLoop()
	e.agentLive.turn = nil
	e.agentLive.workingIDs = nil
	e.agentLive.workingSet = nil
	e.agentLive.workingActivity = ""
	e.agentLive.state = AgentStateIdle
	e.agentLive.stateDetail = ""
	e.inspectorReviewChanges = nil
}

func (e *Editor) refreshAgentTurnDiff() {
	if e == nil || e.agentLive.turn == nil {
		return
	}
	turn := e.agentLive.turn
	changes, removed, reviewChanges := diffSceneObjects(turn.baseDoc, e.Doc)
	turn.changes = changes
	turn.removed = removed
	turn.byID = make(map[string]AgentObjectChange, len(changes))
	for _, c := range changes {
		turn.byID[c.ObjectID] = c
	}
	e.inspectorReviewChanges = reviewChanges
	if len(e.agentLive.workingIDs) == 0 && e.agentLive.state != AgentStateWaiting {
		if len(changes) > 0 {
			e.agentLive.state = AgentStatePendingReview
		} else if e.agentLive.state == AgentStatePendingReview {
			e.agentLive.state = AgentStateIdle
		}
	}
}

type agentFlatSceneObj struct {
	obj      *sceneformat.ObjectDoc
	layer    string
	parentID string
}

func flattenAgentSceneObjects(doc *sceneformat.SceneDoc) ([]string, map[string]agentFlatSceneObj) {
	if doc == nil {
		return nil, nil
	}
	var order []string
	byID := make(map[string]agentFlatSceneObj)
	for li := range doc.Layers {
		layerName := doc.Layers[li].Name
		var walk func(objs []sceneformat.ObjectDoc, parentID string)
		walk = func(objs []sceneformat.ObjectDoc, parentID string) {
			for i := range objs {
				o := &objs[i]
				if _, dup := byID[o.ID]; !dup {
					order = append(order, o.ID)
					byID[o.ID] = agentFlatSceneObj{obj: o, layer: layerName, parentID: parentID}
				}
				walk(o.Children, o.ID)
			}
		}
		walk(doc.Layers[li].Objects, "")
	}
	return order, byID
}

func agentDisplayName(obj *sceneformat.ObjectDoc) string {
	if obj == nil {
		return ""
	}
	if strings.TrimSpace(obj.Name) != "" {
		return obj.Name
	}
	return obj.ID
}

func diffSceneObjects(baseDoc, curDoc *sceneformat.SceneDoc) ([]AgentObjectChange, []AgentRemovedNode, []InspectorReviewChange) {
	baseOrder, baseMap := flattenAgentSceneObjects(baseDoc)
	curOrder, curMap := flattenAgentSceneObjects(curDoc)

	var changes []AgentObjectChange
	var removed []AgentRemovedNode
	var reviewChanges []InspectorReviewChange

	// Added and changed objects in current scene order.
	for _, id := range curOrder {
		cur := curMap[id]
		base, existed := baseMap[id]
		if !existed {
			name := agentDisplayName(cur.obj)
			changes = append(changes, AgentObjectChange{
				ObjectID: id,
				Name:     name,
				Layer:    cur.layer,
				ParentID: cur.parentID,
				Kind:     AgentChangeAdded,
				Detail:   "+ " + name,
			})
			continue
		}
		fields := diffObjectFields(base, cur)
		if len(fields) == 0 {
			continue
		}
		name := agentDisplayName(cur.obj)
		changes = append(changes, AgentObjectChange{
			ObjectID: id,
			Name:     name,
			Layer:    cur.layer,
			ParentID: cur.parentID,
			Kind:     AgentChangeChanged,
			Detail:   formatFieldDiffSummary(fields),
			Fields:   fields,
		})
		for _, f := range fields {
			reviewChanges = append(reviewChanges, InspectorReviewChange{
				ObjectID:  id,
				Component: f.Component,
				Field:     f.Field,
				OldValue:  f.OldValue,
				NewValue:  f.NewValue,
			})
		}
	}

	// Removed objects in baseline scene order.
	for _, id := range baseOrder {
		if _, still := curMap[id]; still {
			continue
		}
		base := baseMap[id]
		name := agentDisplayName(base.obj)
		cp := base.obj.Clone()
		changes = append(changes, AgentObjectChange{
			ObjectID:   id,
			Name:       name,
			Layer:      base.layer,
			ParentID:   base.parentID,
			Kind:       AgentChangeRemoved,
			Detail:     "removed",
			RemovedDoc: &cp,
		})
		removed = append(removed, AgentRemovedNode{
			ObjectID: id,
			Name:     name,
			Layer:    base.layer,
			ParentID: base.parentID,
			Doc:      &cp,
		})
	}

	return changes, removed, reviewChanges
}

func formatFieldDiffSummary(fields []AgentFieldDiff) string {
	if len(fields) == 0 {
		return ""
	}
	one := func(f AgentFieldDiff) string {
		name := f.Field
		if f.Component != "" {
			name = f.Component + "." + f.Field
		} else if prop, ok := strings.CutPrefix(f.Field, "prop:"); ok {
			name = prop
		}
		return fmt.Sprintf("%s %s → %s", name, f.OldValue, f.NewValue)
	}
	if len(fields) == 1 {
		return one(fields[0])
	}
	if len(fields) == 2 && fields[0].Component == "" && fields[1].Component == "" &&
		((fields[0].Field == "x" && fields[1].Field == "y") ||
			(fields[0].Field == "scaleX" && fields[1].Field == "scaleY")) {
		return one(fields[0]) + ", " + one(fields[1])
	}
	return fmt.Sprintf("%s (+%d)", one(fields[0]), len(fields)-1)
}

func diffObjectFields(base, cur agentFlatSceneObj) []AgentFieldDiff {
	b, c := base.obj, cur.obj
	var diffs []AgentFieldDiff
	add := func(comp, field, oldVal, newVal string) {
		if oldVal == newVal {
			return
		}
		diffs = append(diffs, AgentFieldDiff{
			Component: comp,
			Field:     field,
			OldValue:  oldVal,
			NewValue:  newVal,
		})
	}
	if b.Name != c.Name {
		add("", "name", fmtStringVal(b.Name), fmtStringVal(c.Name))
	}
	if b.X != c.X {
		add("", "x", fmtFloat32Val(b.X), fmtFloat32Val(c.X))
	}
	if b.Y != c.Y {
		add("", "y", fmtFloat32Val(b.Y), fmtFloat32Val(c.Y))
	}
	if b.Z != c.Z {
		add("", "z", strconv.Itoa(b.Z), strconv.Itoa(c.Z))
	}
	if b.Rotation != c.Rotation {
		add("", "rotation", fmtFloat32Val(b.Rotation), fmtFloat32Val(c.Rotation))
	}
	if b.EffectiveScaleX() != c.EffectiveScaleX() {
		add("", "scaleX", fmtFloat32Val(b.EffectiveScaleX()), fmtFloat32Val(c.EffectiveScaleX()))
	}
	if b.EffectiveScaleY() != c.EffectiveScaleY() {
		add("", "scaleY", fmtFloat32Val(b.EffectiveScaleY()), fmtFloat32Val(c.EffectiveScaleY()))
	}
	if b.EffectiveAlpha() != c.EffectiveAlpha() {
		add("", "alpha", fmtFloat32Val(b.EffectiveAlpha()), fmtFloat32Val(c.EffectiveAlpha()))
	}
	if b.OriginX != c.OriginX {
		add("", "originX", fmtFloat32Val(b.OriginX), fmtFloat32Val(c.OriginX))
	}
	if b.OriginY != c.OriginY {
		add("", "originY", fmtFloat32Val(b.OriginY), fmtFloat32Val(c.OriginY))
	}
	if b.FlipX != c.FlipX {
		add("", "flipX", strconv.FormatBool(b.FlipX), strconv.FormatBool(c.FlipX))
	}
	if b.FlipY != c.FlipY {
		add("", "flipY", strconv.FormatBool(b.FlipY), strconv.FormatBool(c.FlipY))
	}
	if !spriteEqual(b.Sprite, c.Sprite) {
		add("", "sprite", fmtSpriteRef(b.Sprite), fmtSpriteRef(c.Sprite))
	}
	if b.Material != c.Material {
		add("", "material", fmtStringVal(b.Material), fmtStringVal(c.Material))
	}
	if !reflect.DeepEqual(b.Particles, c.Particles) {
		add("", "particles", fmtParticlesRef(b.Particles), fmtParticlesRef(c.Particles))
	}
	if b.PosZ != c.PosZ {
		add("", "posZ", fmtFloat32Val(b.PosZ), fmtFloat32Val(c.PosZ))
	}
	if b.RotX != c.RotX {
		add("", "rotX", fmtFloat32Val(b.RotX), fmtFloat32Val(c.RotX))
	}
	if b.RotY != c.RotY {
		add("", "rotY", fmtFloat32Val(b.RotY), fmtFloat32Val(c.RotY))
	}
	if b.RotZ != c.RotZ {
		add("", "rotZ", fmtFloat32Val(b.RotZ), fmtFloat32Val(c.RotZ))
	}
	if b.EffectiveScaleZ() != c.EffectiveScaleZ() {
		add("", "scaleZ", fmtFloat32Val(b.EffectiveScaleZ()), fmtFloat32Val(c.EffectiveScaleZ()))
	}
	if b.Depth != c.Depth {
		add("", "depth", fmtFloat32Val(b.Depth), fmtFloat32Val(c.Depth))
	}
	if b.Mesh != c.Mesh {
		add("", "mesh", fmtStringVal(b.Mesh), fmtStringVal(c.Mesh))
	}
	if b.Model != c.Model {
		add("", "model", fmtStringVal(b.Model), fmtStringVal(c.Model))
	}
	if b.ModelNode != c.ModelNode {
		add("", "modelNode", fmtStringVal(b.ModelNode), fmtStringVal(c.ModelNode))
	}
	if b.ModelMesh != c.ModelMesh {
		add("", "modelMesh", fmtStringVal(b.ModelMesh), fmtStringVal(c.ModelMesh))
	}
	if b.GridMap != c.GridMap {
		add("", "gridMap", fmtStringVal(b.GridMap), fmtStringVal(c.GridMap))
	}
	if b.Prefab != c.Prefab {
		add("", "prefab", fmtStringVal(b.Prefab), fmtStringVal(c.Prefab))
	}
	if b.Scene != c.Scene {
		add("", "scene", fmtStringVal(b.Scene), fmtStringVal(c.Scene))
	}
	if !reflect.DeepEqual(b.Occluder, c.Occluder) {
		oldVal, newVal := fmtPresenceVal(b.Occluder != nil), fmtPresenceVal(c.Occluder != nil)
		if oldVal == newVal {
			oldVal, newVal = "previous", "updated"
		}
		add("", "occluder", oldVal, newVal)
	}
	if !reflect.DeepEqual(b.Light, c.Light) {
		oldVal, newVal := fmtLightVal(b.Light), fmtLightVal(c.Light)
		if oldVal == newVal {
			oldVal, newVal = "previous", "updated"
		}
		add("", "light", oldVal, newVal)
	}
	if !reflect.DeepEqual(b.Camera, c.Camera) {
		oldVal, newVal := fmtCameraVal(b.Camera), fmtCameraVal(c.Camera)
		if oldVal == newVal {
			oldVal, newVal = "previous", "updated"
		}
		add("", "camera", oldVal, newVal)
	}
	if !reflect.DeepEqual(b.ReflectionProbe, c.ReflectionProbe) {
		oldVal, newVal := fmtPresenceVal(b.ReflectionProbe != nil), fmtPresenceVal(c.ReflectionProbe != nil)
		if oldVal == newVal {
			oldVal, newVal = "previous", "updated"
		}
		add("", "reflectionProbe", oldVal, newVal)
	}
	if !reflect.DeepEqual(b.LightProbeGrid, c.LightProbeGrid) {
		oldVal, newVal := fmtPresenceVal(b.LightProbeGrid != nil), fmtPresenceVal(c.LightProbeGrid != nil)
		if oldVal == newVal {
			oldVal, newVal = "previous", "updated"
		}
		add("", "lightProbeGrid", oldVal, newVal)
	}
	if base.layer != cur.layer {
		add("", "layer", base.layer, cur.layer)
	}
	if base.parentID != cur.parentID {
		add("", "parent", fmtStringVal(base.parentID), fmtStringVal(cur.parentID))
	}

	// Components diff in deterministic sorted key order.
	compKeys := unionSortedKeys(b.Components, c.Components)
	for _, comp := range compKeys {
		bv, bOK := b.Components[comp]
		cv, cOK := c.Components[comp]
		if !bOK {
			add(comp, "", "none", "added")
			continue
		}
		if !cOK {
			add(comp, "", "present", "removed")
			continue
		}
		bm, bmOK := bv.(map[string]any)
		cm, cmOK := cv.(map[string]any)
		if bmOK && cmOK {
			for _, fk := range unionSortedKeys(bm, cm) {
				bf, bfOK := bm[fk]
				cf, cfOK := cm[fk]
				if !bfOK {
					add(comp, fk, "unset", fmtAnyVal(cf))
				} else if !cfOK {
					add(comp, fk, fmtAnyVal(bf), "unset")
				} else if !reflect.DeepEqual(bf, cf) {
					add(comp, fk, fmtAnyVal(bf), fmtAnyVal(cf))
				}
			}
			continue
		}
		if !reflect.DeepEqual(bv, cv) {
			add(comp, "", fmtAnyVal(bv), fmtAnyVal(cv))
		}
	}

	// Custom properties diff in deterministic sorted key order.
	for _, pk := range unionSortedKeys(b.Properties, c.Properties) {
		bv, bOK := b.Properties[pk]
		cv, cOK := c.Properties[pk]
		if !bOK {
			add("", "prop:"+pk, "unset", fmtAnyVal(cv))
		} else if !cOK {
			add("", "prop:"+pk, fmtAnyVal(bv), "unset")
		} else if !reflect.DeepEqual(bv, cv) {
			add("", "prop:"+pk, fmtAnyVal(bv), fmtAnyVal(cv))
		}
	}

	return diffs
}

func unionSortedKeys[V any](a, b map[string]V) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	return slices.Sorted(maps.Keys(set))
}

func fmtFloat32Val(v float32) string {
	return strconv.FormatFloat(float64(v), 'g', 6, 32)
}

func fmtStringVal(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func fmtSpriteRef(r *sceneformat.SpriteRef) string {
	if r == nil {
		return "none"
	}
	return r.Atlas + "/" + r.Frame
}

func fmtParticlesRef(r *sceneformat.ParticlesRef) string {
	if r == nil {
		return "none"
	}
	return r.File
}

func fmtPresenceVal(present bool) string {
	if !present {
		return "none"
	}
	return "configured"
}

func fmtLightVal(l *sceneformat.LightDoc) string {
	if l == nil {
		return "none"
	}
	if l.Kind != "" {
		return string(l.Kind)
	}
	return "light"
}

func fmtCameraVal(cam *sceneformat.Camera3DDoc) string {
	if cam == nil {
		return "none"
	}
	if cam.Projection != "" {
		return string(cam.Projection)
	}
	return "camera"
}

func fmtAnyVal(v any) string {
	switch x := v.(type) {
	case float32:
		return fmtFloat32Val(x)
	case float64:
		return strconv.FormatFloat(x, 'g', 6, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return fmtStringVal(x)
	default:
		return fmt.Sprintf("%v", v)
	}
}
