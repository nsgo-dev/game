package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

func (e *Editor) defaultShortcutOS() core.OS {
	if e != nil && e.scope != nil {
		return e.scope.Env().OS()
	}
	return core.OSMacOS
}

// AgentTurnOptions configures metadata for an agent turn recorded on the unified History timeline.
type AgentTurnOptions struct {
	// Title is the concise request title shown in the History panel (for example "slimes and coins").
	Title string
	// Summary optionally overrides the headline (defaults to Title).
	Summary string
	// Detail optionally adds context before the change/kept counts (for example "from the template").
	Detail string
	// ChangeCount is the total number of changes proposed during this turn (0 uses the executed sub-command count).
	ChangeCount int
	// KeptCount is the number of changes kept during this turn (0 defaults to ChangeCount).
	KeptCount int
	// Files lists additional project-relative file paths touched by this turn (outside the active scene).
	Files []string
	// UndoLabel optionally overrides History.UndoLabel() for the grouped undo step.
	UndoLabel string
}

type historyPreviewState struct {
	entryID uint64
	entry   HistoryEntry
	doc     *sceneformat.SceneDoc
	scene   *sceneformat.BuiltScene
}

type historyPanelState struct {
	open        bool
	filter      HistoryFilter
	preview     *historyPreviewState
	clock       func() time.Time
	agentDepth  int
	pendingMeta *HistoryMeta
	savingDepth int
	savedFiles  []string
}

// SetHistoryClock sets the clock used to timestamp History timeline entries across all open tabs.
// Passing nil restores time.Now.
func (e *Editor) SetHistoryClock(fn func() time.Time) {
	if e == nil {
		return
	}
	e.historyPanel.clock = fn
	if e.History != nil {
		e.History.SetClock(fn)
	}
	for _, t := range e.tabs {
		if t != nil && t.history != nil {
			t.history.SetClock(fn)
		}
	}
}

// HistoryNow returns the current time according to the editor's configured History clock (or time.Now).
func (e *Editor) HistoryNow() time.Time {
	if e != nil && e.historyPanel.clock != nil {
		return e.historyPanel.clock()
	}
	if e != nil && e.History != nil {
		return e.History.now()
	}
	return time.Now()
}

// EnterAgentContext marks subsequent History edits executed on the editor's goroutine as authored by the agent
// until the matching LeaveAgentContext call. Used by editormcp while executing agent tool calls.
func (e *Editor) EnterAgentContext() {
	if e == nil {
		return
	}
	e.historyPanel.agentDepth++
}

// LeaveAgentContext ends a scope opened by EnterAgentContext.
func (e *Editor) LeaveAgentContext() {
	if e == nil || e.historyPanel.agentDepth <= 0 {
		return
	}
	e.historyPanel.agentDepth--
}

// HistoryPanelOpen reports whether the History timeline panel is open.
func (e *Editor) HistoryPanelOpen() bool {
	return e != nil && e.historyPanel.open
}

// SetHistoryPanelOpen opens or closes the History timeline panel.
// Closing the panel also exits any active point-in-time preview.
func (e *Editor) SetHistoryPanelOpen(open bool) {
	if e == nil {
		return
	}
	changed := e.historyPanel.open != open
	e.historyPanel.open = open
	if !open && e.historyPanel.preview != nil {
		e.historyPanel.preview = nil
		e.s3d.stale = true
		changed = true
	}
	if changed {
		e.notify()
	}
}

// ToggleHistoryPanel toggles the History timeline panel open or closed.
func (e *Editor) ToggleHistoryPanel() {
	if e == nil {
		return
	}
	e.SetHistoryPanelOpen(!e.HistoryPanelOpen())
}

// HistoryFilter returns the active filter tab in the History panel (All, You, or Agent).
func (e *Editor) HistoryFilter() HistoryFilter {
	if e == nil || e.historyPanel.filter == "" {
		return HistoryFilterAll
	}
	return e.historyPanel.filter
}

// SetHistoryFilter switches the active filter tab in the History panel.
func (e *Editor) SetHistoryFilter(filter HistoryFilter) {
	if e == nil {
		return
	}
	switch filter {
	case HistoryFilterAll, HistoryFilterYou, HistoryFilterAgent:
	default:
		filter = HistoryFilterAll
	}
	if e.historyPanel.filter == filter {
		return
	}
	e.historyPanel.filter = filter
	e.notify()
}

