package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/datatable"
)

// AgentTurnStatus describes the lifecycle state of an agent request turn.
type AgentTurnStatus string

const (
	// AgentTurnWorking means the agent is actively making edits in this request.
	AgentTurnWorking AgentTurnStatus = "working"
	// AgentTurnPending means the agent finished the request and edits await Keep or Revert.
	AgentTurnPending AgentTurnStatus = "pending"
	// AgentTurnStopped means the person stopped the agent mid-request; partial edits stay pending and reviewable.
	AgentTurnStopped AgentTurnStatus = "stopped"
	// AgentTurnKept means the person kept the remaining changes as one History undo step.
	AgentTurnKept AgentTurnStatus = "kept"
	// AgentTurnReverted means the person reverted all touched files to their pre-request state.
	AgentTurnReverted AgentTurnStatus = "reverted"
)

// RightPanelTab selects which tab is visible in the editor's right-hand panel.
type RightPanelTab string

const (
	RightPanelInspector RightPanelTab = "inspector"
	RightPanelAgent     RightPanelTab = "agent"
)

// AgentTurnStep is one progress line shown in the Agent panel for the turn.
type AgentTurnStep struct {
	Label string `json:"label"`
	Done  bool   `json:"done"`
}

// AgentTurnFileSummary summarizes derived changes for one file touched by an agent turn.
type AgentTurnFileSummary struct {
	File          string            `json:"file"`
	Kind          AgentTurnFileKind `json:"kind"`
	ChangeCount   int               `json:"changeCount"`
	ActiveCount   int               `json:"activeCount"`
	RevertedCount int               `json:"revertedCount"`
	AddedLines    int               `json:"addedLines"`
	RemovedLines  int               `json:"removedLines"`
}

// AgentTurnSnapshot is a read-only view of the current agent turn for UI and MCP inspection.
type AgentTurnSnapshot struct {
	ID            uint64                 `json:"id"`
	Status        AgentTurnStatus        `json:"status"`
	Prompt        string                 `json:"prompt"`
	Summary       string                 `json:"summary,omitempty"`
	Steps         []AgentTurnStep        `json:"steps,omitempty"`
	Files         []AgentTurnFileSummary `json:"files,omitempty"`
	Changes       []AgentTurnChange      `json:"changes,omitempty"`
	TotalChanges  int                    `json:"totalChanges"`
	ActiveChanges int                    `json:"activeChanges"`
	RevertedCount int                    `json:"revertedCount"`
	AddedLines    int                    `json:"addedLines"`
	RemovedLines  int                    `json:"removedLines"`
	ReviewOpen    bool                   `json:"reviewOpen,omitempty"`
}

// IsPending reports whether the turn has finished or stopped and is awaiting Keep or Revert.
func (s AgentTurnSnapshot) IsPending() bool {
	return s.Status == AgentTurnPending || s.Status == AgentTurnStopped
}

// IsOpen reports whether the turn is working, pending, or stopped (not yet kept or reverted).
func (s AgentTurnSnapshot) IsOpen() bool {
	return s.Status == AgentTurnWorking || s.Status == AgentTurnPending || s.Status == AgentTurnStopped
}

// stagedFileEntry holds in-memory YAML for a non-active project file edited by an agent turn
// (either while pending or after Keep before Save). Nothing is written to disk until Keep + Save.
type stagedFileEntry struct {
	rel          string
	abs          string
	kind         AgentTurnFileKind
	data         []byte
	savedData    []byte
	sceneDoc     *sceneformat.SceneDoc
	tableDoc     *datatable.Document
	clipDoc      *resource.Animation
	dirty        bool
	fromPending  bool
	existedDisk  bool
	exactRestore bool
}

func (s *stagedFileEntry) clone() *stagedFileEntry {
	if s == nil {
		return nil
	}
	cp := *s
	cp.data = append([]byte(nil), s.data...)
	cp.savedData = append([]byte(nil), s.savedData...)
	if s.sceneDoc != nil {
		cp.sceneDoc = s.sceneDoc.Clone()
	}
	if s.tableDoc != nil {
		cp.tableDoc = s.tableDoc.Clone()
	}
	if s.clipDoc != nil {
		cp.clipDoc = cloneClip(s.clipDoc)
	}
	return &cp
}

// turnFile tracks one file touched during an agent turn.
type turnFile struct {
	rel           string // canonical project-relative slash path, or "" for untitled active scene
	abs           string
	kind          AgentTurnFileKind
	isActiveScene bool
	beforeExisted bool
	beforeRaw     []byte // exact pre-turn bytes (including comments/formatting if clean on disk)
	beforeYAML    []byte // canonical pre-turn YAML encoding
	agentYAML     []byte // canonical post-agent YAML encoding (before per-change reverts)

	beforeScene *sceneformat.SceneDoc
	agentScene  *sceneformat.SceneDoc
	beforeProj  *project.File
	agentProj   *project.File
	beforeTable *datatable.Document
	agentTable  *datatable.Document
	beforeClip  *resource.Animation
	agentClip   *resource.Animation

	// lastAppliedScene tracks the last scene doc the turn wrote into live memory so concurrent
	// person edits on untouched objects can be distinguished from agent edits.
	lastAppliedScene *sceneformat.SceneDoc

	reverted map[string]bool
	reasons  map[string]string
}

func (tf *turnFile) clone() *turnFile {
	if tf == nil {
		return nil
	}
	cp := *tf
	cp.beforeRaw = append([]byte(nil), tf.beforeRaw...)
	cp.beforeYAML = append([]byte(nil), tf.beforeYAML...)
	cp.agentYAML = append([]byte(nil), tf.agentYAML...)
	if tf.beforeScene != nil {
		cp.beforeScene = tf.beforeScene.Clone()
	}
	if tf.agentScene != nil {
		cp.agentScene = tf.agentScene.Clone()
	}
	if tf.lastAppliedScene != nil {
		cp.lastAppliedScene = tf.lastAppliedScene.Clone()
	}
	if tf.beforeProj != nil {
		cp.beforeProj = tf.beforeProj.Clone()
	}
	if tf.agentProj != nil {
		cp.agentProj = tf.agentProj.Clone()
	}
	if tf.beforeTable != nil {
		cp.beforeTable = tf.beforeTable.Clone()
	}
	if tf.agentTable != nil {
		cp.agentTable = tf.agentTable.Clone()
	}
	if tf.beforeClip != nil {
		cp.beforeClip = cloneClip(tf.beforeClip)
	}
	if tf.agentClip != nil {
		cp.agentClip = cloneClip(tf.agentClip)
	}
	if tf.reverted != nil {
		cp.reverted = make(map[string]bool, len(tf.reverted))
		for k, v := range tf.reverted {
			cp.reverted[k] = v
		}
	}
	if tf.reasons != nil {
		cp.reasons = make(map[string]string, len(tf.reasons))
		for k, v := range tf.reasons {
			cp.reasons[k] = v
		}
	}
	return &cp
}

func (tf *turnFile) displayPath() string {
	if tf.rel != "" {
		return tf.rel
	}
	return "the open scene"
}

func (tf *turnFile) changes() []AgentTurnChange {
	file := tf.displayPath()
	switch tf.kind {
	case TurnFileScene, TurnFilePrefab:
		return diffTurnSceneDocs(file, tf.beforeScene, tf.agentScene, tf.beforeYAML, tf.agentYAML, tf.reasons, tf.reverted)
	case TurnFileDataTable:
		return diffDataTableDocs(file, tf.beforeTable, tf.agentTable, tf.beforeYAML, tf.agentYAML, tf.reasons, tf.reverted)
	case TurnFileProject:
		return diffYAMLKeys(file, tf.beforeYAML, tf.agentYAML, tf.reasons, tf.reverted)
	case TurnFileClip:
		return diffClipDocs(file, tf.beforeClip, tf.agentClip, tf.beforeYAML, tf.agentYAML, tf.reasons, tf.reverted)
	default:
		return diffYAMLKeys(file, tf.beforeYAML, tf.agentYAML, tf.reasons, tf.reverted)
	}
}

