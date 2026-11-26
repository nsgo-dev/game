package editor

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

const (
	maxRewindCheckpointFileBytes  = 16 * 1024 * 1024 // 16 MiB per file cap
	maxRewindCheckpointTotalBytes = 64 * 1024 * 1024 // 64 MiB per snapshot cap
)

// AgentRewindMode selects what RewindAgentTurn restores when rewinding to before a message.
type AgentRewindMode string

const (
	// AgentRewindBoth restores files from the turns' checkpoints and drops the target and later turns from the thread.
	AgentRewindBoth AgentRewindMode = "both"
	// AgentRewindFilesOnly restores files from the turns' checkpoints while keeping the conversation thread unchanged.
	AgentRewindFilesOnly AgentRewindMode = "files"
	// AgentRewindConversationOnly drops the target and later turns from the thread while keeping all files as they are.
	AgentRewindConversationOnly AgentRewindMode = "conversation"
)

// AgentTurnSummary carries a concise summary of a prior turn still in the thread when dispatching the next AgentRequest.
type AgentTurnSummary struct {
	ID           string
	Prompt       string
	Summary      string
	ChangedFiles []string
}

// AgentRewindPreview describes the files and replies that rewinding to before a message will affect.
type AgentRewindPreview struct {
	TurnID       string
	TurnIndex    int
	Prompt       string
	ChangedFiles []string
	DroppedTurns int
}

// AgentRewindState describes the most recent rewind shown in the post-rewind banner above the composer with Redo.
type AgentRewindState struct {
	TurnID        string
	TurnIndex     int
	Mode          AgentRewindMode
	Prompt        string
	FilesRestored []string
	TurnsDropped  int
}

type fileSnapshot struct {
	exists bool
	data   []byte
	mode   fs.FileMode
	hash   [sha256.Size]byte
}

func (s fileSnapshot) equal(other fileSnapshot) bool {
	if s.exists != other.exists {
		return false
	}
	if !s.exists {
		return true
	}
	return s.hash == other.hash && bytes.Equal(s.data, other.data)
}

type agentTurnCheckpoint struct {
	root string

	// fullBefore holds the complete directory scan while a turn is running;
	// finalizeTurnCheckpoint compacts it into beforeFiles and clears it.
	fullBefore map[string]fileSnapshot

	beforeFiles map[string]fileSnapshot

	beforeDoc          *sceneformat.SceneDoc
	beforeDocYAML      []byte
	beforeSceneSaved   []byte
	beforeFilePath     string
	docChanged         bool
	beforeProject      *project.File
	beforeProjectYAML  []byte
	beforeProjectSaved []byte
	projectChanged     bool

	finalized bool
}

type agentRewindRecord struct {
	state                   AgentRewindState
	root                    string
	previousComposerText    string
	previousTurns           []AgentTurn
	previousSessionID       string
	previousCheckpoints     map[string]*agentTurnCheckpoint
	previousLiveStopped     bool
	threadID                string
	previousThreadSessionID string
	hadThreadSession        bool

	redoFiles          map[string]fileSnapshot
	hadDocRestore      bool
	redoDoc            *sceneformat.SceneDoc
	redoSceneSaved     []byte
	redoFilePath       string
	hadProjectRestore  bool
	redoProject        *project.File
	redoProjectSaved   []byte
	filesRestoredCount int
}

