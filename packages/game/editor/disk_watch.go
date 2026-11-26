package editor

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/resource"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
)

const defaultDiskWatchInterval = 250 * time.Millisecond

// DiskFileKind identifies which open editor file changed on disk outside the editor.
type DiskFileKind string

const (
	DiskFileScene       DiskFileKind = "scene"
	DiskFileProject     DiskFileKind = "project"
	DiskFileClip        DiskFileKind = "clip"
	DiskFilePermissions DiskFileKind = "permissions"
)

// ErrDiskConflict is returned by Save, SaveFile, SaveProject, and SaveClipFile when the target file on disk changed since the editor opened or last saved it, refusing to overwrite external changes until the user confirms.
var ErrDiskConflict = errors.New("editor: file changed on disk since it was opened or last saved")

// DiskConflictError wraps ErrDiskConflict with the conflicting file's kind and path.
type DiskConflictError struct {
	Kind DiskFileKind
	Path string
}

func (e *DiskConflictError) Error() string {
	if e == nil {
		return ErrDiskConflict.Error()
	}
	if e.Path != "" {
		return fmt.Sprintf("editor: %s changed on disk since it was opened or last saved", e.Path)
	}
	return ErrDiskConflict.Error()
}

func (e *DiskConflictError) Unwrap() error { return ErrDiskConflict }

// DiskMergeChoice selects which side to keep for an object when resolving an external scene conflict.
type DiskMergeChoice string

const (
	DiskMergeMine DiskMergeChoice = "mine"
	DiskMergeDisk DiskMergeChoice = "disk"
)

// SceneConflictEntry describes one object touched in the editor, on disk, or on both sides during an external scene conflict.
type SceneConflictEntry struct {
	ObjectID    string
	ObjectName  string
	Layer       string
	MineChanged bool
	DiskChanged bool
	Overlap     bool
	MineSummary string
	DiskSummary string
	Note        string
	Choice      DiskMergeChoice
}

// DiskConflict describes an open file that changed on disk while the editor holds unsaved edits for it.
type DiskConflict struct {
	Kind        DiskFileKind
	Path        string
	Label       string
	Source      string
	Summary     string
	Overlap     int
	DiskObjects int
	Entries     []SceneConflictEntry
}

// SceneObjectDiff summarizes one object-level change from a quiet external scene reload.
type SceneObjectDiff struct {
	ObjectID   string
	ObjectName string
	Layer      string
	Action     string // "added", "removed", "changed"
	Detail     string
}

// DiskReloadNotice describes a clean file that reloaded automatically from disk.
type DiskReloadNotice struct {
	Kind    DiskFileKind
	Path    string
	Label   string
	Source  string
	Summary string
	Diffs   []SceneObjectDiff
}

// DiskPollResult summarizes what PollDiskChanges observed and did.
type DiskPollResult struct {
	Reloaded    []DiskFileKind
	Conflicts   []DiskFileKind
	TreeChanged bool
}

type diskSnapshot struct {
	path   string
	exists bool
	hash   [sha256.Size]byte
	raw    []byte
}

type activeDiskConflict struct {
	kind      DiskFileKind
	path      string
	label     string
	source    string
	diskHash  [sha256.Size]byte
	diskRaw   []byte
	diskScene *sceneformat.SceneDoc
	baseScene *sceneformat.SceneDoc
	diskProj  *project.File
	diskClip  *resource.Animation
	entries   []SceneConflictEntry
}

type gitHeadState struct {
	branch string
	commit string
}

type diskWatchMirrored struct {
	scenePath           string
	sceneHash           [sha256.Size]byte
	sceneHas            bool
	projectPath         string
	projectHash         [sha256.Size]byte
	projectHas          bool
	clipPath            string
	clipHash            [sha256.Size]byte
	clipHas             bool
	projectDir          string
	treeFiles           []ProjectFile
	interval            time.Duration
	scope               *core.Scope
	notifiedSceneHash   [sha256.Size]byte
	notifiedSceneHas    bool
	notifiedProjectHash [sha256.Size]byte
	notifiedProjectHas  bool
	notifiedClipHash    [sha256.Size]byte
	notifiedClipHas     bool
	permPath            string
	permHash            [sha256.Size]byte
	permHas             bool
	notifiedPermHash    [sha256.Size]byte
	notifiedPermHas     bool
}

type diskWatchState struct {
	mu       sync.Mutex
	mirrored diskWatchMirrored
	stopCh   chan struct{}
	running  bool

	// UI-goroutine-only state below.
	sceneDisk         diskSnapshot
	projectDisk       diskSnapshot
	clipDisk          diskSnapshot
	treeFiles         []ProjectFile
	gitHead           gitHeadState
	conflicts         map[DiskFileKind]*activeDiskConflict
	compareOpen       bool
	reloadNotice      *DiskReloadNotice
	showDiffOpen      bool
	saveConflict      *DiskConflictError
	saveConflictTabID uint64
	forceSave         bool
}

func makeDiskSnapshot(filePath string, raw []byte) diskSnapshot {
	if filePath == "" {
		return diskSnapshot{}
	}
	cp := append([]byte(nil), raw...)
	return diskSnapshot{
		path:   filepath.Clean(filePath),
		exists: true,
		hash:   sha256.Sum256(cp),
		raw:    cp,
	}
}

func (e *Editor) recordSceneDiskSnapshot(filePath string, raw []byte) {
	if e == nil {
		return
	}
	snap := makeDiskSnapshot(filePath, raw)
	e.disk.sceneDisk = snap
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileScene)
		if len(e.disk.conflicts) == 0 {
			e.disk.compareOpen = false
		}
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileScene {
		e.disk.saveConflict = nil
		e.disk.saveConflictTabID = 0
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) clearSceneDiskSnapshot() {
	if e == nil {
		return
	}
	e.disk.sceneDisk = diskSnapshot{}
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileScene)
		if len(e.disk.conflicts) == 0 {
			e.disk.compareOpen = false
		}
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileScene {
		e.disk.saveConflict = nil
		e.disk.saveConflictTabID = 0
	}
	e.syncDiskWatchTargets()
}

// onActiveSceneTabChanged resets active-tab-scoped scene conflict UI and re-arms the watcher for the newly active tab's sceneDisk snapshot.
func (e *Editor) onActiveSceneTabChanged() {
	if e == nil {
		return
	}
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileScene)
		e.disk.compareOpen = false
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileScene {
		e.disk.saveConflict = nil
		e.disk.saveConflictTabID = 0
	}
	e.disk.mu.Lock()
	e.disk.mirrored.notifiedSceneHas = false
	e.disk.mu.Unlock()
	e.syncDiskWatchTargets()
}

func (e *Editor) recordProjectDiskSnapshot(filePath string, raw []byte) {
	if e == nil {
		return
	}
	snap := makeDiskSnapshot(filePath, raw)
	e.disk.projectDisk = snap
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileProject)
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileProject {
		e.disk.saveConflict = nil
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) clearProjectDiskSnapshot() {
	if e == nil {
		return
	}
	e.disk.projectDisk = diskSnapshot{}
	e.disk.treeFiles = nil
	e.disk.gitHead = gitHeadState{}
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileProject)
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileProject {
		e.disk.saveConflict = nil
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) recordClipDiskSnapshot(filePath string, raw []byte) {
	if e == nil {
		return
	}
	snap := makeDiskSnapshot(filePath, raw)
	e.disk.clipDisk = snap
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileClip)
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileClip {
		e.disk.saveConflict = nil
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) clearClipDiskSnapshot() {
	if e == nil {
		return
	}
	e.disk.clipDisk = diskSnapshot{}
	if e.disk.conflicts != nil {
		delete(e.disk.conflicts, DiskFileClip)
	}
	if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == DiskFileClip {
		e.disk.saveConflict = nil
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) clearAllDiskState() {
	if e == nil {
		return
	}
	e.disk.sceneDisk = diskSnapshot{}
	e.disk.projectDisk = diskSnapshot{}
	e.disk.clipDisk = diskSnapshot{}
	e.disk.treeFiles = nil
	e.disk.gitHead = gitHeadState{}
	e.disk.conflicts = nil
	e.disk.compareOpen = false
	e.disk.reloadNotice = nil
	e.disk.showDiffOpen = false
	e.disk.saveConflict = nil
	e.disk.saveConflictTabID = 0
	e.syncDiskWatchTargets()
}

func (e *Editor) snapshotProjectTree() {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		if e != nil {
			e.disk.treeFiles = nil
			e.syncDiskWatchTargets()
		}
		return
	}
	files, err := e.ProjectFiles()
	if err == nil {
		e.disk.treeFiles = files
	}
	e.syncDiskWatchTargets()
}

func (e *Editor) snapshotGitHead() {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		if e != nil {
			e.disk.gitHead = gitHeadState{}
		}
		return
	}
	e.disk.gitHead = readGitHead(filepath.Dir(e.ProjectPath))
}