// HistoryEntries returns all entries on the active scene's unified History timeline, newest first.
func (e *Editor) HistoryEntries() []HistoryEntry {
	if e == nil || e.History == nil {
		return nil
	}
	return e.History.Entries()
}

// FilteredHistoryEntries returns the entries matching filter (or the panel's active filter when empty), newest first.
func (e *Editor) FilteredHistoryEntries(filter HistoryFilter) []HistoryEntry {
	if e == nil || e.History == nil {
		return nil
	}
	if filter == "" {
		filter = e.HistoryFilter()
	}
	return e.History.FilteredEntries(filter)
}

// HistoryEntryByID returns the timeline entry with ID id, or false when absent.
func (e *Editor) HistoryEntryByID(id uint64) (HistoryEntry, bool) {
	if e == nil || e.History == nil {
		return HistoryEntry{}, false
	}
	return e.History.Entry(id)
}

// HistoryPreviewActive reports whether the viewport is currently showing a read-only point-in-time history preview.
func (e *Editor) HistoryPreviewActive() bool {
	return e != nil && e.historyPanel.preview != nil
}

// HistoryPreviewEntry returns the entry currently being previewed read-only, if any.
func (e *Editor) HistoryPreviewEntry() (HistoryEntry, bool) {
	if e == nil || e.historyPanel.preview == nil {
		return HistoryEntry{}, false
	}
	return e.historyPanel.preview.entry.Clone(), true
}

// HistoryPreviewDoc returns the read-only SceneDoc currently being previewed, or nil when not previewing.
func (e *Editor) HistoryPreviewDoc() *sceneformat.SceneDoc {
	if e == nil || e.historyPanel.preview == nil {
		return nil
	}
	return e.historyPanel.preview.doc
}

// HistoryPreviewScene returns the built read-only preview scene, or nil when not previewing.
func (e *Editor) HistoryPreviewScene() *sceneformat.BuiltScene {
	if e == nil || e.historyPanel.preview == nil {
		return nil
	}
	return e.historyPanel.preview.scene
}

// PreviewHistoryEntry renders the scene snapshot recorded at timeline entry id read-only in the viewport
// without mutating Editor.Doc or moving Editor.History's undo cursor.
func (e *Editor) PreviewHistoryEntry(id uint64) error {
	if e == nil || e.History == nil {
		return errors.New("editor: history is not initialized")
	}
	if e.playing {
		return ErrPlaying
	}
	idx := e.History.recordIndexByID(id)
	if idx < 0 {
		return fmt.Errorf("editor: history entry %d not found", id)
	}
	rec := e.History.records[idx]
	if len(rec.snapshot.sceneYAML) == 0 {
		return fmt.Errorf("editor: history entry %d has no scene snapshot", id)
	}
	doc, err := sceneformat.ParseWith(rec.snapshot.sceneYAML, sceneformat.ParseOptions{
		File:          rec.snapshot.scenePath,
		Components:    e.components,
		PhysicsLayers: e.PhysicsLayers(),
	})
	if err != nil {
		return fmt.Errorf("editor: parse history preview %d: %w", id, err)
	}
	built, err := sceneformat.Build(doc, e.liveResolver())
	if err != nil {
		return fmt.Errorf("editor: build history preview %d: %w", id, err)
	}
	built.DrawAllTiles = true

	e.cancelTransientInteraction()
	e.historyPanel.preview = &historyPreviewState{
		entryID: id,
		entry:   rec.entry.Clone(),
		doc:     doc,
		scene:   built,
	}
	e.s3d.stale = true
	e.Status = fmt.Sprintf("Previewing %s · editing is off", e.FormatHistoryRelativeTime(rec.entry.Timestamp))
	e.notify()
	return nil
}

// ExitHistoryPreview leaves point-in-time preview mode and returns the viewport to live editing.
func (e *Editor) ExitHistoryPreview() {
	if e == nil || e.historyPanel.preview == nil {
		return
	}
	e.historyPanel.preview = nil
	e.s3d.stale = true
	e.Status = "Ready"
	e.notify()
}

