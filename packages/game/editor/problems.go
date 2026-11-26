package editor

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/l10n"
	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const defaultProblemCheckDebounce = 80 * time.Millisecond

// problemCheckRun tracks one scheduled or in-flight project check and ensures
// its completion channel is closed at most once across concurrent cancellations
// and timer callbacks.
type problemCheckRun struct {
	done      chan struct{}
	closeOnce sync.Once
}

func (r *problemCheckRun) finish() {
	if r != nil {
		r.closeOnce.Do(func() {
			close(r.done)
		})
	}
}

// problemState holds the asynchronous project-check, live-edit debounce, ignore,
// and agent-proposed-fix state for an Editor. Its fields are guarded by mu so
// background check goroutines and callers on any goroutine can read or publish
// results without racing with UI state.
type problemState struct {
	mu          sync.Mutex
	gen         uint64
	version     uint64
	running     bool
	pending     bool
	hasRun      bool
	projectPath string
	rawProblems []assetcheck.Problem
	problems    []assetcheck.Problem
	checkErr    error
	run         *problemCheckRun
	timer       *time.Timer
	scope       *core.Scope

	// ignored maps an ignored problem to the SHA-256 hex digest of its file's
	// live content when ignored. Any subsequent edit to that file changes its
	// digest and evicts its ignored entries so still-holding problems reappear.
	ignored map[assetcheck.Problem]string

	// proposals holds agent-proposed fixes awaiting the person's review.
	// Proposals never mutate documents until ApplyProposedFix is called.
	proposals          []ProposedProblemFix
	nextFixSeq         uint64
	agentPanelOpen     bool
	activeProblem      *assetcheck.Problem
	highlightedLine    string
	highlightedSnippet string
	questions          []ProblemQuestionRecord

	// problemOverlayHidden toggles the Spec Problem viewport overlay (visible by default).
	problemOverlayHidden bool

	// Editor-goroutine-only cache for attributing active-scene problems to objects.
	objCacheRev       uint64
	objCacheProbVer   uint64
	objCacheScenePath string
	objCache          map[string][]assetcheck.Problem

	// checkFn overrides runProjectCheck in internal tests; nil uses runProjectCheckWithOverrides.
	checkFn func(dir string, reg *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error)
}

func (p *problemState) setScope(cx *core.Scope) {
	p.mu.Lock()
	p.scope = cx
	p.mu.Unlock()
}

func (e *Editor) problemDebounce() time.Duration {
	if e != nil && e.ProblemCheckDebounce > 0 {
		return e.ProblemCheckDebounce
	}
	return defaultProblemCheckDebounce
}

// stopProblemCheckTimer cancels any pending debounced or in-flight check and
// detaches the UI scope (used on EditorView cleanup and Editor.Close).
func (e *Editor) stopProblemCheckTimer() {
	if e == nil {
		return
	}
	e.problems.mu.Lock()
	if e.problems.timer != nil {
		e.problems.timer.Stop()
		e.problems.timer = nil
	}
	e.problems.gen++
	e.problems.pending = false
	e.problems.running = false
	oldRun := e.problems.run
	e.problems.run = nil
	e.problems.scope = nil
	e.problems.mu.Unlock()
	oldRun.finish()
}

// resetProblemsForProject cancels any pending or in-flight check and clears all
// project-scoped problems, ignores, proposed fixes, questions, and caches while
// keeping any attached UI scope intact. Called on OpenProject (including same-project
// reopen) and OpenDemo.
func (e *Editor) resetProblemsForProject() {
	if e == nil {
		return
	}
	e.problems.mu.Lock()
	if e.problems.timer != nil {
		e.problems.timer.Stop()
		e.problems.timer = nil
	}
	oldRun := e.problems.run
	e.problems.gen++
	e.problems.version++
	e.problems.running = false
	e.problems.pending = false
	e.problems.hasRun = false
	e.problems.projectPath = ""
	e.problems.rawProblems = nil
	e.problems.problems = nil
	e.problems.checkErr = nil
	e.problems.run = nil
	e.problems.ignored = nil
	e.problems.proposals = nil
	e.problems.activeProblem = nil
	e.problems.highlightedLine = ""
	e.problems.highlightedSnippet = ""
	e.problems.agentPanelOpen = false
	e.problems.questions = nil
	e.problems.mu.Unlock()

	e.problems.objCache = nil
	e.problems.objCacheRev = 0
	e.problems.objCacheProbVer = 0
	e.problems.objCacheScenePath = ""

	oldRun.finish()
}

// CheckProblems runs assetcheck.CheckWith (using the editor's component registry
// and any unsaved open documents) and project.Check over the open project
// asynchronously off the UI goroutine. Each call supersedes any in-flight or
// pending debounced check so stale results never overwrite newer ones. It is a
// no-op when no project is open.
func (e *Editor) CheckProblems() {
	e.startProblemCheck(0)
}

// scheduleLiveProblemCheck debounces a live project check over disk plus all
// open unsaved documents after an edit (by the person or the agent).
func (e *Editor) scheduleLiveProblemCheck() {
	if e == nil || e.Project == nil || e.ProjectPath == "" || e.playing {
		return
	}
	e.startProblemCheck(e.problemDebounce())
}