func (e *Editor) refreshDiskSnapshotsAfterMove() {
	if e == nil {
		return
	}
	if e.FilePath != "" {
		if data, err := os.ReadFile(e.FilePath); err == nil {
			e.recordSceneDiskSnapshot(e.FilePath, data)
		}
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if i == e.activeTab || t.filePath == "" {
			continue
		}
		if data, err := os.ReadFile(t.filePath); err == nil {
			t.sceneDisk = makeDiskSnapshot(t.filePath, data)
			if e.disk.saveConflict != nil && e.disk.saveConflictTabID == t.id {
				e.disk.saveConflict = nil
				e.disk.saveConflictTabID = 0
			}
		}
	}
	if e.ProjectPath != "" {
		if data, err := os.ReadFile(e.ProjectPath); err == nil {
			e.recordProjectDiskSnapshot(e.ProjectPath, data)
		}
	}
	if e.clipMeta.path != "" {
		if data, err := os.ReadFile(e.clipMeta.path); err == nil {
			e.recordClipDiskSnapshot(e.clipMeta.path, data)
		}
	}
	e.snapshotProjectTree()
}

func (e *Editor) syncDiskWatchTargets() {
	if e == nil {
		return
	}
	interval := e.DiskWatchInterval
	if interval <= 0 {
		interval = defaultDiskWatchInterval
	}
	var projectDir string
	if e.Project != nil && e.ProjectPath != "" {
		projectDir = filepath.Dir(e.ProjectPath)
	}
	treeCopy := slices.Clone(e.disk.treeFiles)

	e.disk.mu.Lock()
	m := &e.disk.mirrored
	m.scenePath = e.disk.sceneDisk.path
	m.sceneHash = e.disk.sceneDisk.hash
	m.sceneHas = e.disk.sceneDisk.exists
	if !m.sceneHas || (m.notifiedSceneHas && m.notifiedSceneHash == m.sceneHash) {
		m.notifiedSceneHas = false
	}
	m.projectPath = e.disk.projectDisk.path
	m.projectHash = e.disk.projectDisk.hash
	m.projectHas = e.disk.projectDisk.exists
	if !m.projectHas || (m.notifiedProjectHas && m.notifiedProjectHash == m.projectHash) {
		m.notifiedProjectHas = false
	}
	m.clipPath = e.disk.clipDisk.path
	m.clipHash = e.disk.clipDisk.hash
	m.clipHas = e.disk.clipDisk.exists
	if !m.clipHas || (m.notifiedClipHas && m.notifiedClipHash == m.clipHash) {
		m.notifiedClipHas = false
	}
	m.projectDir = projectDir
	m.permPath = e.PermissionsPath()
	m.permHash = e.perms.diskSnap.hash
	m.permHas = e.perms.diskSnap.exists
	if !m.permHas || (m.notifiedPermHas && m.notifiedPermHash == m.permHash) {
		m.notifiedPermHas = false
	}
	m.treeFiles = treeCopy
	m.interval = interval
	e.disk.mu.Unlock()
}

func (e *Editor) setDiskWatcherScope(cx *core.Scope) {
	if e == nil {
		return
	}
	e.syncDiskWatchTargets()
	e.disk.mu.Lock()
	e.disk.mirrored.scope = cx
	if cx == nil {
		if e.disk.running && e.disk.stopCh != nil {
			close(e.disk.stopCh)
			e.disk.stopCh = nil
			e.disk.running = false
		}
		e.disk.mu.Unlock()
		return
	}
	if !e.disk.running {
		stopCh := make(chan struct{})
		e.disk.stopCh = stopCh
		e.disk.running = true
		e.disk.mu.Unlock()
		go e.diskWatchLoop(stopCh)
		return
	}
	e.disk.mu.Unlock()
}

func (e *Editor) stopDiskWatcher() {
	if e == nil {
		return
	}
	e.disk.mu.Lock()
	e.disk.mirrored.scope = nil
	if e.disk.running && e.disk.stopCh != nil {
		close(e.disk.stopCh)
		e.disk.stopCh = nil
		e.disk.running = false
	}
	e.disk.mu.Unlock()
}

func (e *Editor) diskWatchLoop(stopCh <-chan struct{}) {
	getInterval := func() time.Duration {
		e.disk.mu.Lock()
		d := e.disk.mirrored.interval
		e.disk.mu.Unlock()
		if d <= 0 {
			return defaultDiskWatchInterval
		}
		return d
	}

	timer := time.NewTimer(getInterval())
	defer timer.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-timer.C:
			e.diskWatchTick()
			timer.Reset(getInterval())
		}
	}
}

func (e *Editor) diskWatchTick() {
	e.disk.mu.Lock()
	m := e.disk.mirrored
	treeCopy := slices.Clone(m.treeFiles)
	e.disk.mu.Unlock()

	if m.scope == nil {
		return
	}
	if !m.sceneHas && !m.projectHas && !m.clipHas && m.permPath == "" && m.projectDir == "" {
		return
	}

	changed := false
	var newSceneHash, newProjectHash, newClipHash, newPermHash [sha256.Size]byte
	var sceneTriggered, projectTriggered, clipTriggered, permTriggered bool
	var sceneReverted, projectReverted, clipReverted, permReverted bool

	if m.sceneHas && m.scenePath != "" {
		if data, err := os.ReadFile(m.scenePath); err == nil {
			sum := sha256.Sum256(data)
			if e.isInFlightAutoSaveHash(m.scenePath, sum) {
				// Ignore the editor's own in-flight autosave write until commit updates sceneHash.
			} else if sum == m.sceneHash {
				if m.notifiedSceneHas {
					changed = true
					sceneReverted = true
				}
			} else if !m.notifiedSceneHas || sum != m.notifiedSceneHash {
				changed = true
				sceneTriggered = true
				newSceneHash = sum
			}
		}
	}
	if m.projectHas && m.projectPath != "" {
		if data, err := os.ReadFile(m.projectPath); err == nil {
			sum := sha256.Sum256(data)
			if e.isInFlightAutoSaveHash(m.projectPath, sum) {
				// Ignore the editor's own in-flight autosave write until commit updates projectHash.
			} else if sum == m.projectHash {
				if m.notifiedProjectHas {
					changed = true
					projectReverted = true
				}
			} else if !m.notifiedProjectHas || sum != m.notifiedProjectHash {
				changed = true
				projectTriggered = true
				newProjectHash = sum
			}
		}
	}
	if m.clipHas && m.clipPath != "" {
		if data, err := os.ReadFile(m.clipPath); err == nil {
			sum := sha256.Sum256(data)
			if sum == m.clipHash {
				if m.notifiedClipHas {
					changed = true
					clipReverted = true
				}
			} else if !m.notifiedClipHas || sum != m.notifiedClipHash {
				changed = true
				clipTriggered = true
				newClipHash = sum
			}
		}
	}
	if m.permPath != "" {
		if data, err := readBoundedPermissionsFile(m.permPath); err == nil {
			sum := sha256.Sum256(data)
			if m.permHas && sum == m.permHash {
				if m.notifiedPermHas {
					changed = true
					permReverted = true
				}
			} else if !m.notifiedPermHas || sum != m.notifiedPermHash {
				changed = true
				permTriggered = true
				newPermHash = sum
			}
		} else if errors.Is(err, fs.ErrNotExist) && m.permHas {
			changed = true
			permReverted = true
		}
	}
	if m.projectDir != "" {
		if curTree, err := listProjectTree(m.projectDir); err == nil {
			if !slices.Equal(curTree, treeCopy) {
				changed = true
			}
		}
	}

	if !changed {
		return
	}

	e.disk.mu.Lock()
	if sceneTriggered {
		e.disk.mirrored.notifiedSceneHash = newSceneHash
		e.disk.mirrored.notifiedSceneHas = true
	} else if sceneReverted {
		e.disk.mirrored.notifiedSceneHas = false
	}
	if projectTriggered {
		e.disk.mirrored.notifiedProjectHash = newProjectHash
		e.disk.mirrored.notifiedProjectHas = true
	} else if projectReverted {
		e.disk.mirrored.notifiedProjectHas = false
	}
	if clipTriggered {
		e.disk.mirrored.notifiedClipHash = newClipHash
		e.disk.mirrored.notifiedClipHas = true
	} else if clipReverted {
		e.disk.mirrored.notifiedClipHas = false
	}
	if permTriggered {
		e.disk.mirrored.notifiedPermHash = newPermHash
		e.disk.mirrored.notifiedPermHas = true
	} else if permReverted {
		e.disk.mirrored.notifiedPermHas = false
	}
	scope := e.disk.mirrored.scope
	e.disk.mu.Unlock()

	if scope != nil {
		scope.Post(func() {
			e.PollDiskChanges()
		})
	}
}