// RecordAgentTurn executes fn as a single undoable agent turn on History, recording the agent's
// request title, change/kept counts, and any extra touched project files so the turn appears as
// one entry on the History timeline and can be undone or restored atomically.
func (e *Editor) RecordAgentTurn(opts AgentTurnOptions, fn func() error) error {
	if e == nil || e.History == nil {
		return errors.New("editor: history is not initialized")
	}
	if e.playing {
		return ErrPlaying
	}
	if fn == nil {
		return nil
	}
	if e.HistoryPreviewActive() {
		e.ExitHistoryPreview()
	}

	extraPaths := normalizeHistoryFileList(opts.Files)
	beforeFiles := e.snapshotExtraFiles(extraPaths)
	var sceneBefore []byte
	if e.Doc != nil {
		sceneBefore, _ = e.SaveYAML()
	}
	var projectBefore []byte
	if e.Project != nil {
		projectBefore, _ = project.Encode(e.Project)
	}

	undoLabel := strings.TrimSpace(opts.UndoLabel)
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		summary = strings.TrimSpace(opts.Title)
	}
	if undoLabel == "" {
		if summary != "" {
			undoLabel = "Agent: " + summary
		} else {
			undoLabel = "Agent edit"
		}
	}
	if summary == "" {
		summary = strings.TrimSpace(strings.TrimPrefix(undoLabel, "Agent:"))
		if summary == "" {
			summary = undoLabel
		}
	}

	e.EnterAgentContext()
	defer e.LeaveAgentContext()

	var executedSubCmds int
	prevMeta := e.historyPanel.pendingMeta
	defer func() { e.historyPanel.pendingMeta = prevMeta }()

	err := e.History.Group(undoLabel, func() error {
		if err := fn(); err != nil {
			return err
		}
		if e.History.group != nil {
			executedSubCmds = len(e.History.group.cmds)
		}
		afterFiles := e.snapshotExtraFiles(extraPaths)
		changedFiles := diffExtraFiles(beforeFiles, afterFiles)
		if len(changedFiles) > 0 {
			beforeSubset := make(map[string]historyFileSnapshot, len(changedFiles))
			afterSubset := make(map[string]historyFileSnapshot, len(changedFiles))
			for _, p := range changedFiles {
				beforeSubset[p] = beforeFiles[p]
				afterSubset[p] = afterFiles[p]
			}
			// Record disk file transitions inside the same undo group (already applied by fn).
			cmd := &agentFilesCmd{
				ed:     e,
				label:  undoLabel,
				before: beforeSubset,
				after:  afterSubset,
			}
			if e.History.group != nil {
				e.History.group.cmds = append(e.History.group.cmds, cmd)
			}
		}

		changeCount := opts.ChangeCount
		if changeCount <= 0 {
			changeCount = executedSubCmds + len(changedFiles)
		}
		keptCount := opts.KeptCount
		if keptCount <= 0 && changeCount > 0 {
			keptCount = changeCount
		}
		detail := formatAgentTurnDetail(opts.Detail, changeCount, keptCount)

		var touched []string
		if e.Doc != nil {
			if sceneAfter, err := e.SaveYAML(); err == nil && !bytes.Equal(sceneBefore, sceneAfter) {
				if f := e.historyActiveSceneFile(); f != "" {
					touched = append(touched, f)
				}
			}
		}
		if e.Project != nil {
			if projAfter, err := project.Encode(e.Project); err == nil && !bytes.Equal(projectBefore, projAfter) {
				touched = append(touched, project.FileName)
			}
		}
		touched = append(touched, extraPaths...)
		touched = normalizeHistoryFileList(touched)
		if len(touched) == 0 {
			if f := e.historyActiveSceneFile(); f != "" {
				touched = []string{f}
			}
		}

		e.historyPanel.pendingMeta = &HistoryMeta{
			Author:      HistoryAuthorAgent,
			Summary:     summary,
			Detail:      detail,
			Files:       touched,
			ChangeCount: changeCount,
			KeptCount:   keptCount,
			BeforeFiles: beforeFiles,
			AfterFiles:  afterFiles,
		}
		return nil
	})
	return err
}

func formatAgentTurnDetail(extra string, changes, kept int) string {
	var parts []string
	if extra = strings.TrimSpace(extra); extra != "" {
		parts = append(parts, extra)
	}
	if changes > 0 {
		noun := "changes"
		if changes == 1 {
			noun = "change"
		}
		parts = append(parts, fmt.Sprintf("%d %s", changes, noun))
		if kept > 0 && kept < changes {
			parts = append(parts, fmt.Sprintf("kept %d", kept))
		}
	}
	return strings.Join(parts, " · ")
}