func (tf *turnFile) effectiveState() (yamlBytes []byte, sceneDoc *sceneformat.SceneDoc, proj *project.File, tbl *datatable.Document, clip *resource.Animation, allReverted bool, err error) {
	ch := tf.changes()
	if len(ch) == 0 {
		return append([]byte(nil), tf.beforeYAML...), tf.beforeScene.Clone(), tf.beforeProj.Clone(), tf.beforeTable.Clone(), cloneClip(tf.beforeClip), true, nil
	}
	active := 0
	for _, c := range ch {
		if !c.Reverted {
			active++
		}
	}
	if active == 0 {
		return append([]byte(nil), tf.beforeYAML...), tf.beforeScene.Clone(), tf.beforeProj.Clone(), tf.beforeTable.Clone(), cloneClip(tf.beforeClip), true, nil
	}
	file := tf.displayPath()
	switch tf.kind {
	case TurnFileScene, TurnFilePrefab:
		doc := synthesizeSceneDoc(file, tf.beforeScene, tf.agentScene, tf.reverted)
		enc, encErr := sceneformat.Encode(doc)
		if encErr != nil {
			return nil, nil, nil, nil, nil, false, encErr
		}
		return enc, doc, nil, nil, nil, false, nil
	case TurnFileDataTable:
		doc := synthesizeDataTableDoc(file, tf.beforeTable, tf.agentTable, tf.reverted)
		enc, encErr := doc.Marshal()
		if encErr != nil {
			return nil, nil, nil, nil, nil, false, encErr
		}
		return enc, nil, nil, doc, nil, false, nil
	case TurnFileProject:
		pf, enc, pErr := synthesizeProjectFile(file, tf.beforeYAML, tf.agentYAML, tf.reverted)
		if pErr != nil {
			return nil, nil, nil, nil, nil, false, pErr
		}
		return enc, nil, pf, nil, nil, false, nil
	case TurnFileClip:
		c := synthesizeClipDoc(file, tf.beforeClip, tf.agentClip, tf.reverted)
		enc, cErr := resource.Encode(c)
		if cErr != nil {
			return nil, nil, nil, nil, nil, false, cErr
		}
		return enc, nil, nil, nil, c, false, nil
	default:
		raw, yErr := synthesizeYAMLByKeys(file, tf.beforeYAML, tf.agentYAML, tf.reverted)
		if yErr != nil {
			return nil, nil, nil, nil, nil, false, yErr
		}
		return raw, nil, nil, nil, nil, false, nil
	}
}

type agentTurnState struct {
	id                uint64
	status            AgentTurnStatus
	prompt            string
	summary           string
	steps             []AgentTurnStep
	files             map[string]*turnFile
	fileOrder         []string
	reviewOpen        bool
	reviewHistoryBase int
	startedAt         time.Time
}

func (s *agentTurnState) clone() *agentTurnState {
	if s == nil {
		return nil
	}
	cp := *s
	cp.steps = slices.Clone(s.steps)
	cp.fileOrder = slices.Clone(s.fileOrder)
	if s.files != nil {
		cp.files = make(map[string]*turnFile, len(s.files))
		for k, v := range s.files {
			cp.files[k] = v.clone()
		}
	}
	return &cp
}

type pendingTurnPromptState struct {
	open    bool
	summary string
	proceed func()
	cancel  func()
}

// RightPanelTab returns the currently visible tab of the editor's right-hand panel ("inspector" or "agent").
func (e *Editor) RightPanelTab() RightPanelTab {
	if e.RightRailTab() == RightTabAgent {
		return RightPanelAgent
	}
	return RightPanelInspector
}

// SetRightPanelTab switches the right-hand panel between Inspector and Agent.
func (e *Editor) SetRightPanelTab(tab RightPanelTab) {
	if e == nil {
		return
	}
	if tab == RightPanelAgent {
		e.SetRightRailTab(RightTabAgent)
		return
	}
	e.SetRightRailTab(RightTabInspector)
}

// AgentTurnWorking reports whether an agent turn is currently open and actively editing.
func (e *Editor) AgentTurnWorking() bool {
	return e != nil && e.agentTurn != nil && e.agentTurn.status == AgentTurnWorking
}

// HasPendingAgentTurn reports whether an agent turn is waiting for the person to Keep or Revert
// (including a stopped turn with partial changes, or a working turn with edits).
func (e *Editor) HasPendingAgentTurn() bool {
	if e == nil || e.agentTurn == nil {
		return false
	}
	switch e.agentTurn.status {
	case AgentTurnWorking, AgentTurnPending, AgentTurnStopped:
		return true
	default:
		return false
	}
}

// AgentTurnReviewOpen reports whether the pending turn's review view is currently expanded/active.
func (e *Editor) AgentTurnReviewOpen() bool {
	return e != nil && e.agentTurn != nil && e.agentTurn.reviewOpen &&
		(e.agentTurn.status == AgentTurnPending || e.agentTurn.status == AgentTurnStopped)
}

// SetAgentTurnReviewOpen opens or closes the turn review state for the current pending turn.
func (e *Editor) SetAgentTurnReviewOpen(open bool) {
	if e == nil || e.agentTurn == nil {
		return
	}
	if open && e.agentTurn.status != AgentTurnPending && e.agentTurn.status != AgentTurnStopped {
		return
	}
	if e.agentTurn.reviewOpen == open {
		return
	}
	e.agentTurn.reviewOpen = open
	if open {
		e.agentTurn.reviewHistoryBase = e.History.Index()
		e.SetRightRailTab(RightTabAgent)
	}
	e.notify()
}

// CurrentAgentTurn returns a read-only snapshot of the current (or most recently kept/reverted) multi-file agent turn.
func (e *Editor) CurrentAgentTurn() (AgentTurnSnapshot, bool) {
	if e == nil || e.agentTurn == nil {
		return AgentTurnSnapshot{}, false
	}
	t := e.agentTurn
	snap := AgentTurnSnapshot{
		ID:         t.id,
		Status:     t.status,
		Prompt:     t.prompt,
		Summary:    t.summary,
		Steps:      slices.Clone(t.steps),
		ReviewOpen: t.reviewOpen,
	}
	for _, key := range t.fileOrder {
		tf := t.files[key]
		if tf == nil {
			continue
		}
		fChanges := tf.changes()
		if len(fChanges) == 0 {
			continue
		}
		active := 0
		reverted := 0
		for _, c := range fChanges {
			if c.Reverted {
				reverted++
			} else {
				active++
			}
		}
		effYAML, _, _, _, _, _, err := tf.effectiveState()
		addL, remL := 0, 0
		if err == nil {
			addL, remL = lineDiffCounts(tf.beforeYAML, effYAML)
		}
		snap.Files = append(snap.Files, AgentTurnFileSummary{
			File:          tf.displayPath(),
			Kind:          tf.kind,
			ChangeCount:   len(fChanges),
			ActiveCount:   active,
			RevertedCount: reverted,
			AddedLines:    addL,
			RemovedLines:  remL,
		})
		snap.Changes = append(snap.Changes, fChanges...)
		snap.TotalChanges += len(fChanges)
		snap.ActiveChanges += active
		snap.RevertedCount += reverted
		snap.AddedLines += addL
		snap.RemovedLines += remL
	}
	return snap, true
}

// StartAgentTurn begins a new pending agent turn labelled with prompt.
// Every edit made through the editor's MCP server until EndAgentTurn or StopAgentTurn is grouped
// into this one turn across all touched files, applied in memory immediately with zero disk writes.
func (e *Editor) StartAgentTurn(prompt string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agentTurn != nil {
		switch e.agentTurn.status {
		case AgentTurnWorking:
			return errors.New("editor: an agent turn is already in progress")
		case AgentTurnPending, AgentTurnStopped:
			return errors.New("editor: keep or revert the pending agent turn before starting a new one")
		}
	}
	e.agentTurnSeq++
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	p := strings.TrimSpace(prompt)
	if p == "" {
		p = "Agent request"
	}
	e.agentTurn = &agentTurnState{
		id:        e.agentTurnSeq,
		status:    AgentTurnWorking,
		prompt:    p,
		files:     make(map[string]*turnFile),
		startedAt: now,
	}
	e.lastRevertedTurn = nil
	e.Status = "Agent working…"
	e.notify()
	return nil
}

// RecordAgentTurnStep appends a short progress step to the active agent turn (shown in the Agent panel).
func (e *Editor) RecordAgentTurnStep(label string) {
	if e == nil || e.agentTurn == nil || strings.TrimSpace(label) == "" {
		return
	}
	e.agentTurn.steps = append(e.agentTurn.steps, AgentTurnStep{
		Label: strings.TrimSpace(label),
		Done:  true,
	})
	e.notify()
}