func listProjectTree(root string) ([]ProjectFile, error) {
	var out []ProjectFile
	err := fs.WalkDir(os.DirFS(root), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			return nil
		}
		if p == "." {
			return nil
		}
		if d.IsDir() && skippedProjectDir(d.Name()) {
			return fs.SkipDir
		}
		// Skip transient atomic-write temp files created by writeFileAtomic so diskWatchTick
		// never spuriously reports the editor's own saves as external tree changes.
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".scene-") && strings.HasSuffix(d.Name(), ".tmp") {
			return nil
		}
		out = append(out, ProjectFile{Path: p, Dir: d.IsDir()})
		return nil
	})
	return out, err
}

func readGitHead(projectDir string) gitHeadState {
	if projectDir == "" {
		return gitHeadState{}
	}
	gitDir := findGitDir(projectDir)
	if gitDir == "" {
		return gitHeadState{}
	}
	headBytes, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return gitHeadState{}
	}
	line := strings.TrimSpace(string(headBytes))
	if ref, ok := strings.CutPrefix(line, "ref:"); ok {
		ref = strings.TrimSpace(ref)
		branch := strings.TrimPrefix(ref, "refs/heads/")
		commit := readGitRef(gitDir, ref)
		return gitHeadState{branch: branch, commit: commit}
	}
	return gitHeadState{commit: line}
}

func findGitDir(dir string) string {
	cur := filepath.Clean(dir)
	for {
		candidate := filepath.Join(cur, ".git")
		info, err := os.Stat(candidate)
		if err == nil {
			if info.IsDir() {
				return candidate
			}
			// Git worktree or submodule .git file: "gitdir: <path>"
			if raw, err := os.ReadFile(candidate); err == nil {
				if target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:"); ok {
					target = strings.TrimSpace(target)
					if !filepath.IsAbs(target) {
						target = filepath.Join(cur, target)
					}
					return filepath.Clean(target)
				}
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

func readGitRef(gitDir, ref string) string {
	if raw, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(ref))); err == nil {
		return strings.TrimSpace(string(raw))
	}
	if packed, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(packed), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[1] == ref {
				return fields[0]
			}
		}
	}
	return ""
}

func (e *Editor) detectExternalSource() (source string, gitBatchLabel string) {
	if e.Project == nil || e.ProjectPath == "" {
		return "", ""
	}
	cur := readGitHead(filepath.Dir(e.ProjectPath))
	prev := e.disk.gitHead
	e.disk.gitHead = cur
	if prev.branch != "" && cur.branch != "" && prev.branch != cur.branch {
		return "git checkout", fmt.Sprintf("Switched branch · %s → %s", prev.branch, cur.branch)
	}
	if prev.commit != "" && cur.commit != "" && prev.commit != cur.commit {
		return "git pull", ""
	}
	return "", ""
}

// checkDiskConflict verifies whether path on disk still matches the snapshot recorded when the file was opened or last saved.
// If it changed externally, it records the conflict on the editor and returns *DiskConflictError.
func (e *Editor) checkDiskConflict(kind DiskFileKind, targetPath string, snap diskSnapshot) error {
	if e == nil || e.disk.forceSave || targetPath == "" || !snap.exists || snap.path == "" {
		return nil
	}
	if canonicalTabFilePath(targetPath) != canonicalTabFilePath(snap.path) {
		return nil
	}
	current, err := os.ReadFile(targetPath)
	if err != nil {
		// If the file disappeared on disk, saving recreates it rather than blocking.
		return nil
	}
	sum := sha256.Sum256(current)
	if sum == snap.hash {
		return nil
	}
	e.recordExternalConflictFromBytes(kind, targetPath, current, sum, "")
	conflictErr := &DiskConflictError{Kind: kind, Path: e.displayPathForKind(kind, targetPath)}
	e.disk.saveConflict = conflictErr
	if kind == DiskFileScene {
		e.disk.saveConflictTabID = e.activeTabID()
	} else {
		e.disk.saveConflictTabID = 0
	}
	e.notify()
	return conflictErr
}

// checkTabDiskConflict verifies an open scene tab (active or inactive) against its own sceneDisk snapshot.
func (e *Editor) checkTabDiskConflict(t *sceneTab) error {
	if e == nil || e.disk.forceSave || t == nil || t.filePath == "" || !t.sceneDisk.exists || t.sceneDisk.path == "" {
		return nil
	}
	if t.id == e.activeTabID() {
		return e.checkDiskConflict(DiskFileScene, t.filePath, t.sceneDisk)
	}
	if canonicalTabFilePath(t.filePath) != canonicalTabFilePath(t.sceneDisk.path) {
		return nil
	}
	current, err := os.ReadFile(t.filePath)
	if err != nil {
		return nil
	}
	sum := sha256.Sum256(current)
	if sum == t.sceneDisk.hash {
		return nil
	}
	label := tabFullPath(t)
	if label == "" {
		label = filepath.Base(t.filePath)
	}
	conflictErr := &DiskConflictError{Kind: DiskFileScene, Path: label}
	e.disk.saveConflict = conflictErr
	e.disk.saveConflictTabID = t.id
	e.notify()
	return conflictErr
}

// checkSaveConflicts checks dirty files that Save would write before writing any of them.
func (e *Editor) checkSaveConflicts() error {
	if e == nil || e.disk.forceSave {
		return nil
	}
	if e.ProjectDirty() && e.ProjectPath != "" {
		if err := e.checkDiskConflict(DiskFileProject, e.ProjectPath, e.disk.projectDisk); err != nil {
			return err
		}
	}
	if e.SceneDirty() && e.OnSave == nil && e.FilePath != "" {
		if err := e.checkDiskConflict(DiskFileScene, e.FilePath, e.disk.sceneDisk); err != nil {
			return err
		}
	}
	return nil
}

// checkSaveAllConflicts checks every dirty file that SaveAll would write (project, all dirty file-backed tabs, and open clip) before writing any of them.
func (e *Editor) checkSaveAllConflicts() error {
	if e == nil || e.disk.forceSave {
		return nil
	}
	if e.ProjectDirty() && e.ProjectPath != "" {
		if err := e.checkDiskConflict(DiskFileProject, e.ProjectPath, e.disk.projectDisk); err != nil {
			return err
		}
	}
	e.syncActiveTab()
	for i, t := range e.tabs {
		if e.tabDirtyAt(i) && t.onSave == nil && t.filePath != "" {
			if err := e.checkTabDiskConflict(t); err != nil {
				return err
			}
		}
	}
	if e.ClipDirty() && e.OnSaveClip == nil && e.clipMeta.path != "" {
		if err := e.checkDiskConflict(DiskFileClip, e.clipMeta.path, e.disk.clipDisk); err != nil {
			return err
		}
	}
	return nil
}

func (e *Editor) displayPathForKind(kind DiskFileKind, absPath string) string {
	switch kind {
	case DiskFileScene:
		if e.ScenePath != "" {
			return e.ScenePath
		}
	case DiskFileProject:
		return project.FileName
	case DiskFileClip:
		if rel, ok := e.projectRel(absPath); ok {
			return rel
		}
	}
	if absPath != "" {
		return filepath.Base(absPath)
	}
	return string(kind)
}

func (e *Editor) recordExternalConflictFromBytes(kind DiskFileKind, absPath string, current []byte, sum [sha256.Size]byte, source string) {
	if e.disk.conflicts == nil {
		e.disk.conflicts = make(map[DiskFileKind]*activeDiskConflict)
	}
	label := e.displayPathForKind(kind, absPath)
	c := &activeDiskConflict{
		kind:     kind,
		path:     absPath,
		label:    label,
		source:   source,
		diskHash: sum,
		diskRaw:  append([]byte(nil), current...),
	}
	switch kind {
	case DiskFileScene:
		if diskDoc, err := sceneformat.ParseWith(current, sceneformat.ParseOptions{
			File:          absPath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		}); err == nil {
			c.diskScene = diskDoc
			baseDoc := e.sceneBaseDoc()
			c.baseScene = baseDoc
			c.entries = computeSceneConflictEntries(baseDoc, e.Doc, diskDoc)
			// Preserve existing user choices if conflict updated for the same objects.
			if prev, ok := e.disk.conflicts[DiskFileScene]; ok && prev != nil {
				prevChoices := make(map[string]DiskMergeChoice, len(prev.entries))
				for _, pe := range prev.entries {
					if pe.Overlap {
						prevChoices[pe.ObjectID] = pe.Choice
					}
				}
				for i := range c.entries {
					if c.entries[i].Overlap {
						if ch, ok := prevChoices[c.entries[i].ObjectID]; ok {
							c.entries[i].Choice = ch
						}
					}
				}
			}
		}
	case DiskFileProject:
		if proj, err := project.Parse(current); err == nil {
			c.diskProj = proj
		}
	case DiskFileClip:
		if clip, err := resource.ParseAs[*resource.Animation](current); err == nil {
			c.diskClip = clip
		}
	}
	e.disk.conflicts[kind] = c
	e.disk.mu.Lock()
	switch kind {
	case DiskFileScene:
		e.disk.mirrored.notifiedSceneHash = sum
		e.disk.mirrored.notifiedSceneHas = true
	case DiskFileProject:
		e.disk.mirrored.notifiedProjectHash = sum
		e.disk.mirrored.notifiedProjectHas = true
	case DiskFileClip:
		e.disk.mirrored.notifiedClipHash = sum
		e.disk.mirrored.notifiedClipHas = true
	}
	e.disk.mu.Unlock()
}