// RestoreHistoryEntry non-destructively rolls the open scene, project, and any touched extra files
// back to the state recorded at timeline entry id as ONE new undoable History step.
// Earlier and later entries remain in History and can still be previewed or restored; ONE Undo
// immediately returns to the state right before Restore.
func (e *Editor) RestoreHistoryEntry(id uint64) error {
	if e == nil || e.History == nil {
		return errors.New("editor: history is not initialized")
	}
	if e.playing {
		return ErrPlaying
	}
	idx := e.History.recordIndexByID(id)
	if idx < 0 {
		return fmt.Errorf("editor: history entry %d not found", id)
	}
	target := e.History.records[idx]
	if len(target.snapshot.sceneYAML) == 0 {
		return fmt.Errorf("editor: history entry %d has no scene snapshot", id)
	}

	e.cancelTransientInteraction()
	e.historyPanel.preview = nil
	e.s3d.stale = true

	// Validate the target scene (and project if present) before mutating anything.
	if _, err := sceneformat.ParseWith(target.snapshot.sceneYAML, sceneformat.ParseOptions{
		File:          target.snapshot.scenePath,
		Components:    e.components,
		PhysicsLayers: e.PhysicsLayers(),
	}); err != nil {
		return fmt.Errorf("editor: restore history %d: %w", id, err)
	}
	if len(target.snapshot.projectYAML) > 0 {
		if _, err := project.Parse(target.snapshot.projectYAML); err != nil {
			return fmt.Errorf("editor: restore project %d: %w", id, err)
		}
	}

	// Collect every extra file path touched at or after target so every touched file rolls back cleanly.
	afterFiles := cloneHistoryFileSnapshots(target.snapshot.files)
	if afterFiles == nil {
		afterFiles = make(map[string]historyFileSnapshot)
	}
	var allExtra []string
	for p := range afterFiles {
		allExtra = append(allExtra, p)
	}
	for k := idx + 1; k < len(e.History.records); k++ {
		rec := e.History.records[k]
		for p, snap := range rec.beforeFiles {
			if _, exists := afterFiles[p]; !exists {
				afterFiles[p] = historyFileSnapshot{
					exists: snap.exists,
					data:   slices.Clone(snap.data),
				}
			}
			allExtra = append(allExtra, p)
		}
		for p := range rec.snapshot.files {
			if _, exists := afterFiles[p]; !exists {
				afterFiles[p] = historyFileSnapshot{exists: false}
			}
			allExtra = append(allExtra, p)
		}
	}
	allExtra = normalizeHistoryFileList(allExtra)

	beforeSnap := e.captureHistorySnapshot(allExtra)
	afterSnap := historySnapshot{
		sceneYAML:   slices.Clone(target.snapshot.sceneYAML),
		scenePath:   target.snapshot.scenePath,
		projectYAML: slices.Clone(target.snapshot.projectYAML),
		files:       afterFiles,
	}

	ago := e.FormatHistoryRelativeTime(target.entry.Timestamp)
	summary := "Restored to " + ago
	detail := target.entry.Summary
	if detail == "" {
		detail = ago
	}
	var touched []string
	if f := e.historyActiveSceneFile(); f != "" {
		touched = append(touched, f)
	}
	if len(beforeSnap.projectYAML) > 0 && len(afterSnap.projectYAML) > 0 && !bytes.Equal(beforeSnap.projectYAML, afterSnap.projectYAML) {
		touched = append(touched, project.FileName)
	}
	touched = append(touched, allExtra...)
	touched = normalizeHistoryFileList(touched)

	cmd := &restoreHistoryCmd{
		ed:             e,
		label:          fmt.Sprintf("Restore %s (%s)", ago, detail),
		summary:        summary,
		detail:         detail,
		files:          touched,
		restoredFromID: id,
		before:         beforeSnap,
		after:          afterSnap,
	}
	if err := e.History.execute(cmd); err != nil {
		return err
	}

	toastMsg := fmt.Sprintf("Restored to %s · %s", ago, detail)
	e.Status = toastMsg
	e.ShowToast(Toast{
		ID:             "history-restore",
		Kind:           ToastSuccess,
		Message:        toastMsg,
		ActionLabel:    e.T("toolbar.undo"),
		ActionShortcut: FormatShortcut(e.EffectiveShortcut(CmdUndo), e.defaultShortcutOS()),
		OnAction: func() {
			e.DismissToast()
			e.Undo()
		},
	})
	return nil
}