// EndAgentTurn marks the currently working agent turn as finished (AgentTurnPending) so the person
// can review, revert/restore individual changes, Keep as one undo step, or Revert.
func (e *Editor) EndAgentTurn(summary string) error {
	if e == nil || e.agentTurn == nil || e.agentTurn.status != AgentTurnWorking {
		return errors.New("editor: no agent turn is currently working")
	}
	e.agentTurn.status = AgentTurnPending
	if strings.TrimSpace(summary) != "" {
		e.agentTurn.summary = strings.TrimSpace(summary)
	}
	e.agentTurn.reviewHistoryBase = e.History.Index()
	snap, _ := e.CurrentAgentTurn()
	if snap.TotalChanges > 0 {
		e.Status = fmt.Sprintf("Agent finished · %d %s to review", snap.TotalChanges, turnPlural(snap.TotalChanges, "change", "changes"))
	} else {
		e.Status = "Agent finished · no changes"
	}
	e.notify()
	return nil
}

// StopAgentTurn stops a working agent turn (⌘. or Stop button). Any edits made so far remain
// live in memory and pending as AgentTurnStopped so they can still be reviewed, kept, or reverted.
func (e *Editor) StopAgentTurn() error {
	if e == nil || e.agentTurn == nil || e.agentTurn.status != AgentTurnWorking {
		return errors.New("editor: no agent turn is currently working")
	}
	e.agentTurn.status = AgentTurnStopped
	e.agentTurn.reviewHistoryBase = e.History.Index()
	snap, _ := e.CurrentAgentTurn()
	msg := "Agent stopped"
	if snap.TotalChanges > 0 {
		msg = fmt.Sprintf("Agent stopped · %d %s pending", snap.TotalChanges, turnPlural(snap.TotalChanges, "change", "changes"))
	}
	e.Status = msg
	e.ShowToast(Toast{
		ID:          "agent-turn",
		Kind:        ToastInfo,
		Message:     msg,
		ActionLabel: "Review",
		OnAction: func() {
			e.SetRightPanelTab(RightPanelAgent)
			e.SetAgentTurnReviewOpen(true)
			e.DismissToast()
		},
	})
	return nil
}