func (e *Editor) sceneBaseDoc() *sceneformat.SceneDoc {
	if len(e.disk.sceneDisk.raw) > 0 {
		if doc, err := sceneformat.ParseWith(e.disk.sceneDisk.raw, sceneformat.ParseOptions{
			File:          e.FilePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		}); err == nil {
			return doc
		}
	}
	if len(e.sceneSaved) > 0 {
		if doc, err := sceneformat.ParseWith(e.sceneSaved, sceneformat.ParseOptions{
			File:          e.FilePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		}); err == nil {
			return doc
		}
	}
	return nil
}

// PollDiskChanges checks the open scene, open clip, game.yaml, and the project tree for changes made on disk outside the editor.
// Clean files reload automatically (as an undoable History step with a quiet reload notice); dirty files are never overwritten under the author and instead raise a conflict offering Compare, Reload from disk, and Keep mine. The file browser refreshes automatically whenever the project tree changes.
func (e *Editor) PollDiskChanges() DiskPollResult {
	var res DiskPollResult
	if e == nil {
		return res
	}

	// 1. Check project tree changes (safe even during play).
	var addedProjectFiles []string
	if e.Project != nil && e.ProjectPath != "" {
		if curTree, err := listProjectTree(filepath.Dir(e.ProjectPath)); err == nil {
			if !slices.Equal(curTree, e.disk.treeFiles) {
				if len(e.disk.treeFiles) > 0 {
					prev := make(map[string]struct{}, len(e.disk.treeFiles))
					for _, f := range e.disk.treeFiles {
						if !f.Dir {
							prev[f.Path] = struct{}{}
						}
					}
					for _, f := range curTree {
						if !f.Dir {
							if _, existed := prev[f.Path]; !existed {
								addedProjectFiles = append(addedProjectFiles, f.Path)
							}
						}
					}
				}
				e.disk.treeFiles = curTree
				e.filesRev++
				e.assetsRev++
				e.invalidateProjectIndex()
				e.CheckProblems()
				res.TreeChanged = true
			}
		}
	}
	if !e.playing {
		for _, added := range addedProjectFiles {
			e.NotifyAutomationFileAdded(added)
		}
	}

	if e.playing {
		if res.TreeChanged {
			e.syncDiskWatchTargets()
			e.notify()
		}
		return res
	}

	source, gitBatchLabel := e.detectExternalSource()

	type pendingCleanReload struct {
		kind      DiskFileKind
		path      string
		raw       []byte
		sceneDoc  *sceneformat.SceneDoc
		sceneSave []byte
		projFile  *project.File
		projSave  []byte
		clipDoc   *resource.Animation
		clipSave  []byte
	}
	var cleanReloads []pendingCleanReload
	hadConflictUpdate := false

	clearStaleConflict := func(kind DiskFileKind) {
		if _, existed := e.disk.conflicts[kind]; existed {
			delete(e.disk.conflicts, kind)
			if kind == DiskFileScene {
				e.disk.compareOpen = false
			}
			if e.disk.saveConflict != nil && e.disk.saveConflict.Kind == kind {
				if kind != DiskFileScene || e.disk.saveConflictTabID == 0 || e.disk.saveConflictTabID == e.activeTabID() {
					e.disk.saveConflict = nil
					e.disk.saveConflictTabID = 0
				}
			}
			e.disk.mu.Lock()
			switch kind {
			case DiskFileScene:
				e.disk.mirrored.notifiedSceneHas = false
			case DiskFileProject:
				e.disk.mirrored.notifiedProjectHas = false
			case DiskFileClip:
				e.disk.mirrored.notifiedClipHas = false
			}
			e.disk.mu.Unlock()
			hadConflictUpdate = true
		}
	}

	// 2. Check open game.yaml.
	if e.Project != nil && e.ProjectPath != "" && e.disk.projectDisk.exists {
		if data, err := os.ReadFile(e.ProjectPath); err == nil {
			sum := sha256.Sum256(data)
			if e.isInFlightAutoSaveHash(e.ProjectPath, sum) {
				// Ignore the editor's own in-flight autosave write.
			} else if sum == e.disk.projectDisk.hash {
				clearStaleConflict(DiskFileProject)
			} else {
				if e.ProjectDirty() {
					prev, existed := e.disk.conflicts[DiskFileProject]
					if !existed || prev.diskHash != sum {
						e.recordExternalConflictFromBytes(DiskFileProject, e.ProjectPath, data, sum, source)
						res.Conflicts = append(res.Conflicts, DiskFileProject)
						hadConflictUpdate = true
					}
				} else if f, err := project.Parse(data); err == nil {
					if saved, err := project.Encode(f); err == nil {
						cleanReloads = append(cleanReloads, pendingCleanReload{
							kind:     DiskFileProject,
							path:     e.ProjectPath,
							raw:      data,
							projFile: f,
							projSave: saved,
						})
					}
				}
			}
		}
	}

	// 3. Check open scene file.
	if e.Doc != nil && e.FilePath != "" && e.disk.sceneDisk.exists {
		if data, err := os.ReadFile(e.FilePath); err == nil {
			sum := sha256.Sum256(data)
			if e.isInFlightAutoSaveHash(e.FilePath, sum) {
				// Ignore the editor's own in-flight autosave write.
			} else if sum == e.disk.sceneDisk.hash {
				clearStaleConflict(DiskFileScene)
			} else {
				if e.SceneDirty() {
					prev, existed := e.disk.conflicts[DiskFileScene]
					if !existed || prev.diskHash != sum {
						e.recordExternalConflictFromBytes(DiskFileScene, e.FilePath, data, sum, source)
						res.Conflicts = append(res.Conflicts, DiskFileScene)
						hadConflictUpdate = true
					}
				} else {
					layers := e.PhysicsLayers()
					for _, cr := range cleanReloads {
						if cr.kind == DiskFileProject && cr.projFile != nil {
							layers = cr.projFile.PhysicsLayerTable()
						}
					}
					if doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
						File:          e.FilePath,
						Components:    e.components,
						PhysicsLayers: layers,
					}); err == nil {
						if saved, err := sceneformat.Encode(doc); err == nil {
							cleanReloads = append(cleanReloads, pendingCleanReload{
								kind:      DiskFileScene,
								path:      e.FilePath,
								raw:       data,
								sceneDoc:  doc,
								sceneSave: saved,
							})
						}
					}
				}
			}
		}
	}

	// 4. Check open clip file.
	if e.clip != nil && e.clipMeta.path != "" && e.disk.clipDisk.exists {
		if data, err := os.ReadFile(e.clipMeta.path); err == nil {
			sum := sha256.Sum256(data)
			if sum == e.disk.clipDisk.hash {
				clearStaleConflict(DiskFileClip)
			} else {
				if e.ClipDirty() {
					prev, existed := e.disk.conflicts[DiskFileClip]
					if !existed || prev.diskHash != sum {
						e.recordExternalConflictFromBytes(DiskFileClip, e.clipMeta.path, data, sum, source)
						res.Conflicts = append(res.Conflicts, DiskFileClip)
						hadConflictUpdate = true
					}
				} else if clip, err := resource.ParseAs[*resource.Animation](data); err == nil {
					if err := e.checkClip(clip, e.clipMeta.target); err == nil {
						if saved, err := resource.Encode(clip); err == nil {
							cleanReloads = append(cleanReloads, pendingCleanReload{
								kind:     DiskFileClip,
								path:     e.clipMeta.path,
								raw:      data,
								clipDoc:  clip,
								clipSave: saved,
							})
						}
					}
				}
			}
		}
	}

	if len(cleanReloads) > 0 {
		applyClean := func() error {
			for _, cr := range cleanReloads {
				switch cr.kind {
				case DiskFileProject:
					e.projectSaved = cr.projSave
					e.recordProjectDiskSnapshot(cr.path, cr.raw)
					e.History.Execute(&projectCmd{
						ed:     e,
						label:  "Reload game.yaml from disk",
						before: e.Project.Clone(),
						after:  cr.projFile,
					})
					e.invalidateProjectIndex()
					e.CheckProblems()
					e.setReloadNotice(DiskReloadNotice{
						Kind:    DiskFileProject,
						Path:    cr.path,
						Label:   project.FileName,
						Source:  source,
						Summary: formatReloadSummary(project.FileName, source, 0),
					})
					res.Reloaded = append(res.Reloaded, DiskFileProject)

				case DiskFileScene:
					beforeDoc := e.Doc.Clone()
					diffs := computeSceneDiffs(beforeDoc, cr.sceneDoc)
					e.cancelTransientInteraction()
					e.sceneSaved = cr.sceneSave
					e.recordSceneDiskSnapshot(cr.path, cr.raw)
					label := "Reload " + e.SceneLabel() + " from disk"
					e.History.Execute(&replaceSceneCmd{
						ed:     e,
						label:  label,
						before: beforeDoc,
						after:  cr.sceneDoc,
					})
					e.syncActiveTab()
					e.invalidateProjectIndex()
					e.CheckProblems()
					display := e.SceneLabel()
					summary := formatReloadSummary(display, source, len(diffs))
					e.setReloadNotice(DiskReloadNotice{
						Kind:    DiskFileScene,
						Path:    cr.path,
						Label:   display,
						Source:  source,
						Summary: summary,
						Diffs:   diffs,
					})
					e.Status = summary
					res.Reloaded = append(res.Reloaded, DiskFileScene)

				case DiskFileClip:
					e.clipSaved = cr.clipSave
					e.recordClipDiskSnapshot(cr.path, cr.raw)
					e.History.Execute(&clipCmd{
						ed:     e,
						label:  "Reload clip from disk",
						before: cloneClip(e.clip),
						after:  cr.clipDoc,
					})
					display := e.displayPathForKind(DiskFileClip, cr.path)
					summary := formatReloadSummary(display, source, 0)
					e.setReloadNotice(DiskReloadNotice{
						Kind:    DiskFileClip,
						Path:    cr.path,
						Label:   display,
						Source:  source,
						Summary: summary,
					})
					e.Status = summary
					res.Reloaded = append(res.Reloaded, DiskFileClip)
				}
			}
			return nil
		}

		if gitBatchLabel != "" || len(cleanReloads) > 1 {
			groupLabel := gitBatchLabel
			if groupLabel == "" {
				groupLabel = fmt.Sprintf("Reloaded %d files from disk", len(cleanReloads))
			} else {
				groupLabel = fmt.Sprintf("%s · %d files", groupLabel, len(cleanReloads))
			}
			_ = e.History.Group(groupLabel, applyClean)
		} else {
			_ = applyClean()
		}
	}

	// 5. Check permissions.yaml for external edits (tightened rules apply immediately; loosened rules/profiles are held until confirmed).
	if e.checkPermissionsDiskChanges(source) {
		res.Reloaded = append(res.Reloaded, DiskFilePermissions)
	}

	if res.TreeChanged || len(res.Reloaded) > 0 || hadConflictUpdate {
		e.syncDiskWatchTargets()
		e.notify()
	}
	return res
}