func sameCleanPath(a, b string) bool {
	if a == "" && b == "" {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func skipCheckpointDir(name string) bool {
	switch name {
	case ".git", ".nsgo", "node_modules", ".svn", ".hg":
		return true
	default:
		return false
	}
}

func captureProjectFileTree(root string) map[string]fileSnapshot {
	if root == "" {
		return nil
	}
	cleanRoot := filepath.Clean(root)
	info, err := os.Stat(cleanRoot)
	if err != nil || !info.IsDir() {
		return nil
	}
	out := make(map[string]fileSnapshot)
	var totalBytes int64
	_ = filepath.WalkDir(cleanRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if p == cleanRoot {
			return nil
		}
		// Never follow symlinks when snapshotting checkpoints.
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if skipCheckpointDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() > maxRewindCheckpointFileBytes || totalBytes+fi.Size() > maxRewindCheckpointTotalBytes {
			return nil
		}
		rel, err := filepath.Rel(cleanRoot, p)
		if err != nil {
			return nil
		}
		relSlash := filepath.ToSlash(filepath.Clean(rel))
		if relSlash == "." || relSlash == ".." || strings.HasPrefix(relSlash, "../") || path.IsAbs(relSlash) {
			return nil
		}
		// Never snapshot You-only configuration files (permissions, tools, models, usage).
		if _, isYouOnly := MatchYouOnlyPath(relSlash); isYouOnly {
			return nil
		}
		data, err := readCheckpointFileBounded(p, maxRewindCheckpointFileBytes)
		if err != nil {
			return nil
		}
		if int64(len(data)) > maxRewindCheckpointFileBytes || totalBytes+int64(len(data)) > maxRewindCheckpointTotalBytes {
			return nil
		}
		totalBytes += int64(len(data))
		mode := fi.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		out[relSlash] = fileSnapshot{
			exists: true,
			data:   data,
			mode:   mode,
			hash:   sha256.Sum256(data),
		}
		return nil
	})
	return out
}

func (e *Editor) captureTurnStartCheckpoint() *agentTurnCheckpoint {
	if e == nil {
		return nil
	}
	root := e.agentProjectRoot()
	cp := &agentTurnCheckpoint{
		root:       root,
		fullBefore: captureProjectFileTree(root),
	}
	if e.Doc != nil {
		cp.beforeDoc = e.Doc.Clone()
		cp.beforeFilePath = e.FilePath
		cp.beforeSceneSaved = slices.Clone(e.sceneSaved)
		if data, err := e.SaveYAML(); err == nil {
			cp.beforeDocYAML = data
		}
	}
	if e.Project != nil {
		cp.beforeProject = e.Project.Clone()
		cp.beforeProjectSaved = slices.Clone(e.projectSaved)
		if data, err := project.Encode(e.Project); err == nil {
			cp.beforeProjectYAML = data
		}
	}
	return cp
}

func (e *Editor) finalizeTurnCheckpoint(turnID string) {
	if e == nil || turnID == "" {
		return
	}
	turn := e.findAgentTurn(turnID)
	if turn != nil {
		turn.SessionIDAfter = e.agent.sessionID
	}
	if e.agent.checkpoints == nil {
		return
	}
	cp := e.agent.checkpoints[turnID]
	if cp == nil || cp.finalized {
		return
	}
	cp.finalized = true

	var changed []string
	seen := make(map[string]bool)
	addChanged := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		changed = append(changed, p)
	}

	if cp.root != "" {
		curFiles := captureProjectFileTree(cp.root)
		cp.beforeFiles = make(map[string]fileSnapshot)
		for rel, before := range cp.fullBefore {
			after := curFiles[rel]
			if !before.equal(after) {
				cp.beforeFiles[rel] = before
				addChanged(rel)
			}
		}
		for rel := range curFiles {
			if _, existed := cp.fullBefore[rel]; !existed {
				cp.beforeFiles[rel] = fileSnapshot{exists: false}
				addChanged(rel)
			}
		}
		cp.fullBefore = nil

		// If the turn updated the open scene or game.yaml directly on disk while clean in memory,
		// sync the in-memory document now so the viewport updates immediately and a delayed disk-watch
		// reload cannot bleed into the next turn's checkpoint.
		if e.FilePath != "" && e.isPathAffected(cp.root, e.FilePath, cp.beforeFiles) && !e.SceneDirty() {
			if data, err := os.ReadFile(e.FilePath); err == nil {
				if doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
					File:          e.FilePath,
					Components:    e.components,
					PhysicsLayers: e.PhysicsLayers(),
				}); err == nil {
					e.setDoc(doc)
					if enc, err := sceneformat.Encode(doc); err == nil {
						e.sceneSaved = enc
					}
					e.recordSceneDiskSnapshot(e.FilePath, data)
					e.Rebuild()
					e.pruneSelection()
					e.syncActiveTab()
				}
			}
		}
		if e.ProjectPath != "" && e.isPathAffected(cp.root, e.ProjectPath, cp.beforeFiles) && !e.ProjectDirty() {
			if data, err := os.ReadFile(e.ProjectPath); err == nil {
				if f, err := project.Parse(data); err == nil {
					e.Project = f
					if enc, err := project.Encode(f); err == nil {
						e.projectSaved = enc
					}
					e.syncAudioMixer()
					e.recordProjectDiskSnapshot(e.ProjectPath, data)
				}
			}
		}
	}

	if cp.beforeDoc != nil && e.Doc != nil && sameCleanPath(e.FilePath, cp.beforeFilePath) {
		if curYAML, err := e.SaveYAML(); err == nil && !bytes.Equal(curYAML, cp.beforeDocYAML) {
			cp.docChanged = true
			sceneRel := e.ScenePath
			if sceneRel == "" && e.FilePath != "" && cp.root != "" {
				if rel, err := filepath.Rel(cp.root, e.FilePath); err == nil {
					relSlash := filepath.ToSlash(filepath.Clean(rel))
					if relSlash != "." && !strings.HasPrefix(relSlash, "../") {
						sceneRel = relSlash
					}
				}
			}
			if sceneRel == "" {
				sceneRel = e.SceneLabel()
			}
			addChanged(sceneRel)
		}
	}
	cp.beforeDocYAML = nil

	if cp.beforeProject != nil && e.Project != nil {
		if curProjYAML, err := project.Encode(e.Project); err == nil && !bytes.Equal(curProjYAML, cp.beforeProjectYAML) {
			cp.projectChanged = true
			addChanged(project.FileName)
		}
	}
	cp.beforeProjectYAML = nil

	slices.Sort(changed)
	if turn != nil {
		turn.ChangedFiles = changed
	}
	if len(changed) > 0 {
		e.filesRev++
	}
}

func (e *Editor) agentPriorTurnSummaries() []AgentTurnSummary {
	if e == nil || len(e.agent.turns) == 0 {
		return nil
	}
	out := make([]AgentTurnSummary, 0, len(e.agent.turns))
	for _, t := range e.agent.turns {
		out = append(out, AgentTurnSummary{
			ID:           t.ID,
			Prompt:       t.Prompt,
			Summary:      t.Summary,
			ChangedFiles: slices.Clone(t.ChangedFiles),
		})
	}
	return out
}