func (e *Editor) applyHistorySnapshot(snap historySnapshot) error {
	if len(snap.sceneYAML) > 0 {
		doc, err := sceneformat.ParseWith(snap.sceneYAML, sceneformat.ParseOptions{
			File:          snap.scenePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if err != nil {
			return err
		}
		e.Doc = doc
	}
	if len(snap.projectYAML) > 0 && e.Project != nil {
		pf, err := project.Parse(snap.projectYAML)
		if err != nil {
			return err
		}
		e.Project = pf
	}
	if len(snap.files) > 0 && e.ProjectPath != "" {
		root := filepath.Dir(e.ProjectPath)
		paths := make([]string, 0, len(snap.files))
		for p := range snap.files {
			paths = append(paths, p)
		}
		slices.Sort(paths)
		for _, rel := range paths {
			fsnap := snap.files[rel]
			abs, err := e.historyExtraFileAbsPath(root, rel)
			if err != nil {
				return err
			}
			if !fsnap.exists {
				if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				return err
			}
			if err := writeFileAtomic(abs, fsnap.data); err != nil {
				return err
			}
		}
		e.filesRev++
		e.snapshotProjectTree()
		e.invalidateProjectIndex()
	}
	return nil
}

func (e *Editor) historyExtraFileAbsPath(root, rel string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(rel))
	if filepath.IsAbs(cleaned) || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("editor: history file path %q leaves project root", rel)
	}
	return filepath.Join(root, cleaned), nil
}

func (e *Editor) snapshotExtraFiles(paths []string) map[string]historyFileSnapshot {
	if e == nil || e.ProjectPath == "" || len(paths) == 0 {
		return nil
	}
	root := filepath.Dir(e.ProjectPath)
	out := make(map[string]historyFileSnapshot, len(paths))
	for _, rel := range paths {
		abs, err := e.historyExtraFileAbsPath(root, rel)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			out[rel] = historyFileSnapshot{exists: false}
			continue
		}
		out[rel] = historyFileSnapshot{exists: true, data: data}
	}
	return out
}

func diffExtraFiles(before, after map[string]historyFileSnapshot) []string {
	var changed []string
	seen := make(map[string]bool, len(before)+len(after))
	for k := range before {
		seen[k] = true
	}
	for k := range after {
		seen[k] = true
	}
	for k := range seen {
		b := before[k]
		a := after[k]
		if b.exists != a.exists || !bytes.Equal(b.data, a.data) {
			changed = append(changed, k)
		}
	}
	slices.Sort(changed)
	return changed
}

func (e *Editor) captureHistorySnapshot(extraFiles []string) historySnapshot {
	var snap historySnapshot
	if e == nil {
		return snap
	}
	if e.Doc != nil {
		snap.sceneYAML, _ = e.SaveYAML()
		snap.scenePath = e.historyActiveSceneFile()
	}
	if e.Project != nil {
		snap.projectYAML, _ = project.Encode(e.Project)
	}
	if len(extraFiles) > 0 {
		snap.files = e.snapshotExtraFiles(extraFiles)
	}
	return snap
}

func (e *Editor) historyActiveSceneFile() string {
	if e == nil {
		return ""
	}
	if e.ScenePath != "" {
		return e.ScenePath
	}
	if e.FilePath != "" {
		if rel, ok := e.projectRel(e.FilePath); ok && rel != "" {
			return rel
		}
		return filepath.Base(e.FilePath)
	}
	if e.Doc != nil && e.Doc.Name != "" {
		return e.Doc.Name
	}
	return ""
}

