package editor

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultAutoSaveDelay is how long kept edits wait before saving automatically.
	DefaultAutoSaveDelay = 2 * time.Second
	// DefaultSavingIndicatorDelay is how long a save must stay in flight before the status bar shows "Saving…", avoiding flicker on quick saves.
	DefaultSavingIndicatorDelay = 300 * time.Millisecond
)

// SaveStatusKind identifies the save indicator state shown in the status bar.
type SaveStatusKind int

const (
	// SaveStatusSaved indicates the document is saved (or a save is under the 300 ms indicator threshold).
	SaveStatusSaved SaveStatusKind = iota
	// SaveStatusSaving indicates a save has been running for at least SavingIndicatorDelay (300 ms).
	SaveStatusSaving
	// SaveStatusFailed indicates an autosave failed and remains in the status bar with Retry and Save as… until fixed.
	SaveStatusFailed
)

// String returns a lowercase name for k.
func (k SaveStatusKind) String() string {
	switch k {
	case SaveStatusSaved:
		return "saved"
	case SaveStatusSaving:
		return "saving"
	case SaveStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// SaveErrorKind classifies common filesystem save errors for status bar messages.
type SaveErrorKind string

const (
	SaveErrorNone     SaveErrorKind = ""
	SaveErrorReadOnly SaveErrorKind = "read_only"
	SaveErrorDiskFull SaveErrorKind = "disk_full"
	SaveErrorOther    SaveErrorKind = "other"
)

// SaveStatus describes the current save state shown in the status bar and reported to editormcp.
type SaveStatus struct {
	// Kind is SaveStatusSaved, SaveStatusSaving (only after 300 ms in flight), or SaveStatusFailed.
	Kind SaveStatusKind
	// LastSavedAt is when the last save succeeded (zero if none yet in this session).
	LastSavedAt time.Time
	// HasSaved reports whether a save has succeeded during this session.
	HasSaved bool
	// ErrorKind classifies the active save failure when Kind == SaveStatusFailed.
	ErrorKind SaveErrorKind
	// ErrorMessage is the localized status-bar message when Kind == SaveStatusFailed
	// (for example "Couldn't save: folder is read-only" or "Couldn't save: disk is full · 12 MB free").
	ErrorMessage string
	// RawError is the underlying error string for diagnostics and MCP warnings.
	RawError string
	// TargetPath is the file path that failed to save (project-relative when inside the project, or OS path).
	TargetPath string
	// FailedTabID is the scene tab ID associated with a failed scene save, or 0 for non-tab saves.
	FailedTabID uint64
	// FailedAt is when the active save failure occurred.
	FailedAt time.Time
	// AutoSaveFailure reports whether the active failure came from an automatic save (kept in the status bar, never toasted).
	AutoSaveFailure bool
}

type autoSaveTargetKind int

const (
	autoSaveTargetScene autoSaveTargetKind = iota
	autoSaveTargetProject
)

type autoSavePlanItem struct {
	kind        autoSaveTargetKind
	tabID       uint64
	path        string
	displayPath string
	data        []byte
	nsgoPath    string
	nsgoData    []byte
	onSave      func([]byte) error
}

type autoSaveFlightResult struct {
	gen        uint64
	plan       []autoSavePlanItem
	succeeded  int
	err        error
	failedPath string
	failedTab  uint64
}

type autoSaveState struct {
	mu             sync.Mutex
	scope          *core.Scope
	pending        *autoSaveFlightResult
	inFlightHashes map[string][sha256.Size]byte

	// UI/editor-goroutine-only state below.
	reviewOpen        bool
	unsavedPromptOpen bool

	debounceGen   uint64
	debounceTimer *time.Timer
	debounceDue   time.Time
	debounceArmed bool

	flightGen         uint64
	inFlight          bool
	showingSaving     bool
	flightStartedAt   time.Time
	indicatorTimer    *time.Timer
	indicatorDue      time.Time
	flightWriteDone   chan struct{}
	reschedulePending bool

	lastSavedAt time.Time
	hasSaved    bool

	failed          bool
	autoSaveFailure bool
	errKind         SaveErrorKind
	rawErr          string
	freeBytes       uint64
	hasFreeBytes    bool
	targetPath      string
	failedTabID     uint64
	failedAt        time.Time

	openSaveAs func()
}

func (e *Editor) autoSaveDelay() time.Duration {
	if e != nil && e.AutoSaveDelay > 0 {
		return e.AutoSaveDelay
	}
	return DefaultAutoSaveDelay
}

func (e *Editor) savingIndicatorDelay() time.Duration {
	if e != nil && e.SavingIndicatorDelay > 0 {
		return e.SavingIndicatorDelay
	}
	return DefaultSavingIndicatorDelay
}

func (e *Editor) saveWriter() func(path string, data []byte) error {
	if e != nil && e.SaveFileWriter != nil {
		return e.SaveFileWriter
	}
	return writeFileAtomic
}

// BeginReview marks an interactive edit review as open so automatic saving pauses until EndReview or KeepReview is called.
func (e *Editor) BeginReview() {
	if e == nil || e.autosave.reviewOpen {
		return
	}
	e.autosave.reviewOpen = true
	e.cancelAutoSaveTimer()
	e.notify()
}

// EndReview closes the general interactive edit review. When kept is true and AutoSave is enabled, any dirty changes schedule automatic saving after AutoSaveDelay (2 s).
func (e *Editor) EndReview(kept bool) {
	if e == nil {
		return
	}
	changed := e.autosave.reviewOpen
	e.autosave.reviewOpen = false
	if changed {
		e.RecordAgentReviewOutcome(kept, 1)
	}
	if kept {
		e.scheduleAutoSaveIfNeeded()
	} else {
		e.cancelAutoSaveTimer()
	}
	if changed {
		e.notify()
	}
	if kept {
		e.NotifyAutomationReviewKept(e.ScenePath)
	}
}

// KeepReview closes the general interactive edit review as kept and schedules automatic saving of kept changes after AutoSaveDelay (2 s).
func (e *Editor) KeepReview() {
	e.EndReview(true)
}

// HasOpenReview reports whether any interactive review (scene/agent review, background task review, cutscene review, playtest proposal review, safe rename review, scene merge conflict, or unresolved external disk conflict) is currently open.
// Edits made inside an open review are not kept yet and are not automatically saved.
func (e *Editor) HasOpenReview() bool {
	if e == nil {
		return false
	}
	if e.autosave.reviewOpen {
		return true
	}
	if e.HasPendingReview() {
		return true
	}
	if e.HasOpenTaskReview() {
		return true
	}
	if e.AgentReviewOpen() || e.PermissionsDraftOpen() || e.AgentTurnPending() {
		return true
	}
	if e.CutsceneReviewOpen() {
		return true
	}
	if e.TileReviewOpen() || e.tileReview != nil {
		return true
	}
	if e.playtest.session != nil && e.playtest.session.ReviewProposalID != "" {
		return true
	}
	if e.usages.open && e.usages.mode == ReferencesModeRename {
		return true
	}
	if e.MergeConflictOpen() {
		return true
	}
	if e.DiskCompareOpen() || len(e.disk.conflicts) > 0 {
		return true
	}
	if st := e.FirstRunStage(); st == FirstRunStageReview || st == FirstRunStageBuilding {
		return true
	}
	return false
}

// SaveStatus returns the current save status for the status bar, unsaved dot, and editormcp tool warnings.
func (e *Editor) SaveStatus() SaveStatus {
	if e == nil {
		return SaveStatus{Kind: SaveStatusSaved}
	}
	st := SaveStatus{
		Kind:            SaveStatusSaved,
		LastSavedAt:     e.autosave.lastSavedAt,
		HasSaved:        e.autosave.hasSaved,
		ErrorKind:       SaveErrorNone,
		AutoSaveFailure: e.autosave.autoSaveFailure,
	}
	if e.autosave.failed {
		st.Kind = SaveStatusFailed
		st.ErrorKind = e.autosave.errKind
		st.RawError = e.autosave.rawErr
		st.TargetPath = e.autosave.targetPath
		st.FailedTabID = e.autosave.failedTabID
		st.FailedAt = e.autosave.failedAt
		st.ErrorMessage = e.formatSaveFailureMessage(e.autosave.errKind, e.autosave.freeBytes, e.autosave.hasFreeBytes, e.autosave.rawErr)
		return st
	}
	if e.autosave.inFlight && e.autosave.showingSaving {
		st.Kind = SaveStatusSaving
	}
	return st
}

// SaveFailed reports whether an automatic save is currently in a failed state.
func (e *Editor) SaveFailed() bool {
	return e != nil && e.autosave.failed
}

// SaveFailedForTab reports whether tabID currently has an active save failure.
func (e *Editor) SaveFailedForTab(tabID uint64) bool {
	if e == nil || !e.autosave.failed {
		return false
	}
	if e.autosave.failedTabID == 0 {
		return tabID == e.activeTabID()
	}
	return e.autosave.failedTabID == tabID
}

// SaveStatusTooltip returns the hover tooltip text for the Saved indicator in the status bar.
func (e *Editor) SaveStatusTooltip() string {
	if e == nil {
		return ""
	}
	st := e.SaveStatus()
	if st.Kind == SaveStatusSaved && !st.LastSavedAt.IsZero() {
		return e.T("saveStatus.savedAt", l10n.Param("time", st.LastSavedAt.Format("15:04:05")))
	}
	return e.T("saveStatus.saved")
}

// AutoSavePending reports whether an autosave debounce timer is currently armed.
func (e *Editor) AutoSavePending() bool {
	return e != nil && e.autosave.debounceArmed
}

// AutoSaveInFlight reports whether an autosave write is currently executing.
func (e *Editor) AutoSaveInFlight() bool {
	return e != nil && e.autosave.inFlight
}

func (e *Editor) setAutoSaveScope(cx *core.Scope) {
	if e == nil {
		return
	}
	e.autosave.mu.Lock()
	e.autosave.scope = cx
	e.autosave.mu.Unlock()
	if cx == nil {
		e.stopAutoSave()
		return
	}
	e.scheduleAutoSaveIfNeeded()
}

func (e *Editor) setInFlightAutoSaveHashes(plan []autoSavePlanItem) {
	if e == nil {
		return
	}
	hashes := make(map[string][sha256.Size]byte, len(plan)*2)
	for _, item := range plan {
		if item.onSave != nil {
			continue
		}
		if item.path != "" {
			hashes[filepath.Clean(item.path)] = sha256.Sum256(item.data)
		}
		if item.nsgoPath != "" && len(item.nsgoData) > 0 {
			hashes[filepath.Clean(item.nsgoPath)] = sha256.Sum256(item.nsgoData)
		}
	}
	e.autosave.mu.Lock()
	e.autosave.inFlightHashes = hashes
	e.autosave.mu.Unlock()
}

func (e *Editor) clearInFlightAutoSaveHashes() {
	if e == nil {
		return
	}
	e.autosave.mu.Lock()
	e.autosave.inFlightHashes = nil
	e.autosave.mu.Unlock()
}

func (e *Editor) isInFlightAutoSaveHash(path string, sum [sha256.Size]byte) bool {
	if e == nil || path == "" {
		return false
	}
	key := filepath.Clean(path)
	e.autosave.mu.Lock()
	expected, ok := e.autosave.inFlightHashes[key]
	e.autosave.mu.Unlock()
	return ok && expected == sum
}

func (e *Editor) setUnsavedPromptOpen(open bool) {
	if e == nil || e.autosave.unsavedPromptOpen == open {
		return
	}
	e.autosave.unsavedPromptOpen = open
	if open {
		e.cancelAutoSaveTimer()
	} else {
		e.scheduleAutoSaveIfNeeded()
	}
}

func (e *Editor) isAutoSaveBlockedByPrompt() bool {
	return e != nil && (e.autosave.unsavedPromptOpen || e.UnsavedPromptOpen())
}

func (e *Editor) stopAutoSave() {
	if e == nil {
		return
	}
	e.cancelAutoSaveTimer()
	if e.autosave.indicatorTimer != nil {
		e.autosave.indicatorTimer.Stop()
		e.autosave.indicatorTimer = nil
	}
	e.autosave.flightGen++
	e.autosave.inFlight = false
	e.autosave.showingSaving = false
	e.autosave.reschedulePending = false
	e.autosave.mu.Lock()
	e.autosave.scope = nil
	e.autosave.pending = nil
	e.autosave.inFlightHashes = nil
	e.autosave.mu.Unlock()
}

func (e *Editor) resetAutoSaveState() {
	if e == nil {
		return
	}
	e.cancelAutoSaveTimer()
	if e.autosave.indicatorTimer != nil {
		e.autosave.indicatorTimer.Stop()
		e.autosave.indicatorTimer = nil
	}
	e.autosave.flightGen++
	e.autosave.inFlight = false
	e.autosave.showingSaving = false
	e.autosave.reschedulePending = false
	e.clearSaveFailureFields()
	e.autosave.mu.Lock()
	e.autosave.pending = nil
	e.autosave.inFlightHashes = nil
	e.autosave.mu.Unlock()
}

func (e *Editor) cancelAutoSaveTimer() {
	if e == nil {
		return
	}
	e.autosave.debounceGen++
	if e.autosave.debounceTimer != nil {
		e.autosave.debounceTimer.Stop()
		e.autosave.debounceTimer = nil
	}
	e.autosave.debounceArmed = false
	e.autosave.debounceDue = time.Time{}
}

func (e *Editor) clearSaveFailureFields() {
	e.autosave.failed = false
	e.autosave.autoSaveFailure = false
	e.autosave.errKind = SaveErrorNone
	e.autosave.rawErr = ""
	e.autosave.freeBytes = 0
	e.autosave.hasFreeBytes = false
	e.autosave.targetPath = ""
	e.autosave.failedTabID = 0
	e.autosave.failedAt = time.Time{}
}

func (e *Editor) recordSaveSucceeded() {
	if e == nil {
		return
	}
	e.cancelAutoSaveTimer()
	if e.autosave.indicatorTimer != nil {
		e.autosave.indicatorTimer.Stop()
		e.autosave.indicatorTimer = nil
	}
	e.autosave.showingSaving = false
	e.autosave.lastSavedAt = e.now()
	e.autosave.hasSaved = true
	e.clearSaveFailureFields()
}

// recordSaveSucceededFor records a successful manual save of either the project (isProject == true)
// or a specific scene tab (tabID), preserving an active failure that belongs to another dirty target.
func (e *Editor) recordSaveSucceededFor(tabID uint64, isProject bool) {
	if e == nil {
		return
	}
	if !e.autosave.failed {
		e.recordSaveSucceeded()
		return
	}
	matches := (isProject && e.autosave.failedTabID == 0) || (!isProject && tabID != 0 && e.autosave.failedTabID == tabID)
	if matches || (!e.hasAutoSaveableDirtyChanges() && !e.hasDirtyDocuments()) {
		e.recordSaveSucceeded()
		return
	}
	e.autosave.lastSavedAt = e.now()
	e.autosave.hasSaved = true
}

// clearSaveFailureForTab clears an active save failure when the failing scene tab is closed
// (or when no dirty documents remain).
func (e *Editor) clearSaveFailureForTab(tabID uint64) {
	if e == nil || !e.autosave.failed {
		return
	}
	if (tabID != 0 && e.autosave.failedTabID == tabID) || (!e.hasAutoSaveableDirtyChanges() && !e.hasDirtyDocuments()) {
		e.clearSaveFailureFields()
	}
}

func (e *Editor) clearSaveFailureIfClean() {
	if e == nil || !e.autosave.failed {
		return
	}
	if !e.hasAutoSaveableDirtyChanges() && !e.hasDirtyDocuments() {
		e.clearSaveFailureFields()
	}
}

func (e *Editor) hasInteractiveDragInProgress() bool {
	if e == nil {
		return false
	}
	return e.drag2D.handle != 0 ||
		e.shapeDrag.active ||
		e.pluginDrag ||
		e.pluginDrag3D ||
		e.drag3D.kind != drag3DNone ||
		e.compGizmoDrag.active ||
		e.cameraDrag.active ||
		e.cameraBoundsDrag.active ||
		e.roomDrag.active ||
		e.inspectorScrub.active ||
		e.stroke != nil ||
		e.gridMap.stroke != nil ||
		(e.terrain != nil && e.terrain.stroke != nil)
}

func (e *Editor) resumeAutoSaveAfterInteraction() {
	if e == nil || e.hasInteractiveDragInProgress() {
		return
	}
	if e.autosave.reschedulePending {
		e.autosave.reschedulePending = false
		e.scheduleAutoSaveIfNeeded()
	}
}

func (e *Editor) hasAutoSaveableDirtyChanges() bool {
	if e == nil {
		return false
	}
	if e.ProjectDirty() && e.ProjectPath != "" {
		return true
	}
	if e.SceneDirty() && (e.OnSave != nil || e.FilePath != "") {
		return true
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if e.tabDirtyAt(i) && (t.onSave != nil || t.filePath != "") {
			return true
		}
	}
	return false
}

// scheduleAutoSaveIfNeeded arms (or resets) the 2 s autosave debounce timer when AutoSave is enabled, no review is open, and at least one saveable document is dirty.
func (e *Editor) scheduleAutoSaveIfNeeded() {
	if e == nil {
		return
	}
	if !e.AutoSave() || e.playing || e.HistoryPreviewActive() || e.HasOpenReview() || e.isAutoSaveBlockedByPrompt() {
		e.cancelAutoSaveTimer()
		return
	}
	if !e.hasAutoSaveableDirtyChanges() {
		e.cancelAutoSaveTimer()
		e.clearSaveFailureIfClean()
		return
	}
	if e.hasInteractiveDragInProgress() {
		e.autosave.reschedulePending = true
		return
	}
	e.autosave.reschedulePending = false
	delay := e.autoSaveDelay()
	e.autosave.debounceGen++
	gen := e.autosave.debounceGen
	if e.autosave.debounceTimer != nil {
		e.autosave.debounceTimer.Stop()
		e.autosave.debounceTimer = nil
	}
	e.autosave.debounceArmed = true
	e.autosave.debounceDue = e.now().Add(delay)

	e.autosave.mu.Lock()
	scope := e.autosave.scope
	e.autosave.mu.Unlock()
	if scope != nil {
		e.autosave.debounceTimer = time.AfterFunc(delay, func() {
			scope.Post(func() {
				e.onAutoSaveDebounce(gen)
			})
		})
	}
}

func (e *Editor) onAutoSaveDebounce(gen uint64) {
	if e == nil || gen != e.autosave.debounceGen || !e.autosave.debounceArmed {
		return
	}
	e.autosave.debounceArmed = false
	e.autosave.debounceDue = time.Time{}
	e.autosave.debounceTimer = nil
	e.startAutoSaveFlight()
}

// StepAutoSave advances autosave timers and applies any finished background write against the editor's clock (Editor.Now or time.Now).
// Primarily used by deterministic headless tests; returns true if an autosave state transition occurred.
func (e *Editor) StepAutoSave() bool {
	if e == nil {
		return false
	}
	changed := false
	if e.applyPendingAutoSaveResult() {
		changed = true
	}
	now := e.now()
	if e.autosave.inFlight && !e.autosave.showingSaving && !e.autosave.indicatorDue.IsZero() && !now.Before(e.autosave.indicatorDue) {
		e.onSavingIndicatorTimer(e.autosave.flightGen)
		changed = true
	}
	if e.autosave.debounceArmed && !e.autosave.debounceDue.IsZero() && !now.Before(e.autosave.debounceDue) {
		e.onAutoSaveDebounce(e.autosave.debounceGen)
		changed = true
	}
	return changed
}

// WaitAutoSave blocks until any in-flight autosave disk write finishes and applies its completion on the caller's goroutine (for headless tests).
func (e *Editor) WaitAutoSave() {
	if e == nil {
		return
	}
	done := e.autosave.flightWriteDone
	if done != nil {
		<-done
	}
	e.applyPendingAutoSaveResult()
}

// RetrySave retries saving after an autosave failure (or saves dirty auto-saveable targets immediately).
// On success it clears the failure state and updates the Saved timestamp; on failure it keeps the error in the status bar.
func (e *Editor) RetrySave() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	if e.autosave.inFlight {
		return nil
	}
	e.cancelAutoSaveTimer()
	plan, failedPath, failedTab, err := e.buildAutoSavePlan()
	if err != nil {
		if errors.Is(err, ErrDiskConflict) {
			return err
		}
		e.recordAutoSaveFailure(err, failedPath, failedTab)
		return err
	}
	if len(plan) == 0 {
		// Nothing dirty to write — clear any stale failure cleanly.
		e.recordSaveSucceeded()
		e.notify()
		return nil
	}
	writer := e.saveWriter()
	succeeded := 0
	for _, item := range plan {
		if err := executeAutoSaveItem(item, writer); err != nil {
			if succeeded > 0 {
				e.commitAutoSaveItems(plan[:succeeded])
			}
			target := item.path
			if target == "" {
				target = item.displayPath
			}
			e.recordAutoSaveFailure(err, target, item.tabID)
			return err
		}
		succeeded++
	}
	e.commitAutoSavePlan(plan)
	return nil
}

// openSaveAsFromStatusBar invokes the Save as… sheet callback registered by EditorView.
func (e *Editor) openSaveAsFromStatusBar() {
	if e == nil {
		return
	}
	if e.autosave.openSaveAs != nil {
		e.autosave.openSaveAs()
	}
}

func (e *Editor) startAutoSaveFlight() {
	if e == nil || !e.AutoSave() || e.playing || e.HistoryPreviewActive() || e.HasOpenReview() || e.isAutoSaveBlockedByPrompt() {
		return
	}
	if e.hasInteractiveDragInProgress() {
		e.autosave.reschedulePending = true
		return
	}
	if e.autosave.inFlight {
		e.autosave.reschedulePending = true
		return
	}
	plan, failedPath, failedTab, err := e.buildAutoSavePlan()
	if err != nil {
		if errors.Is(err, ErrDiskConflict) {
			// External disk conflicts surface their own banner/merge workflow; never overwrite disk.
			return
		}
		e.recordAutoSaveFailure(err, failedPath, failedTab)
		return
	}
	if len(plan) == 0 {
		e.clearSaveFailureIfClean()
		return
	}

	// Execute OnSave callbacks synchronously on the editor goroutine so caller-owned hooks
	// never race with editor/test state; run only disk file writes off-goroutine.
	var onSaveSucceeded []autoSavePlanItem
	var diskPlan []autoSavePlanItem
	for _, item := range plan {
		if item.onSave != nil {
			if err := item.onSave(item.data); err != nil {
				if len(onSaveSucceeded) > 0 {
					e.commitAutoSaveItems(onSaveSucceeded)
				}
				target := item.path
				if target == "" {
					target = item.displayPath
				}
				e.recordAutoSaveFailure(err, target, item.tabID)
				return
			}
			onSaveSucceeded = append(onSaveSucceeded, item)
			continue
		}
		diskPlan = append(diskPlan, item)
	}
	if len(diskPlan) == 0 {
		e.commitAutoSavePlan(onSaveSucceeded)
		return
	}
	if len(onSaveSucceeded) > 0 {
		e.commitAutoSaveItems(onSaveSucceeded)
	}

	e.autosave.flightGen++
	gen := e.autosave.flightGen
	e.autosave.inFlight = true
	e.autosave.showingSaving = false
	e.autosave.flightStartedAt = e.now()
	indDelay := e.savingIndicatorDelay()
	e.autosave.indicatorDue = e.autosave.flightStartedAt.Add(indDelay)
	if e.autosave.indicatorTimer != nil {
		e.autosave.indicatorTimer.Stop()
		e.autosave.indicatorTimer = nil
	}

	e.setInFlightAutoSaveHashes(diskPlan)

	e.autosave.mu.Lock()
	scope := e.autosave.scope
	e.autosave.mu.Unlock()
	if scope != nil {
		e.autosave.indicatorTimer = time.AfterFunc(indDelay, func() {
			scope.Post(func() {
				e.onSavingIndicatorTimer(gen)
			})
		})
	}

	writeDone := make(chan struct{})
	e.autosave.flightWriteDone = writeDone
	writer := e.saveWriter()

	go func() {
		var writeErr error
		var errPath string
		var errTab uint64
		succeeded := 0
		for _, item := range diskPlan {
			if err := executeAutoSaveItem(item, writer); err != nil {
				writeErr = err
				errPath = item.path
				if errPath == "" {
					errPath = item.displayPath
				}
				errTab = item.tabID
				break
			}
			succeeded++
		}
		res := &autoSaveFlightResult{
			gen:        gen,
			plan:       diskPlan,
			succeeded:  succeeded,
			err:        writeErr,
			failedPath: errPath,
			failedTab:  errTab,
		}
		e.autosave.mu.Lock()
		e.autosave.pending = res
		postScope := e.autosave.scope
		e.autosave.mu.Unlock()
		close(writeDone)
		if postScope != nil {
			postScope.Post(func() {
				e.applyPendingAutoSaveResult()
			})
		}
	}()
}

func (e *Editor) onSavingIndicatorTimer(gen uint64) {
	if e == nil || !e.autosave.inFlight || gen != e.autosave.flightGen {
		return
	}
	if e.autosave.showingSaving {
		return
	}
	e.autosave.showingSaving = true
	e.notify()
}

func (e *Editor) applyPendingAutoSaveResult() bool {
	if e == nil {
		return false
	}
	e.autosave.mu.Lock()
	res := e.autosave.pending
	e.autosave.pending = nil
	e.autosave.mu.Unlock()
	if res == nil {
		return false
	}
	if res.gen != e.autosave.flightGen {
		return false
	}
	if e.autosave.indicatorTimer != nil {
		e.autosave.indicatorTimer.Stop()
		e.autosave.indicatorTimer = nil
	}
	e.autosave.inFlight = false
	e.autosave.showingSaving = false
	e.autosave.indicatorDue = time.Time{}

	if res.err != nil {
		if res.succeeded > 0 {
			e.commitAutoSaveItems(res.plan[:res.succeeded])
		}
		e.clearInFlightAutoSaveHashes()
		e.recordAutoSaveFailure(res.err, res.failedPath, res.failedTab)
		return true
	}
	e.commitAutoSavePlan(res.plan)
	e.clearInFlightAutoSaveHashes()
	if e.autosave.reschedulePending || e.hasAutoSaveableDirtyChanges() {
		e.autosave.reschedulePending = false
		e.scheduleAutoSaveIfNeeded()
	}
	return true
}

func executeAutoSaveItem(item autoSavePlanItem, writer func(path string, data []byte) error) error {
	switch item.kind {
	case autoSaveTargetProject:
		if err := writer(item.path, item.data); err != nil {
			return err
		}
		if item.nsgoPath != "" && len(item.nsgoData) > 0 {
			if err := writer(item.nsgoPath, item.nsgoData); err != nil {
				return err
			}
		}
		return nil
	default:
		if item.onSave != nil {
			return item.onSave(item.data)
		}
		return writer(item.path, item.data)
	}
}

func (e *Editor) buildAutoSavePlan() ([]autoSavePlanItem, string, uint64, error) {
	if e == nil {
		return nil, "", 0, nil
	}
	e.syncActiveTab()
	var plan []autoSavePlanItem

	if e.ProjectDirty() && e.ProjectPath != "" {
		if err := e.checkDiskConflict(DiskFileProject, e.ProjectPath, e.disk.projectDisk); err != nil {
			return nil, e.ProjectPath, 0, err
		}
		data, err := project.Encode(e.Project)
		if err != nil {
			return nil, e.ProjectPath, 0, err
		}
		nsgoPath := filepath.Join(filepath.Dir(e.ProjectPath), project.NSGoFileName)
		var nsgoData []byte
		old, err := os.ReadFile(nsgoPath)
		switch {
		case err == nil:
			synced, serr := e.Project.SyncNSGo(old)
			if serr != nil {
				return nil, nsgoPath, 0, serr
			}
			if !bytes.Equal(synced, old) {
				nsgoData = synced
			}
		case !errors.Is(err, fs.ErrNotExist):
			return nil, nsgoPath, 0, err
		}
		plan = append(plan, autoSavePlanItem{
			kind:        autoSaveTargetProject,
			path:        e.ProjectPath,
			displayPath: project.FileName,
			data:        data,
			nsgoPath:    nsgoPath,
			nsgoData:    nsgoData,
		})
	}

	for i, t := range e.tabs {
		if !e.tabDirtyAt(i) {
			continue
		}
		if t.onSave == nil && t.filePath == "" {
			// Untitled scene: never autosave or fail until the author picks a file path.
			continue
		}
		if t.onSave == nil {
			if err := e.checkTabDiskConflict(t); err != nil {
				return nil, t.filePath, t.id, err
			}
		}
		var (
			data []byte
			err  error
		)
		if i == e.activeTab {
			data, err = e.SaveYAML()
		} else {
			data, err = sceneformat.Encode(t.doc)
		}
		if err != nil {
			return nil, t.filePath, t.id, err
		}
		display := tabFullPath(t)
		if display == "" {
			display = e.SceneLabel()
		}
		plan = append(plan, autoSavePlanItem{
			kind:        autoSaveTargetScene,
			tabID:       t.id,
			path:        t.filePath,
			displayPath: display,
			data:        append([]byte(nil), data...),
			onSave:      t.onSave,
		})
	}
	return plan, "", 0, nil
}

func (e *Editor) commitAutoSaveItems(plan []autoSavePlanItem) (savedAnyScene, savedAnyProject bool) {
	if e == nil {
		return false, false
	}
	for _, item := range plan {
		switch item.kind {
		case autoSaveTargetProject:
			if e.Project != nil && e.ProjectPath == item.path {
				e.projectSaved = append([]byte(nil), item.data...)
				e.projectSavedOnce = true
				e.recordProjectDiskSnapshot(item.path, item.data)
				savedAnyProject = true
			}
		case autoSaveTargetScene:
			idx := e.findTabByID(item.tabID)
			if idx < 0 {
				continue
			}
			t := e.tabs[idx]
			if item.onSave == nil && t.filePath != item.path {
				continue
			}
			t.sceneSaved = append([]byte(nil), item.data...)
			t.placeholder = false
			if item.onSave == nil && item.path != "" {
				t.sceneDisk = makeDiskSnapshot(item.path, item.data)
				if e.disk.saveConflict != nil && e.disk.saveConflictTabID == t.id {
					e.disk.saveConflict = nil
					e.disk.saveConflictTabID = 0
				}
			}
			if idx == e.activeTab {
				e.sceneSaved = append([]byte(nil), item.data...)
				if item.onSave == nil && item.path != "" {
					e.recordSceneDiskSnapshot(item.path, item.data)
				}
				e.syncActiveTab()
				e.saveViewStateQuietly()
			}
			e.rebuildDependentTabs(t.id)
			savedAnyScene = true
		}
	}
	if savedAnyScene || savedAnyProject {
		e.filesRev++
		e.snapshotProjectTree()
		e.invalidateProjectIndex()
		e.CheckProblems()
		e.RefreshChangesCount()
		for _, item := range plan {
			target := item.path
			if target == "" {
				target = item.displayPath
			}
			if target != "" {
				e.NotifyAutomationFileChanged(target)
			}
		}
	}
	return savedAnyScene, savedAnyProject
}

func (e *Editor) commitAutoSavePlan(plan []autoSavePlanItem) {
	if e == nil {
		return
	}
	e.commitAutoSaveItems(plan)
	e.recordSaveSucceeded()
	e.notify()
}

func (e *Editor) recordAutoSaveFailure(err error, targetPath string, tabID uint64) {
	if e == nil || err == nil {
		return
	}
	kind := classifySaveError(err)
	var freeBytes uint64
	var hasFree bool
	if kind == SaveErrorDiskFull && targetPath != "" {
		if e.FreeSpaceBytes != nil {
			freeBytes, hasFree = e.FreeSpaceBytes(targetPath)
		} else {
			freeBytes, hasFree = availableDiskSpace(targetPath)
		}
	}
	displayTarget := targetPath
	if rel, ok := e.projectRel(targetPath); ok {
		displayTarget = rel
	}
	e.autosave.failed = true
	e.autosave.autoSaveFailure = true
	e.autosave.errKind = kind
	e.autosave.rawErr = err.Error()
	e.autosave.freeBytes = freeBytes
	e.autosave.hasFreeBytes = hasFree
	e.autosave.targetPath = displayTarget
	e.autosave.failedTabID = tabID
	e.autosave.failedAt = e.now()
	// Append to the Output panel history without triggering a toast.
	projectDir := ""
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}
	e.appendOutputEntry(OutputEditor, OutputError, e.formatSaveFailureMessage(kind, freeBytes, hasFree, err.Error()), projectDir)
	e.notify()
}