func (e *Editor) findAgentTurnIndex(turnID string) int {
	if e == nil || turnID == "" {
		return -1
	}
	for i := range e.agent.turns {
		if e.agent.turns[i].ID == turnID {
			return i
		}
	}
	return -1
}

// AgentRewindOpenTurnID returns the turn ID whose rewind popover is currently open, or empty.
func (e *Editor) AgentRewindOpenTurnID() string {
	if e == nil {
		return ""
	}
	return e.agent.rewindOpenTurnID
}

// AgentRewindMode returns the currently selected radio option in the rewind popover.
func (e *Editor) AgentRewindMode() AgentRewindMode {
	if e == nil || e.agent.rewindMode == "" {
		return AgentRewindBoth
	}
	return e.agent.rewindMode
}

// SetAgentRewindMode selects one of the three choices in the rewind popover.
func (e *Editor) SetAgentRewindMode(mode AgentRewindMode) {
	if e == nil {
		return
	}
	switch mode {
	case AgentRewindBoth, AgentRewindFilesOnly, AgentRewindConversationOnly:
	default:
		return
	}
	if e.agent.rewindMode == mode {
		return
	}
	e.agent.rewindMode = mode
	e.notify()
}

// OpenAgentRewindPopover opens the "Rewind to before this message" popover for turnID.
func (e *Editor) OpenAgentRewindPopover(turnID string) bool {
	if e == nil || e.playing || e.AgentWorking() || e.IsPlanRunning() || e.agent.mode == AgentModeNone {
		return false
	}
	if e.findAgentTurnIndex(turnID) < 0 {
		return false
	}
	if e.agent.rewindOpenTurnID != turnID {
		e.agent.rewindMode = AgentRewindBoth
	}
	if e.agentThreads.switcherOpen {
		e.agentThreads.switcherOpen = false
		e.agentThreads.switcherQuery = ""
		e.agentThreads.rev++
	}
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	e.agent.rewindOpenTurnID = turnID
	e.notify()
	return true
}

// ToggleAgentRewindPopover opens the rewind popover for turnID, or closes it if already open for turnID.
func (e *Editor) ToggleAgentRewindPopover(turnID string) bool {
	if e == nil {
		return false
	}
	if e.agent.rewindOpenTurnID == turnID && turnID != "" {
		e.CloseAgentRewindPopover()
		return false
	}
	return e.OpenAgentRewindPopover(turnID)
}

// CloseAgentRewindPopover closes the rewind popover without rewinding.
func (e *Editor) CloseAgentRewindPopover() {
	if e == nil || e.agent.rewindOpenTurnID == "" {
		return
	}
	e.agent.rewindOpenTurnID = ""
	e.notify()
}

// AgentRewindPreview computes the distinct files and dropped turn count for rewinding to before turnID.
func (e *Editor) AgentRewindPreview(turnID string) (AgentRewindPreview, bool) {
	if e == nil {
		return AgentRewindPreview{}, false
	}
	idx := e.findAgentTurnIndex(turnID)
	if idx < 0 {
		return AgentRewindPreview{}, false
	}
	seen := make(map[string]bool)
	var files []string
	for j := idx; j < len(e.agent.turns); j++ {
		t := e.agent.turns[j]
		for _, f := range t.ChangedFiles {
			if f != "" && !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
		if e.agent.checkpoints != nil {
			if cp := e.agent.checkpoints[t.ID]; cp != nil {
				for rel := range cp.beforeFiles {
					if rel != "" && !seen[rel] {
						seen[rel] = true
						files = append(files, rel)
					}
				}
			}
		}
	}
	slices.Sort(files)
	return AgentRewindPreview{
		TurnID:       turnID,
		TurnIndex:    idx,
		Prompt:       e.agent.turns[idx].Prompt,
		ChangedFiles: files,
		DroppedTurns: len(e.agent.turns) - idx,
	}, true
}

// AgentRewindBanner returns the active post-rewind banner state (shown above the composer with Redo), if any.
func (e *Editor) AgentRewindBanner() (AgentRewindState, bool) {
	if e == nil || e.agent.lastRewind == nil {
		return AgentRewindState{}, false
	}
	cp := e.agent.lastRewind.state
	cp.FilesRestored = slices.Clone(cp.FilesRestored)
	return cp, true
}

// CanRedoAgentRewind reports whether a rewound turn can currently be restored with RedoAgentRewind.
func (e *Editor) CanRedoAgentRewind() bool {
	return e != nil && e.agent.lastRewind != nil && !e.playing && !e.AgentWorking() && !e.IsPlanRunning() && e.agent.mode != AgentModeNone
}

// DismissAgentRewindBanner hides the post-rewind banner without redoing.
func (e *Editor) DismissAgentRewindBanner() {
	if e == nil || e.agent.lastRewind == nil {
		return
	}
	e.agent.lastRewind = nil
	e.agent.composerHighlighted = false
	e.notify()
}

// AgentComposerRewound reports whether the composer currently holds a freshly rewound message highlighted with the Spec accent ring.
func (e *Editor) AgentComposerRewound() bool {
	return e != nil && e.agent.composerHighlighted
}

// ConfirmAgentRewind executes the rewind selected in the currently open rewind popover.
func (e *Editor) ConfirmAgentRewind() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.agent.rewindOpenTurnID == "" {
		return errors.New("editor: no rewind popover is open")
	}
	return e.RewindAgentTurn(e.agent.rewindOpenTurnID, e.AgentRewindMode())
}