func (e *Editor) recordHistoryStep(cmd Command, rec *historyRecord) {
	if e == nil || rec == nil {
		return
	}
	// Apply pending meta from RecordAgentTurn or review keep calls first.
	if m := e.historyPanel.pendingMeta; m != nil {
		e.historyPanel.pendingMeta = nil
		if m.Author != "" {
			rec.entry.Author = m.Author
		}
		if m.Summary != "" {
			rec.entry.Summary = m.Summary
		}
		if m.Detail != "" {
			rec.entry.Detail = m.Detail
		}
		if len(m.Files) > 0 {
			rec.entry.Files = slices.Clone(m.Files)
		}
		if m.ChangeCount > 0 {
			rec.entry.ChangeCount = m.ChangeCount
		}
		if m.KeptCount > 0 {
			rec.entry.KeptCount = m.KeptCount
		}
		if m.RestoredFromID > 0 {
			rec.entry.RestoredFromID = m.RestoredFromID
		}
		if len(m.BeforeFiles) > 0 {
			rec.beforeFiles = cloneHistoryFileSnapshots(m.BeforeFiles)
		}
		if len(m.AfterFiles) > 0 {
			rec.snapshot.files = cloneHistoryFileSnapshots(m.AfterFiles)
		}
	} else if e.historyPanel.agentDepth > 0 && rec.entry.Author == HistoryAuthorYou {
		rec.entry.Author = HistoryAuthorAgent
		rec.entry.Summary = strings.TrimSpace(strings.TrimPrefix(rec.entry.Summary, "Agent:"))
		if rec.entry.Summary == "" {
			rec.entry.Summary = strings.TrimSpace(cmd.Label())
		}
	}

	if len(rec.entry.Files) == 0 {
		if f := e.historyActiveSceneFile(); f != "" {
			rec.entry.Files = []string{f}
		}
	}
	rec.entry.Files = normalizeHistoryFileList(rec.entry.Files)

	// Capture scene and project YAML snapshot after the command ran.
	if len(rec.snapshot.sceneYAML) == 0 && e.Doc != nil {
		rec.snapshot.sceneYAML, _ = e.SaveYAML()
		rec.snapshot.scenePath = e.historyActiveSceneFile()
	}
	if len(rec.snapshot.projectYAML) == 0 && e.Project != nil {
		rec.snapshot.projectYAML, _ = project.Encode(e.Project)
	}
}

func (e *Editor) onHistoryCleared() {
	if e == nil {
		return
	}
	if e.historyPanel.preview != nil {
		e.historyPanel.preview = nil
		e.s3d.stale = true
	}
}

func (e *Editor) beginSaveHistoryBatch() {
	if e == nil {
		return
	}
	if e.historyPanel.savingDepth == 0 {
		e.historyPanel.savedFiles = nil
	}
	e.historyPanel.savingDepth++
}

func (e *Editor) noteSavedHistoryFile(p string) {
	if e == nil {
		return
	}
	clean := e.displayHistoryPath(p)
	if clean == "" {
		clean = e.historyActiveSceneFile()
	}
	if clean == "" {
		return
	}
	if e.historyPanel.savingDepth > 0 {
		e.historyPanel.savedFiles = append(e.historyPanel.savedFiles, clean)
		return
	}
	e.recordSavedHistoryMarker([]string{clean})
}

func (e *Editor) endSaveHistoryBatch() {
	if e == nil || e.historyPanel.savingDepth <= 0 {
		return
	}
	e.historyPanel.savingDepth--
	if e.historyPanel.savingDepth == 0 && len(e.historyPanel.savedFiles) > 0 {
		files := normalizeHistoryFileList(e.historyPanel.savedFiles)
		e.historyPanel.savedFiles = nil
		e.recordSavedHistoryMarker(files)
	}
}

func (e *Editor) recordSavedHistoryMarker(files []string) {
	if e == nil || e.History == nil || len(files) == 0 {
		return
	}
	files = normalizeHistoryFileList(files)
	bases := make([]string, 0, len(files))
	for _, f := range files {
		bases = append(bases, filepath.Base(filepath.FromSlash(f)))
	}
	snap := e.captureHistorySnapshot(nil)
	e.History.RecordSaved(files, strings.Join(bases, " · "), snap)
}

func (e *Editor) displayHistoryPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if rel, ok := e.projectRel(p); ok && rel != "" {
		return rel
	}
	if filepath.IsAbs(p) {
		return filepath.Base(p)
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func normalizeHistoryFileList(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = filepath.ToSlash(p)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// FormatHistoryRelativeTime formats ts relative to the editor's current History clock
// (for example "just now", "2 min ago", "2 hr ago", "yesterday 18:12", or "2026-11-23 14:20").
func (e *Editor) FormatHistoryRelativeTime(ts time.Time) string {
	return formatHistoryRelativeTime(ts, e.HistoryNow())
}

func formatHistoryRelativeTime(ts, now time.Time) string {
	if ts.IsZero() {
		return "just now"
	}
	d := now.Sub(ts)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d / time.Minute)
		return fmt.Sprintf("%d min ago", mins)
	}
	if sameCalendarDay(ts, now) {
		hrs := int(d / time.Hour)
		if hrs == 1 {
			return "1 hr ago"
		}
		return fmt.Sprintf("%d hr ago", hrs)
	}
	if sameCalendarDay(ts, now.AddDate(0, 0, -1)) {
		return "yesterday " + ts.Format("15:04")
	}
	return ts.Format("2006-01-02 15:04")
}