func (e *Editor) startProblemCheck(delay time.Duration) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		e.resetProblemsForProject()
		return
	}

	projectPath := e.ProjectPath
	dir := filepath.Dir(projectPath)
	reg := e.components
	overrides := e.unsavedProjectFiles()

	myRun := &problemCheckRun{done: make(chan struct{})}
	var oldPendingRun *problemCheckRun

	e.problems.mu.Lock()
	if e.problems.timer != nil {
		e.problems.timer.Stop()
		e.problems.timer = nil
	}
	if e.problems.pending && e.problems.run != nil {
		oldPendingRun = e.problems.run
	}
	e.problems.gen++
	gen := e.problems.gen
	if e.problems.projectPath != projectPath {
		e.problems.projectPath = projectPath
		e.problems.rawProblems = nil
		e.problems.problems = nil
		e.problems.checkErr = nil
		e.problems.hasRun = false
		e.problems.ignored = nil
		e.problems.proposals = nil
		e.problems.activeProblem = nil
		e.problems.highlightedLine = ""
		e.problems.highlightedSnippet = ""
		e.problems.agentPanelOpen = false
		e.problems.questions = nil
		e.problems.version++
	}
	e.problems.running = true
	e.problems.pending = delay > 0
	e.problems.run = myRun
	fn := e.problems.checkFn
	if delay > 0 {
		e.problems.timer = time.AfterFunc(delay, func() {
			e.problems.mu.Lock()
			if e.problems.gen != gen {
				e.problems.mu.Unlock()
				myRun.finish()
				return
			}
			e.problems.timer = nil
			e.problems.pending = false
			e.problems.mu.Unlock()
			e.runProblemCheckAsync(gen, dir, reg, overrides, fn, myRun)
		})
		e.problems.mu.Unlock()
		oldPendingRun.finish()
		return
	}
	e.problems.mu.Unlock()
	oldPendingRun.finish()
	e.notify()

	go e.runProblemCheckAsync(gen, dir, reg, overrides, fn, myRun)
}

func (e *Editor) runProblemCheckAsync(
	gen uint64,
	dir string,
	reg *sceneformat.ComponentRegistry,
	overrides map[string][]byte,
	fn func(dir string, reg *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error),
	myRun *problemCheckRun,
) {
	defer myRun.finish()

	var probs []assetcheck.Problem
	var err error
	if fn != nil {
		probs, err = fn(dir, reg)
	} else {
		probs, err = runProjectCheckWithOverrides(dir, reg, overrides)
	}

	// Collect ignored file paths under mu, then compute their content hashes off the
	// UI thread and outside mu so disk I/O never blocks lock holders or UI frames.
	e.problems.mu.Lock()
	if e.problems.gen != gen {
		e.problems.mu.Unlock()
		return
	}
	var ignoredFiles []string
	if len(e.problems.ignored) > 0 {
		seen := make(map[string]struct{}, len(e.problems.ignored))
		for p := range e.problems.ignored {
			if _, ok := seen[p.File]; !ok {
				seen[p.File] = struct{}{}
				ignoredFiles = append(ignoredFiles, p.File)
			}
		}
	}
	e.problems.mu.Unlock()

	var fileHashes map[string]string
	if len(ignoredFiles) > 0 {
		fileHashes = make(map[string]string, len(ignoredFiles))
		for _, f := range ignoredFiles {
			fileHashes[f] = fileContentHash(dir, f, overrides)
		}
	}

	e.problems.mu.Lock()
	if e.problems.gen != gen {
		e.problems.mu.Unlock()
		return
	}
	e.evictChangedIgnoresWithHashesLocked(fileHashes)
	e.problems.rawProblems = probs
	e.problems.checkErr = err
	e.recomputeVisibleProblemsLocked()
	e.pruneStaleProposalsLocked()
	e.problems.running = false
	e.problems.pending = false
	e.problems.hasRun = true
	e.problems.version++
	scope := e.problems.scope
	e.problems.mu.Unlock()

	if scope != nil {
		scope.Post(func() {
			e.notify()
		})
	}
}

func (e *Editor) evictChangedIgnoresWithHashesLocked(fileHashes map[string]string) bool {
	if len(e.problems.ignored) == 0 || len(fileHashes) == 0 {
		return false
	}
	changed := false
	for p, savedHash := range e.problems.ignored {
		curHash, ok := fileHashes[p.File]
		if !ok {
			continue
		}
		if curHash != savedHash {
			delete(e.problems.ignored, p)
			changed = true
		}
	}
	if len(e.problems.ignored) == 0 {
		e.problems.ignored = nil
	}
	return changed
}

func (e *Editor) recomputeVisibleProblemsLocked() {
	if len(e.problems.rawProblems) == 0 {
		e.problems.problems = nil
		return
	}
	if len(e.problems.ignored) == 0 {
		e.problems.problems = slices.Clone(e.problems.rawProblems)
		return
	}
	visible := make([]assetcheck.Problem, 0, len(e.problems.rawProblems))
	for _, p := range e.problems.rawProblems {
		if _, ign := e.problems.ignored[p]; !ign {
			visible = append(visible, p)
		}
	}
	e.problems.problems = visible
}

