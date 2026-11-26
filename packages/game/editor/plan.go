package editor

import (
	"errors"
	"fmt"
	"math"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

const (
	maxPlanSteps              = 64
	maxPlanQuestions          = 16
	maxPlanOptionsPerQuestion = 12
	maxPlanFieldsPerStep      = 16
	maxPlanGhostsPerStep      = 64
	maxResolvedPlanGhosts     = 256
	maxPlanNodesPerStep       = 64
	maxPlanFiles              = 128
	maxPlanRepeatCount        = 64
	maxPlanGhostOverrides     = 64
	maxPlanPreviewSegments    = 32
	maxPlanTextLen            = 4096
	maxPlanNoteLen            = 2048
)

// PlanPhase is the lifecycle state of an agent-proposed plan.
type PlanPhase string

const (
	// PlanPhaseNone indicates no plan is active.
	PlanPhaseNone PlanPhase = ""
	// PlanPhaseDraft indicates the agent proposed a plan and is waiting for the person to answer questions, edit steps, and approve.
	PlanPhaseDraft PlanPhase = "draft"
	// PlanPhaseRunning indicates the person approved (or chose Just do it) and the agent is executing steps.
	PlanPhaseRunning PlanPhase = "running"
	// PlanPhaseStopped indicates the person stopped the plan before completion.
	PlanPhaseStopped PlanPhase = "stopped"
	// PlanPhaseDone indicates the plan finished and its step checklist was attached to the turn's review.
	PlanPhaseDone PlanPhase = "done"
)

// PlanStepStatus tracks execution progress of one step in an active or reviewed plan.
type PlanStepStatus string

const (
	PlanStepPending PlanStepStatus = "pending"
	PlanStepRunning PlanStepStatus = "running"
	PlanStepDone    PlanStepStatus = "done"
	PlanStepFailed  PlanStepStatus = "failed"
	PlanStepSkipped PlanStepStatus = "skipped"
)

// PlanFileTag classifies how a file in a plan will be changed.
type PlanFileTag string

const (
	PlanFileEdit   PlanFileTag = "edit"
	PlanFileNew    PlanFileTag = "new"
	PlanFileDelete PlanFileTag = "delete"
)

// PlanEstimate holds optional human-readable duration and token budget estimates for a plan.
type PlanEstimate struct {
	Duration string `json:"duration,omitempty"`
	Tokens   string `json:"tokens,omitempty"`
}

// PlanFile lists one file touched by the plan for the "FILES IN THIS PLAN" panel.
type PlanFile struct {
	Path string      `json:"path"`
	Tag  PlanFileTag `json:"tag,omitempty"`
}

// PlanStripSegment is one segment in a horizontal timing/state strip preview on a question option.
type PlanStripSegment struct {
	From   float32 `json:"from"`   // 0..100 percentage
	To     float32 `json:"to"`     // 0..100 percentage
	Active bool    `json:"active"` // true = lit segment, false = recessed track
}

// PlanOptionPreview describes an optional visual thumbnail shown on a clarifying question option card.
type PlanOptionPreview struct {
	// Kind selects the visual style: "room" (mini room sketch with gate & lever markers) or "strip" (timing/state bar).
	Kind             string             `json:"kind"`
	RoomSplitPercent float32            `json:"roomSplitPercent,omitempty"` // default 46
	GatePercent      float32            `json:"gatePercent,omitempty"`      // 0..100 horizontal position of gate marker
	LeverPercent     float32            `json:"leverPercent,omitempty"`     // 0..100 horizontal position of lever marker
	Segments         []PlanStripSegment `json:"segments,omitempty"`         // for kind == "strip"
}

// PlanGhostOverride adjusts or replaces planned viewport ghosts when a clarifying question option is selected.
type PlanGhostOverride struct {
	Step        int         `json:"step"`
	GhostID     string      `json:"ghostId,omitempty"`
	X           *float32    `json:"x,omitempty"`
	Y           *float32    `json:"y,omitempty"`
	Width       *float32    `json:"width,omitempty"`
	Height      *float32    `json:"height,omitempty"`
	Label       string      `json:"label,omitempty"`
	LabelX      *float32    `json:"labelX,omitempty"`
	LabelY      *float32    `json:"labelY,omitempty"`
	ReplaceWith []PlanGhost `json:"replaceWith,omitempty"`
}

// PlanOption is one selectable choice in a clarifying question on a proposed plan.
type PlanOption struct {
	ID                 string              `json:"id"`
	Label              string              `json:"label"`
	Note               string              `json:"note,omitempty"`
	Suggested          bool                `json:"suggested,omitempty"`
	Preview            *PlanOptionPreview  `json:"preview,omitempty"`
	StepTitleOverrides map[int]string      `json:"stepTitleOverrides,omitempty"`
	GhostOverrides     []PlanGhostOverride `json:"ghostOverrides,omitempty"`
}

// PlanQuestion is a clarifying question the person answers before approving a plan.
type PlanQuestion struct {
	ID         string       `json:"id"`
	Label      string       `json:"label,omitempty"`
	Prompt     string       `json:"prompt"`
	Options    []PlanOption `json:"options"`
	SelectedID string       `json:"selectedId,omitempty"`
}

// SuggestedOption returns the option marked Suggested (falling back to an option noted "Suggested" or index 0).
func (q PlanQuestion) SuggestedOption() (PlanOption, bool) {
	if len(q.Options) == 0 {
		return PlanOption{}, false
	}
	for _, opt := range q.Options {
		if opt.Suggested {
			return opt, true
		}
	}
	for _, opt := range q.Options {
		if strings.EqualFold(strings.TrimSpace(opt.Note), "suggested") {
			return opt, true
		}
	}
	return q.Options[0], true
}

// SelectedOption returns the currently chosen option, if answered.
func (q PlanQuestion) SelectedOption() (PlanOption, bool) {
	for _, opt := range q.Options {
		if opt.ID == q.SelectedID {
			return opt, true
		}
	}
	return PlanOption{}, false
}

// PlanStepField is one editable numeric parameter on a plan step (rendered with - / value / + steppers and limits).
type PlanStepField struct {
	Key           string `json:"key"`
	Label         string `json:"label"`
	Value         int    `json:"value"`
	Min           int    `json:"min"`
	Max           int    `json:"max"`
	Step          int    `json:"step,omitempty"`
	Hint          string `json:"hint,omitempty"`
	CountsObjects bool   `json:"countsObjects,omitempty"`
}

// PlanGhost describes a planned rectangle or object ghost drawn in the 2D viewport for a plan step.
type PlanGhost struct {
	ID          string   `json:"id,omitempty"`
	Step        int      `json:"step,omitempty"`
	X           float32  `json:"x"`
	Y           float32  `json:"y"`
	Width       float32  `json:"width"`
	Height      float32  `json:"height"`
	Radius      float32  `json:"radius,omitempty"`
	Label       string   `json:"label,omitempty"`
	ActiveLabel string   `json:"activeLabel,omitempty"`
	LabelX      *float32 `json:"labelX,omitempty"`
	LabelY      *float32 `json:"labelY,omitempty"`
	// RepeatField optionally names a PlanStepField.Key on the owning step so changing a count stepper
	// updates how many ghost boxes are drawn across SpanWidth (or stepped by StepX / StaggerY).
	RepeatField string  `json:"repeatField,omitempty"`
	SpanWidth   float32 `json:"spanWidth,omitempty"`
	StepX       float32 `json:"stepX,omitempty"`
	StaggerY    float32 `json:"staggerY,omitempty"`
	// ObjectID optionally links this ghost to a planned or existing scene object whose manual edits pause while running.
	ObjectID string `json:"objectId,omitempty"`
}

// PlanTreeNode describes a planned node shown in the Scene Tree with a dashed marker and step badge,
// or an existing layer/object annotated with a step badge when Existing is true.
type PlanTreeNode struct {
	Name     string `json:"name"`
	Layer    string `json:"layer,omitempty"`
	Existing bool   `json:"existing,omitempty"`
	Step     int    `json:"step,omitempty"`
	ObjectID string `json:"objectId,omitempty"`
}

// PlanStep is one numbered step in an agent-proposed plan.
type PlanStep struct {
	Number           int              `json:"number"`
	ID               string           `json:"id,omitempty"`
	Title            string           `json:"title"`
	Files            []string         `json:"files,omitempty"`
	FilesLabel       string           `json:"filesLabel,omitempty"`
	Adds             string           `json:"adds,omitempty"`
	Objects          int              `json:"objects,omitempty"`
	Permission       string           `json:"permission,omitempty"`
	PermissionAction PermissionAction `json:"permissionAction,omitempty"`
	PermissionTarget string           `json:"permissionTarget,omitempty"`
	Fields           []PlanStepField  `json:"fields,omitempty"`
	Note             string           `json:"note,omitempty"`
	NotePlaceholder  string           `json:"notePlaceholder,omitempty"`
	Removed          bool             `json:"removed,omitempty"`
	Status           PlanStepStatus   `json:"status,omitempty"`
	Ghosts           []PlanGhost      `json:"ghosts,omitempty"`
	Nodes            []PlanTreeNode   `json:"nodes,omitempty"`
}

// AgentPlan is an agent-proposed plan shown in the Agent panel, Scene Tree, and 2D viewport before edits begin.
type AgentPlan struct {
	ID               string         `json:"id"`
	TurnID           string         `json:"turnId,omitempty"`
	Prompt           string         `json:"prompt,omitempty"`
	Title            string         `json:"title"`
	Summary          string         `json:"summary,omitempty"`
	Estimate         PlanEstimate   `json:"estimate,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	Questions        []PlanQuestion `json:"questions,omitempty"`
	Steps            []PlanStep     `json:"steps"`
	Files            []PlanFile     `json:"files,omitempty"`
	GhostsVisible    bool           `json:"ghostsVisible"`
	Phase            PlanPhase      `json:"phase"`
	ActiveStep       int            `json:"activeStep,omitempty"`
	EditingStep      int            `json:"editingStep,omitempty"`
	ReopenedQuestion string         `json:"reopenedQuestion,omitempty"`
	PickedForYou     bool           `json:"pickedForYou,omitempty"`
	NothingWritten   bool           `json:"nothingWritten,omitempty"`
	WrittenSteps     int            `json:"writtenSteps,omitempty"`
}

// Clone returns a deep copy of p.
func (p *AgentPlan) Clone() *AgentPlan {
	if p == nil {
		return nil
	}
	cp := *p
	if p.Questions != nil {
		cp.Questions = make([]PlanQuestion, len(p.Questions))
		for i, q := range p.Questions {
			qc := q
			if q.Options != nil {
				qc.Options = make([]PlanOption, len(q.Options))
				for j, opt := range q.Options {
					oc := opt
					if opt.Preview != nil {
						pr := *opt.Preview
						pr.Segments = slices.Clone(opt.Preview.Segments)
						oc.Preview = &pr
					}
					if opt.StepTitleOverrides != nil {
						oc.StepTitleOverrides = make(map[int]string, len(opt.StepTitleOverrides))
						for k, v := range opt.StepTitleOverrides {
							oc.StepTitleOverrides[k] = v
						}
					}
					if opt.GhostOverrides != nil {
						oc.GhostOverrides = make([]PlanGhostOverride, len(opt.GhostOverrides))
						for k, go_ := range opt.GhostOverrides {
							goc := go_
							goc.X = cloneFloat32Ptr(go_.X)
							goc.Y = cloneFloat32Ptr(go_.Y)
							goc.Width = cloneFloat32Ptr(go_.Width)
							goc.Height = cloneFloat32Ptr(go_.Height)
							goc.LabelX = cloneFloat32Ptr(go_.LabelX)
							goc.LabelY = cloneFloat32Ptr(go_.LabelY)
							goc.ReplaceWith = clonePlanGhosts(go_.ReplaceWith)
							oc.GhostOverrides[k] = goc
						}
					}
					qc.Options[j] = oc
				}
			}
			cp.Questions[i] = qc
		}
	}
	if p.Steps != nil {
		cp.Steps = make([]PlanStep, len(p.Steps))
		for i, st := range p.Steps {
			sc := st
			sc.Files = slices.Clone(st.Files)
			sc.Fields = slices.Clone(st.Fields)
			sc.Ghosts = clonePlanGhosts(st.Ghosts)
			sc.Nodes = slices.Clone(st.Nodes)
			cp.Steps[i] = sc
		}
	}
	cp.Files = slices.Clone(p.Files)
	return &cp
}

func clonePlanGhosts(in []PlanGhost) []PlanGhost {
	if in == nil {
		return nil
	}
	out := make([]PlanGhost, len(in))
	for i, g := range in {
		gc := g
		gc.LabelX = cloneFloat32Ptr(g.LabelX)
		gc.LabelY = cloneFloat32Ptr(g.LabelY)
		out[i] = gc
	}
	return out
}

// PlanAnswerDecision records the person's final answer to one clarifying question.
type PlanAnswerDecision struct {
	QuestionID   string `json:"questionId"`
	Label        string `json:"label,omitempty"`
	OptionID     string `json:"optionId"`
	OptionLabel  string `json:"optionLabel"`
	PickedForYou bool   `json:"pickedForYou,omitempty"`
}

// PlanStepDecision records the finalized state of one step returned to the agent after approval or stop.
type PlanStepDecision struct {
	Number     int            `json:"number"`
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Files      []string       `json:"files,omitempty"`
	Adds       string         `json:"adds,omitempty"`
	Objects    int            `json:"objects,omitempty"`
	Permission string         `json:"permission,omitempty"`
	Removed    bool           `json:"removed,omitempty"`
	Fields     map[string]int `json:"fields,omitempty"`
	Note       string         `json:"note,omitempty"`
	Status     PlanStepStatus `json:"status,omitempty"`
}

// AgentPlanDecision is the structured response returned by plan_propose once the person decides.
type AgentPlanDecision struct {
	PlanID         string               `json:"planId"`
	Decision       string               `json:"decision"` // "approved", "stopped", or "pending"
	PickedForYou   bool                 `json:"pickedForYou,omitempty"`
	NothingWritten bool                 `json:"nothingWritten,omitempty"`
	Summary        string               `json:"summary,omitempty"`
	Answers        []PlanAnswerDecision `json:"answers,omitempty"`
	Steps          []PlanStepDecision   `json:"steps,omitempty"`
	RemovedSteps   []int                `json:"removedSteps,omitempty"`
	ActiveFiles    []string             `json:"activeFiles,omitempty"`
}

// PlanReviewStep records one step of a plan inside an AgentTurn's review checklist.
type PlanReviewStep struct {
	Number  int            `json:"number"`
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Files   []string       `json:"files,omitempty"`
	Removed bool           `json:"removed,omitempty"`
	Status  PlanStepStatus `json:"status"`
	Note    string         `json:"note,omitempty"`
}

// AgentPlanReview is attached to an AgentTurn so the turn's review lists the plan's steps and marks which were done.
type AgentPlanReview struct {
	PlanID         string           `json:"planId"`
	Title          string           `json:"title"`
	Phase          PlanPhase        `json:"phase"`
	PickedForYou   bool             `json:"pickedForYou,omitempty"`
	NothingWritten bool             `json:"nothingWritten,omitempty"`
	Steps          []PlanReviewStep `json:"steps"`
}

// DoneCount returns how many non-removed steps in r are marked done and the total non-removed step count.
func (r *AgentPlanReview) DoneCount() (done, total int) {
	if r == nil {
		return 0, 0
	}
	for _, st := range r.Steps {
		if st.Removed {
			continue
		}
		total++
		if st.Status == PlanStepDone {
			done++
		}
	}
	return done, total
}

// ResolvedPlanGhost is a concrete ghost rectangle ready to draw in the 2D viewport.
type ResolvedPlanGhost struct {
	ID       string
	Step     int
	X        float32
	Y        float32
	Width    float32
	Height   float32
	Radius   float32
	Label    string
	LabelX   float32
	LabelY   float32
	HasLabel bool
	Active   bool
	ObjectID string
}

// ResolvedPlanTreeNode is a planned Scene Tree node after applying field interpolation and removed-step filtering.
type ResolvedPlanTreeNode struct {
	Name     string
	Layer    string
	Existing bool
	Step     int
	ObjectID string
}

// ResolvedPlanFile is one row in the "FILES IN THIS PLAN" panel, with InPlan false when every referencing step was removed.
type ResolvedPlanFile struct {
	Path   string
	Tag    PlanFileTag
	InPlan bool
}

// PlanPermissionSummary explains why a plan was requested based on the active permissions.yaml rules.
type PlanPermissionSummary struct {
	Needed     bool
	EditsGo    bool
	RunsGame   bool
	FileCount  int
	AskReasons []string
	Reason     string
}

type agentPlanState struct {
	mu       sync.Mutex
	seq      uint64
	planSeq  uint64
	active   *AgentPlan
	waiters  []chan AgentPlanDecision
	decided  bool
	lastDec  AgentPlanDecision
	noteRev  uint64
	ghostRev uint64
}

func (e *Editor) resetPlanForProject() {
	if e == nil {
		return
	}
	e.plan.mu.Lock()
	active := e.plan.active
	waiters := e.plan.waiters
	e.plan.active = nil
	e.plan.waiters = nil
	e.plan.decided = false
	e.plan.seq++
	e.plan.mu.Unlock()
	if active != nil && len(waiters) > 0 {
		dec := AgentPlanDecision{
			PlanID:         active.ID,
			Decision:       "stopped",
			NothingWritten: true,
			Summary:        "Stopped because the project changed.",
		}
		for _, ch := range waiters {
			select {
			case ch <- dec:
			default:
			}
			close(ch)
		}
	}
}

// PlanSeq returns a monotonically increasing sequence number for plan state changes.
func (e *Editor) PlanSeq() uint64 {
	if e == nil {
		return 0
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return e.plan.seq
}

// ActivePlan returns a deep copy of the currently active agent plan, or nil when none is active.
func (e *Editor) ActivePlan() *AgentPlan {
	if e == nil {
		return nil
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return e.plan.active.Clone()
}

// IsPlanRunning reports whether an approved plan is currently executing.
func (e *Editor) IsPlanRunning() bool {
	if e == nil {
		return false
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return e.plan.active != nil && e.plan.active.Phase == PlanPhaseRunning
}

// IsObjectPausedByPlan reports whether manual edits to scene object id are paused while the agent executes a plan.
func (e *Editor) IsObjectPausedByPlan(id string) bool {
	if e == nil || id == "" {
		return false
	}
	if e.AgentActorActive() {
		return false
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseRunning {
		return false
	}
	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		for _, g := range st.Ghosts {
			if g.ObjectID == id {
				return true
			}
		}
		for _, n := range st.Nodes {
			if n.ObjectID == id {
				return true
			}
		}
	}
	for _, q := range p.Questions {
		opt, ok := q.SelectedOption()
		if !ok {
			opt, ok = q.SuggestedOption()
		}
		if !ok {
			continue
		}
		for _, ov := range opt.GhostOverrides {
			st := findPlanStepPtr(p, ov.Step)
			if st == nil || st.Removed {
				continue
			}
			for _, g := range ov.ReplaceWith {
				if g.ObjectID == id {
					return true
				}
			}
		}
	}
	return false
}

func (e *Editor) checkObjectNotPausedByPlan(id string) error {
	if e.IsObjectPausedByPlan(id) {
		return fmt.Errorf("editor: manual edits to %q are paused while the agent runs its plan", id)
	}
	return nil
}

// PlanNeededForEdit evaluates files and actions against the project's active permissions.yaml rules
// to determine whether a request should propose a plan first and why.
func (e *Editor) PlanNeededForEdit(files []string, actions []PermissionAction) (bool, string) {
	p := &AgentPlan{
		Steps: []PlanStep{{
			Number: 1,
			Title:  "Edit",
			Files:  files,
		}},
	}
	for _, a := range actions {
		p.Steps = append(p.Steps, PlanStep{
			Number:           len(p.Steps) + 1,
			Title:            a.DisplayName(),
			PermissionAction: a,
		})
	}
	sum := e.EvaluatePlanPermissions(p)
	return sum.Needed, sum.Reason
}

// EvaluatePlanPermissions inspects plan's live steps against the editor's active permission settings
// and produces the human-readable footer reason (for example "Planned because it edits Go and 3 files").
func (e *Editor) EvaluatePlanPermissions(p *AgentPlan) PlanPermissionSummary {
	if p == nil {
		return PlanPermissionSummary{}
	}
	perms := DefaultPermissions()
	if e != nil {
		perms = e.Permissions()
	}

	fileSet := livePlanFilePaths(p)
	sum := PlanPermissionSummary{
		FileCount: len(fileSet),
	}

	var reasons []string
	editsGo := false
	runsGame := false
	asksOnFile := false

	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		for _, f := range st.Files {
			clean := strings.TrimSpace(f)
			if clean == "" {
				continue
			}
			if strings.HasSuffix(strings.ToLower(clean), ".go") {
				editsGo = true
			}
			act := PermissionActionEdit
			if strings.HasSuffix(strings.ToLower(clean), ".go") {
				act = PermissionActionCode
			}
			for _, pf := range p.Files {
				if pf.Path == clean && pf.Tag == PlanFileNew {
					act = PermissionActionCreate
					break
				}
			}
			ev := perms.Evaluate(PermissionRequest{Action: act, Target: clean})
			if ev.Decision == PermissionAsk || ev.Decision == PermissionDeny {
				asksOnFile = true
			}
		}
		if st.PermissionAction == PermissionActionCode || strings.Contains(strings.ToLower(st.Permission), "go") {
			editsGo = true
		}
		if st.PermissionAction == PermissionActionRun || strings.Contains(strings.ToLower(st.Permission), "run") {
			runsGame = true
			target := st.PermissionTarget
			if target == "" {
				target = PermissionRunPlaytests
			}
			ev := perms.Evaluate(PermissionRequest{Action: PermissionActionRun, Target: target})
			if ev.Decision == PermissionAsk || ev.Decision == PermissionDeny {
				sum.AskReasons = append(sum.AskReasons, "runs the game")
			}
		}
	}

	sum.EditsGo = editsGo
	sum.RunsGame = runsGame

	if editsGo {
		reasons = append(reasons, "edits Go")
	}
	if sum.FileCount >= 2 {
		reasons = append(reasons, fmt.Sprintf("%d files", sum.FileCount))
	} else if sum.FileCount == 1 && (asksOnFile || perms.Profile == PermissionProfileCautious) {
		reasons = append(reasons, "1 file")
	}

	if editsGo || asksOnFile || sum.FileCount >= 3 || perms.Profile == PermissionProfileCautious || len(sum.AskReasons) > 0 {
		sum.Needed = true
	}

	if strings.TrimSpace(p.Reason) != "" {
		sum.Reason = strings.TrimSpace(p.Reason)
		return sum
	}
	switch {
	case editsGo && sum.FileCount > 0:
		fileWord := "files"
		if sum.FileCount == 1 {
			fileWord = "file"
		}
		sum.Reason = fmt.Sprintf("Planned because it edits Go and %d %s", sum.FileCount, fileWord)
	case editsGo:
		sum.Reason = "Planned because it edits Go"
	case len(reasons) > 0:
		sum.Reason = "Planned because it touches " + strings.Join(reasons, " and ")
	case perms.Profile == PermissionProfileCautious:
		sum.Reason = "Planned because your Cautious permission rules ask before edits"
	default:
		sum.Reason = "Planned under your permission rules"
	}
	return sum
}

// ProposeAgentPlan stages a proposed plan in Draft phase without modifying any scene or file,
// switches the right rail to the Agent tab, and returns a channel that receives the person's decision
// (approved with answers/edited steps, or stopped).
func (e *Editor) ProposeAgentPlan(proposal AgentPlan) (*AgentPlan, <-chan AgentPlanDecision, error) {
	if e == nil {
		return nil, nil, errors.New("editor: nil editor")
	}
	staged := proposal.Clone()
	if err := normalizeAndValidatePlan(staged, e); err != nil {
		return nil, nil, err
	}

	// Cancel any previous unresolved waiters cleanly before installing the new plan.
	e.plan.mu.Lock()
	prev := e.plan.active
	prevWaiters := e.plan.waiters
	e.plan.planSeq++
	if staged.ID == "" {
		staged.ID = fmt.Sprintf("plan-%d", e.plan.planSeq)
	}
	staged.Phase = PlanPhaseDraft
	staged.GhostsVisible = true
	staged.NothingWritten = true
	staged.WrittenSteps = 0
	staged.ActiveStep = 0
	staged.EditingStep = 0
	staged.ReopenedQuestion = ""
	staged.PickedForYou = false

	ch := make(chan AgentPlanDecision, 1)
	stored := staged.Clone()
	e.plan.active = stored
	e.plan.waiters = []chan AgentPlanDecision{ch}
	e.plan.decided = false
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()

	if prev != nil && len(prevWaiters) > 0 {
		dec := AgentPlanDecision{
			PlanID:         prev.ID,
			Decision:       "stopped",
			NothingWritten: prev.NothingWritten,
			Summary:        "Replaced by a newer plan.",
		}
		for _, w := range prevWaiters {
			select {
			case w <- dec:
			default:
			}
			close(w)
		}
	}

	// Link to an existing turn or create a turn for this proposal so the review checklist has a home.
	turnID := stored.TurnID
	if turnID == "" && len(e.agent.turns) > 0 {
		last := &e.agent.turns[len(e.agent.turns)-1]
		if !last.Done && !last.External {
			turnID = last.ID
		}
	}
	if turnID == "" && strings.TrimSpace(stored.Prompt) != "" {
		now := e.nowTime()
		e.agent.turnSeq++
		e.agent.turnChangeSeq++
		turnID = fmt.Sprintf("turn-%d", e.agent.turnSeq)
		e.agent.turns = append(e.agent.turns, AgentTurn{
			ID:        turnID,
			Prompt:    strings.TrimSpace(stored.Prompt),
			Source:    "panel",
			Status:    "pending",
			CreatedAt: now,
			StartedAt: now,
		})
	}
	if turnID != "" {
		e.plan.mu.Lock()
		if e.plan.active != nil && e.plan.active.ID == stored.ID {
			e.plan.active.TurnID = turnID
		}
		e.plan.mu.Unlock()
		stored.TurnID = turnID
	}

	if e.agent.mode != AgentModeNone {
		e.agent.rightTab = RightTabAgent
	}
	e.agent.status = AgentStatusWaitingForYou
	e.agent.openRequest = &AgentOpenRequest{
		ID:       stored.ID,
		Title:    stored.Title,
		Body:     e.PlanSummaryLine(),
		OpenedAt: e.nowTime(),
	}
	e.updateAgentAnnouncement()
	e.notify()

	return stored.Clone(), ch, nil
}

func cleanPlanFilePath(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("editor: plan file path cannot be empty")
	}
	if len(trimmed) > maxPlanTextLen || strings.ContainsRune(trimmed, 0) {
		return "", fmt.Errorf("editor: invalid plan file path %q", raw)
	}
	slashed := strings.ReplaceAll(trimmed, `\`, "/")
	if path.IsAbs(slashed) || filepath.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" {
		return "", fmt.Errorf("editor: plan file path %q must be relative to the project root", raw)
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("editor: plan file path %q leaves the project root", raw)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") {
		return "", fmt.Errorf("editor: plan file path %q cannot target .git metadata", raw)
	}
	return clean, nil
}

func validPlanStepStatus(s PlanStepStatus) bool {
	switch s {
	case PlanStepPending, PlanStepRunning, PlanStepDone, PlanStepFailed, PlanStepSkipped:
		return true
	default:
		return false
	}
}

func validatePlanGhost(g *PlanGhost, stepNum int) error {
	if !isFiniteFloat32(g.X) || !isFiniteFloat32(g.Y) || !isFiniteFloat32(g.Width) || !isFiniteFloat32(g.Height) ||
		!isFiniteFloat32(g.Radius) || !isFiniteFloat32(g.SpanWidth) || !isFiniteFloat32(g.StepX) || !isFiniteFloat32(g.StaggerY) {
		return fmt.Errorf("editor: plan step %d ghost has non-finite geometry", stepNum)
	}
	if g.Width < 0 || g.Height < 0 || g.Radius < 0 {
		return fmt.Errorf("editor: plan step %d ghost has negative size", stepNum)
	}
	if g.LabelX != nil && !isFiniteFloat32(*g.LabelX) {
		return fmt.Errorf("editor: plan step %d ghost labelX must be finite", stepNum)
	}
	if g.LabelY != nil && !isFiniteFloat32(*g.LabelY) {
		return fmt.Errorf("editor: plan step %d ghost labelY must be finite", stepNum)
	}
	if len(g.Label) > maxPlanTextLen || len(g.ActiveLabel) > maxPlanTextLen {
		return fmt.Errorf("editor: plan step %d ghost label exceeds %d bytes", stepNum, maxPlanTextLen)
	}
	if g.Step <= 0 {
		g.Step = stepNum
	}
	return nil
}

func normalizeAndValidatePlan(p *AgentPlan, e *Editor) error {
	p.Title = strings.TrimSpace(p.Title)
	if p.Title == "" {
		return errors.New("editor: plan title is required")
	}
	if len(p.Title) > maxPlanTextLen || len(p.Prompt) > maxPlanTextLen || len(p.Summary) > maxPlanTextLen || len(p.Reason) > maxPlanTextLen {
		return fmt.Errorf("editor: plan text field exceeds %d bytes", maxPlanTextLen)
	}
	if len(p.Steps) == 0 {
		return errors.New("editor: plan must contain at least one step")
	}
	if len(p.Steps) > maxPlanSteps {
		return fmt.Errorf("editor: plan has %d steps (max %d)", len(p.Steps), maxPlanSteps)
	}
	if len(p.Questions) > maxPlanQuestions {
		return fmt.Errorf("editor: plan has %d questions (max %d)", len(p.Questions), maxPlanQuestions)
	}
	if len(p.Files) > maxPlanFiles {
		return fmt.Errorf("editor: plan has %d files (max %d)", len(p.Files), maxPlanFiles)
	}
	seenQ := map[string]bool{}
	for i := range p.Questions {
		q := &p.Questions[i]
		q.ID = strings.TrimSpace(q.ID)
		if q.ID == "" {
			q.ID = fmt.Sprintf("q%d", i+1)
		}
		if len(q.ID) > 128 {
			return fmt.Errorf("editor: plan question id %q is too long", q.ID)
		}
		if seenQ[q.ID] {
			return fmt.Errorf("editor: duplicate plan question id %q", q.ID)
		}
		seenQ[q.ID] = true
		q.Prompt = strings.TrimSpace(q.Prompt)
		if q.Prompt == "" {
			return fmt.Errorf("editor: plan question %q is missing prompt", q.ID)
		}
		if len(q.Prompt) > maxPlanTextLen || len(q.Label) > maxPlanTextLen {
			return fmt.Errorf("editor: plan question %q text exceeds %d bytes", q.ID, maxPlanTextLen)
		}
		if len(q.Options) == 0 {
			return fmt.Errorf("editor: plan question %q must have at least one option", q.ID)
		}
		if len(q.Options) > maxPlanOptionsPerQuestion {
			return fmt.Errorf("editor: plan question %q has %d options (max %d)", q.ID, len(q.Options), maxPlanOptionsPerQuestion)
		}
		seenOpt := map[string]bool{}
		for j := range q.Options {
			opt := &q.Options[j]
			opt.ID = strings.TrimSpace(opt.ID)
			if opt.ID == "" {
				opt.ID = fmt.Sprintf("opt%d", j+1)
			}
			if len(opt.ID) > 128 {
				return fmt.Errorf("editor: plan question %q option id %q is too long", q.ID, opt.ID)
			}
			if seenOpt[opt.ID] {
				return fmt.Errorf("editor: plan question %q has duplicate option id %q", q.ID, opt.ID)
			}
			seenOpt[opt.ID] = true
			opt.Label = strings.TrimSpace(opt.Label)
			if opt.Label == "" {
				return fmt.Errorf("editor: plan question %q option %q is missing label", q.ID, opt.ID)
			}
			if len(opt.Label) > maxPlanTextLen || len(opt.Note) > maxPlanTextLen {
				return fmt.Errorf("editor: plan question %q option %q text exceeds %d bytes", q.ID, opt.ID, maxPlanTextLen)
			}
			if opt.Preview != nil {
				if opt.Preview.Kind != "" && opt.Preview.Kind != "room" && opt.Preview.Kind != "strip" {
					return fmt.Errorf("editor: plan question %q option %q has invalid preview kind %q", q.ID, opt.ID, opt.Preview.Kind)
				}
				if !isFiniteFloat32(opt.Preview.RoomSplitPercent) || !isFiniteFloat32(opt.Preview.GatePercent) || !isFiniteFloat32(opt.Preview.LeverPercent) {
					return fmt.Errorf("editor: plan question %q option %q preview coordinates must be finite", q.ID, opt.ID)
				}
				if len(opt.Preview.Segments) > maxPlanPreviewSegments {
					return fmt.Errorf("editor: plan question %q option %q has too many preview segments (max %d)", q.ID, opt.ID, maxPlanPreviewSegments)
				}
				for _, seg := range opt.Preview.Segments {
					if !isFiniteFloat32(seg.From) || !isFiniteFloat32(seg.To) {
						return fmt.Errorf("editor: plan question %q option %q preview segment must be finite", q.ID, opt.ID)
					}
				}
			}
			if len(opt.GhostOverrides) > maxPlanGhostOverrides {
				return fmt.Errorf("editor: plan question %q option %q has too many ghost overrides (max %d)", q.ID, opt.ID, maxPlanGhostOverrides)
			}
			for oi := range opt.GhostOverrides {
				ov := &opt.GhostOverrides[oi]
				if (ov.X != nil && !isFiniteFloat32(*ov.X)) || (ov.Y != nil && !isFiniteFloat32(*ov.Y)) ||
					(ov.Width != nil && (!isFiniteFloat32(*ov.Width) || *ov.Width < 0)) ||
					(ov.Height != nil && (!isFiniteFloat32(*ov.Height) || *ov.Height < 0)) ||
					(ov.LabelX != nil && !isFiniteFloat32(*ov.LabelX)) || (ov.LabelY != nil && !isFiniteFloat32(*ov.LabelY)) {
					return fmt.Errorf("editor: plan question %q option %q ghost override has invalid geometry", q.ID, opt.ID)
				}
				if len(ov.ReplaceWith) > maxPlanGhostsPerStep {
					return fmt.Errorf("editor: plan question %q option %q ghost override has too many replacement ghosts (max %d)", q.ID, opt.ID, maxPlanGhostsPerStep)
				}
				for ri := range ov.ReplaceWith {
					if err := validatePlanGhost(&ov.ReplaceWith[ri], ov.Step); err != nil {
						return err
					}
				}
			}
		}
		if q.SelectedID != "" && !seenOpt[q.SelectedID] {
			return fmt.Errorf("editor: plan question %q selectedId %q does not match any option", q.ID, q.SelectedID)
		}
	}

	seenSteps := map[int]bool{}
	for i := range p.Steps {
		st := &p.Steps[i]
		if st.Number <= 0 {
			st.Number = i + 1
		}
		if seenSteps[st.Number] {
			return fmt.Errorf("editor: duplicate plan step number %d", st.Number)
		}
		seenSteps[st.Number] = true
		if strings.TrimSpace(st.ID) == "" {
			st.ID = fmt.Sprintf("step-%d", st.Number)
		}
		st.Title = strings.TrimSpace(st.Title)
		if st.Title == "" {
			return fmt.Errorf("editor: plan step %d is missing title", st.Number)
		}
		if len(st.Title) > maxPlanTextLen || len(st.Adds) > maxPlanTextLen || len(st.FilesLabel) > maxPlanTextLen || len(st.Note) > maxPlanNoteLen {
			return fmt.Errorf("editor: plan step %d text exceeds limit", st.Number)
		}
		if st.Status == "" {
			st.Status = PlanStepPending
		} else if !validPlanStepStatus(st.Status) {
			return fmt.Errorf("editor: plan step %d has invalid status %q", st.Number, st.Status)
		}
		if st.PermissionAction != "" && !st.PermissionAction.Valid() {
			return fmt.Errorf("editor: plan step %d has invalid permissionAction %q", st.Number, st.PermissionAction)
		}
		if len(st.Files) > maxPlanFiles {
			return fmt.Errorf("editor: plan step %d has %d files (max %d)", st.Number, len(st.Files), maxPlanFiles)
		}
		for fi := range st.Files {
			clean, err := cleanPlanFilePath(st.Files[fi])
			if err != nil {
				return err
			}
			if e != nil {
				if dom, ok := e.CheckYouOnlyTarget(clean); ok {
					return fmt.Errorf("editor: plan step %d cannot target You-only %s file %q", st.Number, dom.DisplayName(), clean)
				}
			}
			st.Files[fi] = clean
		}
		if len(st.Fields) > maxPlanFieldsPerStep {
			return fmt.Errorf("editor: plan step %d has %d fields (max %d)", st.Number, len(st.Fields), maxPlanFieldsPerStep)
		}
		for j := range st.Fields {
			f := &st.Fields[j]
			f.Key = strings.TrimSpace(f.Key)
			if f.Key == "" {
				return fmt.Errorf("editor: plan step %d field %d is missing key", st.Number, j+1)
			}
			if f.Label == "" {
				f.Label = f.Key
			}
			if f.Step <= 0 {
				f.Step = 1
			}
			if f.Max < f.Min {
				f.Max = f.Min
			}
			if f.Value < f.Min {
				f.Value = f.Min
			}
			if f.Value > f.Max {
				f.Value = f.Max
			}
			if f.Hint == "" {
				f.Hint = fmt.Sprintf("%d to %d", f.Min, f.Max)
			}
		}
		if len(st.Ghosts) > maxPlanGhostsPerStep {
			return fmt.Errorf("editor: plan step %d has %d ghosts (max %d)", st.Number, len(st.Ghosts), maxPlanGhostsPerStep)
		}
		for j := range st.Ghosts {
			if err := validatePlanGhost(&st.Ghosts[j], st.Number); err != nil {
				return err
			}
		}
		if len(st.Nodes) > maxPlanNodesPerStep {
			return fmt.Errorf("editor: plan step %d has %d nodes (max %d)", st.Number, len(st.Nodes), maxPlanNodesPerStep)
		}
		for j := range st.Nodes {
			if len(st.Nodes[j].Name) > maxPlanTextLen {
				return fmt.Errorf("editor: plan step %d node name exceeds %d bytes", st.Number, maxPlanTextLen)
			}
			if st.Nodes[j].Step <= 0 {
				st.Nodes[j].Step = st.Number
			}
		}
		// Auto-infer permission badge if not explicitly provided when a step edits Go or runs the game.
		if st.Permission == "" && e != nil {
			perms := e.Permissions()
			for _, f := range st.Files {
				if strings.HasSuffix(strings.ToLower(strings.TrimSpace(f)), ".go") {
					ev := perms.Evaluate(PermissionRequest{Action: PermissionActionCode, Target: f})
					if ev.Decision != PermissionAllow {
						st.Permission = "Edit Go"
						if st.PermissionAction == "" {
							st.PermissionAction = PermissionActionCode
						}
						break
					}
				}
			}
		}
	}

	// Build complete file list from explicit Files plus any files referenced by steps.
	seenFiles := map[string]bool{}
	validFiles := make([]PlanFile, 0, len(p.Files))
	for i := range p.Files {
		clean, err := cleanPlanFilePath(p.Files[i].Path)
		if err != nil {
			return err
		}
		if e != nil {
			if dom, ok := e.CheckYouOnlyTarget(clean); ok {
				return fmt.Errorf("editor: plan cannot target You-only %s file %q", dom.DisplayName(), clean)
			}
		}
		tag := p.Files[i].Tag
		if tag == "" {
			tag = PlanFileEdit
		}
		if tag != PlanFileEdit && tag != PlanFileNew && tag != PlanFileDelete {
			return fmt.Errorf("editor: plan file %q has invalid tag %q", clean, tag)
		}
		if seenFiles[clean] {
			continue
		}
		seenFiles[clean] = true
		validFiles = append(validFiles, PlanFile{Path: clean, Tag: tag})
	}
	p.Files = validFiles
	for _, st := range p.Steps {
		for _, f := range st.Files {
			if seenFiles[f] {
				continue
			}
			if len(p.Files) >= maxPlanFiles {
				return fmt.Errorf("editor: plan touches more than %d files", maxPlanFiles)
			}
			seenFiles[f] = true
			p.Files = append(p.Files, PlanFile{Path: f, Tag: PlanFileEdit})
		}
	}
	return nil
}

// AnswerPlanQuestion selects optionID for questionID in the active draft plan.
// It collapses the answered question row and immediately updates planned ghosts and step titles.
func (e *Editor) AnswerPlanQuestion(questionID, optionID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil {
		e.plan.mu.Unlock()
		return errors.New("editor: no active plan")
	}
	if p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: plan answers can only be changed while in draft")
	}
	foundQ := false
	for i := range p.Questions {
		if p.Questions[i].ID == questionID {
			foundQ = true
			validOpt := false
			for _, opt := range p.Questions[i].Options {
				if opt.ID == optionID {
					validOpt = true
					break
				}
			}
			if !validOpt {
				e.plan.mu.Unlock()
				return fmt.Errorf("editor: unknown option %q for question %q", optionID, questionID)
			}
			p.Questions[i].SelectedID = optionID
			if p.ReopenedQuestion == questionID {
				p.ReopenedQuestion = ""
			}
			break
		}
	}
	if !foundQ {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan question %q", questionID)
	}
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()
	e.notify()
	return nil
}

// ReopenPlanQuestion expands an already-answered question so the person can change their pick.
func (e *Editor) ReopenPlanQuestion(questionID string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	for _, q := range p.Questions {
		if q.ID == questionID {
			p.ReopenedQuestion = questionID
			e.plan.seq++
			e.plan.mu.Unlock()
			e.notify()
			return nil
		}
	}
	e.plan.mu.Unlock()
	return fmt.Errorf("editor: unknown plan question %q", questionID)
}

// EditPlanStep opens (or toggles closed) the inline editor for stepNumber (1-based). Passing 0 closes the editor.
func (e *Editor) EditPlanStep(stepNumber int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	if stepNumber <= 0 {
		if p.EditingStep == 0 {
			e.plan.mu.Unlock()
			return nil
		}
		p.EditingStep = 0
		e.plan.seq++
		e.plan.mu.Unlock()
		e.notify()
		return nil
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil || st.Removed {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: plan step %d is not editable", stepNumber)
	}
	if p.EditingStep == stepNumber {
		p.EditingStep = 0
	} else {
		p.EditingStep = stepNumber
	}
	e.plan.seq++
	e.plan.mu.Unlock()
	e.notify()
	return nil
}

// SetPlanStepField updates numeric fieldKey on stepNumber, clamping value to [Min, Max].
func (e *Editor) SetPlanStepField(stepNumber int, fieldKey string, value int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	for i := range st.Fields {
		if st.Fields[i].Key == fieldKey {
			clamped := max(st.Fields[i].Min, min(st.Fields[i].Max, value))
			if st.Fields[i].Value == clamped {
				e.plan.mu.Unlock()
				return nil
			}
			st.Fields[i].Value = clamped
			e.plan.seq++
			e.plan.ghostRev++
			e.plan.mu.Unlock()
			e.notify()
			return nil
		}
	}
	e.plan.mu.Unlock()
	return fmt.Errorf("editor: step %d has no field %q", stepNumber, fieldKey)
}

// AdjustPlanStepField steps fieldKey on stepNumber by delta*Step, clamping to [Min, Max].
func (e *Editor) AdjustPlanStepField(stepNumber int, fieldKey string, delta int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	for i := range st.Fields {
		if st.Fields[i].Key == fieldKey {
			step := st.Fields[i].Step
			if step <= 0 {
				step = 1
			}
			next := max(st.Fields[i].Min, min(st.Fields[i].Max, st.Fields[i].Value+delta*step))
			if st.Fields[i].Value == next {
				e.plan.mu.Unlock()
				return nil
			}
			st.Fields[i].Value = next
			e.plan.seq++
			e.plan.ghostRev++
			e.plan.mu.Unlock()
			e.notify()
			return nil
		}
	}
	e.plan.mu.Unlock()
	return fmt.Errorf("editor: step %d has no field %q", stepNumber, fieldKey)
}

// SetPlanStepNote stores a free-text note telling the agent how to change stepNumber.
func (e *Editor) SetPlanStepNote(stepNumber int, note string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if len(note) > maxPlanNoteLen {
		return fmt.Errorf("editor: plan step note exceeds %d bytes", maxPlanNoteLen)
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	if st.Note == note {
		e.plan.mu.Unlock()
		return nil
	}
	st.Note = note
	e.plan.noteRev++
	e.plan.mu.Unlock()
	e.notify()
	return nil
}

// RemovePlanStep marks stepNumber as removed in Draft phase, hiding its ghosts and planned nodes
// and striking out any files exclusive to that step.
func (e *Editor) RemovePlanStep(stepNumber int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	if st.Removed {
		e.plan.mu.Unlock()
		return nil
	}
	st.Removed = true
	if p.EditingStep == stepNumber {
		p.EditingStep = 0
	}
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()
	e.notify()
	return nil
}

// RestorePlanStep restores a previously removed step in Draft phase.
func (e *Editor) RestorePlanStep(stepNumber int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseDraft {
		e.plan.mu.Unlock()
		return errors.New("editor: no draft plan")
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	if !st.Removed {
		e.plan.mu.Unlock()
		return nil
	}
	st.Removed = false
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()
	e.notify()
	return nil
}

// PlanGhostsVisible reports whether planned viewport ghosts are currently toggled on.
func (e *Editor) PlanGhostsVisible() bool {
	if e == nil {
		return false
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return e.plan.active != nil && e.plan.active.GhostsVisible
}

// SetPlanGhostsVisible turns planned viewport ghosts on or off without altering the plan or scene.
func (e *Editor) SetPlanGhostsVisible(visible bool) {
	if e == nil {
		return
	}
	e.plan.mu.Lock()
	if e.plan.active == nil || e.plan.active.GhostsVisible == visible {
		e.plan.mu.Unlock()
		return
	}
	e.plan.active.GhostsVisible = visible
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()
	e.notify()
}

// TogglePlanGhosts flips the viewport's "Plan ghosts" toggle.
func (e *Editor) TogglePlanGhosts() {
	if e == nil {
		return
	}
	e.SetPlanGhostsVisible(!e.PlanGhostsVisible())
}

// UnansweredPlanQuestions returns how many clarifying questions in the active plan have no selected option yet.
func (e *Editor) UnansweredPlanQuestions() int {
	if e == nil {
		return 0
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return unansweredPlanQuestions(e.plan.active)
}

func unansweredPlanQuestions(p *AgentPlan) int {
	if p == nil {
		return 0
	}
	n := 0
	for _, q := range p.Questions {
		if q.SelectedID == "" {
			n++
		}
	}
	return n
}

// CanApprovePlan reports whether Approve is currently enabled and why not when disabled.
func (e *Editor) CanApprovePlan() (bool, string) {
	if e == nil {
		return false, "No active plan"
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return canApprovePlan(e.plan.active)
}

func canApprovePlan(p *AgentPlan) (bool, string) {
	if p == nil {
		return false, "No active plan"
	}
	if p.Phase != PlanPhaseDraft && p.Phase != PlanPhaseStopped {
		return false, "Plan is not waiting for approval"
	}
	if u := unansweredPlanQuestions(p); u > 0 {
		return false, "Answer the questions, or choose Just do it"
	}
	if livePlanStepCount(p) == 0 {
		return false, "Restore at least one step to approve"
	}
	return true, "Approve and start"
}

// ApproveAgentPlan approves the active draft (or stopped) plan once all clarifying questions are answered,
// transitioning the plan to Running and delivering the decision to the waiting plan_propose MCP call.
// It refuses with an error if any question is still unanswered or if all steps were removed.
func (e *Editor) ApproveAgentPlan() (AgentPlanDecision, error) {
	if e == nil {
		return AgentPlanDecision{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return AgentPlanDecision{}, ErrPlaying
	}
	if e.HistoryPreviewActive() {
		e.ExitHistoryPreview()
	}
	e.plan.mu.Lock()
	p := e.plan.active
	ok, reason := canApprovePlan(p)
	if !ok {
		e.plan.mu.Unlock()
		return AgentPlanDecision{}, fmt.Errorf("editor: cannot approve plan: %s", reason)
	}
	dec, waiters := e.startPlanLocked(p, false)
	cp := p.Clone()
	e.plan.mu.Unlock()

	e.onPlanStartedUI(cp, dec, waiters)
	return dec, nil
}

// JustDoItAgentPlan picks the suggested option for every unanswered clarifying question,
// records that the agent picked them ("Picked for you: ..."), and starts executing the plan.
func (e *Editor) JustDoItAgentPlan() (AgentPlanDecision, error) {
	if e == nil {
		return AgentPlanDecision{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return AgentPlanDecision{}, ErrPlaying
	}
	if e.HistoryPreviewActive() {
		e.ExitHistoryPreview()
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || (p.Phase != PlanPhaseDraft && p.Phase != PlanPhaseStopped) {
		e.plan.mu.Unlock()
		return AgentPlanDecision{}, errors.New("editor: no draft plan to run")
	}
	if livePlanStepCount(p) == 0 {
		e.plan.mu.Unlock()
		return AgentPlanDecision{}, errors.New("editor: restore at least one step before running")
	}
	pickedAny := false
	for i := range p.Questions {
		if p.Questions[i].SelectedID == "" {
			if opt, ok := p.Questions[i].SuggestedOption(); ok {
				p.Questions[i].SelectedID = opt.ID
				pickedAny = true
			}
		}
	}
	dec, waiters := e.startPlanLocked(p, pickedAny)
	cp := p.Clone()
	e.plan.mu.Unlock()

	e.onPlanStartedUI(cp, dec, waiters)
	return dec, nil
}

func (e *Editor) startPlanLocked(p *AgentPlan, pickedForYou bool) (AgentPlanDecision, []chan AgentPlanDecision) {
	p.Phase = PlanPhaseRunning
	p.EditingStep = 0
	p.ReopenedQuestion = ""
	if pickedForYou {
		p.PickedForYou = true
	}
	firstLive := 0
	for i := range p.Steps {
		if p.Steps[i].Removed {
			p.Steps[i].Status = PlanStepSkipped
			continue
		}
		if firstLive == 0 {
			firstLive = p.Steps[i].Number
			p.Steps[i].Status = PlanStepRunning
		} else if p.Steps[i].Status != PlanStepDone {
			p.Steps[i].Status = PlanStepPending
		}
	}
	p.ActiveStep = firstLive
	dec := buildPlanDecisionLocked(p, "approved")
	waiters := e.plan.waiters
	e.plan.waiters = nil
	e.plan.decided = true
	e.plan.lastDec = dec
	e.plan.seq++
	e.plan.ghostRev++
	return dec, waiters
}

func (e *Editor) onPlanStartedUI(p *AgentPlan, dec AgentPlanDecision, waiters []chan AgentPlanDecision) {
	liveTotal := livePlanStepCount(p)
	e.agent.openRequest = nil
	e.agent.status = AgentStatusWorking
	e.agent.workingStep = 1
	e.agent.workingTotal = liveTotal
	e.syncPlanToTurn(p)
	e.updateAgentAnnouncement()
	e.notify()

	deliverPlanDecision(waiters, dec)
	if e.OnPlanDecision != nil {
		e.OnPlanDecision(dec)
	}
}

// StopAgentPlan stops the active draft or running plan. When stopped before step 1 writes anything,
// NothingWritten remains true, every file stays unchanged, and Edit plan / Run again are offered.
func (e *Editor) StopAgentPlan() (AgentPlanDecision, error) {
	if e == nil {
		return AgentPlanDecision{}, errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil {
		e.plan.mu.Unlock()
		return AgentPlanDecision{}, errors.New("editor: no active plan")
	}
	p.Phase = PlanPhaseStopped
	p.EditingStep = 0
	p.ReopenedQuestion = ""
	p.ActiveStep = 0
	doneSteps := 0
	for i := range p.Steps {
		if p.Steps[i].Status == PlanStepDone {
			doneSteps++
		} else if p.Steps[i].Status == PlanStepRunning {
			p.Steps[i].Status = PlanStepPending
		}
	}
	p.WrittenSteps = doneSteps
	p.NothingWritten = doneSteps == 0
	dec := buildPlanDecisionLocked(p, "stopped")
	waiters := e.plan.waiters
	e.plan.waiters = nil
	e.plan.decided = true
	e.plan.lastDec = dec
	e.plan.seq++
	e.plan.ghostRev++
	cp := p.Clone()
	e.plan.mu.Unlock()

	e.agent.openRequest = nil
	e.syncPlanToTurn(cp)
	e.restoreIdleAgentStatus()
	e.notify()

	deliverPlanDecision(waiters, dec)
	if e.OnPlanDecision != nil {
		e.OnPlanDecision(dec)
	}
	return dec, nil
}

// EditStoppedPlan returns a stopped plan to Draft phase so the person can revise answers or steps.
func (e *Editor) EditStoppedPlan() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil || p.Phase != PlanPhaseStopped {
		e.plan.mu.Unlock()
		return errors.New("editor: no stopped plan to edit")
	}
	p.Phase = PlanPhaseDraft
	p.PickedForYou = false
	p.ActiveStep = 0
	e.plan.seq++
	e.plan.ghostRev++
	title := p.Title
	id := p.ID
	cp := p.Clone()
	e.plan.mu.Unlock()

	e.agent.status = AgentStatusWaitingForYou
	e.agent.openRequest = &AgentOpenRequest{
		ID:       id,
		Title:    title,
		Body:     e.PlanSummaryLine(),
		OpenedAt: e.nowTime(),
	}
	e.syncPlanToTurn(cp)
	e.updateAgentAnnouncement()
	e.notify()
	return nil
}

// RunStoppedPlanAgain restarts a stopped plan (same validation as ApproveAgentPlan).
func (e *Editor) RunStoppedPlanAgain() (AgentPlanDecision, error) {
	return e.ApproveAgentPlan()
}

// UpdateAgentPlanStep updates stepNumber's execution status while a plan is running,
// advancing ActiveStep to the next pending step and syncing the turn's review checklist.
func (e *Editor) UpdateAgentPlanStep(stepNumber int, status PlanStepStatus) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if !validPlanStepStatus(status) {
		return fmt.Errorf("editor: invalid plan step status %q", status)
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil {
		e.plan.mu.Unlock()
		return errors.New("editor: no active plan")
	}
	if p.Phase == PlanPhaseStopped {
		e.plan.mu.Unlock()
		return errors.New("editor: plan was stopped")
	}
	if p.Phase != PlanPhaseRunning {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: plan is not running (phase: %s)", p.Phase)
	}
	st := findPlanStepPtr(p, stepNumber)
	if st == nil {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: unknown plan step %d", stepNumber)
	}
	if st.Removed && status != PlanStepSkipped {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: plan step %d was removed and cannot be marked %s", stepNumber, status)
	}
	st.Status = status
	// Recompute WrittenSteps accurately (idempotent across repeated updates).
	doneSteps := 0
	for i := range p.Steps {
		if !p.Steps[i].Removed && p.Steps[i].Status == PlanStepDone {
			doneSteps++
		}
	}
	p.WrittenSteps = doneSteps
	if doneSteps > 0 {
		p.NothingWritten = false
	}
	// Advance ActiveStep, preferring an explicitly-started running step if one was just set.
	nextActive := 0
	liveTotal := 0
	if status == PlanStepRunning && !st.Removed {
		nextActive = st.Number
	}
	for i := range p.Steps {
		if p.Steps[i].Removed {
			continue
		}
		liveTotal++
		if p.Steps[i].Status == PlanStepRunning && nextActive == 0 {
			nextActive = p.Steps[i].Number
		}
	}
	if nextActive == 0 {
		for i := range p.Steps {
			if p.Steps[i].Removed {
				continue
			}
			if p.Steps[i].Status == PlanStepPending {
				p.Steps[i].Status = PlanStepRunning
				nextActive = p.Steps[i].Number
				break
			}
		}
	}
	p.ActiveStep = nextActive
	liveIdx := 0
	if nextActive > 0 {
		liveIdx = liveStepPosition(p, nextActive)
	}
	e.plan.seq++
	e.plan.ghostRev++
	cp := p.Clone()
	e.plan.mu.Unlock()

	if liveIdx > 0 {
		e.agent.workingStep = liveIdx
		e.agent.workingTotal = liveTotal
	}
	e.syncPlanToTurn(cp)
	e.updateAgentAnnouncement()
	e.notify()
	return nil
}

// CompleteAgentPlan finishes the active plan, attaches its step checklist to the linked AgentTurn's review,
// and restores the idle agent status.
func (e *Editor) CompleteAgentPlan() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.plan.mu.Lock()
	p := e.plan.active
	if p == nil {
		e.plan.mu.Unlock()
		return errors.New("editor: no active plan")
	}
	if p.Phase != PlanPhaseRunning {
		e.plan.mu.Unlock()
		return fmt.Errorf("editor: cannot complete plan in phase %q", p.Phase)
	}
	p.Phase = PlanPhaseDone
	p.ActiveStep = 0
	doneSteps := 0
	for i := range p.Steps {
		if p.Steps[i].Removed {
			p.Steps[i].Status = PlanStepSkipped
			continue
		}
		if p.Steps[i].Status == PlanStepRunning {
			p.Steps[i].Status = PlanStepDone
		}
		if p.Steps[i].Status == PlanStepDone {
			doneSteps++
		}
	}
	p.WrittenSteps = doneSteps
	if doneSteps > 0 {
		p.NothingWritten = false
	}
	cp := p.Clone()
	e.plan.active = nil
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()

	e.agent.openRequest = nil
	e.syncPlanToTurn(cp)
	e.restoreIdleAgentStatus()
	e.notify()
	return nil
}

// DismissAgentPlan clears any active plan without writing any files.
func (e *Editor) DismissAgentPlan() {
	if e == nil {
		return
	}
	e.plan.mu.Lock()
	p := e.plan.active
	waiters := e.plan.waiters
	var dec AgentPlanDecision
	if p != nil && len(waiters) > 0 {
		dec = buildPlanDecisionLocked(p, "stopped")
	}
	e.plan.active = nil
	e.plan.waiters = nil
	e.plan.seq++
	e.plan.ghostRev++
	e.plan.mu.Unlock()

	if p != nil && len(waiters) > 0 {
		deliverPlanDecision(waiters, dec)
	}
	e.agent.openRequest = nil
	e.restoreIdleAgentStatus()
	e.notify()
}

func (e *Editor) syncPlanToTurn(p *AgentPlan) {
	if e == nil || p == nil {
		return
	}
	review := buildPlanReview(p)
	var turn *AgentTurn
	if p.TurnID != "" {
		turn = e.findAgentTurn(p.TurnID)
	}
	if turn == nil && len(e.agent.turns) > 0 {
		turn = &e.agent.turns[len(e.agent.turns)-1]
	}
	if turn == nil {
		now := e.nowTime()
		e.agent.turnSeq++
		id := fmt.Sprintf("turn-%d", e.agent.turnSeq)
		prompt := strings.TrimSpace(p.Prompt)
		if prompt == "" {
			prompt = p.Title
		}
		e.agent.turns = append(e.agent.turns, AgentTurn{
			ID:        id,
			Prompt:    prompt,
			Source:    "panel",
			Status:    string(p.Phase),
			Done:      p.Phase == PlanPhaseDone,
			CreatedAt: now,
			StartedAt: now,
			Plan:      review,
		})
		e.plan.mu.Lock()
		if e.plan.active != nil && e.plan.active.ID == p.ID && e.plan.active.TurnID == "" {
			e.plan.active.TurnID = id
		}
		e.plan.mu.Unlock()
		e.agent.turnChangeSeq++
		return
	}
	turn.Plan = review
	switch p.Phase {
	case PlanPhaseDraft:
		turn.Status = "pending"
		turn.Stopped = false
	case PlanPhaseRunning:
		turn.Status = "running"
		turn.Stopped = false
	case PlanPhaseDone:
		turn.Status = "done"
		turn.Done = true
		turn.Stopped = false
		turn.FinishedAt = e.nowTime()
	case PlanPhaseStopped:
		turn.Status = "stopped"
		turn.Stopped = !p.NothingWritten
	}
	e.agent.turnChangeSeq++
}

func buildPlanReview(p *AgentPlan) *AgentPlanReview {
	if p == nil {
		return nil
	}
	steps := make([]PlanReviewStep, len(p.Steps))
	for i, st := range p.Steps {
		status := st.Status
		if st.Removed {
			status = PlanStepSkipped
		} else if status == "" {
			status = PlanStepPending
		}
		steps[i] = PlanReviewStep{
			Number:  st.Number,
			ID:      st.ID,
			Title:   resolvedPlanStepTitle(p, st),
			Files:   slices.Clone(st.Files),
			Removed: st.Removed,
			Status:  status,
			Note:    st.Note,
		}
	}
	return &AgentPlanReview{
		PlanID:         p.ID,
		Title:          p.Title,
		Phase:          p.Phase,
		PickedForYou:   p.PickedForYou,
		NothingWritten: p.NothingWritten,
		Steps:          steps,
	}
}

func deliverPlanDecision(waiters []chan AgentPlanDecision, dec AgentPlanDecision) {
	for _, ch := range waiters {
		select {
		case ch <- dec:
		default:
		}
		close(ch)
	}
}

func buildPlanDecisionLocked(p *AgentPlan, decision string) AgentPlanDecision {
	answers := make([]PlanAnswerDecision, 0, len(p.Questions))
	for _, q := range p.Questions {
		opt, ok := q.SelectedOption()
		if !ok {
			continue
		}
		answers = append(answers, PlanAnswerDecision{
			QuestionID:   q.ID,
			Label:        questionShortLabel(q),
			OptionID:     opt.ID,
			OptionLabel:  opt.Label,
			PickedForYou: p.PickedForYou,
		})
	}
	steps := make([]PlanStepDecision, len(p.Steps))
	var removed []int
	for i, st := range p.Steps {
		if st.Removed {
			removed = append(removed, st.Number)
		}
		var fields map[string]int
		if len(st.Fields) > 0 {
			fields = make(map[string]int, len(st.Fields))
			for _, f := range st.Fields {
				fields[f.Key] = f.Value
			}
		}
		steps[i] = PlanStepDecision{
			Number:     st.Number,
			ID:         st.ID,
			Title:      resolvedPlanStepTitle(p, st),
			Files:      slices.Clone(st.Files),
			Adds:       resolvedPlanStepAdds(p, st),
			Objects:    resolvedPlanStepObjects(st),
			Permission: st.Permission,
			Removed:    st.Removed,
			Fields:     fields,
			Note:       st.Note,
			Status:     st.Status,
		}
	}
	return AgentPlanDecision{
		PlanID:         p.ID,
		Decision:       decision,
		PickedForYou:   p.PickedForYou,
		NothingWritten: p.NothingWritten,
		Summary:        formatPlanSummary(p),
		Answers:        answers,
		Steps:          steps,
		RemovedSteps:   removed,
		ActiveFiles:    livePlanFilePaths(p),
	}
}

func findPlanStepPtr(p *AgentPlan, stepNumber int) *PlanStep {
	if p == nil {
		return nil
	}
	for i := range p.Steps {
		if p.Steps[i].Number == stepNumber {
			return &p.Steps[i]
		}
	}
	return nil
}

func livePlanStepCount(p *AgentPlan) int {
	if p == nil {
		return 0
	}
	n := 0
	for _, st := range p.Steps {
		if !st.Removed {
			n++
		}
	}
	return n
}

func liveStepPosition(p *AgentPlan, stepNumber int) int {
	if p == nil || stepNumber <= 0 {
		return 0
	}
	pos := 0
	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		pos++
		if st.Number == stepNumber {
			return pos
		}
	}
	return 0
}

func livePlanFilePaths(p *AgentPlan) []string {
	if p == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		for _, f := range st.Files {
			clean := strings.TrimSpace(f)
			if clean == "" || seen[clean] {
				continue
			}
			seen[clean] = true
			out = append(out, clean)
		}
	}
	return out
}

func questionShortLabel(q PlanQuestion) string {
	if strings.TrimSpace(q.Label) != "" {
		return strings.TrimSpace(q.Label)
	}
	return q.ID
}

func resolvedPlanStepObjects(st PlanStep) int {
	hasCountField := false
	sum := 0
	for _, f := range st.Fields {
		if f.CountsObjects {
			hasCountField = true
			sum += f.Value
		}
	}
	if hasCountField {
		return sum
	}
	return st.Objects
}

func interpolatePlanText(raw string, p *AgentPlan, st *PlanStep) string {
	if !strings.Contains(raw, "{") {
		return raw
	}
	out := raw
	if st != nil {
		for _, f := range st.Fields {
			out = strings.ReplaceAll(out, "{"+f.Key+"}", strconv.Itoa(f.Value))
			out = strings.ReplaceAll(out, "{"+f.Key+"+2:02d}", fmt.Sprintf("%02d", f.Value+2))
		}
		out = strings.ReplaceAll(out, "{objects}", strconv.Itoa(resolvedPlanStepObjects(*st)))
	}
	if p != nil {
		for _, q := range p.Questions {
			opt, ok := q.SelectedOption()
			if !ok {
				opt, ok = q.SuggestedOption()
			}
			if ok {
				out = strings.ReplaceAll(out, "{answer."+q.ID+"}", opt.Label)
				out = strings.ReplaceAll(out, "{answer."+q.ID+".lower}", strings.ToLower(opt.Label))
			}
		}
	}
	return out
}

func resolvedPlanStepTitle(p *AgentPlan, st PlanStep) string {
	title := st.Title
	if p != nil {
		for _, q := range p.Questions {
			opt, ok := q.SelectedOption()
			if !ok {
				continue
			}
			if override, exists := opt.StepTitleOverrides[st.Number]; exists && strings.TrimSpace(override) != "" {
				title = override
			}
		}
	}
	return interpolatePlanText(title, p, &st)
}

func resolvedPlanStepAdds(p *AgentPlan, st PlanStep) string {
	if strings.TrimSpace(st.Adds) != "" {
		return interpolatePlanText(st.Adds, p, &st)
	}
	if objs := resolvedPlanStepObjects(st); objs > 0 {
		if objs == 1 {
			return "1 object"
		}
		return fmt.Sprintf("%d objects", objs)
	}
	return ""
}

func resolvedPlanStepFilesLabel(st PlanStep) string {
	if strings.TrimSpace(st.FilesLabel) != "" {
		return st.FilesLabel
	}
	if len(st.Files) == 0 {
		return "no files"
	}
	names := make([]string, 0, len(st.Files))
	for _, f := range st.Files {
		names = append(names, path.Base(strings.TrimSpace(f)))
	}
	return strings.Join(names, " · ")
}

func formatPlanSummary(p *AgentPlan) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.Summary) != "" && livePlanStepCount(p) == len(p.Steps) {
		return p.Summary
	}
	liveSteps := livePlanStepCount(p)
	files := livePlanFilePaths(p)
	objects := 0
	for _, st := range p.Steps {
		if !st.Removed {
			objects += resolvedPlanStepObjects(st)
		}
	}
	stepWord := "steps"
	if liveSteps == 1 {
		stepWord = "step"
	}
	fileWord := "files"
	if len(files) == 1 {
		fileWord = "file"
	}
	objWord := "objects"
	if objects == 1 {
		objWord = "object"
	}
	parts := []string{
		fmt.Sprintf("%d %s", liveSteps, stepWord),
		fmt.Sprintf("%d %s", len(files), fileWord),
		fmt.Sprintf("%d %s", objects, objWord),
	}
	if strings.TrimSpace(p.Estimate.Duration) != "" {
		parts = append(parts, strings.TrimSpace(p.Estimate.Duration))
	}
	if strings.TrimSpace(p.Estimate.Tokens) != "" {
		parts = append(parts, strings.TrimSpace(p.Estimate.Tokens))
	}
	return strings.Join(parts, " · ")
}

// PlanSummaryLine returns the live plan card summary (for example "5 steps · 3 files · 10 objects · about 2 min · ~35k tokens").
func (e *Editor) PlanSummaryLine() string {
	if e == nil {
		return ""
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return formatPlanSummary(e.plan.active)
}

// PlanStatusBarLine returns the compact status-bar summary for the active plan (for example "Plan · 5 steps · 10 objects").
func (e *Editor) PlanStatusBarLine() string {
	if e == nil {
		return ""
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil {
		return ""
	}
	live := livePlanStepCount(p)
	objects := 0
	for _, st := range p.Steps {
		if !st.Removed {
			objects += resolvedPlanStepObjects(st)
		}
	}
	stepWord := "steps"
	if live == 1 {
		stepWord = "step"
	}
	objWord := "objects"
	if objects == 1 {
		objWord = "object"
	}
	return fmt.Sprintf("Plan · %d %s · %d %s", live, stepWord, objects, objWord)
}

// PlanHeaderPillText returns the top header pill label for the active plan phase.
func (e *Editor) PlanHeaderPillText() string {
	if e == nil {
		return ""
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil {
		return ""
	}
	live := livePlanStepCount(p)
	switch p.Phase {
	case PlanPhaseRunning:
		pos := max(1, liveStepPosition(p, p.ActiveStep))
		return fmt.Sprintf("Agent is working · step %d of %d · manual edits to planned objects are paused", pos, max(1, live))
	case PlanPhaseStopped:
		if p.NothingWritten {
			return "Plan stopped · nothing was written"
		}
		return fmt.Sprintf("Plan stopped · %d of %d steps done", p.WrittenSteps, max(1, live))
	case PlanPhaseDraft:
		return "Agent proposed a plan · waiting for you"
	default:
		return ""
	}
}

// PlanPickedLine formats the answer summary banner shown once the plan leaves Draft ("Picked for you: ..." or "Your answers: ...").
func (e *Editor) PlanPickedLine() string {
	if e == nil {
		return ""
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	return formatPlanPickedLine(e.plan.active)
}

func formatPlanPickedLine(p *AgentPlan) string {
	if p == nil || len(p.Questions) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p.Questions))
	for _, q := range p.Questions {
		opt, ok := q.SelectedOption()
		if !ok {
			opt, ok = q.SuggestedOption()
		}
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", strings.ToLower(questionShortLabel(q)), strings.ToLower(opt.Label)))
	}
	if len(parts) == 0 {
		return ""
	}
	prefix := "Your answers: "
	if p.PickedForYou {
		prefix = "Picked for you: "
	}
	return prefix + strings.Join(parts, ", ") + "."
}

// ResolvedPlanFiles returns every file in the active plan with InPlan set to false when all steps touching it were removed.
func (e *Editor) ResolvedPlanFiles() []ResolvedPlanFile {
	if e == nil {
		return nil
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil {
		return nil
	}
	liveSet := map[string]bool{}
	for _, f := range livePlanFilePaths(p) {
		liveSet[f] = true
	}
	out := make([]ResolvedPlanFile, 0, len(p.Files))
	for _, f := range p.Files {
		tag := f.Tag
		if tag == "" {
			tag = PlanFileEdit
		}
		out = append(out, ResolvedPlanFile{
			Path:   f.Path,
			Tag:    tag,
			InPlan: liveSet[f.Path],
		})
	}
	return out
}

// ResolvedPlanTreeNodes returns planned Scene Tree nodes for non-removed steps with interpolated names.
func (e *Editor) ResolvedPlanTreeNodes() []ResolvedPlanTreeNode {
	if e == nil {
		return nil
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil {
		return nil
	}
	var out []ResolvedPlanTreeNode
	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		for _, n := range st.Nodes {
			out = append(out, ResolvedPlanTreeNode{
				Name:     interpolatePlanText(n.Name, p, &st),
				Layer:    n.Layer,
				Existing: n.Existing,
				Step:     st.Number,
				ObjectID: n.ObjectID,
			})
		}
	}
	return out
}

// PlannedNewTreeNodeCount returns how many non-existing planned nodes are scheduled in the active plan.
func (e *Editor) PlannedNewTreeNodeCount() int {
	if e == nil {
		return 0
	}
	n := 0
	for _, node := range e.ResolvedPlanTreeNodes() {
		if !node.Existing {
			n++
		}
	}
	return n
}

// PlanStepBadgeForNode returns the 1-based plan step number badge for an existing Scene Tree layer or object, or 0 if none.
func (e *Editor) PlanStepBadgeForNode(name string) int {
	if e == nil || name == "" {
		return 0
	}
	for _, n := range e.ResolvedPlanTreeNodes() {
		if n.Existing && (n.Name == name || n.ObjectID == name) {
			return n.Step
		}
	}
	return 0
}

// ResolvedPlanGhosts returns the concrete viewport ghosts for all non-removed steps of the active plan,
// applying selected question option overrides and numeric repeat fields. Returns nil when ghosts are toggled off.
func (e *Editor) ResolvedPlanGhosts() []ResolvedPlanGhost {
	if e == nil {
		return nil
	}
	e.plan.mu.Lock()
	defer e.plan.mu.Unlock()
	p := e.plan.active
	if p == nil || !p.GhostsVisible {
		return nil
	}
	return resolvePlanGhostsLocked(p)
}

func resolvePlanGhostsLocked(p *AgentPlan) []ResolvedPlanGhost {
	if p == nil {
		return nil
	}
	// Collect active overrides from selected (or default suggested) question options.
	var overrides []PlanGhostOverride
	for _, q := range p.Questions {
		opt, ok := q.SelectedOption()
		if !ok {
			opt, ok = q.SuggestedOption()
		}
		if ok && len(opt.GhostOverrides) > 0 {
			overrides = append(overrides, opt.GhostOverrides...)
		}
	}

	var out []ResolvedPlanGhost
	for _, st := range p.Steps {
		if st.Removed {
			continue
		}
		active := p.Phase == PlanPhaseRunning && p.ActiveStep == st.Number
		baseGhosts := clonePlanGhosts(st.Ghosts)

		// Apply question overrides targeting this step.
		for _, ov := range overrides {
			if ov.Step != st.Number {
				continue
			}
			if len(ov.ReplaceWith) > 0 {
				baseGhosts = clonePlanGhosts(ov.ReplaceWith)
				continue
			}
			for gi := range baseGhosts {
				if ov.GhostID != "" && baseGhosts[gi].ID != ov.GhostID {
					continue
				}
				if ov.X != nil {
					baseGhosts[gi].X = *ov.X
				}
				if ov.Y != nil {
					baseGhosts[gi].Y = *ov.Y
				}
				if ov.Width != nil {
					baseGhosts[gi].Width = *ov.Width
				}
				if ov.Height != nil {
					baseGhosts[gi].Height = *ov.Height
				}
				if ov.Label != "" {
					baseGhosts[gi].Label = ov.Label
				}
				if ov.LabelX != nil {
					baseGhosts[gi].LabelX = cloneFloat32Ptr(ov.LabelX)
				}
				if ov.LabelY != nil {
					baseGhosts[gi].LabelY = cloneFloat32Ptr(ov.LabelY)
				}
			}
		}

		// Expand ghosts (including RepeatField driven by numeric steppers).
		for _, g := range baseGhosts {
			label := g.Label
			if active && strings.TrimSpace(g.ActiveLabel) != "" {
				label = g.ActiveLabel
			}
			label = interpolatePlanText(label, p, &st)

			if g.RepeatField != "" {
				count := 0
				for _, f := range st.Fields {
					if f.Key == g.RepeatField {
						count = f.Value
						break
					}
				}
				if count > maxPlanRepeatCount {
					count = maxPlanRepeatCount
				}
				for idx := 0; idx < count && len(out) < maxResolvedPlanGhosts; idx++ {
					gx := g.X
					gy := g.Y
					if g.SpanWidth > 0 && count > 0 {
						gx = g.X + (float32(idx)+0.5)*g.SpanWidth/float32(count) - g.Width*0.5
					} else if g.StepX != 0 {
						gx = g.X + float32(idx)*g.StepX
					}
					if g.StaggerY != 0 && idx%2 == 1 {
						gy += g.StaggerY
					}
					rg := ResolvedPlanGhost{
						ID:       g.ID,
						Step:     st.Number,
						X:        gx,
						Y:        gy,
						Width:    g.Width,
						Height:   g.Height,
						Radius:   g.Radius,
						Active:   active,
						ObjectID: g.ObjectID,
					}
					if idx == 0 && label != "" {
						rg.HasLabel = true
						rg.Label = label
						rg.LabelX = gx
						rg.LabelY = gy - 24
						if g.LabelX != nil {
							rg.LabelX = *g.LabelX
						}
						if g.LabelY != nil {
							rg.LabelY = *g.LabelY
						}
					}
					out = append(out, rg)
				}
				continue
			}

			if len(out) >= maxResolvedPlanGhosts {
				return out
			}
			rg := ResolvedPlanGhost{
				ID:       g.ID,
				Step:     st.Number,
				X:        g.X,
				Y:        g.Y,
				Width:    g.Width,
				Height:   g.Height,
				Radius:   g.Radius,
				Active:   active,
				ObjectID: g.ObjectID,
			}
			if label != "" {
				rg.HasLabel = true
				rg.Label = label
				rg.LabelX = g.X + 6
				rg.LabelY = g.Y + 6
				if g.LabelX != nil {
					rg.LabelX = *g.LabelX
				}
				if g.LabelY != nil {
					rg.LabelY = *g.LabelY
				}
			}
			out = append(out, rg)
		}
	}
	return out
}

// drawPlanGhostsOverlay draws the active plan's agent-violet ghosts onto the 2D viewport canvas:
// dashed outlines while planned/queued, solid outline on the actively running step, plus step-numbered pill labels.
func (e *Editor) drawPlanGhostsOverlay(d *core.Drawing) {
	if e == nil || d == nil || e.playing || e.HistoryPreviewActive() {
		return
	}
	ghosts := e.ResolvedPlanGhosts()
	if len(ghosts) == 0 {
		return
	}
	spec := e.SpecPalette(nil)
	d.Save()

	// Pass 1: draw ghost boxes.
	for _, g := range ghosts {
		if g.Width <= 0 || g.Height <= 0 {
			continue
		}
		pts := worldRectCanvasCorners(e, g.X, g.Y, g.Width, g.Height)
		if len(pts) != 4 {
			continue
		}
		x0, y0 := pts[0].X, pts[0].Y
		x1, y1 := pts[2].X, pts[2].Y
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		if y1 < y0 {
			y0, y1 = y1, y0
		}
		w := max(float32(2), x1-x0)
		h := max(float32(2), y1-y0)

		fillAlpha := float32(0.11)
		strokeCol := ui.Alpha(spec.Agent.Edge, 0.78)
		if g.Active {
			fillAlpha = 0.18
			strokeCol = spec.Agent.Edge
			d.SetLineDash()
		} else {
			d.SetLineDash(5, 4)
		}
		d.SetFillColor(ui.Alpha(spec.Agent.Fg, fillAlpha))
		d.FillRect(x0, y0, w, h)
		d.SetStrokeColor(strokeCol).SetLineWidth(1.5)
		d.StrokeRect(x0, y0, w, h)
	}
	d.SetLineDash()

	// Pass 2: draw step-numbered pill labels on top of boxes.
	pillBg := ui.Alpha(spec.PillBg, 0.94)
	labelFont := core.Font{Size: 11}
	badgeFont := core.Font{Size: 10, Weight: core.FontWeightSemibold}
	for _, g := range ghosts {
		if !g.HasLabel || g.Label == "" {
			continue
		}
		lx, ly := e.WorldToCanvas(g.LabelX, g.LabelY)
		if math.IsNaN(float64(lx)) || math.IsNaN(float64(ly)) || math.IsInf(float64(lx), 0) || math.IsInf(float64(ly), 0) {
			continue
		}
		stepStr := strconv.Itoa(g.Step)
		var textW float32
		for _, r := range g.Label {
			if r > 0x7F {
				textW += 11.2
			} else {
				textW += 6.4
			}
		}
		const pillH float32 = 20
		const badgeD float32 = 16
		pillW := badgeD + textW + 14

		borderCol := ui.Alpha(spec.Agent.Edge, 0.55)
		if g.Active {
			borderCol = spec.Agent.Edge
		}
		// Pill background and 1px outline.
		d.SetFillColor(pillBg).FillRect(lx, ly, pillW, pillH)
		d.SetStrokeColor(borderCol).SetLineWidth(1).StrokeRect(lx, ly, pillW, pillH)

		// Step number circle badge (agent.fg fill + agent.onFill high-contrast digit).
		cx := lx + 2 + badgeD*0.5
		cy := ly + pillH*0.5
		d.SetFillColor(spec.Agent.Fg)
		d.BeginPath().Arc(cx, cy, badgeD*0.5, 0, float32(2*math.Pi), false).Fill()
		d.SetFillColor(spec.Agent.OnFill).FillText(stepStr, cx-float32(len(stepStr))*3.0, cy+3.5, badgeFont)

		// Pill label text.
		d.SetFillColor(spec.Agent.Fg).FillText(g.Label, lx+badgeD+7, ly+14, labelFont)
	}
	d.Restore()
}

// planGhostCanvasRect converts g's world rectangle into canvas coordinates (helper for tests/diagnostics).
func (e *Editor) planGhostCanvasRect(g ResolvedPlanGhost) (x, y, w, h float32) {
	pts := worldRectCanvasCorners(e, g.X, g.Y, g.Width, g.Height)
	if len(pts) != 4 {
		return 0, 0, 0, 0
	}
	x0, y0 := pts[0].X, pts[0].Y
	x1, y1 := pts[2].X, pts[2].Y
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	return x0, y0, x1 - x0, y1 - y0
}