// FormatHistoryDayLabel returns the uppercase day group header for ts ("TODAY", "YESTERDAY", or "YYYY-MM-DD").
func (e *Editor) FormatHistoryDayLabel(ts time.Time) string {
	return formatHistoryDayLabel(ts, e.HistoryNow())
}

func formatHistoryDayLabel(ts, now time.Time) string {
	if ts.IsZero() || sameCalendarDay(ts, now) {
		return "TODAY"
	}
	if sameCalendarDay(ts, now.AddDate(0, 0, -1)) {
		return "YESTERDAY"
	}
	return strings.ToUpper(ts.Format("2006-01-02"))
}

func sameCalendarDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// FormatHistoryEntryHeadline formats the primary line of an entry, matching AskInPlace.dc.html
// (for example "Agent · slimes and coins", "You · moved Knight", or "Saved").
func FormatHistoryEntryHeadline(entry HistoryEntry) string {
	if entry.Kind == HistoryEntrySaved {
		return "Saved"
	}
	if prefix := entry.Author.Label(); prefix != "" {
		return prefix + " · " + entry.Summary
	}
	return entry.Summary
}

// FormatHistoryEntrySubtitle formats the secondary metadata line of an entry with its relative time
// (for example "6 changes · kept 5 · 2 min ago", "x 416 → 452 · 4 min ago", or "level_01.scene.yaml · 15 min ago").
func FormatHistoryEntrySubtitle(entry HistoryEntry, relTime string) string {
	var parts []string
	if entry.Detail != "" {
		parts = append(parts, entry.Detail)
	} else if len(entry.Files) > 0 {
		parts = append(parts, filepath.Base(filepath.FromSlash(entry.Files[0])))
	}
	if relTime != "" {
		parts = append(parts, relTime)
	}
	return strings.Join(parts, " · ")
}

type restoreHistoryCmd struct {
	ed             *Editor
	label          string
	summary        string
	detail         string
	files          []string
	restoredFromID uint64
	before         historySnapshot
	after          historySnapshot
}

func (c *restoreHistoryCmd) Label() string { return c.label }
func (c *restoreHistoryCmd) Do()           { _ = c.do() }
func (c *restoreHistoryCmd) Undo()         { _ = c.undo() }

func (c *restoreHistoryCmd) do() error {
	return c.ed.applyHistorySnapshot(c.after)
}

func (c *restoreHistoryCmd) undo() error {
	return c.ed.applyHistorySnapshot(c.before)
}

func (c *restoreHistoryCmd) historyMeta() HistoryMeta {
	return HistoryMeta{
		Author:         HistoryAuthorYou,
		Summary:        c.summary,
		Detail:         c.detail,
		Files:          slices.Clone(c.files),
		RestoredFromID: c.restoredFromID,
		BeforeFiles:    cloneHistoryFileSnapshots(c.before.files),
		AfterFiles:     cloneHistoryFileSnapshots(c.after.files),
	}
}

type agentFilesCmd struct {
	ed      *Editor
	label   string
	before  map[string]historyFileSnapshot
	after   map[string]historyFileSnapshot
	applied bool
}

func (c *agentFilesCmd) Label() string { return c.label }
func (c *agentFilesCmd) Do()           { _ = c.do() }
func (c *agentFilesCmd) Undo()         { _ = c.undo() }

func (c *agentFilesCmd) do() error {
	if err := c.ed.applyHistorySnapshot(historySnapshot{files: c.after}); err != nil {
		return err
	}
	c.applied = true
	return nil
}

func (c *agentFilesCmd) undo() error {
	if err := c.ed.applyHistorySnapshot(historySnapshot{files: c.before}); err != nil {
		return err
	}
	c.applied = false
	return nil
}