func turnPlural(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// canonicalTurnFileKey resolves a file path to its canonical key in turn.files and returns
// (key, absPath, kind, isActiveScene).
func (e *Editor) canonicalTurnFileKey(rel string) (key, abs string, kind AgentTurnFileKind, isActive bool, err error) {
	cleaned := strings.TrimSpace(strings.ReplaceAll(rel, `\`, "/"))
	if cleaned == "" || (e.ScenePath != "" && path.Clean(cleaned) == e.ScenePath) {
		key = e.ScenePath
		abs = e.FilePath
		kind = TurnFileScene
		if key != "" {
			kind = classifyTurnFileKind(key, e.Project)
		}
		return key, abs, kind, true, nil
	}
	cleaned = path.Clean(cleaned)
	if !validScenePath(cleaned) {
		return "", "", TurnFileOther, false, fmt.Errorf("editor: %q is not a slash-separated path inside the project", rel)
	}
	if e.Project == nil || e.ProjectPath == "" {
		return "", "", TurnFileOther, false, ErrNoProject
	}
	abs, err = e.projectFile(cleaned)
	if err != nil {
		return "", "", TurnFileOther, false, err
	}
	kind = classifyTurnFileKind(cleaned, e.Project)
	isActive = e.ScenePath != "" && cleaned == e.ScenePath
	return cleaned, abs, kind, isActive, nil
}

// ensureTurnFile snapshots the pre-turn state of file into e.agentTurn on first touch.
func (e *Editor) ensureTurnFile(rel string) (*turnFile, error) {
	if e.agentTurn == nil || e.agentTurn.status != AgentTurnWorking {
		return nil, errors.New("editor: no working agent turn")
	}
	key, abs, kind, isActive, err := e.canonicalTurnFileKey(rel)
	if err != nil {
		return nil, err
	}
	if existing, ok := e.agentTurn.files[key]; ok {
		return existing, nil
	}
	tf := &turnFile{
		rel:           key,
		abs:           abs,
		kind:          kind,
		isActiveScene: isActive,
		reverted:      make(map[string]bool),
		reasons:       make(map[string]string),
	}

	switch kind {
	case TurnFileScene, TurnFilePrefab:
		var doc *sceneformat.SceneDoc
		var raw []byte
		var existed bool
		if isActive {
			doc = e.Doc.Clone()
			enc, encErr := e.SaveYAML()
			if encErr != nil {
				return nil, encErr
			}
			tf.beforeYAML = enc
			raw = enc
			if abs != "" {
				if diskBytes, rErr := os.ReadFile(abs); rErr == nil {
					existed = true
					if !e.SceneDirty() {
						raw = diskBytes
					}
				}
			}
		} else if tabIdx := e.findTabByPath(key, abs); tabIdx >= 0 {
			t := e.tabs[tabIdx]
			doc = t.doc.Clone()
			enc, encErr := sceneformat.Encode(doc)
			if encErr != nil {
				return nil, encErr
			}
			tf.beforeYAML = enc
			raw = enc
			if abs != "" {
				if diskBytes, rErr := os.ReadFile(abs); rErr == nil {
					existed = true
					if !e.tabDirtyAt(tabIdx) {
						raw = diskBytes
					}
				}
			}
		} else if staged, ok := e.stagedFiles[key]; ok && staged.sceneDoc != nil {
			doc = staged.sceneDoc.Clone()
			enc, encErr := sceneformat.Encode(doc)
			if encErr != nil {
				return nil, encErr
			}
			tf.beforeYAML = enc
			raw = append([]byte(nil), staged.data...)
			existed = staged.existedDisk
		} else {
			diskBytes, rErr := os.ReadFile(abs)
			if rErr != nil {
				if !errors.Is(rErr, os.ErrNotExist) {
					return nil, rErr
				}
				doc = newSceneDoc(key, e.Project)
				enc, encErr := sceneformat.Encode(doc)
				if encErr != nil {
					return nil, encErr
				}
				tf.beforeYAML = enc
				raw = nil
				existed = false
			} else {
				parsed, pErr := sceneformat.ParseWith(diskBytes, sceneformat.ParseOptions{
					File:          abs,
					Components:    e.components,
					PhysicsLayers: e.PhysicsLayers(),
				})
				if pErr != nil {
					return nil, pErr
				}
				enc, encErr := sceneformat.Encode(parsed)
				if encErr != nil {
					return nil, encErr
				}
				doc = parsed
				tf.beforeYAML = enc
				raw = diskBytes
				existed = true
			}
		}
		tf.beforeExisted = existed
		tf.beforeRaw = append([]byte(nil), raw...)
		tf.beforeScene = doc.Clone()
		tf.agentScene = doc.Clone()
		tf.lastAppliedScene = doc.Clone()
		tf.agentYAML = append([]byte(nil), tf.beforeYAML...)

	case TurnFileProject:
		if e.Project == nil {
			return nil, ErrNoProject
		}
		enc, encErr := project.Encode(e.Project)
		if encErr != nil {
			return nil, encErr
		}
		raw := enc
		existed := false
		if e.ProjectPath != "" {
			if diskBytes, rErr := os.ReadFile(e.ProjectPath); rErr == nil {
				existed = true
				if !e.ProjectDirty() {
					raw = diskBytes
				}
			}
		}
		tf.abs = e.ProjectPath
		tf.beforeExisted = existed
		tf.beforeRaw = append([]byte(nil), raw...)
		tf.beforeYAML = enc
		tf.agentYAML = append([]byte(nil), enc...)
		tf.beforeProj = e.Project.Clone()
		tf.agentProj = e.Project.Clone()

	case TurnFileDataTable:
		var doc *datatable.Document
		var raw []byte
		var existed bool
		if e.table != nil && e.table.doc != nil && e.dataTableMatchPath(e.table.path) == key {
			doc = e.table.doc.Clone()
			enc, encErr := doc.Marshal()
			if encErr != nil {
				return nil, encErr
			}
			tf.beforeYAML = enc
			raw = enc
			if abs != "" {
				if diskBytes, rErr := os.ReadFile(abs); rErr == nil {
					existed = true
					if !e.DataTableDirty() {
						raw = diskBytes
					}
				}
			}
		} else if staged, ok := e.stagedFiles[key]; ok && staged.tableDoc != nil {
			doc = staged.tableDoc.Clone()
			enc, encErr := doc.Marshal()
			if encErr != nil {
				return nil, encErr
			}
			tf.beforeYAML = enc
			raw = append([]byte(nil), staged.data...)
			existed = staged.existedDisk
		} else {
			diskBytes, rErr := os.ReadFile(abs)
			if rErr != nil {
				if !errors.Is(rErr, os.ErrNotExist) {
					return nil, rErr
				}
				doc = datatable.NewDocument()
				enc, encErr := doc.Marshal()
				if encErr != nil {
					return nil, encErr
				}
				tf.beforeYAML = enc
				raw = nil
				existed = false
			} else {
				parsed, pErr := datatable.ParseDocument(abs, diskBytes)
				if pErr != nil {
					return nil, pErr
				}
				enc, encErr := parsed.Marshal()
				if encErr != nil {
					return nil, encErr
				}
				doc = parsed
				tf.beforeYAML = enc
				raw = diskBytes
				existed = true
			}
		}
		tf.beforeExisted = existed
		tf.beforeRaw = append([]byte(nil), raw...)
		tf.beforeTable = doc.Clone()
		tf.agentTable = doc.Clone()
		tf.agentYAML = append([]byte(nil), tf.beforeYAML...)

	case TurnFileClip:
		var clip *resource.Animation
		var raw []byte
		var existed bool
		if e.clip != nil && e.clipMeta.path != "" {
			if clipRel, ok := e.projectRel(e.clipMeta.path); ok && clipRel == key {
				clip = cloneClip(e.clip)
				enc, encErr := resource.Encode(clip)
				if encErr != nil {
					return nil, encErr
				}
				tf.beforeYAML = enc
				raw = enc
				if diskBytes, rErr := os.ReadFile(abs); rErr == nil {
					existed = true
					if !e.ClipDirty() {
						raw = diskBytes
					}
				}
			}
		}
		if clip == nil {
			if staged, ok := e.stagedFiles[key]; ok && staged.clipDoc != nil {
				clip = cloneClip(staged.clipDoc)
				enc, encErr := resource.Encode(clip)
				if encErr != nil {
					return nil, encErr
				}
				tf.beforeYAML = enc
				raw = append([]byte(nil), staged.data...)
				existed = staged.existedDisk
			} else {
				diskBytes, rErr := os.ReadFile(abs)
				if rErr != nil {
					if !errors.Is(rErr, os.ErrNotExist) {
						return nil, rErr
					}
					clip = &resource.Animation{Header: resource.NewHeader(resource.KindAnimation), Length: 1}
					enc, encErr := resource.Encode(clip)
					if encErr != nil {
						return nil, encErr
					}
					tf.beforeYAML = enc
					raw = nil
					existed = false
				} else {
					parsed, pErr := resource.ParseAs[*resource.Animation](diskBytes)
					if pErr != nil {
						return nil, pErr
					}
					enc, encErr := resource.Encode(parsed)
					if encErr != nil {
						return nil, encErr
					}
					clip = parsed
					tf.beforeYAML = enc
					raw = diskBytes
					existed = true
				}
			}
		}
		tf.beforeExisted = existed
		tf.beforeRaw = append([]byte(nil), raw...)
		tf.beforeClip = cloneClip(clip)
		tf.agentClip = cloneClip(clip)
		tf.agentYAML = append([]byte(nil), tf.beforeYAML...)

	default:
		var raw []byte
		var existed bool
		if staged, ok := e.stagedFiles[key]; ok {
			raw = append([]byte(nil), staged.data...)
			existed = staged.existedDisk
		} else if diskBytes, rErr := os.ReadFile(abs); rErr == nil {
			raw = diskBytes
			existed = true
		} else if !errors.Is(rErr, os.ErrNotExist) {
			return nil, rErr
		}
		tf.beforeExisted = existed
		tf.beforeRaw = append([]byte(nil), raw...)
		tf.beforeYAML = append([]byte(nil), raw...)
		tf.agentYAML = append([]byte(nil), raw...)
	}

	e.agentTurn.files[key] = tf
	e.agentTurn.fileOrder = append(e.agentTurn.fileOrder, key)
	return tf, nil
}

// RunAgentSceneEdit executes fn on the scene or prefab named by file (empty for the active open scene)
// inside the currently working agent turn. Edits apply in memory immediately without pushing onto
// History and without writing to disk. If fn returns an error, all mutations from fn are rolled back.
// Optional reasons (keyed by object ID, "obj:<id>", "obj:<id>::<field>", or "*") attach why notes
// to the derived structural changes.
func (e *Editor) RunAgentSceneEdit(file string, reasons map[string]string, fn func(target *Editor) error) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.AgentTurnWorking() {
		return errors.New("editor: no working agent turn")
	}
	tf, err := e.ensureTurnFile(file)
	if err != nil {
		return err
	}
	if tf.kind != TurnFileScene && tf.kind != TurnFilePrefab {
		return fmt.Errorf("editor: %q is not a scene or prefab file", file)
	}

	if tf.isActiveScene {
		preEdit := e.Doc.Clone()
		prev := e.History.beginPreview("agent-turn")
		err = func() (runErr error) {
			defer func() {
				if r := recover(); r != nil {
					cmds := e.History.endPreview(prev)
					(&groupCmd{cmds: cmds}).rollback(len(cmds))
					panic(r)
				}
			}()
			return fn(e)
		}()
		cmds := e.History.endPreview(prev)
		if err != nil {
			(&groupCmd{cmds: cmds}).rollback(len(cmds))
			e.afterMutate()
			return err
		}
		postEdit := e.Doc.Clone()
		tf.agentScene = applySceneDelta(tf.agentScene, preEdit, postEdit)
		tf.lastAppliedScene = postEdit.Clone()
		enc, encErr := sceneformat.Encode(tf.agentScene)
		if encErr != nil {
			return encErr
		}
		tf.agentYAML = enc
		recordReasonsForDelta(tf, preEdit, postEdit, reasons)
		e.afterMutate()
		return nil
	}

	// Non-active scene or prefab: execute fn on a sub-editor initialized with the current in-memory doc.
	curDoc := e.currentSceneDocForTurnFile(tf)
	preEdit := curDoc.Clone()
	sub, err := New(curDoc.Clone(), e.Resolver)
	if err != nil {
		return err
	}
	sub.components = e.components
	sub.Project = e.Project
	sub.ProjectPath = e.ProjectPath
	sub.FilePath = tf.abs
	sub.ScenePath = tf.rel
	sub.ModelResolver = e.ModelResolver
	if err := fn(sub); err != nil {
		return err
	}
	postEdit := sub.Doc.Clone()
	tf.agentScene = applySceneDelta(tf.agentScene, preEdit, postEdit)
	tf.lastAppliedScene = postEdit.Clone()
	enc, encErr := sceneformat.Encode(tf.agentScene)
	if encErr != nil {
		return encErr
	}
	tf.agentYAML = enc
	recordReasonsForDelta(tf, preEdit, postEdit, reasons)
	e.applyTurnFileLive(tf, postEdit, enc, nil, nil, nil, true)
	e.notify()
	return nil
}

func recordReasonsForDelta(tf *turnFile, preEdit, postEdit *sceneformat.SceneDoc, reasons map[string]string) {
	if len(reasons) == 0 {
		return
	}
	for k, v := range reasons {
		v = strings.TrimSpace(v)
		if v == "" || k == "*" {
			continue
		}
		tf.reasons[k] = v
	}
	if globalWhy := strings.TrimSpace(reasons["*"]); globalWhy != "" {
		delta := diffTurnSceneDocs(tf.displayPath(), preEdit, postEdit, nil, nil, nil, nil)
		for _, ch := range delta {
			if lookupReason(tf.reasons, ch.ID, "obj:"+ch.ObjectID+"::"+ch.Field, "obj:"+ch.ObjectID) == "" {
				tf.reasons[ch.ID] = globalWhy
			}
		}
	}
}

func (e *Editor) currentSceneDocForTurnFile(tf *turnFile) *sceneformat.SceneDoc {
	if tf.isActiveScene {
		return e.Doc
	}
	if idx := e.findTabByPath(tf.rel, tf.abs); idx >= 0 && e.tabs[idx].doc != nil {
		return e.tabs[idx].doc
	}
	if staged, ok := e.stagedFiles[tf.rel]; ok && staged.sceneDoc != nil {
		return staged.sceneDoc
	}
	if tf.agentScene != nil {
		return tf.agentScene
	}
	return tf.beforeScene
}

// RecordAgentFileEdit validates and applies raw YAML content to file (game.yaml, data table, clip,
// prefab, or scene) in memory inside the currently working agent turn without writing to disk.
func (e *Editor) RecordAgentFileEdit(file string, yamlData []byte, why string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.AgentTurnWorking() {
		return errors.New("editor: no working agent turn")
	}
	tf, err := e.ensureTurnFile(file)
	if err != nil {
		return err
	}
	why = strings.TrimSpace(why)

	switch tf.kind {
	case TurnFileScene, TurnFilePrefab:
		doc, pErr := sceneformat.ParseWith(yamlData, sceneformat.ParseOptions{
			File:          tf.abs,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if pErr != nil {
			return pErr
		}
		enc, encErr := sceneformat.Encode(doc)
		if encErr != nil {
			return encErr
		}
		pre := tf.agentScene.Clone()
		tf.agentScene = doc.Clone()
		tf.lastAppliedScene = doc.Clone()
		tf.agentYAML = enc
		if why != "" {
			recordReasonsForDelta(tf, pre, doc, map[string]string{"*": why})
		}
		e.applyTurnFileLive(tf, doc, enc, nil, nil, nil, true)
		e.notify()
		return nil

	case TurnFileProject:
		pf, pErr := project.Parse(yamlData)
		if pErr != nil {
			return pErr
		}
		if vErr := project.Validate(pf); vErr != nil {
			return vErr
		}
		enc, encErr := project.Encode(pf)
		if encErr != nil {
			return encErr
		}
		prevYAML := tf.agentYAML
		tf.agentProj = pf.Clone()
		tf.agentYAML = enc
		if why != "" {
			for _, ch := range diffYAMLKeys(tf.displayPath(), prevYAML, enc, nil, nil) {
				tf.reasons[ch.ID] = why
			}
			tf.reasons["*"] = why
		}
		e.applyTurnFileLive(tf, nil, enc, pf, nil, nil, true)
		e.notify()
		return nil

	case TurnFileDataTable:
		doc, pErr := datatable.ParseDocument(tf.abs, yamlData)
		if pErr != nil {
			return pErr
		}
		if rt := e.DataTableRowTypeFor(tf.abs); rt != nil {
			if vErr := rt.ValidateDocument(tf.abs, doc); vErr != nil {
				return vErr
			}
		}
		enc, encErr := doc.Marshal()
		if encErr != nil {
			return encErr
		}
		prevTbl := tf.agentTable.Clone()
		tf.agentTable = doc.Clone()
		tf.agentYAML = enc
		if why != "" {
			for _, ch := range diffDataTableDocs(tf.displayPath(), prevTbl, doc, nil, enc, nil, nil) {
				tf.reasons[ch.ID] = why
			}
			tf.reasons["*"] = why
		}
		e.applyTurnFileLive(tf, nil, enc, nil, doc, nil, true)
		e.notify()
		return nil

	case TurnFileClip:
		clip, pErr := resource.ParseAs[*resource.Animation](yamlData)
		if pErr != nil {
			return pErr
		}
		enc, encErr := resource.Encode(clip)
		if encErr != nil {
			return encErr
		}
		prevClip := cloneClip(tf.agentClip)
		tf.agentClip = cloneClip(clip)
		tf.agentYAML = enc
		if why != "" {
			for _, ch := range diffClipDocs(tf.displayPath(), prevClip, clip, nil, enc, nil, nil) {
				tf.reasons[ch.ID] = why
			}
			tf.reasons["*"] = why
		}
		e.applyTurnFileLive(tf, nil, enc, nil, nil, clip, true)
		e.notify()
		return nil

	default:
		var node yaml.Node
		if yErr := yaml.Unmarshal(yamlData, &node); yErr != nil {
			return yErr
		}
		prevYAML := tf.agentYAML
		tf.agentYAML = append([]byte(nil), yamlData...)
		if why != "" {
			for _, ch := range diffYAMLKeys(tf.displayPath(), prevYAML, yamlData, nil, nil) {
				tf.reasons[ch.ID] = why
			}
			tf.reasons["*"] = why
		}
		e.applyTurnFileLive(tf, nil, yamlData, nil, nil, nil, true)
		e.notify()
		return nil
	}
}

// applyTurnFileLive pushes the given in-memory file state into the live Editor (Doc/tabs/Project/table/clip/stagedFiles)
// without touching disk.
func (e *Editor) applyTurnFileLive(tf *turnFile, sceneDoc *sceneformat.SceneDoc, yamlData []byte, proj *project.File, tbl *datatable.Document, clip *resource.Animation, fromPending bool) {
	switch tf.kind {
	case TurnFileScene, TurnFilePrefab:
		if tf.isActiveScene && sceneDoc != nil {
			e.setDoc(sceneDoc.Clone())
			e.Rebuild()
			e.syncActiveTab()
			return
		}
		if idx := e.findTabByPath(tf.rel, tf.abs); idx >= 0 && sceneDoc != nil {
			t := e.tabs[idx]
			t.doc = sceneDoc.Clone()
			if built, bErr := sceneformat.Build(t.doc, e.resolverForScene(t.scenePath)); bErr == nil {
				built.DrawAllTiles = true
				t.built = built
				t.buildErr = nil
			} else {
				t.buildErr = bErr
			}
			if idx == e.activeTab {
				e.loadActiveTab()
			}
			return
		}
		e.setStagedFile(tf.rel, tf.abs, tf.kind, yamlData, tf.beforeRaw, sceneDoc, nil, nil, !bytes.Equal(yamlData, tf.beforeYAML), fromPending, tf.beforeExisted, false)

	case TurnFileProject:
		if proj != nil {
			e.Project = proj.Clone()
		}

	case TurnFileDataTable:
		if e.table != nil && e.dataTableMatchPath(e.table.path) == tf.rel && tbl != nil {
			e.table.doc = tbl.Clone()
			e.table.lastErr = nil
			return
		}
		e.setStagedFile(tf.rel, tf.abs, tf.kind, yamlData, tf.beforeRaw, nil, tbl, nil, !bytes.Equal(yamlData, tf.beforeYAML), fromPending, tf.beforeExisted, false)

	case TurnFileClip:
		if e.clip != nil && e.clipMeta.path != "" {
			if clipRel, ok := e.projectRel(e.clipMeta.path); ok && clipRel == tf.rel && clip != nil {
				e.clip = cloneClip(clip)
				e.refreshClipPreview()
				return
			}
		}
		e.setStagedFile(tf.rel, tf.abs, tf.kind, yamlData, tf.beforeRaw, nil, nil, clip, !bytes.Equal(yamlData, tf.beforeYAML), fromPending, tf.beforeExisted, false)

	default:
		e.setStagedFile(tf.rel, tf.abs, tf.kind, yamlData, tf.beforeRaw, nil, nil, nil, !bytes.Equal(yamlData, tf.beforeYAML), fromPending, tf.beforeExisted, false)
	}
}

func (e *Editor) setStagedFile(rel, abs string, kind AgentTurnFileKind, data, savedData []byte, sceneDoc *sceneformat.SceneDoc, tableDoc *datatable.Document, clipDoc *resource.Animation, dirty, fromPending, existedDisk, exactRestore bool) {
	if rel == "" {
		return
	}
	if e.stagedFiles == nil {
		e.stagedFiles = make(map[string]*stagedFileEntry)
	}
	if !dirty && !fromPending {
		// If memory matches disk bytes, no need to retain a staged override.
		if existedDisk && abs != "" {
			if disk, err := os.ReadFile(abs); err == nil && bytes.Equal(disk, data) {
				delete(e.stagedFiles, rel)
				return
			}
		}
	}
	entry := &stagedFileEntry{
		rel:          rel,
		abs:          abs,
		kind:         kind,
		data:         append([]byte(nil), data...),
		savedData:    append([]byte(nil), savedData...),
		dirty:        dirty,
		fromPending:  fromPending,
		existedDisk:  existedDisk,
		exactRestore: exactRestore,
	}
	if sceneDoc != nil {
		entry.sceneDoc = sceneDoc.Clone()
	}
	if tableDoc != nil {
		entry.tableDoc = tableDoc.Clone()
	}
	if clipDoc != nil {
		entry.clipDoc = cloneClip(clipDoc)
	}
	e.stagedFiles[rel] = entry
}

// StagedFileYAML returns the live in-memory YAML bytes for rel (a slash-separated project path)
// if rel is currently open in a non-active scene tab or held in memory by an active/kept agent turn.
func (e *Editor) StagedFileYAML(rel string) ([]byte, bool) {
	if e == nil || rel == "" {
		return nil, false
	}
	clean := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
	// Active scene tab
	if e.ScenePath != "" && clean == e.ScenePath {
		if data, err := e.SaveYAML(); err == nil {
			return data, e.SceneDirty()
		}
	}
	// Other open scene tabs
	if abs, err := e.projectFile(clean); err == nil {
		if idx := e.findTabByPath(clean, abs); idx >= 0 && idx != e.activeTab {
			if e.tabDirtyAt(idx) {
				if data, encErr := sceneformat.Encode(e.tabs[idx].doc); encErr == nil {
					return data, true
				}
			}
		}
	}
	// Open data table
	if e.table != nil && e.table.doc != nil && e.dataTableMatchPath(e.table.path) == clean && e.DataTableDirty() {
		if data, err := e.SaveDataTableYAML(); err == nil {
			return data, true
		}
	}
	// Open clip
	if e.clip != nil && e.clipMeta.path != "" {
		if clipRel, ok := e.projectRel(e.clipMeta.path); ok && clipRel == clean && e.ClipDirty() {
			if data, err := e.SaveClipYAML(); err == nil {
				return data, true
			}
		}
	}
	// Staged non-open files
	if s, ok := e.stagedFiles[clean]; ok {
		return append([]byte(nil), s.data...), true
	}
	return nil, false
}

// hasDirtyKeptStagedFiles reports whether any non-pending staged file has unsaved edits waiting to be saved.
func (e *Editor) hasDirtyKeptStagedFiles() bool {
	if e == nil {
		return false
	}
	for _, s := range e.stagedFiles {
		if s.dirty && !s.fromPending {
			return true
		}
	}
	return false
}

// saveKeptStagedFiles writes every kept (non-pending) dirty staged file atomically to disk.
func (e *Editor) saveKeptStagedFiles() (bool, error) {
	if e == nil || len(e.stagedFiles) == 0 {
		return false, nil
	}
	keys := make([]string, 0, len(e.stagedFiles))
	for k, s := range e.stagedFiles {
		if s.dirty && !s.fromPending {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return false, nil
	}
	slices.Sort(keys)
	for _, k := range keys {
		s := e.stagedFiles[k]
		if s.abs == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(s.abs), 0o755); err != nil {
			return false, err
		}
		if err := writeFileAtomic(s.abs, s.data); err != nil {
			return false, err
		}
		s.savedData = append([]byte(nil), s.data...)
		s.dirty = false
		s.existedDisk = true
	}
	e.filesRev++
	e.invalidateProjectIndex()
	e.rebuildAllInactiveTabs()
	return true, nil
}

// --- Per-change and per-file Revert / Restore while pending ---

func (e *Editor) requirePendingTurnForToggle() error {
	if e == nil || e.agentTurn == nil {
		return errors.New("editor: no pending agent turn")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agentTurn.status != AgentTurnPending && e.agentTurn.status != AgentTurnStopped {
		return errors.New("editor: changes can only be reverted or restored while the turn is pending")
	}
	return nil
}

func (e *Editor) recomputeAndApplyTurnFile(tf *turnFile) error {
	effYAML, sceneDoc, proj, tbl, clip, allReverted, err := tf.effectiveState()
	if err != nil {
		return err
	}
	if tf.kind == TurnFileScene || tf.kind == TurnFilePrefab {
		if tf.isActiveScene {
			// Merge delta relative to lastAppliedScene so concurrent person edits on untouched objects stay intact.
			next := applySceneDelta(e.Doc, tf.lastAppliedScene, sceneDoc)
			tf.lastAppliedScene = sceneDoc.Clone()
			e.setDoc(next)
			e.Rebuild()
			e.syncActiveTab()
			return nil
		}
		tf.lastAppliedScene = sceneDoc.Clone()
		if allReverted {
			e.applyTurnFileLive(tf, tf.beforeScene, tf.beforeRaw, nil, nil, nil, false)
			return nil
		}
		e.applyTurnFileLive(tf, sceneDoc, effYAML, nil, nil, nil, true)
		return nil
	}
	if allReverted {
		e.applyTurnFileLive(tf, tf.beforeScene, tf.beforeRaw, tf.beforeProj, tf.beforeTable, tf.beforeClip, false)
		return nil
	}
	e.applyTurnFileLive(tf, sceneDoc, effYAML, proj, tbl, clip, true)
	return nil
}

// RevertAgentTurnChange marks changeID as reverted inside the pending turn and immediately removes
// that change from live in-memory state.
func (e *Editor) RevertAgentTurnChange(changeID string) error {
	if err := e.requirePendingTurnForToggle(); err != nil {
		return err
	}
	tf := e.findTurnFileForChange(changeID)
	if tf == nil {
		return fmt.Errorf("editor: change %q not found in pending turn", changeID)
	}
	if tf.reverted[changeID] {
		return nil
	}
	tf.reverted[changeID] = true
	if err := e.recomputeAndApplyTurnFile(tf); err != nil {
		delete(tf.reverted, changeID)
		return err
	}
	e.notify()
	return nil
}

// RestoreAgentTurnChange restores a previously reverted changeID inside the pending turn and
// immediately brings it back in live in-memory state.
func (e *Editor) RestoreAgentTurnChange(changeID string) error {
	if err := e.requirePendingTurnForToggle(); err != nil {
		return err
	}
	tf := e.findTurnFileForChange(changeID)
	if tf == nil {
		return fmt.Errorf("editor: change %q not found in pending turn", changeID)
	}
	if !tf.reverted[changeID] {
		return nil
	}
	delete(tf.reverted, changeID)
	if err := e.recomputeAndApplyTurnFile(tf); err != nil {
		tf.reverted[changeID] = true
		return err
	}
	e.notify()
	return nil
}

// ToggleAgentTurnChange reverts changeID if active, or restores it if reverted.
func (e *Editor) ToggleAgentTurnChange(changeID string) error {
	if err := e.requirePendingTurnForToggle(); err != nil {
		return err
	}
	tf := e.findTurnFileForChange(changeID)
	if tf == nil {
		return fmt.Errorf("editor: change %q not found in pending turn", changeID)
	}
	if tf.reverted[changeID] {
		return e.RestoreAgentTurnChange(changeID)
	}
	return e.RevertAgentTurnChange(changeID)
}

// RevertAgentTurnFile marks every change in file as reverted and restores that file in memory.
func (e *Editor) RevertAgentTurnFile(file string) error {
	if err := e.requirePendingTurnForToggle(); err != nil {
		return err
	}
	tf := e.findTurnFileByDisplay(file)
	if tf == nil {
		return fmt.Errorf("editor: file %q is not in the pending turn", file)
	}
	for _, ch := range tf.changes() {
		tf.reverted[ch.ID] = true
	}
	if err := e.recomputeAndApplyTurnFile(tf); err != nil {
		return err
	}
	e.notify()
	return nil
}

// RestoreAgentTurnFile restores every reverted change in file in memory.
func (e *Editor) RestoreAgentTurnFile(file string) error {
	if err := e.requirePendingTurnForToggle(); err != nil {
		return err
	}
	tf := e.findTurnFileByDisplay(file)
	if tf == nil {
		return fmt.Errorf("editor: file %q is not in the pending turn", file)
	}
	clear(tf.reverted)
	if err := e.recomputeAndApplyTurnFile(tf); err != nil {
		return err
	}
	e.notify()
	return nil
}

func (e *Editor) findTurnFileForChange(changeID string) *turnFile {
	if e.agentTurn == nil {
		return nil
	}
	for _, key := range e.agentTurn.fileOrder {
		tf := e.agentTurn.files[key]
		if tf == nil {
			continue
		}
		for _, ch := range tf.changes() {
			if ch.ID == changeID {
				return tf
			}
		}
	}
	return nil
}

func (e *Editor) findTurnFileByDisplay(file string) *turnFile {
	if e.agentTurn == nil {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(file), `\`, "/"))
	for _, key := range e.agentTurn.fileOrder {
		tf := e.agentTurn.files[key]
		if tf != nil && (tf.rel == file || tf.rel == clean || tf.displayPath() == file) {
			return tf
		}
	}
	return nil
}

// --- Keep & Revert as ONE Undo Step + Spec Toast ---

type turnCmdFile struct {
	rel           string
	abs           string
	kind          AgentTurnFileKind
	isActiveScene bool
	beforeExisted bool
	beforeRaw     []byte
	beforeYAML    []byte
	afterYAML     []byte
	beforeScene   *sceneformat.SceneDoc
	afterScene    *sceneformat.SceneDoc
	beforeProj    *project.File
	afterProj     *project.File
	beforeTable   *datatable.Document
	afterTable    *datatable.Document
	beforeClip    *resource.Animation
	afterClip     *resource.Animation
}

// agentTurnCmd commits an entire multi-file kept agent turn as ONE History step tagged with AuthorAgent.
type agentTurnCmd struct {
	ed           *Editor
	label        string
	totalChanges int
	keptChanges  int
	files        []turnCmdFile
}

func (c *agentTurnCmd) Label() string         { return c.label }
func (c *agentTurnCmd) Author() CommandAuthor { return AuthorAgent }
func (c *agentTurnCmd) Actor() HistoryActor   { return HistoryActorAgent }
func (c *agentTurnCmd) historyMeta() HistoryMeta {
	return HistoryMeta{
		Author:      HistoryAuthorAgent,
		Summary:     c.label,
		Detail:      formatAgentTurnDetail("", c.totalChanges, c.keptChanges),
		Files:       c.TouchedFiles(),
		ChangeCount: c.totalChanges,
		KeptCount:   c.keptChanges,
	}
}
func (c *agentTurnCmd) TouchedFiles() []string {
	out := make([]string, 0, len(c.files))
	for _, f := range c.files {
		if f.rel != "" {
			out = append(out, f.rel)
		} else {
			out = append(out, "the open scene")
		}
	}
	return out
}

func (c *agentTurnCmd) Do() {
	c.apply(true)
}

func (c *agentTurnCmd) Undo() {
	c.apply(false)
}

func (c *agentTurnCmd) apply(redo bool) {
	ed := c.ed
	if ed == nil {
		return
	}
	for i := range c.files {
		f := &c.files[i]
		targetYAML := f.beforeRaw
		if redo {
			targetYAML = f.afterYAML
		}
		switch f.kind {
		case TurnFileScene, TurnFilePrefab:
			fromDoc, toDoc := f.afterScene, f.beforeScene
			if redo {
				fromDoc, toDoc = f.beforeScene, f.afterScene
			}
			if f.isActiveScene {
				next := applySceneDelta(ed.Doc, fromDoc, toDoc)
				ed.setDoc(next)
				ed.Rebuild()
				ed.syncActiveTab()
			} else if idx := ed.findTabByPath(f.rel, f.abs); idx >= 0 {
				t := ed.tabs[idx]
				t.doc = applySceneDelta(t.doc, fromDoc, toDoc)
				if built, bErr := sceneformat.Build(t.doc, ed.resolverForScene(t.scenePath)); bErr == nil {
					built.DrawAllTiles = true
					t.built = built
					t.buildErr = nil
				} else {
					t.buildErr = bErr
				}
				if idx == ed.activeTab {
					ed.loadActiveTab()
				}
			} else {
				dirty := true
				if !redo && f.beforeExisted && f.abs != "" {
					if disk, err := os.ReadFile(f.abs); err == nil && bytes.Equal(disk, targetYAML) {
						dirty = false
					}
				}
				ed.setStagedFile(f.rel, f.abs, f.kind, targetYAML, f.beforeRaw, toDoc, nil, nil, dirty, false, f.beforeExisted, !redo)
			}

		case TurnFileProject:
			if redo {
				ed.Project = f.afterProj.Clone()
			} else {
				ed.Project = f.beforeProj.Clone()
			}

		case TurnFileDataTable:
			toTbl := f.beforeTable
			if redo {
				toTbl = f.afterTable
			}
			if ed.table != nil && ed.dataTableMatchPath(ed.table.path) == f.rel {
				ed.table.doc = toTbl.Clone()
				ed.table.lastErr = nil
			} else {
				dirty := true
				if !redo && f.beforeExisted && f.abs != "" {
					if disk, err := os.ReadFile(f.abs); err == nil && bytes.Equal(disk, targetYAML) {
						dirty = false
					}
				}
				ed.setStagedFile(f.rel, f.abs, f.kind, targetYAML, f.beforeRaw, nil, toTbl, nil, dirty, false, f.beforeExisted, !redo)
			}

		case TurnFileClip:
			toClip := f.beforeClip
			if redo {
				toClip = f.afterClip
			}
			if ed.clip != nil && ed.clipMeta.path != "" {
				if clipRel, ok := ed.projectRel(ed.clipMeta.path); ok && clipRel == f.rel {
					ed.clip = cloneClip(toClip)
					ed.refreshClipPreview()
					continue
				}
			}
			dirty := true
			if !redo && f.beforeExisted && f.abs != "" {
				if disk, err := os.ReadFile(f.abs); err == nil && bytes.Equal(disk, targetYAML) {
					dirty = false
				}
			}
			ed.setStagedFile(f.rel, f.abs, f.kind, targetYAML, f.beforeRaw, nil, nil, toClip, dirty, false, f.beforeExisted, !redo)

		default:
			dirty := true
			if !redo && f.beforeExisted && f.abs != "" {
				if disk, err := os.ReadFile(f.abs); err == nil && bytes.Equal(disk, targetYAML) {
					dirty = false
				}
			}
			ed.setStagedFile(f.rel, f.abs, f.kind, targetYAML, f.beforeRaw, nil, nil, nil, dirty, false, f.beforeExisted, !redo)
		}
	}
	ed.notify()
}

// KeepAgentTurn commits all non-reverted changes in the pending (or working/stopped) agent turn as ONE
// History command labelled with the request, marks the turn Kept, and displays a Spec toast with Undo.
// Edits remain in memory until the person saves.
func (e *Editor) KeepAgentTurn() error {
	if e == nil || e.agentTurn == nil {
		return errors.New("editor: no pending agent turn to keep")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agentTurn.status != AgentTurnWorking && e.agentTurn.status != AgentTurnPending && e.agentTurn.status != AgentTurnStopped {
		return errors.New("editor: agent turn is not open")
	}

	e.ExitHistoryPreview()

	turn := e.agentTurn
	var cmdFiles []turnCmdFile
	totalCount := 0
	activeCount := 0
	for _, key := range turn.fileOrder {
		tf := turn.files[key]
		if tf == nil {
			continue
		}
		for _, ch := range tf.changes() {
			totalCount++
			if !ch.Reverted {
				activeCount++
			}
		}
		effYAML, sceneDoc, proj, tbl, clip, allReverted, err := tf.effectiveState()
		if err != nil {
			return err
		}
		if allReverted || bytes.Equal(effYAML, tf.beforeYAML) {
			// Clear any pending staged entry for this file.
			if tf.rel != "" {
				delete(e.stagedFiles, tf.rel)
			}
			continue
		}
		// Ensure staged non-open files are promoted from pending -> kept-unsaved so Save/SaveAll persists them.
		if !tf.isActiveScene {
			if st, ok := e.stagedFiles[tf.rel]; ok {
				st.fromPending = false
				st.dirty = true
				st.data = append([]byte(nil), effYAML...)
				if sceneDoc != nil {
					st.sceneDoc = sceneDoc.Clone()
				}
				if tbl != nil {
					st.tableDoc = tbl.Clone()
				}
				if clip != nil {
					st.clipDoc = cloneClip(clip)
				}
			}
		}
		cmdFiles = append(cmdFiles, turnCmdFile{
			rel:           tf.rel,
			abs:           tf.abs,
			kind:          tf.kind,
			isActiveScene: tf.isActiveScene,
			beforeExisted: tf.beforeExisted,
			beforeRaw:     append([]byte(nil), tf.beforeRaw...),
			beforeYAML:    append([]byte(nil), tf.beforeYAML...),
			afterYAML:     append([]byte(nil), effYAML...),
			beforeScene:   tf.beforeScene.Clone(),
			afterScene:    sceneDoc.Clone(),
			beforeProj:    tf.beforeProj.Clone(),
			afterProj:     proj.Clone(),
			beforeTable:   tf.beforeTable.Clone(),
			afterTable:    tbl.Clone(),
			beforeClip:    cloneClip(tf.beforeClip),
			afterClip:     cloneClip(clip),
		})
	}

	turn.status = AgentTurnKept
	turn.reviewOpen = false
	e.lastRevertedTurn = nil

	if len(cmdFiles) > 0 {
		label := turn.prompt
		if label == "" {
			label = "Agent turn"
		}
		cmd := &agentTurnCmd{
			ed:           e,
			label:        label,
			totalChanges: totalCount,
			keptChanges:  activeCount,
			files:        cmdFiles,
		}
		e.History.pushCommitted(cmd)
	}
	e.scheduleAutoSaveIfNeeded()

	fileCount := len(cmdFiles)
	msg := fmt.Sprintf("Kept %d %s across %d %s", activeCount, turnPlural(activeCount, "change", "changes"), fileCount, turnPlural(fileCount, "file", "files"))
	if activeCount == 0 {
		msg = "Kept · no changes"
	} else if fileCount == 1 {
		msg = fmt.Sprintf("Kept %d %s", activeCount, turnPlural(activeCount, "change", "changes"))
	}
	e.Status = msg
	e.ShowToast(Toast{
		ID:             "agent-turn",
		Kind:           ToastSuccess,
		Message:        msg,
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction: func() {
			e.Undo()
			e.DismissToast()
		},
	})
	return nil
}

// RevertAgentTurn restores every touched file back to its exact pre-request state, marks the turn
// Reverted, and displays a Spec toast whose Undo action brings the turn back as pending.
func (e *Editor) RevertAgentTurn() error {
	if e == nil || e.agentTurn == nil {
		return errors.New("editor: no pending agent turn to revert")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agentTurn.status != AgentTurnWorking && e.agentTurn.status != AgentTurnPending && e.agentTurn.status != AgentTurnStopped {
		return errors.New("editor: agent turn is not open")
	}

	turn := e.agentTurn
	snap, _ := e.CurrentAgentTurn()

	// Save full snapshot so the toast's Undo action brings the turn back as pending.
	savedForUndo := turn.clone()
	if savedForUndo.status == AgentTurnWorking {
		savedForUndo.status = AgentTurnPending
	}

	for _, key := range turn.fileOrder {
		tf := turn.files[key]
		if tf == nil {
			continue
		}
		switch tf.kind {
		case TurnFileScene, TurnFilePrefab:
			if tf.isActiveScene {
				next := applySceneDelta(e.Doc, tf.lastAppliedScene, tf.beforeScene)
				tf.lastAppliedScene = tf.beforeScene.Clone()
				e.setDoc(next)
				e.Rebuild()
				e.syncActiveTab()
			} else if idx := e.findTabByPath(tf.rel, tf.abs); idx >= 0 {
				t := e.tabs[idx]
				t.doc = applySceneDelta(t.doc, tf.lastAppliedScene, tf.beforeScene)
				tf.lastAppliedScene = tf.beforeScene.Clone()
				if built, bErr := sceneformat.Build(t.doc, e.resolverForScene(t.scenePath)); bErr == nil {
					built.DrawAllTiles = true
					t.built = built
					t.buildErr = nil
				} else {
					t.buildErr = bErr
				}
				if idx == e.activeTab {
					e.loadActiveTab()
				}
			} else {
				delete(e.stagedFiles, tf.rel)
			}
		case TurnFileProject:
			if tf.beforeProj != nil {
				e.Project = tf.beforeProj.Clone()
			}
		case TurnFileDataTable:
			if e.table != nil && e.dataTableMatchPath(e.table.path) == tf.rel && tf.beforeTable != nil {
				e.table.doc = tf.beforeTable.Clone()
				e.table.lastErr = nil
			} else {
				delete(e.stagedFiles, tf.rel)
			}
		case TurnFileClip:
			if e.clip != nil && e.clipMeta.path != "" {
				if clipRel, ok := e.projectRel(e.clipMeta.path); ok && clipRel == tf.rel && tf.beforeClip != nil {
					e.clip = cloneClip(tf.beforeClip)
					e.refreshClipPreview()
					continue
				}
			}
			delete(e.stagedFiles, tf.rel)
		default:
			delete(e.stagedFiles, tf.rel)
		}
	}

	e.lastRevertedTurn = savedForUndo
	turn.status = AgentTurnReverted
	turn.reviewOpen = false

	msg := fmt.Sprintf("Reverted %d %s", snap.TotalChanges, turnPlural(snap.TotalChanges, "change", "changes"))
	if snap.TotalChanges == 0 {
		msg = "Reverted"
	}
	e.Status = msg
	e.ShowToast(Toast{
		ID:             "agent-turn",
		Kind:           ToastInfo,
		Message:        msg,
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction: func() {
			e.UndoRevertAgentTurn()
		},
	})
	return nil
}

// UndoRevertAgentTurn brings the most recently reverted agent turn back as pending, re-applying all
// non-reverted changes in memory across every touched file.
func (e *Editor) UndoRevertAgentTurn() bool {
	if e == nil || e.playing || e.lastRevertedTurn == nil {
		return false
	}
	restored := e.lastRevertedTurn.clone()
	e.lastRevertedTurn = nil
	e.agentTurn = restored

	for _, key := range restored.fileOrder {
		tf := restored.files[key]
		if tf == nil {
			continue
		}
		effYAML, sceneDoc, proj, tbl, clip, allReverted, err := tf.effectiveState()
		if err != nil {
			continue
		}
		if allReverted {
			continue
		}
		if tf.kind == TurnFileScene || tf.kind == TurnFilePrefab {
			if tf.isActiveScene {
				next := applySceneDelta(e.Doc, tf.beforeScene, sceneDoc)
				tf.lastAppliedScene = sceneDoc.Clone()
				e.setDoc(next)
				e.Rebuild()
				e.syncActiveTab()
				continue
			}
			tf.lastAppliedScene = sceneDoc.Clone()
		}
		e.applyTurnFileLive(tf, sceneDoc, effYAML, proj, tbl, clip, true)
	}
	e.DismissToast()
	e.Status = "Restored pending agent turn"
	e.notify()
	return true
}

// undoForAgentTurn enforces Checkpoints.dc.html's ⌘Z rules:
//   - Agent working: undo only the person's own History edits (agent edits are not in History).
//   - In a review: undo person edits made inside the review first; once none remain, revert the review.
//   - Idle: fall through to ordinary History.Undo() (which undoes either a person step or a kept agent turn).
func (e *Editor) undoForAgentTurn() (handled bool, undone bool) {
	if e == nil || e.agentTurn == nil {
		return false, false
	}
	// If the user is actively in a review (pending/stopped with reviewOpen):
	if e.AgentTurnReviewOpen() {
		// If the person made edits after entering review, undo those edits first.
		if e.History != nil && e.History.Index() > e.agentTurn.reviewHistoryBase {
			return false, false
		}
		// No person edits left inside the review: ⌘Z reverts the review (toast Undo brings it back).
		if err := e.RevertAgentTurn(); err == nil {
			return true, true
		}
	}
	return false, false
}

// --- Pending Turn Prompt (window close, open another scene, open another project) ---

// PendingTurnPromptOpen reports whether the pending-turn Keep/Revert/Cancel confirmation dialog is open.
func (e *Editor) PendingTurnPromptOpen() bool {
	return e != nil && e.pendingTurnPrompt.open
}

// PendingTurnPromptSummary describes the blocked action waiting on Keep, Revert, or Cancel.
func (e *Editor) PendingTurnPromptSummary() string {
	if e == nil || !e.pendingTurnPrompt.open {
		return ""
	}
	return e.pendingTurnPrompt.summary
}

func (e *Editor) askPendingTurn(summary string, proceed, cancel func()) {
	if cancel == nil {
		cancel = func() {}
	}
	if !e.HasPendingAgentTurn() {
		proceed()
		return
	}
	e.pendingTurnPrompt = pendingTurnPromptState{
		open:    true,
		summary: summary,
		proceed: proceed,
		cancel:  cancel,
	}
	e.notify()
}

// RequestOpenScene opens rel in a tab. When no agent turn is pending, it opens immediately (matching
// existing tab behavior). When an agent turn is pending, it asks to Keep, Revert, or Cancel first.
func (e *Editor) RequestOpenScene(rel string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.HasPendingAgentTurn() {
		return e.OpenScene(rel)
	}
	e.askPendingTurn("Open "+rel, func() {
		e.report(e.OpenScene(rel))
	}, nil)
	return nil
}

// ConfirmKeepPendingTurnPrompt keeps the pending agent turn and runs the waiting action.
func (e *Editor) ConfirmKeepPendingTurnPrompt() error {
	if e == nil || !e.pendingTurnPrompt.open {
		return nil
	}
	proceed := e.pendingTurnPrompt.proceed
	e.pendingTurnPrompt = pendingTurnPromptState{}
	if err := e.KeepAgentTurn(); err != nil {
		e.notify()
		return err
	}
	e.notify()
	if proceed != nil {
		proceed()
	}
	return nil
}

// ConfirmRevertPendingTurnPrompt reverts the pending agent turn and runs the waiting action.
func (e *Editor) ConfirmRevertPendingTurnPrompt() error {
	if e == nil || !e.pendingTurnPrompt.open {
		return nil
	}
	proceed := e.pendingTurnPrompt.proceed
	e.pendingTurnPrompt = pendingTurnPromptState{}
	if err := e.RevertAgentTurn(); err != nil {
		e.notify()
		return err
	}
	e.notify()
	if proceed != nil {
		proceed()
	}
	return nil
}

// CancelPendingTurnPrompt dismisses the pending-turn prompt and leaves the turn pending.
func (e *Editor) CancelPendingTurnPrompt() {
	if e == nil || !e.pendingTurnPrompt.open {
		return
	}
	cancel := e.pendingTurnPrompt.cancel
	e.pendingTurnPrompt = pendingTurnPromptState{}
	e.notify()
	if cancel != nil {
		cancel()
	}
}

func (e *Editor) resetAgentTurnForProject() {
	e.agentTurn = nil
	e.lastRevertedTurn = nil
	e.stagedFiles = nil
	e.pendingTurnPrompt = pendingTurnPromptState{}
}