func (e *Editor) pruneStaleProposalsLocked() {
	if len(e.problems.proposals) == 0 {
		return
	}
	kept := e.problems.proposals[:0]
	for _, prop := range e.problems.proposals {
		if slices.Contains(e.problems.problems, prop.Problem) {
			kept = append(kept, prop)
		}
	}
	if len(kept) == 0 {
		e.problems.proposals = nil
	} else {
		e.problems.proposals = kept
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// readProjectFileBounded validates rel against root (rejecting traversal,
// backslashes, colons, control chars, absolute paths, and symlink escapes via
// resolveProjectFileWithinRoot) and reads up to maxBytes.
func readProjectFileBounded(root, rel string, maxBytes int64) ([]byte, error) {
	_, abs, err := resolveProjectFileWithinRoot(root, rel, false)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("editor: %q is a directory", rel)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("editor: file %q exceeds maximum size (%d bytes)", rel, maxBytes)
	}
	if maxBytes > 0 {
		data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxBytes {
			return nil, fmt.Errorf("editor: file %q exceeds maximum size (%d bytes)", rel, maxBytes)
		}
		return data, nil
	}
	return io.ReadAll(f)
}

func fileContentHash(dir, relPath string, overrides map[string][]byte) string {
	if relPath == "" {
		return ""
	}
	if dir != "" {
		cleanRel, _, err := resolveProjectFileWithinRoot(dir, relPath, true)
		if err != nil {
			return "invalid:" + err.Error()
		}
		relPath = cleanRel
	}
	if overrides != nil {
		if data, ok := overrides[relPath]; ok {
			return sha256Hex(data)
		}
	}
	if dir == "" {
		return ""
	}
	data, err := readProjectFileBounded(dir, relPath, maxInspectorYAMLBytes)
	if err != nil {
		return "missing:" + err.Error()
	}
	return sha256Hex(data)
}

// Problems returns a copy of the visible (non-ignored) problems found by the
// last completed project check, sorted by file, line, column and message. It is
// safe to call from any goroutine.
func (e *Editor) Problems() []assetcheck.Problem {
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	if len(e.problems.problems) == 0 {
		return nil
	}
	out := make([]assetcheck.Problem, len(e.problems.problems))
	copy(out, e.problems.problems)
	return out
}

// ProblemCount returns the number of visible (non-ignored) problems found by
// the last completed project check. It is safe to call from any goroutine.
func (e *Editor) ProblemCount() int {
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return len(e.problems.problems)
}

// ProblemErr returns any fatal error that prevented checking the open project's
// root directory, or nil. It is safe to call from any goroutine.
func (e *Editor) ProblemErr() error {
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return e.problems.checkErr
}

// CheckingProblems reports whether a background or debounced project check is
// in flight. It is safe to call from any goroutine.
func (e *Editor) CheckingProblems() bool {
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return e.problems.running || e.problems.pending
}

// WaitForProblems blocks until any pending debounced or in-flight project check
// (including newer checks started while waiting) finishes. It returns
// immediately when idle.
func (e *Editor) WaitForProblems() {
	if e == nil {
		return
	}
	for {
		e.problems.mu.Lock()
		running := e.problems.running || e.problems.pending
		run := e.problems.run
		e.problems.mu.Unlock()
		if !running || run == nil {
			return
		}
		<-run.done
	}
}

// IgnoreProblem hides p from the Problems panel, status bar count, scene tree
// badges, and viewport problem overlay until p.File changes again (in the editor
// or on disk). If p still holds after a subsequent edit to p.File, it reappears.
func (e *Editor) IgnoreProblem(p assetcheck.Problem) {
	if e == nil || p.File == "" || e.Playing() || e.StartScreenOpen() || e.MergeConflictOpen() {
		return
	}
	dir := ""
	if e.ProjectPath != "" {
		dir = filepath.Dir(e.ProjectPath)
		cleanRel, _, err := resolveProjectFileWithinRoot(dir, p.File, true)
		if err != nil {
			return
		}
		p.File = cleanRel
	}
	overrides := e.unsavedProjectFiles()
	hash := fileContentHash(dir, p.File, overrides)

	e.problems.mu.Lock()
	if e.problems.ignored == nil {
		e.problems.ignored = make(map[assetcheck.Problem]string)
	}
	e.problems.ignored[p] = hash
	e.recomputeVisibleProblemsLocked()
	e.pruneStaleProposalsLocked()
	if e.problems.activeProblem != nil && *e.problems.activeProblem == p {
		if len(e.problems.proposals) > 0 {
			next := e.problems.proposals[0].Problem
			e.problems.activeProblem = &next
		} else {
			e.problems.activeProblem = nil
			e.problems.highlightedLine = ""
			e.problems.highlightedSnippet = ""
			e.problems.agentPanelOpen = false
		}
	}
	e.problems.version++
	e.problems.mu.Unlock()

	e.Status = "Ignored problem at " + p.Location()
	e.notify()
}

// IsProblemIgnored reports whether p is currently ignored.
func (e *Editor) IsProblemIgnored(p assetcheck.Problem) bool {
	if e == nil {
		return false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	_, ok := e.problems.ignored[p]
	return ok
}

// ClearIgnoredProblems restores all ignored problems immediately.
func (e *Editor) ClearIgnoredProblems() {
	if e == nil {
		return
	}
	e.problems.mu.Lock()
	if len(e.problems.ignored) == 0 {
		e.problems.mu.Unlock()
		return
	}
	e.problems.ignored = nil
	e.recomputeVisibleProblemsLocked()
	e.problems.version++
	e.problems.mu.Unlock()
	e.notify()
}

// SelectFile selects relPath, a slash-separated path from the open project's
// root, in the file browser.
func (e *Editor) SelectFile(relPath string) {
	e.selectFile(relPath)
	e.notify()
}

func (e *Editor) selectFile(relPath string) {
	if e.SelectedFile != relPath {
		e.importPreview.resetInteraction()
		e.stopModelPreviewLoop()
	}
	e.SelectedFile = relPath
	e.selectedFileRev++
	e.inspectAssetFile = relPath != "" && e.AssetImportKind(relPath) != AssetImportNone
	if e.inspectAssetFile {
		e.CancelCameraDrag()
		e.CancelCameraBoundsDrag()
		e.CancelRoomDrag()
	}
}

// OpenProblem navigates to problem p in the open project: if p.File names a scene file (*.scene.yaml, *.scene.yml, or a scene listed under Project.Autoload.Scenes), it opens that scene with OpenScene (and updates ScenePath and SelectedFile, and clears OnSave), unless it is the scene already open (ScenePath), which keeps its edits and undo history and is only selected; if opening the scene fails (for example because the YAML does not parse), it still selects p.File in the file browser and returns the error. For any non-scene file, it selects p.File in the file browser. It returns ErrNoProject when no project is open. Opening another scene opens it in a tab without losing unsaved edits in other tabs.
func (e *Editor) OpenProblem(p assetcheck.Problem) error {
	if err := e.problemActionBlockedErr("opening a problem"); err != nil {
		return err
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if p.File == "" {
		return nil
	}
	cleanRel, _, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), p.File, true)
	if err != nil {
		return err
	}
	p.File = cleanRel
	loc := p.Location()
	if isScreenFile(p.File) {
		e.selectFile(p.File)
		return e.OpenHUDScreen(p.File)
	}
	if isProjectScene(e.Project, p.File) && p.File != e.ScenePath {
		// OpenScene, as the file browser opens scenes, so OnSave, which persisted the scene open before, does not get this one.
		if err := e.OpenScene(p.File); err != nil {
			e.selectFile(p.File)
			e.notify()
			return err
		}
		e.selectFile(p.File)
		e.Status = "Opened " + loc
		e.notify()
		return nil
	}
	e.selectFile(p.File)
	e.Status = "Selected " + loc
	e.notify()
	return nil
}

// problemsStatusText returns the live status bar label for the open project's
// problem count, or "" when no project is open.
func (e *Editor) problemsStatusText() string {
	if e.Project == nil || e.ProjectPath == "" {
		return ""
	}
	e.problems.mu.Lock()
	count := len(e.problems.problems)
	running := e.problems.running || e.problems.pending
	hasRun := e.problems.hasRun
	err := e.problems.checkErr
	e.problems.mu.Unlock()
	if err != nil {
		return e.T("problems.statusError")
	}
	if running && !hasRun {
		return e.T("problems.statusChecking")
	}
	return e.T("problems.statusCount", l10n.Param("count", count))
}

// isProjectScene reports whether relPath is a scene file of project f: either
// named *.scene.yaml / *.scene.yml or listed under f.Autoload.Scenes.
func isProjectScene(f *project.File, relPath string) bool {
	lower := strings.ToLower(relPath)
	if strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml") {
		return true
	}
	if f != nil && f.Autoload != nil {
		for _, s := range f.Autoload.Scenes {
			if s.Path == relPath {
				return true
			}
		}
	}
	return false
}