func (e *Editor) setReloadNotice(n DiskReloadNotice) {
	cp := n
	cp.Diffs = slices.Clone(n.Diffs)
	e.disk.reloadNotice = &cp
	e.disk.showDiffOpen = false
	label := cp.Label
	if label == "" {
		label = filepath.Base(cp.Path)
	}
	target := cp.Path
	if target == "" {
		target = label
	}
	e.EmitToastEvent(ToastEvent{
		Kind:        ToastEventDiskChanged,
		Target:      target,
		Message:     fmt.Sprintf("%s changed on disk and was reloaded", label),
		ActionLabel: "Show",
		OnAction: func() {
			if len(cp.Diffs) > 0 {
				e.SetDiskReloadDiffOpen(true)
			} else if rel, ok := e.projectRel(cp.Path); ok {
				e.SelectFile(rel)
			} else if label != "" {
				e.SelectFile(label)
			}
		},
	})
}

func formatReloadSummary(label, source string, objectCount int) string {
	base := label + " changed on disk"
	var parts []string
	if source != "" {
		parts = append(parts, source)
	}
	if objectCount == 1 {
		parts = append(parts, "1 object")
	} else if objectCount > 1 {
		parts = append(parts, fmt.Sprintf("%d objects", objectCount))
	}
	if len(parts) == 0 {
		return base
	}
	return base + " · " + strings.Join(parts, " · ")
}

// DiskConflicts returns all currently unresolved external-change conflicts in deterministic order (scene, project, clip).
func (e *Editor) DiskConflicts() []DiskConflict {
	if e == nil || len(e.disk.conflicts) == 0 {
		return nil
	}
	order := []DiskFileKind{DiskFileScene, DiskFileProject, DiskFileClip}
	var out []DiskConflict
	for _, k := range order {
		c, ok := e.disk.conflicts[k]
		if !ok || c == nil {
			continue
		}
		out = append(out, c.snapshot())
	}
	return out
}

// PrimaryDiskConflict returns the highest-priority active external-change conflict (scene, then project, then clip), if any.
func (e *Editor) PrimaryDiskConflict() (DiskConflict, bool) {
	list := e.DiskConflicts()
	if len(list) == 0 {
		return DiskConflict{}, false
	}
	return list[0], true
}

func (c *activeDiskConflict) snapshot() DiskConflict {
	entries := slices.Clone(c.entries)
	overlap := 0
	diskObjs := 0
	for _, ent := range entries {
		if ent.Overlap {
			overlap++
		}
		if ent.DiskChanged {
			diskObjs++
		}
	}
	var parts []string
	if c.source != "" {
		parts = append(parts, c.source)
	}
	if c.kind == DiskFileScene {
		if diskObjs == 1 {
			parts = append(parts, "1 object changed on disk")
		} else if diskObjs > 1 {
			parts = append(parts, fmt.Sprintf("%d objects changed on disk", diskObjs))
		}
		if overlap == 1 {
			parts = append(parts, "1 overlap")
		} else if overlap > 1 {
			parts = append(parts, fmt.Sprintf("%d overlaps", overlap))
		}
	}
	summary := c.label + " changed on disk"
	if len(parts) > 0 {
		summary += " · " + strings.Join(parts, " · ")
	}
	return DiskConflict{
		Kind:        c.kind,
		Path:        c.path,
		Label:       c.label,
		Source:      c.source,
		Summary:     summary,
		Overlap:     overlap,
		DiskObjects: diskObjs,
		Entries:     entries,
	}
}

// DiskReloadNotice returns the latest quiet external reload notice, if not dismissed.
func (e *Editor) DiskReloadNotice() (DiskReloadNotice, bool) {
	if e == nil || e.disk.reloadNotice == nil {
		return DiskReloadNotice{}, false
	}
	cp := *e.disk.reloadNotice
	cp.Diffs = slices.Clone(cp.Diffs)
	return cp, true
}

// DismissDiskReloadNotice clears the quiet external reload notice.
func (e *Editor) DismissDiskReloadNotice() {
	if e == nil || e.disk.reloadNotice == nil {
		return
	}
	e.disk.reloadNotice = nil
	e.disk.showDiffOpen = false
	e.notify()
}

// DiskReloadDiffOpen reports whether the quiet reload diff drawer is expanded.
func (e *Editor) DiskReloadDiffOpen() bool {
	return e != nil && e.disk.reloadNotice != nil && e.disk.showDiffOpen
}

// SetDiskReloadDiffOpen opens or closes the quiet reload diff details.
func (e *Editor) SetDiskReloadDiffOpen(open bool) {
	if e == nil || e.disk.reloadNotice == nil {
		return
	}
	if e.disk.showDiffOpen == open {
		return
	}
	e.disk.showDiffOpen = open
	e.notify()
}

// DiskCompareOpen reports whether the per-object Compare merge picker is open for an external scene conflict.
func (e *Editor) DiskCompareOpen() bool {
	if e == nil {
		return false
	}
	_, ok := e.disk.conflicts[DiskFileScene]
	return ok && e.disk.compareOpen
}

// SetDiskCompareOpen opens or closes the per-object Compare merge picker for an active scene conflict.
func (e *Editor) SetDiskCompareOpen(open bool) {
	if e == nil {
		return
	}
	if open {
		if _, ok := e.disk.conflicts[DiskFileScene]; !ok {
			return
		}
	}
	if e.disk.compareOpen == open {
		return
	}
	e.disk.compareOpen = open
	e.notify()
}

// SaveConflictPromptOpen reports whether a Save operation hit an external disk conflict and is waiting for the author to confirm overwrite, reload, or cancel.
func (e *Editor) SaveConflictPromptOpen() bool {
	return e != nil && e.disk.saveConflict != nil
}

// PendingSaveConflict returns the pending save conflict error, if any.
func (e *Editor) PendingSaveConflict() (*DiskConflictError, bool) {
	if e == nil || e.disk.saveConflict == nil {
		return nil, false
	}
	cp := *e.disk.saveConflict
	return &cp, true
}

// CancelSaveConflict dismisses the save-conflict confirmation prompt without overwriting the file on disk.
func (e *Editor) CancelSaveConflict() {
	if e == nil || e.disk.saveConflict == nil {
		return
	}
	e.disk.saveConflict = nil
	e.disk.saveConflictTabID = 0
	e.notify()
}