// RewindAgentTurn rewinds the active agent thread to before turnID using mode:
//   - AgentRewindBoth restores files from the turns' checkpoints and drops the target and later turns (restoring the session ID from before turnID).
//   - AgentRewindFilesOnly restores files from the turns' checkpoints while keeping the conversation thread and session unchanged.
//   - AgentRewindConversationOnly drops the target and later turns (and restores the session ID from before turnID) while keeping all files untouched.
//
// In all modes, the rewound message returns to the composer and a post-rewind banner with Redo is shown.
func (e *Editor) RewindAgentTurn(turnID string, mode AgentRewindMode) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	if e.AgentWorking() || e.IsPlanRunning() {
		return errors.New("editor: cannot rewind while the agent is working")
	}
	switch mode {
	case AgentRewindBoth, AgentRewindFilesOnly, AgentRewindConversationOnly:
	default:
		return fmt.Errorf("editor: unknown rewind mode %q", mode)
	}
	idx := e.findAgentTurnIndex(turnID)
	if idx < 0 {
		return fmt.Errorf("editor: unknown agent turn %q", turnID)
	}

	preview, _ := e.AgentRewindPreview(turnID)
	targetTurn := e.agent.turns[idx]

	restoreFiles := mode == AgentRewindBoth || mode == AgentRewindFilesOnly
	restoreConv := mode == AgentRewindBoth || mode == AgentRewindConversationOnly

	// Collect the earliest pre-turn file & document snapshots across turns [idx..len-1].
	var root string
	targetBeforeFiles := make(map[string]fileSnapshot)
	var beforeDoc *sceneformat.SceneDoc
	var beforeSceneSaved []byte
	var beforeFilePath string
	var docChanged bool
	var beforeProject *project.File
	var beforeProjectSaved []byte
	var projectChanged bool

	for j := idx; j < len(e.agent.turns); j++ {
		if e.agent.checkpoints == nil {
			continue
		}
		cp := e.agent.checkpoints[e.agent.turns[j].ID]
		if cp == nil {
			continue
		}
		if root == "" && cp.root != "" {
			root = cp.root
		}
		for rel, snap := range cp.beforeFiles {
			if _, exists := targetBeforeFiles[rel]; !exists {
				targetBeforeFiles[rel] = snap
			}
		}
		if cp.docChanged && !docChanged && cp.beforeDoc != nil && sameCleanPath(e.FilePath, cp.beforeFilePath) {
			docChanged = true
			beforeDoc = cp.beforeDoc.Clone()
			beforeSceneSaved = slices.Clone(cp.beforeSceneSaved)
			beforeFilePath = cp.beforeFilePath
		}
		if cp.projectChanged && !projectChanged && cp.beforeProject != nil {
			projectChanged = true
			beforeProject = cp.beforeProject.Clone()
			beforeProjectSaved = slices.Clone(cp.beforeProjectSaved)
		}
	}
	if root == "" {
		root = e.agentProjectRoot()
	}

	// Clone current thread state for Redo before mutating anything.
	prevDraft := e.composerDraft
	if prevDraft == "" {
		prevDraft = e.agent.composerText
	}
	rec := &agentRewindRecord{
		state: AgentRewindState{
			TurnID:        turnID,
			TurnIndex:     idx,
			Mode:          mode,
			Prompt:        targetTurn.Prompt,
			FilesRestored: slices.Clone(preview.ChangedFiles),
			TurnsDropped:  preview.DroppedTurns,
		},
		root:                 root,
		previousComposerText: prevDraft,
		previousTurns:        e.AgentTurns(),
		previousSessionID:    e.agent.sessionID,
		previousCheckpoints:  cloneCheckpointMap(e.agent.checkpoints),
		previousLiveStopped:  e.agent.liveStopped,
	}

	if restoreFiles {
		// Snapshot the current disk state of every affected file so Redo can restore it byte for byte.
		if root != "" && len(targetBeforeFiles) > 0 {
			redoFiles, err := snapshotDiskFiles(root, targetBeforeFiles)
			if err != nil {
				return err
			}
			rec.redoFiles = redoFiles
		}
		if e.Doc != nil && (docChanged || e.isPathAffected(root, e.FilePath, targetBeforeFiles)) {
			rec.hadDocRestore = true
			rec.redoDoc = e.Doc.Clone()
			rec.redoSceneSaved = slices.Clone(e.sceneSaved)
			rec.redoFilePath = e.FilePath
		}
		if e.Project != nil && (projectChanged || e.isPathAffected(root, e.ProjectPath, targetBeforeFiles)) {
			rec.hadProjectRestore = true
			rec.redoProject = e.Project.Clone()
			rec.redoProjectSaved = slices.Clone(e.projectSaved)
		}

		// Apply file checkpoint restoration to disk atomically (with rollback on error).
		if root != "" && len(targetBeforeFiles) > 0 {
			if err := applyFileSnapshots(root, targetBeforeFiles, rec.redoFiles); err != nil {
				return err
			}
			rec.filesRestoredCount = len(targetBeforeFiles)
		}

		if e.HistoryPreviewActive() {
			e.ExitHistoryPreview()
		}

		// Keep open scene, inactive tabs, project, and file browser in sync with the restored files.
		e.syncEditorAfterFileRestore(root, targetBeforeFiles, docChanged, beforeFilePath, beforeDoc, beforeSceneSaved, projectChanged, beforeProject, beforeProjectSaved)
	} else {
		rec.state.FilesRestored = nil
	}

	if restoreConv {
		droppedTurnIDs := make(map[string]bool, len(e.agent.turns)-idx)
		droppedPlanIDs := make(map[string]bool)
		for j := idx; j < len(e.agent.turns); j++ {
			id := e.agent.turns[j].ID
			droppedTurnIDs[id] = true
			if e.agent.turns[j].Plan != nil && e.agent.turns[j].Plan.PlanID != "" {
				droppedPlanIDs[e.agent.turns[j].Plan.PlanID] = true
			}
			if e.agent.checkpoints != nil {
				delete(e.agent.checkpoints, id)
			}
			if e.agent.pointingTurnID == id {
				e.agent.pointingTurnID = ""
			}
			if e.agent.openReviewTurnID == id {
				e.agent.openReviewTurnID = ""
			}
		}
		e.clearActivePlanForRewind(droppedTurnIDs, droppedPlanIDs)
		e.agent.turns = e.agent.turns[:idx]
		e.agent.sessionID = targetTurn.SessionIDBefore
		if targetTurn.ThreadID != "" {
			if t := e.findPersistedAgentThread(targetTurn.ThreadID); t != nil {
				rec.threadID = t.ID
				rec.previousThreadSessionID = t.SessionID
				rec.hadThreadSession = true
				if t.SessionID != targetTurn.SessionIDBefore {
					t.SessionID = targetTurn.SessionIDBefore
					_ = e.persistAgentThread(t)
					e.agentThreads.rev++
				}
			} else if e.agentThreads.draftThread != nil && e.agentThreads.draftThread.ID == targetTurn.ThreadID {
				rec.threadID = e.agentThreads.draftThread.ID
				rec.previousThreadSessionID = e.agentThreads.draftThread.SessionID
				rec.hadThreadSession = true
				if e.agentThreads.draftThread.SessionID != targetTurn.SessionIDBefore {
					e.agentThreads.draftThread.SessionID = targetTurn.SessionIDBefore
					e.agentThreads.rev++
				}
			}
		}
		if e.agent.openRequest != nil {
			e.agent.openRequest = nil
		}
		e.agent.liveStopped = false
		e.agent.liveDeniedKeys = nil
		e.agent.liveQueuedFollowUps = nil
		e.agent.liveActivity = ""
		e.restoreIdleAgentStatus()
	} else {
		rec.state.TurnsDropped = 0
		if restoreFiles {
			for j := idx; j < len(e.agent.turns); j++ {
				if e.agent.turns[j].PendingReview != nil && !e.agent.turns[j].PendingReview.Reverted {
					e.agent.turns[j].PendingReview.Kept = false
					e.agent.turns[j].PendingReview.Reverted = true
				}
				if e.agent.turns[j].Review != nil && !e.agent.turns[j].Review.Discarded {
					e.agent.turns[j].Review.Kept = false
					e.agent.turns[j].Review.Discarded = true
				}
			}
		}
	}

	if targetTurn.Prompt != "" {
		e.composerDraft = targetTurn.Prompt
		e.agent.composerText = targetTurn.Prompt
		e.agent.composerHighlighted = true
		e.agentComposer.pickerOpen = false
		e.agentComposer.pickerOpenedByAt = false
		e.agentComposer.pickerQuery = ""
		e.agentComposer.pickerActiveIndex = 0
		e.agentComposer.breakdownOpen = false
		e.agentComposer.hoveredChipID = ""
	} else {
		e.agent.composerHighlighted = false
	}
	e.agent.rewindOpenTurnID = ""
	e.agent.lastRewind = rec
	e.agent.turnChangeSeq++
	e.agent.liveAnnouncement = e.formatRewindBannerText(rec.state)
	e.Status = e.agent.liveAnnouncement
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