// --- Live unsaved document capture & overlay FS ---

func (e *Editor) projectRelPathForFile(absPath string) string {
	if e == nil || e.ProjectPath == "" || absPath == "" {
		return ""
	}
	root := filepath.Dir(e.ProjectPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// hasOpenProjectSceneTab reports in O(len(tabs)) without encoding any YAML
// whether the editor currently has an open project scene tab. Used by
// liveResolver() on 60 Hz Rebuild() paths to avoid per-frame YAML serialization.
func (e *Editor) hasOpenProjectSceneTab() bool {
	if e == nil {
		return false
	}
	if e.Doc != nil && (e.ScenePath != "" || e.projectRelPathForFile(e.FilePath) != "") {
		return true
	}
	for i, tab := range e.tabs {
		if i == e.activeTab || tab == nil || tab.doc == nil {
			continue
		}
		if tab.scenePath != "" || e.projectRelPathForFile(tab.filePath) != "" {
			return true
		}
	}
	return false
}

// inactiveTabLiveYAML returns the cached (yamlBytes, dirty) state for an inactive
// tab, encoding its SceneDoc at most once until the tab is synced, moved, or saved.
func (e *Editor) inactiveTabLiveYAML(tab *sceneTab) ([]byte, bool) {
	if tab == nil || tab.doc == nil {
		return nil, false
	}
	if tab.liveYAMLValid &&
		tab.liveYAMLDoc == tab.doc &&
		tab.liveYAMLFilesRev == e.filesRev &&
		bytes.Equal(tab.liveYAMLSaved, tab.sceneSaved) {
		return tab.liveYAML, tab.liveYAMLDirty
	}
	data, dirty := tabDirtyLiveYAML(tab.doc, tab.sceneSaved)
	tab.liveYAMLValid = true
	tab.liveYAMLDirty = dirty
	tab.liveYAML = data
	tab.liveYAMLDoc = tab.doc
	tab.liveYAMLSaved = tab.sceneSaved
	tab.liveYAMLFilesRev = e.filesRev
	return data, dirty
}

func (e *Editor) hasDirtySceneTab() bool {
	if e == nil {
		return false
	}
	if e.ScenePath != "" && e.Doc != nil {
		if _, dirty := tabDirtyLiveYAML(e.Doc, e.sceneSaved); dirty {
			return true
		}
	}
	for i, tab := range e.tabs {
		if i == e.activeTab || tab == nil || tab.doc == nil {
			continue
		}
		rel := tab.scenePath
		if rel == "" {
			rel = e.projectRelPathForFile(tab.filePath)
		}
		if rel == "" {
			continue
		}
		if _, dirty := e.inactiveTabLiveYAML(tab); dirty {
			return true
		}
	}
	return false
}

func (e *Editor) liveOpenSceneDoc(relPath string) *sceneformat.SceneDoc {
	if e == nil || relPath == "" {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(relPath, `\`, "/"))
	activeRel := e.ScenePath
	if activeRel == "" {
		activeRel = e.projectRelPathForFile(e.FilePath)
	}
	if clean == activeRel && e.Doc != nil {
		if _, dirty := tabDirtyLiveYAML(e.Doc, e.sceneSaved); dirty {
			return e.Doc.Clone()
		}
		return nil
	}
	for i, tab := range e.tabs {
		if i == e.activeTab || tab == nil || tab.doc == nil {
			continue
		}
		rel := tab.scenePath
		if rel == "" {
			rel = e.projectRelPathForFile(tab.filePath)
		}
		if rel == clean {
			if _, dirty := e.inactiveTabLiveYAML(tab); dirty {
				return tab.doc.Clone()
			}
			return nil
		}
	}
	return nil
}

// tabDirtyLiveYAML encodes doc once and reports whether it differs from saved,
// returning the encoded live bytes (falling back to lenient YAML when doc
// currently violates sceneformat.Validate) without encoding a second time.
func tabDirtyLiveYAML(doc *sceneformat.SceneDoc, saved []byte) ([]byte, bool) {
	if doc == nil {
		return nil, false
	}
	if data, err := sceneformat.Encode(doc); err == nil {
		if bytes.Equal(data, saved) {
			return nil, false
		}
		return data, true
	}
	data, err := encodeSceneDocLenient(doc)
	if err != nil {
		return nil, true
	}
	return data, true
}

func encodeSceneDocLive(doc *sceneformat.SceneDoc) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("editor: nil scene document")
	}
	if data, err := sceneformat.Encode(doc); err == nil {
		return data, nil
	}
	return encodeSceneDocLenient(doc)
}

// encodeSceneDocLenient encodes doc when it currently violates Validate (for example
// a broken connection target in a single scene) so assetcheck can parse and report
// exact line and column positions.
func encodeSceneDocLenient(doc *sceneformat.SceneDoc) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("editor: nil scene document")
	}
	var buf bytes.Buffer
	buf.WriteString(sceneformat.SchemaCommentFor(doc.Version) + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// unsavedProjectFiles captures immutable YAML snapshots of every open dirty
// project document (open scene tabs, game.yaml, tileset) on the editor goroutine
// so background checks inspect live unsaved edits without touching Editor state.
// Active scenes are encoded at most once; untouched inactive tabs reuse their cache.
func (e *Editor) unsavedProjectFiles() map[string][]byte {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil
	}
	var overrides map[string][]byte
	put := func(rel string, data []byte) {
		if len(data) == 0 {
			return
		}
		clean := path.Clean(strings.ReplaceAll(rel, `\`, "/"))
		if clean == "" || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
			return
		}
		if overrides == nil {
			overrides = make(map[string][]byte)
		}
		overrides[clean] = data
	}

	activeRel := e.ScenePath
	if activeRel == "" {
		activeRel = e.projectRelPathForFile(e.FilePath)
	}
	if activeRel != "" && e.Doc != nil {
		if data, dirty := tabDirtyLiveYAML(e.Doc, e.sceneSaved); dirty {
			put(activeRel, data)
		}
	}
	for i, tab := range e.tabs {
		if i == e.activeTab || tab == nil || tab.doc == nil {
			continue
		}
		rel := tab.scenePath
		if rel == "" {
			rel = e.projectRelPathForFile(tab.filePath)
		}
		if rel == "" {
			continue
		}
		if data, dirty := e.inactiveTabLiveYAML(tab); dirty {
			put(rel, data)
		}
	}
	if e.Project != nil {
		if data, err := project.Encode(e.Project); err == nil && !bytes.Equal(data, e.projectSaved) {
			put(project.FileName, data)
		}
	}
	if e.TilesetOpen() && e.TilesetPath() != "" && e.TilesetDirty() {
		if data, err := e.SaveTilesetYAML(); err == nil {
			put(e.TilesetPath(), data)
		}
	}
	return overrides
}

// overlayFS layers in-memory unsaved project files on top of a disk fs.FS so
// assetcheck.CheckWith and project.Check validate unsaved edits transparently.
type overlayFS struct {
	base  fs.FS
	files map[string][]byte
}

func newOverlayFS(base fs.FS, overrides map[string][]byte) fs.FS {
	if len(overrides) == 0 {
		return base
	}
	return &overlayFS{base: base, files: overrides}
}

func (o *overlayFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := o.files[name]; ok {
		return &overlayMemFile{
			name: path.Base(name),
			data: data,
		}, nil
	}
	if o.hasDirPrefix(name) {
		entries, err := o.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &overlayDirFile{name: path.Base(name), entries: entries}, nil
	}
	f, err := o.base.Open(name)
	if err != nil {
		return nil, err
	}
	info, statErr := f.Stat()
	if statErr == nil && info.IsDir() {
		_ = f.Close()
		entries, derr := o.ReadDir(name)
		if derr != nil {
			return nil, derr
		}
		return &overlayDirFile{name: info.Name(), entries: entries}, nil
	}
	return f, nil
}

func (o *overlayFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := o.files[name]; ok {
		return slices.Clone(data), nil
	}
	return fs.ReadFile(o.base, name)
}

func (o *overlayFS) Stat(name string) (fs.FileInfo, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrInvalid}
	}
	if data, ok := o.files[name]; ok {
		return overlayFileInfo{name: path.Base(name), size: int64(len(data))}, nil
	}
	if info, err := fs.Stat(o.base, name); err == nil {
		return info, nil
	} else if o.hasDirPrefix(name) {
		return overlayFileInfo{name: path.Base(name), dir: true}, nil
	} else {
		return nil, err
	}
}

func (o *overlayFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	byName := make(map[string]fs.DirEntry)
	baseEntries, baseErr := fs.ReadDir(o.base, name)
	if baseErr == nil {
		for _, ent := range baseEntries {
			byName[ent.Name()] = ent
		}
	}
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}
	foundOverlay := false
	for p, data := range o.files {
		if prefix != "" && !strings.HasPrefix(p, prefix) {
			continue
		}
		rest := strings.TrimPrefix(p, prefix)
		if rest == "" {
			continue
		}
		foundOverlay = true
		if child, _, hasSlash := strings.Cut(rest, "/"); hasSlash {
			if _, exists := byName[child]; !exists {
				byName[child] = overlayDirEntry{info: overlayFileInfo{name: child, dir: true}}
			}
		} else {
			byName[rest] = overlayDirEntry{info: overlayFileInfo{name: rest, size: int64(len(data))}}
		}
	}
	if baseErr != nil && !foundOverlay {
		return nil, baseErr
	}
	out := make([]fs.DirEntry, 0, len(byName))
	for _, ent := range byName {
		out = append(out, ent)
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int { return cmp.Compare(a.Name(), b.Name()) })
	return out, nil
}

func (o *overlayFS) Sub(dir string) (fs.FS, error) {
	if !fs.ValidPath(dir) {
		return nil, &fs.PathError{Op: "sub", Path: dir, Err: fs.ErrInvalid}
	}
	if dir == "." {
		return o, nil
	}
	subBase, err := fs.Sub(o.base, dir)
	if err != nil {
		return nil, err
	}
	prefix := dir + "/"
	var subFiles map[string][]byte
	for p, data := range o.files {
		if rest, ok := strings.CutPrefix(p, prefix); ok && rest != "" {
			if subFiles == nil {
				subFiles = make(map[string][]byte)
			}
			subFiles[rest] = data
		}
	}
	return newOverlayFS(subBase, subFiles), nil
}

func (o *overlayFS) hasDirPrefix(name string) bool {
	if name == "." {
		return len(o.files) > 0
	}
	prefix := name + "/"
	for p := range o.files {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

type overlayFileInfo struct {
	name string
	size int64
	dir  bool
}

func (i overlayFileInfo) Name() string { return i.name }
func (i overlayFileInfo) Size() int64  { return i.size }
func (i overlayFileInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (i overlayFileInfo) ModTime() time.Time { return time.Time{} }
func (i overlayFileInfo) IsDir() bool        { return i.dir }
func (i overlayFileInfo) Sys() any           { return nil }

type overlayDirEntry struct {
	info overlayFileInfo
}

func (e overlayDirEntry) Name() string               { return e.info.name }
func (e overlayDirEntry) IsDir() bool                { return e.info.dir }
func (e overlayDirEntry) Type() fs.FileMode          { return e.info.Mode().Type() }
func (e overlayDirEntry) Info() (fs.FileInfo, error) { return e.info, nil }

type overlayMemFile struct {
	name string
	data []byte
	off  int
}

func (f *overlayMemFile) Stat() (fs.FileInfo, error) {
	return overlayFileInfo{name: f.name, size: int64(len(f.data))}, nil
}

func (f *overlayMemFile) Read(p []byte) (int, error) {
	if f.off >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.off:])
	f.off += n
	return n, nil
}

func (f *overlayMemFile) Close() error { return nil }

type overlayDirFile struct {
	name    string
	entries []fs.DirEntry
	off     int
}

func (d *overlayDirFile) Stat() (fs.FileInfo, error) {
	return overlayFileInfo{name: d.name, dir: true}, nil
}

func (d *overlayDirFile) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: errors.New("is a directory")}
}

func (d *overlayDirFile) Close() error { return nil }

func (d *overlayDirFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if n <= 0 {
		out := d.entries[d.off:]
		d.off = len(d.entries)
		return out, nil
	}
	if d.off >= len(d.entries) {
		return nil, io.EOF
	}
	end := min(d.off+n, len(d.entries))
	out := d.entries[d.off:end]
	d.off = end
	return out, nil
}

// --- Project check runner ---

// runProjectCheck runs assetcheck.CheckWith (with reg) and project.Check over
// dir on disk.
func runProjectCheck(dir string, reg *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error) {
	return runProjectCheckWithOverrides(dir, reg, nil)
}

// runProjectCheckWithOverrides runs assetcheck.CheckWith (with reg) and
// project.Check over dir overlaid with any unsaved in-memory project files,
// combining and sorting their problems without duplicating faults that
// assetcheck already reported with file and line.
func runProjectCheckWithOverrides(dir string, reg *sceneformat.ComponentRegistry, overrides map[string][]byte) ([]assetcheck.Problem, error) {
	fsys := newOverlayFS(os.DirFS(dir), overrides)
	report, err := assetcheck.CheckWith(fsys, assetcheck.Options{Components: reg})
	if err != nil {
		return nil, err
	}
	problems := slices.Clone(report.Problems)

	proj, projErr := project.Check(fsys)
	if projErr != nil {
		problems = append(problems, projectCheckProblems(proj, projErr, problems)...)
	}

	slices.SortFunc(problems, func(a, b assetcheck.Problem) int {
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), cmp.Compare(a.Message, b.Message))
	})
	problems = slices.Compact(problems)
	return problems, nil
}

// projectCheckProblems turns the errors returned by project.Check into
// assetcheck.Problem entries, omitting duplicates of problems assetcheck
// already reported with more specific location information.
func projectCheckProblems(f *project.File, err error, existing []assetcheck.Problem) []assetcheck.Problem {
	var out []assetcheck.Problem
	for _, sub := range flattenErrors(err) {
		msg := sub.Error()
		if f == nil {
			// ReadFS failed on game.yaml. If assetcheck already reported a game.yaml
			// parse error via refs.Scan, skip the duplicate.
			if hasFileLineProblem(existing, project.FileName, 0) {
				continue
			}
			line, col := errorPos(sub)
			out = append(out, assetcheck.Problem{
				File:    project.FileName,
				Line:    line,
				Column:  col,
				Message: msg,
			})
			continue
		}

		if p, handled, dup := matchAutoloadError(f, sub, existing); handled {
			if !dup {
				out = append(out, p)
			}
			continue
		}

		file := project.FileName
		if strings.HasPrefix(msg, "project: "+project.NSGoFileName) {
			file = project.NSGoFileName
		} else if pe, ok := errors.AsType[*fs.PathError](sub); ok && pe.Path == project.NSGoFileName {
			file = project.NSGoFileName
		}
		line, col := errorPos(sub)
		out = append(out, assetcheck.Problem{
			File:    file,
			Line:    line,
			Column:  col,
			Message: msg,
		})
	}
	return out
}

// matchAutoloadError maps a LoadAutoloads error to its project file and line,
// reporting dup=true when assetcheck already recorded the same fault.
func matchAutoloadError(f *project.File, err error, existing []assetcheck.Problem) (prob assetcheck.Problem, handled, dup bool) {
	if f == nil || f.Autoload == nil {
		return assetcheck.Problem{}, false, false
	}
	msg := err.Error()
	for _, s := range f.Autoload.Scenes {
		parsePrefix := fmt.Sprintf("project: autoload scene %q (%s): ", s.Name, s.Path)
		if strings.HasPrefix(msg, parsePrefix) {
			if hasFileProblem(existing, s.Path) {
				return assetcheck.Problem{}, true, true
			}
			line, col := errorPos(err)
			return assetcheck.Problem{
				File:    s.Path,
				Line:    line,
				Column:  col,
				Message: msg,
			}, true, false
		}
		readPrefix := fmt.Sprintf("project: autoload scene %q: ", s.Name)
		if strings.HasPrefix(msg, readPrefix) {
			if hasProblemMentioning(existing, project.FileName, s.Path) {
				return assetcheck.Problem{}, true, true
			}
			return assetcheck.Problem{
				File:    project.FileName,
				Line:    0,
				Message: msg,
			}, true, false
		}
	}
	for _, r := range f.Autoload.Resources {
		readPrefix := fmt.Sprintf("project: autoload resource %q: ", r.Name)
		if strings.HasPrefix(msg, readPrefix) {
			if hasProblemMentioning(existing, project.FileName, r.Path) {
				return assetcheck.Problem{}, true, true
			}
			return assetcheck.Problem{
				File:    project.FileName,
				Line:    0,
				Message: msg,
			}, true, false
		}
	}
	return assetcheck.Problem{}, false, false
}

func flattenErrors(err error) []error {
	if err == nil {
		return nil
	}
	if j, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, e := range j.Unwrap() {
			out = append(out, flattenErrors(e)...)
		}
		return out
	}
	return []error{err}
}

func errorPos(err error) (int, int) {
	if le, ok := errors.AsType[*loaderr.Error](err); ok && le.Line > 0 {
		return le.Line, le.Column
	}
	return 0, 0
}

func hasFileProblem(problems []assetcheck.Problem, file string) bool {
	return slices.ContainsFunc(problems, func(p assetcheck.Problem) bool {
		return p.File == file
	})
}

func hasFileLineProblem(problems []assetcheck.Problem, file string, line int) bool {
	return slices.ContainsFunc(problems, func(p assetcheck.Problem) bool {
		return p.File == file && p.Line == line
	})
}

func hasProblemMentioning(problems []assetcheck.Problem, file, path string) bool {
	quoted := strconv.Quote(path)
	return slices.ContainsFunc(problems, func(p assetcheck.Problem) bool {
		return p.File == file && strings.Contains(p.Message, quoted)
	})
}

// problemIsWarning reports whether p should be rendered with the Spec warning
// token (warning fill, edge, and alert icon) rather than the problem/error token.
func problemIsWarning(p assetcheck.Problem) bool {
	msg := strings.ToLower(strings.TrimSpace(p.Message))
	return strings.HasPrefix(msg, "warning:") || strings.HasPrefix(msg, "warn:")
}

// --- Problem-to-object attribution for scene tree badges and viewport Problem overlays ---

var (
	connProblemRE     = regexp.MustCompile(`connection (\d+) \("([^"]+)" ([^ ]+) -> "([^"]+)" ([^)]+)\)`)
	connIdxRE         = regexp.MustCompile(`connection (\d+)`)
	objectProblemRE   = regexp.MustCompile(`object "([^"]+)"`)
	overrideProblemRE = regexp.MustCompile(`override of object "([^"]+)"`)
)

// ObjectProblems returns the live visible problems attributed to objectID in
// the currently open scene. Call on the editor goroutine.
func (e *Editor) ObjectProblems(objectID string) []assetcheck.Problem {
	if e == nil || objectID == "" {
		return nil
	}
	m := e.openSceneObjectProblems()
	return slices.Clone(m[objectID])
}

// ObjectProblemCount returns how many live visible problems are attributed to
// objectID in the currently open scene. Call on the editor goroutine.
func (e *Editor) ObjectProblemCount(objectID string) int {
	if e == nil || objectID == "" {
		return 0
	}
	return len(e.openSceneObjectProblems()[objectID])
}

func (e *Editor) openSceneObjectProblems() map[string][]assetcheck.Problem {
	if e == nil || e.Doc == nil || e.ScenePath == "" {
		return nil
	}
	e.problems.mu.Lock()
	probVer := e.problems.version
	probs := slices.Clone(e.problems.problems)
	e.problems.mu.Unlock()

	if e.problems.objCache != nil &&
		e.problems.objCacheRev == e.revision &&
		e.problems.objCacheProbVer == probVer &&
		e.problems.objCacheScenePath == e.ScenePath {
		return e.problems.objCache
	}

	out := make(map[string][]assetcheck.Problem)
	var lineSpans []yamlObjectSpan
	var spansLoaded bool

	loadSpans := func() []yamlObjectSpan {
		if spansLoaded {
			return lineSpans
		}
		spansLoaded = true
		data, err := e.liveFileBytes(e.ScenePath)
		if err == nil && len(data) > 0 {
			lineSpans = extractYAMLObjectSpans(data)
		}
		return lineSpans
	}

	for _, p := range probs {
		if p.File != e.ScenePath {
			continue
		}
		objID := e.attributeProblemObject(p, loadSpans)
		if objID != "" {
			out[objID] = append(out[objID], p)
		}
	}

	e.problems.objCacheRev = e.revision
	e.problems.objCacheProbVer = probVer
	e.problems.objCacheScenePath = e.ScenePath
	e.problems.objCache = out
	return out
}

func (e *Editor) attributeProblemObject(p assetcheck.Problem, loadSpans func() []yamlObjectSpan) string {
	hasObj := func(id string) bool {
		if id == "" {
			return false
		}
		if obj, _ := e.FindResolvedObject(id); obj != nil {
			return true
		}
		if obj, _, _ := e.FindObject(id); obj != nil {
			return true
		}
		return false
	}

	// 1. Explicit connection error: attach to the live endpoint (for example,
	// when a lever's connection targets missing "door_3", attach to "lever").
	if m := connProblemRE.FindStringSubmatch(p.Message); len(m) == 6 {
		fromID, toID := m[2], m[4]
		switch {
		case strings.Contains(p.Message, "handler") && hasObj(toID):
			return toID
		case strings.Contains(p.Message, "event") && hasObj(fromID):
			return fromID
		case hasObj(fromID):
			return fromID
		case hasObj(toID):
			return toID
		}
	}
	if m := connIdxRE.FindStringSubmatch(p.Message); len(m) == 2 && e.Doc != nil {
		if idx, err := strconv.Atoi(m[1]); err == nil && idx >= 0 && idx < len(e.Doc.Connections) {
			c := e.Doc.Connections[idx]
			if hasObj(c.From) {
				return c.From
			}
			if hasObj(c.To) {
				return c.To
			}
		}
	}

	// 2. Override / component / named object error.
	if m := overrideProblemRE.FindStringSubmatch(p.Message); len(m) == 2 && hasObj(m[1]) {
		return m[1]
	}
	if m := objectProblemRE.FindStringSubmatch(p.Message); len(m) == 2 && hasObj(m[1]) {
		return m[1]
	}

	// 3. YAML line span matching under layers[*].objects[*] (recursive).
	if p.Line > 0 {
		bestID := ""
		bestWidth := 1 << 30
		for _, sp := range loadSpans() {
			if p.Line >= sp.startLine && p.Line <= sp.endLine && hasObj(sp.id) {
				w := sp.endLine - sp.startLine
				if w < bestWidth {
					bestWidth = w
					bestID = sp.id
				}
			}
		}
		if bestID != "" {
			return bestID
		}
	}
	return ""
}

type yamlObjectSpan struct {
	id        string
	startLine int
	endLine   int
}

func extractYAMLObjectSpans(data []byte) []yamlObjectSpan {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	var layers *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "layers" {
			layers = doc.Content[i+1]
			break
		}
	}
	if layers == nil || layers.Kind != yaml.SequenceNode {
		return nil
	}
	var out []yamlObjectSpan
	var walkObjs func(seq *yaml.Node)
	walkObjs = func(seq *yaml.Node) {
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			id := ""
			var children *yaml.Node
			for i := 0; i+1 < len(item.Content); i += 2 {
				switch item.Content[i].Value {
				case "id":
					id = item.Content[i+1].Value
				case "children":
					children = item.Content[i+1]
				}
			}
			if id != "" {
				start, end := yamlNodeLineRange(item)
				out = append(out, yamlObjectSpan{id: id, startLine: start, endLine: end})
			}
			if children != nil {
				walkObjs(children)
			}
		}
	}
	for _, layer := range layers.Content {
		if layer.Kind != yaml.MappingNode {
			continue
		}
		for i := 0; i+1 < len(layer.Content); i += 2 {
			if layer.Content[i].Value == "objects" {
				walkObjs(layer.Content[i+1])
			}
		}
	}
	return out
}

func yamlNodeLineRange(n *yaml.Node) (int, int) {
	if n == nil {
		return 0, 0
	}
	lo, hi := n.Line, n.Line
	var walk func(cur *yaml.Node)
	walk = func(cur *yaml.Node) {
		if cur == nil {
			return
		}
		if cur.Line > 0 {
			if lo == 0 || cur.Line < lo {
				lo = cur.Line
			}
			if cur.Line > hi {
				hi = cur.Line
			}
		}
		for _, child := range cur.Content {
			walk(child)
		}
	}
	walk(n)
	return lo, hi
}