// ForceSave writes dirty changes (project and scene) to disk even when the target file changed externally since it was opened or last saved.
func (e *Editor) ForceSave() error {
	if e == nil {
		return nil
	}
	e.disk.forceSave = true
	defer func() { e.disk.forceSave = false }()
	err := e.Save()
	if err == nil {
		e.disk.saveConflict = nil
		e.disk.saveConflictTabID = 0
		e.notify()
	}
	return err
}

// ConfirmOverwriteSave confirms overwriting the file that triggered SaveConflictPromptOpen.
func (e *Editor) ConfirmOverwriteSave() error {
	if e == nil {
		return nil
	}
	pending := e.disk.saveConflict
	pendingTabID := e.disk.saveConflictTabID
	e.disk.forceSave = true
	defer func() { e.disk.forceSave = false }()
	var err error
	if pending != nil {
		switch pending.Kind {
		case DiskFileClip:
			if e.clipMeta.path != "" {
				err = e.SaveClipFile(e.clipMeta.path)
			}
		case DiskFileProject:
			err = e.SaveProject()
		case DiskFileScene:
			if pendingTabID != 0 && pendingTabID != e.activeTabID() {
				if idx := e.findTabByID(pendingTabID); idx >= 0 {
					err = e.SaveTab(idx)
				} else {
					err = e.Save()
				}
			} else {
				err = e.Save()
			}
		default:
			err = e.Save()
		}
	} else {
		err = e.Save()
	}
	if err == nil {
		e.disk.saveConflict = nil
		e.disk.saveConflictTabID = 0
		e.notify()
	}
	return err
}

// reloadPendingSaveConflict dismisses the save-conflict prompt and reloads the conflicting file (activating its scene tab first when the conflict belonged to an inactive tab).
func (e *Editor) reloadPendingSaveConflict() error {
	if e == nil {
		return nil
	}
	kind := DiskFileScene
	if e.disk.saveConflict != nil {
		kind = e.disk.saveConflict.Kind
	}
	pendingTabID := e.disk.saveConflictTabID
	e.CancelSaveConflict()
	if kind == DiskFileScene && pendingTabID != 0 && pendingTabID != e.activeTabID() {
		if idx := e.findTabByID(pendingTabID); idx >= 0 {
			if err := e.SelectTab(idx); err != nil {
				return err
			}
		}
	}
	return e.ReloadFromDisk(kind)
}

// ReloadFromDisk reloads the open file identified by kind from disk, recording the author's previous edits in History as "Unsaved edits before reload" so Undo restores them.
func (e *Editor) ReloadFromDisk(kind DiskFileKind) error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	switch kind {
	case DiskFileScene:
		if e.FilePath == "" {
			return ErrNoSceneFile
		}
		data, err := os.ReadFile(e.FilePath)
		if err != nil {
			return err
		}
		doc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          e.FilePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if err != nil {
			return err
		}
		saved, err := sceneformat.Encode(doc)
		if err != nil {
			return err
		}
		e.cancelTransientInteraction()
		e.sceneSaved = saved
		e.recordSceneDiskSnapshot(e.FilePath, data)
		e.History.Execute(&replaceSceneCmd{
			ed:     e,
			label:  "Unsaved edits before reload",
			before: e.Doc.Clone(),
			after:  doc,
		})
		e.syncActiveTab()
		e.scheduleAutoSaveIfNeeded()
		e.Status = "Reloaded " + e.SceneLabel() + " from disk"
		e.notify()
		e.CheckProblems()
		return nil

	case DiskFileProject:
		if e.Project == nil || e.ProjectPath == "" {
			return ErrNoProject
		}
		data, err := os.ReadFile(e.ProjectPath)
		if err != nil {
			return err
		}
		f, err := project.Parse(data)
		if err != nil {
			return fmt.Errorf("%s: %w", e.ProjectPath, err)
		}
		saved, err := project.Encode(f)
		if err != nil {
			return err
		}
		e.projectSaved = saved
		e.recordProjectDiskSnapshot(e.ProjectPath, data)
		e.History.Execute(&projectCmd{
			ed:     e,
			label:  "Unsaved edits before reload",
			before: e.Project.Clone(),
			after:  f,
		})
		e.scheduleAutoSaveIfNeeded()
		e.Status = "Reloaded " + project.FileName + " from disk"
		e.notify()
		e.CheckProblems()
		return nil

	case DiskFileClip:
		if e.clip == nil || e.clipMeta.path == "" {
			return ErrNoClip
		}
		data, err := os.ReadFile(e.clipMeta.path)
		if err != nil {
			return err
		}
		clip, err := resource.ParseAs[*resource.Animation](data)
		if err != nil {
			return fmt.Errorf("%s: %w", e.clipMeta.path, err)
		}
		if err := e.checkClip(clip, e.clipMeta.target); err != nil {
			return fmt.Errorf("%s: %w", e.clipMeta.path, err)
		}
		saved, err := resource.Encode(clip)
		if err != nil {
			return err
		}
		e.clipSaved = saved
		e.recordClipDiskSnapshot(e.clipMeta.path, data)
		e.History.Execute(&clipCmd{
			ed:     e,
			label:  "Unsaved edits before reload",
			before: cloneClip(e.clip),
			after:  clip,
		})
		e.scheduleAutoSaveIfNeeded()
		e.Status = "Reloaded clip from disk"
		e.notify()
		return nil
	default:
		return fmt.Errorf("editor: unknown disk file kind %q", kind)
	}
}

// KeepLocalVersion resolves an external disk conflict for kind by keeping the editor's current in-memory document, acknowledging the current on-disk file so a subsequent Save will succeed, and recording the disk version in History as "Changed outside the editor" so Undo can inspect or revert to it.
func (e *Editor) KeepLocalVersion(kind DiskFileKind) error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	switch kind {
	case DiskFileScene:
		if e.FilePath == "" {
			return ErrNoSceneFile
		}
		data, err := os.ReadFile(e.FilePath)
		if err != nil {
			return err
		}
		diskDoc, err := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          e.FilePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if err == nil {
			if saved, encErr := sceneformat.Encode(diskDoc); encErr == nil {
				e.sceneSaved = saved
			}
			cur := e.Doc.Clone()
			e.recordSceneDiskSnapshot(e.FilePath, data)
			e.History.Execute(&replaceSceneCmd{
				ed:     e,
				label:  "Changed outside the editor",
				before: diskDoc,
				after:  cur,
			})
		} else {
			e.recordSceneDiskSnapshot(e.FilePath, data)
		}
		e.syncActiveTab()
		e.scheduleAutoSaveIfNeeded()
		e.Status = "Kept your version of " + e.SceneLabel()
		e.notify()
		return nil

	case DiskFileProject:
		if e.Project == nil || e.ProjectPath == "" {
			return ErrNoProject
		}
		data, err := os.ReadFile(e.ProjectPath)
		if err != nil {
			return err
		}
		if diskProj, err := project.Parse(data); err == nil {
			if saved, encErr := project.Encode(diskProj); encErr == nil {
				e.projectSaved = saved
			}
			cur := e.Project.Clone()
			e.recordProjectDiskSnapshot(e.ProjectPath, data)
			e.History.Execute(&projectCmd{
				ed:     e,
				label:  "Changed outside the editor",
				before: diskProj,
				after:  cur,
			})
		} else {
			e.recordProjectDiskSnapshot(e.ProjectPath, data)
		}
		e.scheduleAutoSaveIfNeeded()
		e.Status = "Kept your version of " + project.FileName
		e.notify()
		return nil

	case DiskFileClip:
		if e.clip == nil || e.clipMeta.path == "" {
			return ErrNoClip
		}
		data, err := os.ReadFile(e.clipMeta.path)
		if err != nil {
			return err
		}
		if diskClip, err := resource.ParseAs[*resource.Animation](data); err == nil && e.checkClip(diskClip, e.clipMeta.target) == nil {
			if saved, encErr := resource.Encode(diskClip); encErr == nil {
				e.clipSaved = saved
			}
			cur := cloneClip(e.clip)
			e.recordClipDiskSnapshot(e.clipMeta.path, data)
			e.History.Execute(&clipCmd{
				ed:     e,
				label:  "Changed outside the editor",
				before: diskClip,
				after:  cur,
			})
		} else {
			e.recordClipDiskSnapshot(e.clipMeta.path, data)
		}
		e.Status = "Kept your version of clip"
		e.notify()
		return nil
	default:
		return fmt.Errorf("editor: unknown disk file kind %q", kind)
	}
}