// RedoAgentRewind restores the files, conversation turns, session ID, and composer draft from the last RewindAgentTurn.
func (e *Editor) RedoAgentRewind() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if e.agent.mode == AgentModeNone {
		return errors.New("editor: agent is disabled")
	}
	if e.AgentWorking() || e.IsPlanRunning() {
		return errors.New("editor: cannot redo rewind while the agent is working")
	}
	rec := e.agent.lastRewind
	if rec == nil {
		return errors.New("editor: nothing to redo")
	}

	restoreFiles := rec.state.Mode == AgentRewindBoth || rec.state.Mode == AgentRewindFilesOnly
	restoreConv := rec.state.Mode == AgentRewindBoth || rec.state.Mode == AgentRewindConversationOnly

	if restoreFiles {
		if rec.root != "" && len(rec.redoFiles) > 0 {
			rollback, err := snapshotDiskFiles(rec.root, rec.redoFiles)
			if err != nil {
				return err
			}
			if err := applyFileSnapshots(rec.root, rec.redoFiles, rollback); err != nil {
				return err
			}
		}
		if e.HistoryPreviewActive() {
			e.ExitHistoryPreview()
		}
		e.syncEditorAfterFileRestore(
			rec.root,
			rec.redoFiles,
			rec.hadDocRestore,
			rec.redoFilePath,
			rec.redoDoc,
			rec.redoSceneSaved,
			rec.hadProjectRestore,
			rec.redoProject,
			rec.redoProjectSaved,
		)
	}

	if restoreConv {
		e.agent.turns = cloneAgentTurns(rec.previousTurns)
		e.agent.sessionID = rec.previousSessionID
		e.agent.checkpoints = cloneCheckpointMap(rec.previousCheckpoints)
		e.agent.liveStopped = rec.previousLiveStopped
		if rec.hadThreadSession && rec.threadID != "" {
			if t := e.findPersistedAgentThread(rec.threadID); t != nil {
				if t.SessionID != rec.previousThreadSessionID {
					t.SessionID = rec.previousThreadSessionID
					_ = e.persistAgentThread(t)
					e.agentThreads.rev++
				}
			} else if e.agentThreads.draftThread != nil && e.agentThreads.draftThread.ID == rec.threadID {
				if e.agentThreads.draftThread.SessionID != rec.previousThreadSessionID {
					e.agentThreads.draftThread.SessionID = rec.previousThreadSessionID
					e.agentThreads.rev++
				}
			}
		}
		e.restoreIdleAgentStatus()
	} else if restoreFiles {
		prevByID := make(map[string]AgentTurn, len(rec.previousTurns))
		for _, pt := range rec.previousTurns {
			prevByID[pt.ID] = pt
		}
		for j := rec.state.TurnIndex; j >= 0 && j < len(e.agent.turns); j++ {
			if prev, ok := prevByID[e.agent.turns[j].ID]; ok {
				cp := cloneAgentTurn(prev)
				e.agent.turns[j].PendingReview = cp.PendingReview
				e.agent.turns[j].Review = cp.Review
			}
		}
	}

	e.composerDraft = rec.previousComposerText
	e.agent.composerText = rec.previousComposerText
	e.agent.composerHighlighted = false
	e.agentComposer.pickerOpen = false
	e.agentComposer.pickerOpenedByAt = false
	e.agentComposer.pickerQuery = ""
	e.agentComposer.pickerActiveIndex = 0
	e.agentComposer.breakdownOpen = false
	e.agentComposer.hoveredChipID = ""
	e.agent.rewindOpenTurnID = ""
	e.agent.lastRewind = nil
	e.agent.turnChangeSeq++
	e.updateAgentAnnouncement()
	e.Status = e.T("agent.rewind.redidStatus")
	e.scheduleAutoSaveIfNeeded()
	e.notify()
	return nil
}