func classifySaveError(err error) SaveErrorKind {
	if err == nil {
		return SaveErrorNone
	}
	if errors.Is(err, syscall.ENOSPC) {
		return SaveErrorDiskFull
	}
	if errors.Is(err, fs.ErrPermission) || os.IsPermission(err) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EROFS) {
		return SaveErrorReadOnly
	}
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no space left"), strings.Contains(lower, "disk is full"), strings.Contains(lower, "disk full"), strings.Contains(lower, "enospc"):
		return SaveErrorDiskFull
	case strings.Contains(lower, "read-only"), strings.Contains(lower, "read only"), strings.Contains(lower, "permission denied"), strings.Contains(lower, "access is denied"), strings.Contains(lower, "erofs"):
		return SaveErrorReadOnly
	default:
		return SaveErrorOther
	}
}

func (e *Editor) formatSaveFailureMessage(kind SaveErrorKind, freeBytes uint64, hasFree bool, rawErr string) string {
	switch kind {
	case SaveErrorReadOnly:
		return e.T("saveStatus.errReadOnly")
	case SaveErrorDiskFull:
		if hasFree {
			return e.T("saveStatus.errDiskFullFree", l10n.Param("free", formatFreeSpaceBytes(freeBytes)))
		}
		return e.T("saveStatus.errDiskFull")
	default:
		reason := strings.TrimSpace(strings.TrimPrefix(rawErr, "editor: "))
		if reason == "" {
			reason = "unknown error"
		}
		return e.T("saveStatus.errOther", l10n.Param("reason", reason))
	}
}

func formatFreeSpaceBytes(b uint64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%d MB", b/mb)
	case b >= kb:
		return fmt.Sprintf("%d KB", b/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