// SetSceneConflictChoice sets whether objectID uses the editor's version (DiskMergeMine) or the disk version (DiskMergeDisk) when resolving an active external scene conflict.
func (e *Editor) SetSceneConflictChoice(objectID string, choice DiskMergeChoice) error {
	if e == nil {
		return nil
	}
	if choice != DiskMergeMine && choice != DiskMergeDisk {
		return fmt.Errorf("editor: invalid merge choice %q", choice)
	}
	c, ok := e.disk.conflicts[DiskFileScene]
	if !ok || c == nil {
		return errors.New("editor: no active scene disk conflict")
	}
	for i := range c.entries {
		if c.entries[i].ObjectID == objectID {
			if !c.entries[i].Overlap {
				return fmt.Errorf("editor: object %q was only changed on one side", objectID)
			}
			if c.entries[i].Choice != choice {
				c.entries[i].Choice = choice
				e.notify()
			}
			return nil
		}
	}
	return fmt.Errorf("editor: object %q is not in the scene conflict", objectID)
}

// ApplySceneConflictMerge merges the editor's open scene with the conflicting on-disk scene according to each entry's Choice (overlapping objects use their selected Mine/Disk pick; objects touched on only one side take that side), records the merged scene as one undoable History step, and acknowledges the on-disk snapshot so Save can persist the merged result.
func (e *Editor) ApplySceneConflictMerge() error {
	if e == nil {
		return nil
	}
	if e.playing {
		return ErrPlaying
	}
	c, ok := e.disk.conflicts[DiskFileScene]
	if !ok || c == nil || c.diskScene == nil {
		return errors.New("editor: no active scene disk conflict to merge")
	}
	merged, err := mergeSceneDocs(c.baseScene, e.Doc, c.diskScene, c.entries)
	if err != nil {
		return err
	}
	if err := sceneformat.Validate(merged); err != nil {
		return err
	}
	e.cancelTransientInteraction()

	if saved, err := sceneformat.Encode(c.diskScene); err == nil {
		e.sceneSaved = saved
	}
	e.recordSceneDiskSnapshot(c.path, c.diskRaw)
	e.History.Execute(&replaceSceneCmd{
		ed:     e,
		label:  "Merge scene with disk",
		before: e.Doc.Clone(),
		after:  merged,
	})
	e.syncActiveTab()
	e.Status = "Merged " + e.SceneLabel() + " with disk changes (unsaved)"
	e.notify()
	e.CheckProblems()
	return nil
}

// --- 3-way object diff & merge helpers ---

type diffIndexedObject struct {
	layer    string
	parentID string
	obj      *sceneformat.ObjectDoc
}

func indexSceneObjects(doc *sceneformat.SceneDoc) (map[string]diffIndexedObject, []string) {
	m := make(map[string]diffIndexedObject)
	var order []string
	if doc == nil {
		return m, order
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		sceneformat.WalkObjects(l.Objects, func(o, parent *sceneformat.ObjectDoc) bool {
			pid := ""
			if parent != nil {
				pid = parent.ID
			}
			m[o.ID] = diffIndexedObject{layer: l.Name, parentID: pid, obj: o}
			order = append(order, o.ID)
			return true
		})
	}
	return m, order
}

func objectEqual(a, b *sceneformat.ObjectDoc) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// Compare object fields ignoring children (children have their own IDs and entries).
	ac := *a
	bc := *b
	ac.Children = nil
	bc.Children = nil
	return reflect.DeepEqual(ac, bc)
}

func diskObjectDisplayName(o *sceneformat.ObjectDoc, fallback string) string {
	if o != nil && o.Name != "" {
		return o.Name
	}
	return fallback
}

func summarizeObjectChange(base, cur *sceneformat.ObjectDoc) string {
	if base == nil && cur != nil {
		return "added"
	}
	if base != nil && cur == nil {
		return "removed"
	}
	if base == nil && cur == nil {
		return "—"
	}
	var parts []string
	if base.X != cur.X {
		parts = append(parts, "x "+strconv.FormatFloat(float64(cur.X), 'f', -1, 32))
	}
	if base.Y != cur.Y {
		parts = append(parts, "y "+strconv.FormatFloat(float64(cur.Y), 'f', -1, 32))
	}
	if base.PosZ != cur.PosZ {
		parts = append(parts, "posZ "+strconv.FormatFloat(float64(cur.PosZ), 'f', -1, 32))
	}
	if base.Z != cur.Z {
		parts = append(parts, fmt.Sprintf("z %d", cur.Z))
	}
	if base.Rotation != cur.Rotation {
		parts = append(parts, "rot "+strconv.FormatFloat(float64(cur.Rotation), 'f', 2, 32))
	}
	if base.Name != cur.Name {
		parts = append(parts, "name "+cur.Name)
	}
	if !reflect.DeepEqual(base.Sprite, cur.Sprite) {
		if cur.Sprite != nil {
			parts = append(parts, "sprite "+cur.Sprite.Frame)
		} else {
			parts = append(parts, "sprite cleared")
		}
	}
	if !reflect.DeepEqual(base.Properties, cur.Properties) {
		parts = append(parts, "properties")
	}
	if !reflect.DeepEqual(base.Components, cur.Components) {
		parts = append(parts, "components")
	}
	if len(parts) == 0 {
		return "edited"
	}
	if len(parts) > 2 {
		parts = parts[:2]
	}
	return strings.Join(parts, ", ")
}