func cloneAgentTurns(turns []AgentTurn) []AgentTurn {
	if len(turns) == 0 {
		return nil
	}
	out := make([]AgentTurn, len(turns))
	for i, t := range turns {
		out[i] = cloneAgentTurn(t)
	}
	return out
}

func cloneCheckpointMap(m map[string]*agentTurnCheckpoint) map[string]*agentTurnCheckpoint {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]*agentTurnCheckpoint, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func readCheckpointFileBounded(abs string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("editor: checkpoint file %q exceeds %d byte limit", filepath.Base(abs), maxBytes)
	}
	return data, nil
}

func resolveSafeCheckpointPath(root, relSlash string) (string, error) {
	if root == "" {
		return "", errors.New("editor: checkpoint root is empty")
	}
	if strings.ContainsAny(relSlash, "\\\x00") {
		return "", fmt.Errorf("editor: invalid checkpoint path %q", relSlash)
	}
	cleanRoot := filepath.Clean(root)
	cleanRel := path.Clean(relSlash)
	if cleanRel == "" || cleanRel == "." || cleanRel == ".." || strings.HasPrefix(cleanRel, "../") || path.IsAbs(cleanRel) {
		return "", fmt.Errorf("editor: invalid checkpoint path %q", relSlash)
	}
	parts := strings.Split(cleanRel, "/")
	for _, seg := range parts {
		if skipCheckpointDir(seg) {
			return "", fmt.Errorf("editor: refusing to touch protected path %q", relSlash)
		}
	}
	if _, isYouOnly := MatchYouOnlyPath(cleanRel); isYouOnly {
		return "", fmt.Errorf("editor: refusing to touch You-only file %q", relSlash)
	}
	// Verify no existing ancestor component or target inside root is a symlink.
	cur := cleanRoot
	for _, seg := range parts {
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				break
			}
			return "", err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("editor: refusing to follow symlink at %q", relSlash)
		}
	}
	abs := filepath.Join(cleanRoot, filepath.FromSlash(cleanRel))
	relCheck, err := filepath.Rel(cleanRoot, abs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("editor: checkpoint path %q escapes project root", relSlash)
	}
	if resolvedRoot, ok := resolvePathThroughExistingAncestors(cleanRoot); ok {
		resolvedAbs, okAbs := resolvePathThroughExistingAncestors(abs)
		if !okAbs {
			return "", fmt.Errorf("editor: cannot resolve checkpoint path %q", relSlash)
		}
		realRel, relErr := filepath.Rel(resolvedRoot, resolvedAbs)
		if relErr != nil || realRel == "." || realRel == ".." || strings.HasPrefix(realRel, ".."+string(os.PathSeparator)) {
			return "", fmt.Errorf("editor: checkpoint path %q escapes project root", relSlash)
		}
	}
	return abs, nil
}