func computeSceneDiffs(before, after *sceneformat.SceneDoc) []SceneObjectDiff {
	bm, bOrder := indexSceneObjects(before)
	am, aOrder := indexSceneObjects(after)
	seen := make(map[string]bool, len(bm)+len(am))
	var ids []string
	for _, id := range aOrder {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range bOrder {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	var diffs []SceneObjectDiff
	for _, id := range ids {
		bo, inB := bm[id]
		ao, inA := am[id]
		switch {
		case !inB && inA:
			diffs = append(diffs, SceneObjectDiff{
				ObjectID:   id,
				ObjectName: diskObjectDisplayName(ao.obj, id),
				Layer:      ao.layer,
				Action:     "added",
				Detail:     "added",
			})
		case inB && !inA:
			diffs = append(diffs, SceneObjectDiff{
				ObjectID:   id,
				ObjectName: diskObjectDisplayName(bo.obj, id),
				Layer:      bo.layer,
				Action:     "removed",
				Detail:     "removed",
			})
		case inB && inA && (!objectEqual(bo.obj, ao.obj) || bo.layer != ao.layer):
			detail := summarizeObjectChange(bo.obj, ao.obj)
			if bo.layer != ao.layer {
				detail = "layer " + ao.layer
			}
			diffs = append(diffs, SceneObjectDiff{
				ObjectID:   id,
				ObjectName: diskObjectDisplayName(ao.obj, id),
				Layer:      ao.layer,
				Action:     "changed",
				Detail:     detail,
			})
		}
	}
	return diffs
}

func computeSceneConflictEntries(base, mine, disk *sceneformat.SceneDoc) []SceneConflictEntry {
	bm, bOrder := indexSceneObjects(base)
	mm, mOrder := indexSceneObjects(mine)
	dm, dOrder := indexSceneObjects(disk)

	seen := make(map[string]bool, len(bm)+len(mm)+len(dm))
	var ids []string
	for _, list := range [][]string{mOrder, dOrder, bOrder} {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}

	var overlaps, singleSide []SceneConflictEntry
	for _, id := range ids {
		bo, inB := bm[id]
		mo, inM := mm[id]
		do, inD := dm[id]

		var bObj, mObj, dObj *sceneformat.ObjectDoc
		if inB {
			bObj = bo.obj
		}
		if inM {
			mObj = mo.obj
		}
		if inD {
			dObj = do.obj
		}

		mineChanged := !objectEqual(bObj, mObj) || (inB && inM && bo.layer != mo.layer)
		diskChanged := !objectEqual(bObj, dObj) || (inB && inD && bo.layer != do.layer)
		if !mineChanged && !diskChanged {
			continue
		}
		// If both sides made the exact same change, no conflict entry is needed.
		if mineChanged && diskChanged && objectEqual(mObj, dObj) && mo.layer == do.layer {
			continue
		}

		layer := mo.layer
		if layer == "" {
			layer = do.layer
		}
		if layer == "" {
			layer = bo.layer
		}
		name := diskObjectDisplayName(mObj, diskObjectDisplayName(dObj, diskObjectDisplayName(bObj, id)))

		ent := SceneConflictEntry{
			ObjectID:    id,
			ObjectName:  name,
			Layer:       layer,
			MineChanged: mineChanged,
			DiskChanged: diskChanged,
			Overlap:     mineChanged && diskChanged,
		}
		if mineChanged {
			ent.MineSummary = summarizeObjectChange(bObj, mObj)
		} else {
			ent.MineSummary = "—"
		}
		if diskChanged {
			ent.DiskSummary = summarizeObjectChange(bObj, dObj)
		} else {
			ent.DiskSummary = "—"
		}

		switch {
		case ent.Overlap:
			ent.Choice = DiskMergeMine
			ent.Note = "changed on both sides"
			overlaps = append(overlaps, ent)
		case diskChanged:
			ent.Choice = DiskMergeDisk
			ent.Note = "only on disk · " + ent.DiskSummary
			singleSide = append(singleSide, ent)
		default:
			ent.Choice = DiskMergeMine
			ent.Note = "only yours · " + ent.MineSummary
			singleSide = append(singleSide, ent)
		}
	}
	return append(overlaps, singleSide...)
}

func mergeSceneDocs(base, mine, disk *sceneformat.SceneDoc, entries []SceneConflictEntry) (*sceneformat.SceneDoc, error) {
	if mine == nil || disk == nil {
		return nil, errors.New("editor: cannot merge nil scene document")
	}
	merged := mine.Clone()
	diskClone := disk.Clone()

	// Apply non-conflicting scene-level metadata changes from disk (when mine didn't change them from base).
	if base != nil {
		if mine.Version == base.Version && disk.Version > base.Version && disk.Version > merged.Version {
			merged.Version = disk.Version
		}
		if mine.Name == base.Name && disk.Name != base.Name {
			merged.Name = disk.Name
		}
		if mine.NavMesh == base.NavMesh && disk.NavMesh != base.NavMesh {
			merged.NavMesh = disk.NavMesh
		}
		if mine.BackgroundColor == base.BackgroundColor && disk.BackgroundColor != base.BackgroundColor {
			merged.BackgroundColor = disk.BackgroundColor
		}
		if mine.AmbientColor == base.AmbientColor && disk.AmbientColor != base.AmbientColor {
			merged.AmbientColor = disk.AmbientColor
		}
		if mine.VirtualWidth == base.VirtualWidth && mine.VirtualHeight == base.VirtualHeight &&
			(disk.VirtualWidth != base.VirtualWidth || disk.VirtualHeight != base.VirtualHeight) {
			merged.VirtualWidth = disk.VirtualWidth
			merged.VirtualHeight = disk.VirtualHeight
		}
		if reflect.DeepEqual(mine.Camera, base.Camera) && !reflect.DeepEqual(disk.Camera, base.Camera) {
			merged.Camera = diskClone.Camera
		}
		if reflect.DeepEqual(mine.Environment, base.Environment) && !reflect.DeepEqual(disk.Environment, base.Environment) {
			merged.Environment = diskClone.Environment
			if merged.Environment != nil && merged.Version < sceneformat.CurrentVersion {
				merged.Version = sceneformat.CurrentVersion
			}
		}
	}

	// Ensure any layers present in disk that will receive disk-chosen objects exist in merged.
	diskObjs, _ := indexSceneObjects(disk)

	for _, ent := range entries {
		if ent.Choice != DiskMergeDisk {
			continue
		}
		do, existsOnDisk := diskObjs[ent.ObjectID]
		if !existsOnDisk {
			diskRemoveObjectFromDoc(merged, ent.ObjectID)
			continue
		}
		diskCopy := cloneSingleObjectWithoutChildren(do.obj)
		if (diskCopy.Light != nil || diskCopy.Camera != nil) && merged.Version < sceneformat.CurrentVersion {
			merged.Version = sceneformat.CurrentVersion
		}
		if existing := findObjectPtrInDoc(merged, ent.ObjectID); existing != nil {
			// Preserve merged children (children have their own entries).
			diskCopy.Children = existing.Children
			*existing = *diskCopy
			// If layer changed on disk, move top-level object to target layer.
			if curLayer := findObjectLayerInDoc(merged, ent.ObjectID); curLayer != "" && curLayer != do.layer {
				if removed, ok := removeTopLevelObjectFromLayer(merged, curLayer, ent.ObjectID); ok {
					layer := ensureLayerFromDisk(merged, disk, do.layer)
					layer.Objects = append(layer.Objects, removed)
				}
			}
		} else {
			// Object was added on disk (or removed in mine and user chose disk).
			// Preserve parent-child hierarchy when its parent exists in merged.
			if do.parentID != "" {
				if parent := findObjectPtrInDoc(merged, do.parentID); parent != nil {
					parent.Children = append(parent.Children, *diskCopy)
					continue
				}
			}
			layer := ensureLayerFromDisk(merged, disk, do.layer)
			layer.Objects = append(layer.Objects, *diskCopy)
		}
	}

	return merged, nil
}

func cloneSingleObjectWithoutChildren(o *sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
	if o == nil {
		return nil
	}
	tmp := &sceneformat.SceneDoc{
		Version: sceneformat.CurrentVersion,
		Layers: []sceneformat.LayerDoc{
			{Name: "tmp", Objects: []sceneformat.ObjectDoc{*o}},
		},
	}
	cloned := tmp.Clone().Layers[0].Objects[0]
	cloned.Children = nil
	return &cloned
}

func findObjectPtrInDoc(doc *sceneformat.SceneDoc, id string) *sceneformat.ObjectDoc {
	if doc == nil {
		return nil
	}
	for i := range doc.Layers {
		if o := findObjectPtrInSlice(doc.Layers[i].Objects, id); o != nil {
			return o
		}
	}
	return nil
}

func findObjectPtrInSlice(objs []sceneformat.ObjectDoc, id string) *sceneformat.ObjectDoc {
	for i := range objs {
		if objs[i].ID == id {
			return &objs[i]
		}
		if child := findObjectPtrInSlice(objs[i].Children, id); child != nil {
			return child
		}
	}
	return nil
}

func findObjectLayerInDoc(doc *sceneformat.SceneDoc, id string) string {
	if doc == nil {
		return ""
	}
	for i := range doc.Layers {
		if findObjectPtrInSlice(doc.Layers[i].Objects, id) != nil {
			return doc.Layers[i].Name
		}
	}
	return ""
}

func diskRemoveObjectFromDoc(doc *sceneformat.SceneDoc, id string) bool {
	if doc == nil {
		return false
	}
	for i := range doc.Layers {
		var removed bool
		doc.Layers[i].Objects, removed = removeObjectFromSlice(doc.Layers[i].Objects, id)
		if removed {
			return true
		}
	}
	return false
}

func removeObjectFromSlice(objs []sceneformat.ObjectDoc, id string) ([]sceneformat.ObjectDoc, bool) {
	for i := range objs {
		if objs[i].ID == id {
			return append(objs[:i], objs[i+1:]...), true
		}
		var removed bool
		objs[i].Children, removed = removeObjectFromSlice(objs[i].Children, id)
		if removed {
			return objs, true
		}
	}
	return objs, false
}

func removeTopLevelObjectFromLayer(doc *sceneformat.SceneDoc, layerName, id string) (sceneformat.ObjectDoc, bool) {
	for i := range doc.Layers {
		if doc.Layers[i].Name != layerName {
			continue
		}
		for j := range doc.Layers[i].Objects {
			if doc.Layers[i].Objects[j].ID == id {
				obj := doc.Layers[i].Objects[j]
				doc.Layers[i].Objects = append(doc.Layers[i].Objects[:j], doc.Layers[i].Objects[j+1:]...)
				return obj, true
			}
		}
	}
	return sceneformat.ObjectDoc{}, false
}

func ensureLayerFromDisk(merged, disk *sceneformat.SceneDoc, layerName string) *sceneformat.LayerDoc {
	for i := range merged.Layers {
		if merged.Layers[i].Name == layerName {
			return &merged.Layers[i]
		}
	}
	diskClone := disk.Clone()
	for i := range diskClone.Layers {
		if diskClone.Layers[i].Name == layerName {
			l := diskClone.Layers[i]
			l.Objects = nil
			merged.Layers = append(merged.Layers, l)
			return &merged.Layers[len(merged.Layers)-1]
		}
	}
	merged.Layers = append(merged.Layers, sceneformat.LayerDoc{Name: layerName, Space: sceneformat.SpaceWorld})
	return &merged.Layers[len(merged.Layers)-1]
}

// discardClip restores the open clip to its saved state (or closes an unsaved new clip) when the author chooses Discard.
func (e *Editor) discardClip() {
	if e == nil || !e.ClipDirty() {
		return
	}
	if len(e.clipSaved) == 0 {
		e.stopClipLoop()
		e.clip = nil
		e.clipMeta = clipMeta{}
		e.clipScene = nil
		e.clipPlayer = nil
		e.clipErr = nil
		e.clearClipDiskSnapshot()
		return
	}
	if clip, err := resource.ParseAs[*resource.Animation](e.clipSaved); err == nil {
		e.clip = clip
		e.refreshClipPreview()
	}
}

// clearDiskWatchNotified resets the watcher's dedup hashes (for example after Stop() exits Play mode) so any external change that arrived during Play is picked up immediately.
func (e *Editor) clearDiskWatchNotified() {
	if e == nil {
		return
	}
	e.disk.mu.Lock()
	e.disk.mirrored.notifiedSceneHas = false
	e.disk.mirrored.notifiedProjectHas = false
	e.disk.mirrored.notifiedClipHas = false
	e.disk.mu.Unlock()
}