func snapshotDiskFiles(root string, keys map[string]fileSnapshot) (map[string]fileSnapshot, error) {
	out := make(map[string]fileSnapshot, len(keys))
	var totalBytes int64
	for rel := range keys {
		abs, err := resolveSafeCheckpointPath(root, rel)
		if err != nil {
			return nil, err
		}
		fi, err := os.Lstat(abs)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				out[rel] = fileSnapshot{exists: false}
				continue
			}
			return nil, err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("editor: refusing to snapshot symlink %q", rel)
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("editor: refusing to snapshot non-regular file %q", rel)
		}
		if fi.Size() > maxRewindCheckpointFileBytes || totalBytes+fi.Size() > maxRewindCheckpointTotalBytes {
			return nil, fmt.Errorf("editor: file %q exceeds checkpoint size limit", rel)
		}
		data, err := readCheckpointFileBounded(abs, maxRewindCheckpointFileBytes)
		if err != nil {
			return nil, err
		}
		if totalBytes+int64(len(data)) > maxRewindCheckpointTotalBytes {
			return nil, fmt.Errorf("editor: rewind snapshot exceeds %d byte total limit", maxRewindCheckpointTotalBytes)
		}
		totalBytes += int64(len(data))
		mode := fi.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		out[rel] = fileSnapshot{
			exists: true,
			data:   data,
			mode:   mode,
			hash:   sha256.Sum256(data),
		}
	}
	return out, nil
}

func applyFileSnapshots(root string, target, rollback map[string]fileSnapshot) error {
	cleanRoot := filepath.Clean(root)
	rels := make([]string, 0, len(target))
	for rel := range target {
		rels = append(rels, rel)
	}
	slices.Sort(rels)

	applied := make([]string, 0, len(rels))
	rollbackApplied := func() {
		for i := len(applied) - 1; i >= 0; i-- {
			rel := applied[i]
			orig, ok := rollback[rel]
			if !ok {
				continue
			}
			abs, err := resolveSafeCheckpointPath(cleanRoot, rel)
			if err != nil {
				continue
			}
			if orig.exists {
				_ = os.MkdirAll(filepath.Dir(abs), 0o755)
				_ = writeFileAtomic(abs, orig.data)
				if orig.mode != 0 {
					_ = os.Chmod(abs, orig.mode)
				}
			} else {
				if fi, lerr := os.Lstat(abs); lerr == nil && !fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
					_ = os.Remove(abs)
					pruneEmptyParents(cleanRoot, filepath.Dir(abs))
				}
			}
		}
	}

	for _, rel := range rels {
		snap := target[rel]
		abs, err := resolveSafeCheckpointPath(cleanRoot, rel)
		if err != nil {
			rollbackApplied()
			return err
		}
		if snap.exists {
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				rollbackApplied()
				return err
			}
			if err := writeFileAtomic(abs, snap.data); err != nil {
				rollbackApplied()
				return err
			}
			if snap.mode != 0 {
				_ = os.Chmod(abs, snap.mode)
			}
		} else {
			if fi, lerr := os.Lstat(abs); lerr == nil {
				if fi.IsDir() {
					rollbackApplied()
					return fmt.Errorf("editor: refusing to remove directory at checkpoint file path %q", rel)
				}
				if fi.Mode()&os.ModeSymlink != 0 {
					rollbackApplied()
					return fmt.Errorf("editor: refusing to remove symlink at checkpoint file path %q", rel)
				}
			} else if !errors.Is(lerr, os.ErrNotExist) {
				rollbackApplied()
				return lerr
			}
			if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackApplied()
				return err
			}
			pruneEmptyParents(cleanRoot, filepath.Dir(abs))
		}
		applied = append(applied, rel)
	}
	return nil
}

func pruneEmptyParents(root, dir string) {
	cleanRoot := filepath.Clean(root)
	cur := filepath.Clean(dir)
	for cur != cleanRoot && strings.HasPrefix(cur, cleanRoot+string(os.PathSeparator)) {
		fi, err := os.Lstat(cur)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return
		}
		entries, err := os.ReadDir(cur)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(cur); err != nil {
			return
		}
		cur = filepath.Dir(cur)
	}
}

func (e *Editor) isPathAffected(root, absPath string, files map[string]fileSnapshot) bool {
	if root == "" || absPath == "" || len(files) == 0 {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(absPath))
	if err != nil {
		return false
	}
	relSlash := filepath.ToSlash(filepath.Clean(rel))
	_, ok := files[relSlash]
	return ok
}

func (e *Editor) syncEditorAfterFileRestore(
	root string,
	restoredFiles map[string]fileSnapshot,
	docChanged bool,
	expectedFilePath string,
	fallbackDoc *sceneformat.SceneDoc,
	fallbackSceneSaved []byte,
	projectChanged bool,
	fallbackProject *project.File,
	fallbackProjectSaved []byte,
) {
	if e == nil {
		return
	}
	// Reload open project file if it changed on disk or in memory.
	if e.ProjectPath != "" && e.isPathAffected(root, e.ProjectPath, restoredFiles) {
		if data, err := os.ReadFile(e.ProjectPath); err == nil {
			if f, err := project.Parse(data); err == nil {
				e.Project = f
				if enc, err := project.Encode(f); err == nil {
					e.projectSaved = enc
				}
				e.syncAudioMixer()
				e.recordProjectDiskSnapshot(e.ProjectPath, data)
			}
		}
	} else if projectChanged && fallbackProject != nil {
		e.Project = fallbackProject.Clone()
		e.projectSaved = slices.Clone(fallbackProjectSaved)
		e.syncAudioMixer()
	}

	// Reload active open scene if its file was restored on disk, or restore in-memory doc snapshot
	// when the same scene is still open.
	sceneReloaded := false
	if e.FilePath != "" && e.isPathAffected(root, e.FilePath, restoredFiles) {
		if data, err := os.ReadFile(e.FilePath); err == nil {
			if doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
				File:          e.FilePath,
				Components:    e.components,
				PhysicsLayers: e.PhysicsLayers(),
			}); err == nil {
				e.setDoc(doc)
				if enc, err := sceneformat.Encode(doc); err == nil {
					e.sceneSaved = enc
				}
				e.recordSceneDiskSnapshot(e.FilePath, data)
				sceneReloaded = true
			}
		}
	}
	if !sceneReloaded && docChanged && fallbackDoc != nil && sameCleanPath(e.FilePath, expectedFilePath) {
		e.setDoc(fallbackDoc.Clone())
		e.sceneSaved = slices.Clone(fallbackSceneSaved)
		sceneReloaded = true
	}
	if sceneReloaded && e.Doc != nil {
		e.Rebuild()
		e.pruneSelection()
		e.syncActiveTab()
	}

	// Also refresh any inactive open scene tabs whose backing file was restored.
	for i, tab := range e.tabs {
		if i == e.activeTab || tab == nil || tab.filePath == "" {
			continue
		}
		if !e.isPathAffected(root, tab.filePath, restoredFiles) {
			continue
		}
		data, err := os.ReadFile(tab.filePath)
		if err != nil {
			continue
		}
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          tab.filePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if err != nil {
			continue
		}
		tab.doc = doc
		if enc, err := sceneformat.Encode(doc); err == nil {
			tab.sceneSaved = enc
		}
		tab.sceneDisk = makeDiskSnapshot(tab.filePath, data)
		built, buildErr := sceneformat.Build(doc, e.Resolver)
		tab.built = built
		tab.buildErr = buildErr
	}

	e.filesRev++
	e.snapshotProjectTree()
	e.invalidateProjectIndex()
	e.RefreshChangesCount()
	if e.Project != nil {
		e.CheckProblems()
	}
}

func truncateRewindPrompt(prompt string, maxRunes int) string {
	s := strings.Join(strings.Fields(strings.TrimSpace(prompt)), " ")
	runes := []rune(s)
	if len(runes) <= maxRunes || maxRunes <= 1 {
		return s
	}
	prefix := string(runes[:maxRunes])
	// Prefer a word boundary when spaces exist (avoid cutting Latin words mid-word).
	if runes[maxRunes] != ' ' {
		if idx := strings.LastIndex(prefix, " "); idx >= len(prefix)/2 {
			prefix = prefix[:idx]
		}
	}
	cut := strings.TrimRight(prefix, " ,.;:!?")
	if cut == "" {
		cut = string(runes[:maxRunes])
	}
	return cut + "…"
}

func (e *Editor) formatRewindModeLabel(mode AgentRewindMode) string {
	switch mode {
	case AgentRewindFilesOnly:
		return e.T("agent.rewind.modeFiles")
	case AgentRewindConversationOnly:
		return e.T("agent.rewind.modeConv")
	default:
		return e.T("agent.rewind.modeBoth")
	}
}

func (e *Editor) formatRewindBannerText(st AgentRewindState) string {
	mode := e.formatRewindModeLabel(st.Mode)
	prompt := truncateRewindPrompt(st.Prompt, 23)
	if prompt == "" {
		return e.T("agent.rewind.bannerNoPrompt", l10n.Param("mode", mode))
	}
	return e.T("agent.rewind.banner", l10n.Param("mode", mode), l10n.Param("prompt", prompt))
}

func (e *Editor) formatRewindBothDetail(preview AgentRewindPreview) string {
	files := len(preview.ChangedFiles)
	replies := max(1, preview.DroppedTurns)
	switch {
	case files == 0 && replies == 1:
		return e.T("agent.rewind.bothDetailNoFilesOneReply")
	case files == 0:
		return e.T("agent.rewind.bothDetailNoFilesManyReplies", l10n.Param("replies", replies))
	case files == 1 && replies == 1:
		return e.T("agent.rewind.bothDetailOneFileOneReply")
	case files == 1:
		return e.T("agent.rewind.bothDetailOneFileManyReplies", l10n.Param("replies", replies))
	case replies == 1:
		return e.T("agent.rewind.bothDetailManyFilesOneReply", l10n.Param("files", files))
	default:
		return e.T("agent.rewind.bothDetailManyFilesManyReplies", l10n.Param("files", files), l10n.Param("replies", replies))
	}
}
