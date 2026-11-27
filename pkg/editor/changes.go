package editor

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/resource"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/sound"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
	"github.com/nsgo-dev/nsgo/audio"
	"github.com/nsgo-dev/nsgo/core"
	"go.yaml.in/yaml/v3"
)

const (
	maxChangesDiffReadBytes     int64 = 2 << 20  // 2 MiB cap for untracked file reads and git diff output
	maxChangesDiffLines               = 600      // max diff lines kept per file for UI rendering
	maxChangesSceneBytes        int64 = 4 << 20  // 4 MiB cap for scene YAML parsing
	maxChangesAudioInspectBytes int64 = 8 << 20  // 8 MiB cap for eager WAV decoding/waveform extraction
	maxChangesAuditionBytes     int64 = 32 << 20 // 32 MiB cap for audio audition playback
	defaultChangesChecksTimeout       = 5 * time.Minute
)

// ChangesCategory groups changed working-tree files in the Changes panel.
type ChangesCategory string

const (
	ChangesCategoryScenes ChangesCategory = "SCENES"
	ChangesCategoryData   ChangesCategory = "DATA"
	ChangesCategoryCode   ChangesCategory = "CODE"
	ChangesCategoryAssets ChangesCategory = "ASSETS"
)

var changesCategoryOrder = []ChangesCategory{
	ChangesCategoryScenes,
	ChangesCategoryData,
	ChangesCategoryCode,
	ChangesCategoryAssets,
}

// ChangesFileStatus describes a file's Git working-tree state compared with HEAD.
type ChangesFileStatus string

const (
	ChangesStatusModified  ChangesFileStatus = "modified"
	ChangesStatusAdded     ChangesFileStatus = "added"
	ChangesStatusDeleted   ChangesFileStatus = "deleted"
	ChangesStatusUntracked ChangesFileStatus = "untracked"
	ChangesStatusRenamed   ChangesFileStatus = "renamed"
)

// SceneObjectChangeKind classifies how a scene object differs from HEAD.
type SceneObjectChangeKind string

const (
	SceneObjectAdded   SceneObjectChangeKind = "added"
	SceneObjectChanged SceneObjectChangeKind = "changed"
	SceneObjectRemoved SceneObjectChangeKind = "removed"
)

// SceneObjectChange describes one added, changed, or removed object in a scene compared with HEAD.
type SceneObjectChange struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Layer   string                 `json:"layer,omitempty"`
	Kind    SceneObjectChangeKind  `json:"kind"`
	Summary string                 `json:"summary,omitempty"`
	HeadObj *sceneformat.ObjectDoc `json:"-"`
	NowObj  *sceneformat.ObjectDoc `json:"-"`
}

// SceneComparison holds the parsed HEAD and working-tree versions of a changed scene along with per-object deltas.
type SceneComparison struct {
	HeadDoc   *sceneformat.SceneDoc   `json:"-"`
	NowDoc    *sceneformat.SceneDoc   `json:"-"`
	HeadBuilt *sceneformat.BuiltScene `json:"-"`
	NowBuilt  *sceneformat.BuiltScene `json:"-"`
	Added     []SceneObjectChange     `json:"added,omitempty"`
	Changed   []SceneObjectChange     `json:"changed,omitempty"`
	Removed   []SceneObjectChange     `json:"removed,omitempty"`
	HeadErr   string                  `json:"headErr,omitempty"`
	NowErr    string                  `json:"nowErr,omitempty"`
}

// DiffLineKind identifies the role of one unified diff line.
type DiffLineKind string

const (
	DiffLineContext DiffLineKind = "context"
	DiffLineAdd     DiffLineKind = "add"
	DiffLineDel     DiffLineKind = "del"
	DiffLineHunk    DiffLineKind = "hunk"
	DiffLineMeta    DiffLineKind = "meta"
)

// DiffLine is one line in a unified diff preview against HEAD.
type DiffLine struct {
	OldLine int          `json:"oldLine,omitempty"`
	NewLine int          `json:"newLine,omitempty"`
	Sign    string       `json:"sign,omitempty"`
	Text    string       `json:"text"`
	Kind    DiffLineKind `json:"kind"`
}

// AudioChangeInfo holds audition metadata, waveform bars, and incoming project references for a changed audio file.
type AudioChangeInfo struct {
	SizeBytes  int64         `json:"sizeBytes"`
	Format     string        `json:"format"`
	SampleRate int           `json:"sampleRate,omitempty"`
	Channels   int           `json:"channels,omitempty"`
	Duration   time.Duration `json:"duration,omitempty"`
	Waveform   []float32     `json:"waveform,omitempty"`
	UsedBy     []string      `json:"usedBy,omitempty"`
}

// ChangedFile represents one modified, added, deleted, renamed, or untracked file compared with HEAD.
type ChangedFile struct {
	Path        string            `json:"path"`
	OldPath     string            `json:"oldPath,omitempty"`
	ProjectPath string            `json:"projectPath,omitempty"`
	DisplayName string            `json:"displayName"`
	Category    ChangesCategory   `json:"category"`
	Status      ChangesFileStatus `json:"status"`
	StatusCode  string            `json:"statusCode"`
	Included    bool              `json:"included"`
	Binary      bool              `json:"binary,omitempty"`
	Detail      string            `json:"detail,omitempty"`
	PlusLines   int               `json:"plusLines,omitempty"`
	MinusLines  int               `json:"minusLines,omitempty"`
	Diff        []DiffLine        `json:"diff,omitempty"`
	Scene       *SceneComparison  `json:"scene,omitempty"`
	Audio       *AudioChangeInfo  `json:"audio,omitempty"`
}

// ChangesCheckStatus records the latest nsgo check and nsgo test outcomes shown in the Changes panel.
type ChangesCheckStatus struct {
	Ran          bool      `json:"ran"`
	RanAt        time.Time `json:"ranAt,omitempty"`
	CheckPassed  bool      `json:"checkPassed"`
	CheckSummary string    `json:"checkSummary,omitempty"`
	CheckOutput  string    `json:"checkOutput,omitempty"`
	TestPassed   bool      `json:"testPassed"`
	TestCount    int       `json:"testCount,omitempty"`
	TestSummary  string    `json:"testSummary,omitempty"`
	TestOutput   string    `json:"testOutput,omitempty"`
}

// CommitDraftRequest supplies the included files and review/playtest context to Editor.CommitMessageDrafter.
type CommitDraftRequest struct {
	RepoDir         string
	Branch          string
	HeadSHA         string
	Files           []ChangedFile
	Reviews         int
	PlaytestSummary string
	AgentRequests   int
	Checks          ChangesCheckStatus
}

// CommitDraft is a drafted commit message and provenance metadata for the Changes commit card.
type CommitDraft struct {
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Provenance  string `json:"provenance,omitempty"`
	Attribution string `json:"attribution,omitempty"`
}

// CommitMilestone records a commit created from the Changes panel so History and the Changes panel can display it.
type CommitMilestone struct {
	SHA             string    `json:"sha"`
	Summary         string    `json:"summary"`
	Description     string    `json:"description,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	Files           []string  `json:"files,omitempty"`
	Timestamp       time.Time `json:"timestamp,omitempty"`
	Pushed          bool      `json:"pushed,omitempty"`
	AgentRequests   int       `json:"agentRequests,omitempty"`
	PlaytestSummary string    `json:"playtestSummary,omitempty"`
}

// ChangesSceneViewMode controls whether a changed scene renders HEAD and Now side by side, HEAD only, or Now only.
type ChangesSceneViewMode string

const (
	ChangesSceneViewSideBySide ChangesSceneViewMode = "side-by-side"
	ChangesSceneViewHead       ChangesSceneViewMode = "head"
	ChangesSceneViewNow        ChangesSceneViewMode = "now"
)

// ChangesSession holds the live state of the Changes & Commit screen.
type ChangesSession struct {
	RepoDir       string
	ProjectDir    string
	ProjectPrefix string
	ProjectName   string
	SceneName     string

	Branch     string
	Branches   []string
	HeadSHA    string
	HasHead    bool
	Upstream   string
	BaseBranch string
	Ahead      int
	Behind     int

	Files        []ChangedFile
	SelectedPath string
	SceneView    ChangesSceneViewMode
	Checks       ChangesCheckStatus

	DraftSummary      string
	DraftDescription  string
	DraftProvenance   string
	DraftAttribution  string
	ReviewCount       int
	PlaytestSummary   string
	AgentRequestCount int
	draftEdited       bool

	Committed  bool
	LastCommit *CommitMilestone
	Milestones []CommitMilestone

	GHAvailable       bool
	LastPRDescription string
	LastPRURL         string
	LastErr           string
	LastNotice        string
}

type changesState struct {
	open            bool
	session         *ChangesSession
	count           int
	hasRepo         bool
	milestones      []CommitMilestone
	auditionPath    string
	audioMixer      *sound.Mixer
	audioMixerScope *core.Scope
	audioMusic      *sound.Music
	rev             uint64
	draftRev        uint64

	opMu      sync.Mutex
	opGen     uint64
	opCancel  context.CancelFunc
	opDone    chan struct{}
	busy      bool
	busyLabel string
}

var (
	// ErrNoChangesSession is returned when a Changes action runs while the Changes panel has no active session.
	ErrNoChangesSession = errors.New("editor: changes panel is not open")
	// ErrMergeConflictInProgress is returned when a Changes action is blocked by an unresolved Git merge conflict.
	ErrMergeConflictInProgress = errors.New("editor: resolve the active Git merge conflict before using Changes")
	// ErrUnsavedChanges is returned when switching branches or pulling while open editor files have unsaved changes.
	ErrUnsavedChanges = errors.New("editor: save or discard unsaved changes before switching branches or pulling")
	// ErrChangesBusy is returned when an asynchronous Changes operation is already running.
	ErrChangesBusy = errors.New("editor: a Git or check operation is already in progress")
)

func (e *Editor) changesStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.changes.rev
}

func (e *Editor) changesDraftRev() uint64 {
	if e == nil {
		return 0
	}
	return e.changes.draftRev
}

// ChangesBusy reports whether an asynchronous Changes operation (checks, pull, push, or PR creation) is running.
func (e *Editor) ChangesBusy() bool {
	if e == nil {
		return false
	}
	e.changes.opMu.Lock()
	busy := e.changes.busy
	e.changes.opMu.Unlock()
	return busy
}

// ChangesBusyLabel returns a short status label for the currently running asynchronous Changes operation, or empty when idle.
func (e *Editor) ChangesBusyLabel() string {
	if e == nil {
		return ""
	}
	e.changes.opMu.Lock()
	label := e.changes.busyLabel
	e.changes.opMu.Unlock()
	return label
}

// WaitChangesOp blocks until any in-flight asynchronous Changes operation completes (primarily for tests).
func (e *Editor) WaitChangesOp() {
	if e == nil {
		return
	}
	e.changes.opMu.Lock()
	done := e.changes.opDone
	e.changes.opMu.Unlock()
	if done != nil {
		<-done
	}
}

func (e *Editor) beginChangesOp(label string, timeout time.Duration) (ctx context.Context, gen uint64, done chan struct{}, cancel context.CancelFunc, err error) {
	e.changes.opMu.Lock()
	if e.changes.busy {
		e.changes.opMu.Unlock()
		return nil, 0, nil, nil, ErrChangesBusy
	}
	e.changes.opGen++
	gen = e.changes.opGen
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	done = make(chan struct{})
	e.changes.opCancel = cancel
	e.changes.opDone = done
	e.changes.busy = true
	e.changes.busyLabel = label
	e.changes.opMu.Unlock()
	e.changes.rev++
	e.notify()
	return ctx, gen, done, cancel, nil
}

func (e *Editor) finishChangesOp(gen uint64, cancel context.CancelFunc) bool {
	if cancel != nil {
		cancel()
	}
	e.changes.opMu.Lock()
	stale := e.changes.opGen != gen
	if !stale {
		e.changes.opCancel = nil
		e.changes.opDone = nil
		e.changes.busy = false
		e.changes.busyLabel = ""
	}
	e.changes.opMu.Unlock()
	return !stale
}

func (e *Editor) cancelChangesOps() {
	if e == nil {
		return
	}
	e.changes.opMu.Lock()
	e.changes.opGen++
	cancel := e.changes.opCancel
	e.changes.opCancel = nil
	e.changes.opDone = nil
	e.changes.busy = false
	e.changes.busyLabel = ""
	e.changes.opMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ChangesOpen reports whether the Changes & Commit screen is currently active.
func (e *Editor) ChangesOpen() bool {
	return e != nil && e.changes.open && e.changes.session != nil
}

// ChangesSession returns the active Changes & Commit session, or nil when closed.
func (e *Editor) ChangesSession() *ChangesSession {
	if e == nil {
		return nil
	}
	return e.changes.session
}

// ChangesCount returns the last observed number of changed working-tree files in the open project's Git repository.
func (e *Editor) ChangesCount() int {
	if e == nil {
		return 0
	}
	if e.changes.open && e.changes.session != nil {
		return len(e.changes.session.Files)
	}
	return e.changes.count
}

// ChangesAuditioningPath returns the repo-relative path of the audio file currently auditioning in the Changes panel, or empty when idle.
func (e *Editor) ChangesAuditioningPath() string {
	if e == nil {
		return ""
	}
	return e.changes.auditionPath
}

// CommitMilestones returns a copy of all commit milestones recorded during this editor session, most recent first.
func (e *Editor) CommitMilestones() []CommitMilestone {
	if e == nil {
		return nil
	}
	if len(e.changes.milestones) > 0 {
		return slices.Clone(e.changes.milestones)
	}
	if e.History != nil {
		return e.History.Milestones()
	}
	return nil
}

// CloseChangesPanel closes the Changes & Commit screen and stops any audio audition started from it.
func (e *Editor) CloseChangesPanel() {
	if e == nil || !e.changes.open {
		return
	}
	e.stopChangedAudioPlayback()
	if e.changes.session != nil {
		e.changes.count = len(e.changes.session.Files)
	}
	e.changes.open = false
	e.changes.rev++
	e.notify()
}

// resolveChangesRepoScope resolves symlinks on repoRoot and projectDir and reports whether projectDir
// is a strict subdirectory of repoRoot (e.g. a project nested inside a monorepo/parent repository).
func resolveChangesRepoScope(repoRoot, projectDir string) (realRoot, projectPrefix string, scoped bool) {
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		absRoot = filepath.Clean(repoRoot)
	}
	if eval, err := filepath.EvalSymlinks(absRoot); err == nil {
		realRoot = eval
	} else {
		realRoot = absRoot
	}
	if projectDir == "" {
		return realRoot, "", false
	}
	absProj, err := filepath.Abs(projectDir)
	if err != nil {
		absProj = filepath.Clean(projectDir)
	}
	realProj := absProj
	if eval, err := filepath.EvalSymlinks(absProj); err == nil {
		realProj = eval
	}
	rel, err := filepath.Rel(realRoot, realProj)
	if err != nil || rel == "" || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return realRoot, "", false
	}
	return realRoot, filepath.ToSlash(rel) + "/", true
}

// resolveRepoFilePath verifies that relPath stays inside repoRoot (and inside projectPrefix when scoped)
// and that no symlink escapes the allowed root, returning the resolved absolute path.
func resolveRepoFilePath(repoRoot, projectPrefix, relPath string) (string, error) {
	if repoRoot == "" {
		return "", errors.New("editor: empty repository directory")
	}
	if relPath == "" || strings.ContainsRune(relPath, 0) || filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" || strings.Contains(relPath, `\`) {
		return "", fmt.Errorf("editor: invalid repository path %q", relPath)
	}
	cleaned := path.Clean(relPath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("editor: path %q escapes repository root", relPath)
	}
	if projectPrefix != "" && !strings.HasPrefix(cleaned, projectPrefix) {
		return "", fmt.Errorf("editor: path %q is outside project scope %q", relPath, projectPrefix)
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	realRoot := absRoot
	if eval, err := filepath.EvalSymlinks(absRoot); err == nil {
		realRoot = eval
	}
	boundary := realRoot
	if projectPrefix != "" {
		projBoundary := filepath.Join(realRoot, filepath.FromSlash(strings.TrimSuffix(projectPrefix, "/")))
		if eval, err := filepath.EvalSymlinks(projBoundary); err == nil {
			boundary = eval
		} else {
			boundary = projBoundary
		}
	}
	candidate := filepath.Join(realRoot, filepath.FromSlash(cleaned))
	_, abs, err := resolvePathWithinRoot(boundary, candidate)
	if err != nil {
		return "", fmt.Errorf("editor: %s resolves outside allowed root", relPath)
	}
	return abs, nil
}

func (e *Editor) hasGitMergeInProgress(repoRoot string) bool {
	if e == nil {
		return false
	}
	if e.MergeConflictOpen() || len(e.merge.detectedScenes) > 0 {
		return true
	}
	if repoRoot == "" {
		return false
	}
	_, active := e.detectGitConflictOp(repoRoot)
	return active
}

// RefreshChangesCount updates the lightweight working-tree changed-file count displayed in the status bar.
func (e *Editor) RefreshChangesCount() {
	if e == nil {
		return
	}
	dir := e.gitProjectDir()
	if dir == "" {
		changed := e.changes.count != 0 || e.changes.hasRepo
		e.changes.count = 0
		e.changes.hasRepo = false
		if changed {
			e.changes.rev++
			e.notify()
		}
		return
	}
	repoRoot, err := e.runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		changed := e.changes.count != 0 || e.changes.hasRepo
		e.changes.count = 0
		e.changes.hasRepo = false
		if changed {
			e.changes.rev++
			e.notify()
		}
		return
	}
	e.changes.hasRepo = true
	_, projectPrefix, scoped := resolveChangesRepoScope(repoRoot, e.resolveChangesProjectDir(repoRoot))
	statusArgs := []string{"-c", "core.quotepath=false", "status", "--porcelain=v1", "-z", "-uall"}
	if scoped {
		statusArgs = append(statusArgs, "--", strings.TrimSuffix(projectPrefix, "/"))
	}
	raw, err := e.runGitRaw(repoRoot, statusArgs...)
	if err != nil {
		return
	}
	entries := filterPorcelainByScope(parseGitPorcelain(string(raw)), projectPrefix)
	e.changes.count = len(entries)
	e.changes.rev++
	e.notify()
}

// OpenChangesPanel opens the Changes & Commit screen for the open project's Git repository and refreshes its working-tree diff against HEAD.
func (e *Editor) OpenChangesPanel() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.Playing() {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return ErrMergeConflictInProgress
	}
	dir := e.gitProjectDir()
	if dir == "" && e.changes.session != nil {
		dir = e.changes.session.RepoDir
	}
	if dir == "" {
		return errors.New("editor: open a project or file inside a Git repository to view Changes")
	}
	return e.OpenChangesPanelIn(dir)
}

// OpenChangesPanelIn opens the Changes & Commit screen for the Git repository containing dir.
func (e *Editor) OpenChangesPanelIn(dir string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.Playing() {
		return ErrPlaying
	}
	if e.MergeConflictOpen() {
		return ErrMergeConflictInProgress
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("editor: empty directory")
	}
	repoRoot, err := e.runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil || repoRoot == "" {
		return fmt.Errorf("editor: %s is not inside a Git repository", dir)
	}
	if e.hasGitMergeInProgress(repoRoot) {
		return ErrMergeConflictInProgress
	}
	projDir := e.resolveChangesProjectDir(repoRoot)
	_, projectPrefix, _ := resolveChangesRepoScope(repoRoot, projDir)
	if e.changes.session == nil || filepath.Clean(e.changes.session.RepoDir) != filepath.Clean(repoRoot) || e.changes.session.ProjectPrefix != projectPrefix {
		e.changes.session = &ChangesSession{
			RepoDir:       repoRoot,
			ProjectDir:    projDir,
			ProjectPrefix: projectPrefix,
			SceneView:     ChangesSceneViewSideBySide,
			Milestones:    slices.Clone(e.changes.milestones),
		}
	} else {
		e.changes.session.RepoDir = repoRoot
		e.changes.session.ProjectDir = projDir
		e.changes.session.ProjectPrefix = projectPrefix
		if e.changes.session.SceneView == "" {
			e.changes.session.SceneView = ChangesSceneViewSideBySide
		}
	}
	e.changes.open = true
	e.changes.hasRepo = true
	if err := e.refreshChangesSession(false); err != nil {
		return err
	}
	return nil
}

// RefreshChanges reloads Git branch, upstream ahead/behind counts, changed files, diffs, and scene comparisons for the active Changes session.
func (e *Editor) RefreshChanges() error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	return e.refreshChangesSession(false)
}

func (e *Editor) resolveChangesProjectDir(repoRoot string) string {
	if e != nil && e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	return repoRoot
}

func (e *Editor) refreshChangesSession(keepCommitted bool) error {
	sess := e.changes.session
	if sess == nil {
		return ErrNoChangesSession
	}
	repoRoot := sess.RepoDir
	if repoRoot == "" {
		return errors.New("editor: empty repository directory")
	}

	// Preserve existing include checkboxes across refreshes.
	prevExcluded := make(map[string]bool, len(sess.Files))
	for _, f := range sess.Files {
		if !f.Included {
			prevExcluded[f.Path] = true
		}
	}
	prevSelected := sess.SelectedPath

	// Project and scene header labels.
	projName := filepath.Base(repoRoot)
	if e.Project != nil && e.Project.Name != "" {
		projName = e.Project.Name
	}
	sceneName := "scene"
	switch {
	case e.Doc != nil && e.Doc.Name != "":
		sceneName = e.Doc.Name
	case e.ScenePath != "":
		sceneName = trimSceneFilename(e.ScenePath)
	case e.FilePath != "":
		sceneName = trimSceneFilename(e.FilePath)
	}
	sess.ProjectName = projName
	sess.SceneName = sceneName

	// Branch, HEAD, branches list, and ahead/behind counts.
	if b, err := e.runGit(repoRoot, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && b != "" {
		sess.Branch = b
	} else if sess.Branch == "" {
		sess.Branch = "HEAD"
	}
	if sha, err := e.runGit(repoRoot, "rev-parse", "--short", "HEAD"); err == nil && sha != "" {
		sess.HeadSHA = sha
		sess.HasHead = true
	} else {
		sess.HeadSHA = "unborn"
		sess.HasHead = false
	}
	if out, err := e.runGit(repoRoot, "for-each-ref", "--format=%(refname:short)", "refs/heads"); err == nil {
		var branches []string
		for _, line := range strings.Split(out, "\n") {
			if b := strings.TrimSpace(line); b != "" {
				branches = append(branches, b)
			}
		}
		slices.Sort(branches)
		if sess.Branch != "" && sess.Branch != "HEAD" && !slices.Contains(branches, sess.Branch) {
			branches = append([]string{sess.Branch}, branches...)
		}
		sess.Branches = branches
	}

	sess.Upstream = ""
	sess.Ahead = 0
	sess.Behind = 0
	if up, err := e.runGit(repoRoot, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil && up != "" {
		sess.Upstream = up
		if counts, cerr := e.runGit(repoRoot, "rev-list", "--left-right", "--count", "@{u}...HEAD"); cerr == nil {
			fields := strings.Fields(counts)
			if len(fields) == 2 {
				sess.Behind, _ = strconv.Atoi(fields[0])
				sess.Ahead, _ = strconv.Atoi(fields[1])
			}
		}
	}
	sess.BaseBranch = detectBaseBranch(sess.Branches, sess.Upstream, sess.Branch)
	sess.GHAvailable = e.detectGHAvailable()

	// Working-tree status against HEAD, scoped strictly to ProjectPrefix when nested inside a parent repository.
	_, projectPrefix, scoped := resolveChangesRepoScope(repoRoot, sess.ProjectDir)
	sess.ProjectPrefix = projectPrefix
	statusArgs := []string{"-c", "core.quotepath=false", "status", "--porcelain=v1", "-z", "-uall"}
	if scoped {
		statusArgs = append(statusArgs, "--", strings.TrimSuffix(projectPrefix, "/"))
	}
	rawStatus, err := e.runGitRaw(repoRoot, statusArgs...)
	if err != nil {
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return fmt.Errorf("editor: git status: %w", err)
	}
	porcelain := filterPorcelainByScope(parseGitPorcelain(string(rawStatus)), projectPrefix)

	// Collect audio targets once so incoming references are scanned in a single pass per refresh.
	audioTargets := make(map[string]bool)
	for _, entry := range porcelain {
		projRel := entry.Path
		if projectPrefix != "" {
			projRel = strings.TrimPrefix(projRel, projectPrefix)
		}
		if e.classifyChangedFile(entry.Path, projRel) == ChangesCategoryAssets && isAudioPath(entry.Path) {
			audioTargets[projRel] = true
		}
	}
	var audioRefs map[string][]string
	if len(audioTargets) > 0 {
		audioRefs = e.scanAudioIncomingReferences(sess.ProjectDir, audioTargets)
	}

	files := make([]ChangedFile, 0, len(porcelain))
	for _, entry := range porcelain {
		projRel := entry.Path
		if projectPrefix != "" && strings.HasPrefix(projRel, projectPrefix) {
			projRel = strings.TrimPrefix(projRel, projectPrefix)
		}
		display := projRel
		cat := e.classifyChangedFile(entry.Path, projRel)
		cf := ChangedFile{
			Path:        entry.Path,
			OldPath:     entry.OldPath,
			ProjectPath: projRel,
			DisplayName: display,
			Category:    cat,
			Status:      entry.Status,
			StatusCode:  entry.Code,
			Included:    !prevExcluded[entry.Path],
		}
		e.populateChangedFileDetails(repoRoot, projectPrefix, sess.HasHead, audioRefs, &cf)
		files = append(files, cf)
	}

	// Deterministic order: category order, then path.
	catRank := func(c ChangesCategory) int {
		for i, g := range changesCategoryOrder {
			if g == c {
				return i
			}
		}
		return len(changesCategoryOrder)
	}
	slices.SortStableFunc(files, func(a, b ChangedFile) int {
		if ra, rb := catRank(a.Category), catRank(b.Category); ra != rb {
			return ra - rb
		}
		return strings.Compare(a.Path, b.Path)
	})

	sess.Files = files
	e.changes.count = len(files)

	// Maintain selected file if still present, otherwise pick the first changed file.
	foundSel := false
	for _, f := range files {
		if f.Path == prevSelected {
			foundSel = true
			break
		}
	}
	if !foundSel {
		if len(files) > 0 {
			sess.SelectedPath = files[0].Path
		} else {
			sess.SelectedPath = ""
		}
	}

	if !keepCommitted {
		sess.Committed = false
	}
	sess.LastErr = ""
	if len(e.changes.milestones) > 0 {
		sess.Milestones = slices.Clone(e.changes.milestones)
	}

	// Auto-draft commit message on first open or when untouched by the author.
	if !sess.draftEdited && len(files) > 0 && !sess.Committed {
		e.applyCommitDraft(sess)
	}

	e.changes.rev++
	e.changes.draftRev++
	e.notify()
	return nil
}

type porcelainEntry struct {
	Path    string
	OldPath string
	Status  ChangesFileStatus
	Code    string
}

func filterPorcelainByScope(entries []porcelainEntry, projectPrefix string) []porcelainEntry {
	if projectPrefix == "" || len(entries) == 0 {
		return entries
	}
	out := make([]porcelainEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Path, projectPrefix) {
			if entry.OldPath != "" && !strings.HasPrefix(entry.OldPath, projectPrefix) {
				entry.OldPath = ""
			}
			out = append(out, entry)
		}
	}
	return out
}

func parseGitPorcelain(raw string) []porcelainEntry {
	if strings.ContainsRune(raw, 0) {
		return parseGitPorcelainZ(raw)
	}
	var out []porcelainEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		xy := line[:2]
		rest := line[3:]
		if rest == "" {
			continue
		}
		oldPath := ""
		// Renamed or copied: "old -> new"
		if strings.Contains(xy, "R") || strings.Contains(xy, "C") {
			if before, after, ok := strings.Cut(rest, " -> "); ok {
				oldPath = unquoteGitPath(before)
				rest = after
			}
		}
		p := filepath.ToSlash(unquoteGitPath(rest))
		if p == "" || strings.HasSuffix(p, "/") {
			continue
		}
		oldPath = filepath.ToSlash(oldPath)
		status, code := porcelainStatus(xy)
		out = append(out, porcelainEntry{
			Path:    p,
			OldPath: oldPath,
			Status:  status,
			Code:    code,
		})
	}
	return out
}

// parseGitPorcelainZ parses NUL-delimited `git status --porcelain=v1 -z` output,
// preserving both new Path and OldPath on renames/copies without quoting ambiguity.
func parseGitPorcelainZ(raw string) []porcelainEntry {
	var out []porcelainEntry
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < 4 {
			continue
		}
		xy := rec[:2]
		p := filepath.ToSlash(rec[3:])
		oldPath := ""
		if xy[0] == 'R' || xy[1] == 'R' || xy[0] == 'C' || xy[1] == 'C' {
			if i+1 < len(records) {
				i++
				oldPath = filepath.ToSlash(records[i])
			}
		}
		if p == "" || strings.HasSuffix(p, "/") {
			continue
		}
		status, code := porcelainStatus(xy)
		out = append(out, porcelainEntry{
			Path:    p,
			OldPath: oldPath,
			Status:  status,
			Code:    code,
		})
	}
	return out
}

func unquoteGitPath(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if uq, err := strconv.Unquote(s); err == nil {
			return uq
		}
	}
	return s
}

func porcelainStatus(xy string) (ChangesFileStatus, string) {
	if xy == "??" {
		return ChangesStatusUntracked, "A"
	}
	x, y := xy[0], xy[1]
	switch {
	case x == 'D' || y == 'D':
		return ChangesStatusDeleted, "D"
	case x == 'R' || y == 'R':
		return ChangesStatusRenamed, "R"
	case x == 'A' || y == 'A':
		return ChangesStatusAdded, "A"
	default:
		return ChangesStatusModified, "M"
	}
}

func detectBaseBranch(branches []string, upstream, current string) string {
	if upstream != "" {
		if _, rest, ok := strings.Cut(upstream, "/"); ok && rest != "" && rest != current {
			return rest
		}
	}
	for _, cand := range []string{"main", "master"} {
		if cand != current && slices.Contains(branches, cand) {
			return cand
		}
	}
	return "main"
}

func (e *Editor) classifyChangedFile(repoRel, projRel string) ChangesCategory {
	lower := strings.ToLower(repoRel)
	if isProjectScene(e.Project, repoRel) || isProjectScene(e.Project, projRel) ||
		strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml") {
		return ChangesCategoryScenes
	}
	ext := strings.ToLower(path.Ext(repoRel))
	switch ext {
	case ".go", ".mod", ".sum", ".sh", ".bash", ".zsh", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".py", ".wgsl", ".glsl", ".hlsl", ".metal":
		return ChangesCategoryCode
	case ".ogg", ".wav", ".mp3", ".flac", ".m4a", ".aac",
		".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".svg", ".ico", ".hdr", ".pic",
		".gltf", ".glb", ".obj", ".fbx", ".bin",
		".ttf", ".otf", ".woff", ".woff2", ".mp4", ".webm":
		return ChangesCategoryAssets
	default:
		return ChangesCategoryData
	}
}

func isAudioPath(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".wav", ".ogg", ".mp3", ".flac":
		return true
	default:
		return false
	}
}

func (e *Editor) populateChangedFileDetails(repoRoot, projectPrefix string, hasHead bool, audioRefs map[string][]string, cf *ChangedFile) {
	absPath, pathErr := resolveRepoFilePath(repoRoot, projectPrefix, cf.Path)

	// Populate unified diff & line counts for non-binary files (Scenes, Data, Code).
	if cf.Category != ChangesCategoryAssets {
		diffLines, plus, minus, binary := e.computeFileDiff(repoRoot, projectPrefix, cf.Path, cf.OldPath, cf.Status, hasHead)
		cf.Diff = diffLines
		cf.PlusLines = plus
		cf.MinusLines = minus
		cf.Binary = binary
	}

	switch cf.Category {
	case ChangesCategoryScenes:
		sc := e.compareSceneWithHEAD(repoRoot, projectPrefix, cf.Path, cf.OldPath, cf.Status, hasHead)
		cf.Scene = sc
		cf.Detail = formatSceneChangeDetail(sc, cf.Status, cf.PlusLines, cf.MinusLines)
	case ChangesCategoryAssets:
		if isAudioPath(cf.Path) {
			info := e.inspectAudioChange(absPath, pathErr, audioRefs[cf.ProjectPath])
			cf.Audio = info
			cf.Detail = formatAudioChangeDetail(info, cf.Status)
		} else {
			var sz int64
			if pathErr == nil {
				if st, err := os.Stat(absPath); err == nil {
					sz = st.Size()
				}
			}
			cf.Detail = formatAssetChangeDetail(sz, cf.Status)
		}
	default:
		if cf.Binary {
			var sz int64
			if pathErr == nil {
				if st, err := os.Stat(absPath); err == nil {
					sz = st.Size()
				}
			}
			cf.Detail = formatBinaryChangeDetail(sz, cf.Status)
		} else {
			cf.Detail = formatTextChangeDetail(cf.Status, cf.PlusLines, cf.MinusLines, cf.Diff)
		}
	}
}

func isBinaryData(data []byte) bool {
	n := min(len(data), 8192)
	return bytes.IndexByte(data[:n], 0) >= 0
}

func isGitBinaryDiff(raw []byte) bool {
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "Binary files ") && strings.HasSuffix(strings.TrimRight(line, "\r"), " differ") {
			return true
		}
		if strings.TrimRight(line, "\r") == "GIT binary patch" {
			return true
		}
		if strings.HasPrefix(line, "@@ ") {
			return false
		}
	}
	return false
}

func (e *Editor) computeFileDiff(repoRoot, projectPrefix, relPath, oldPath string, status ChangesFileStatus, hasHead bool) ([]DiffLine, int, int, bool) {
	readUntracked := func() ([]DiffLine, int, int, bool) {
		absPath, err := resolveRepoFilePath(repoRoot, projectPrefix, relPath)
		if err != nil {
			return []DiffLine{{Text: err.Error(), Kind: DiffLineMeta}}, 0, 0, false
		}
		data, truncated, size, err := readFileBounded(absPath, maxChangesDiffReadBytes)
		if err != nil {
			return nil, 0, 0, false
		}
		if isBinaryData(data) {
			return []DiffLine{{
				Text: fmt.Sprintf("Binary file (%s) — preview not shown", formatHumanBytes(size)),
				Kind: DiffLineMeta,
			}}, 0, 0, true
		}
		lines, plus, minus := buildAddedFileDiff(string(data))
		if truncated {
			lines = append(lines, DiffLine{
				Text: fmt.Sprintf("... file truncated at %s (total %s)", formatHumanBytes(maxChangesDiffReadBytes), formatHumanBytes(size)),
				Kind: DiffLineMeta,
			})
		}
		return lines, plus, minus, false
	}

	if status == ChangesStatusUntracked || !hasHead {
		return readUntracked()
	}

	diffArgs := []string{"diff", "--no-color", "-M", "-U3", "HEAD", "--"}
	if status == ChangesStatusRenamed && oldPath != "" {
		diffArgs = append(diffArgs, oldPath, relPath)
	} else {
		diffArgs = append(diffArgs, relPath)
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
	defer cancel()
	raw, truncated, err := e.runGitRawBoundedCtx(ctx, repoRoot, maxChangesDiffReadBytes, diffArgs...)
	if err != nil || len(raw) == 0 {
		if status == ChangesStatusAdded {
			return readUntracked()
		}
		return nil, 0, 0, false
	}
	if isGitBinaryDiff(raw) || isBinaryData(raw) {
		return []DiffLine{{Text: "Binary file changed — diff not shown", Kind: DiffLineMeta}}, 0, 0, true
	}
	lines, plus, minus := parseUnifiedDiff(string(raw))
	if truncated {
		lines = append(lines, DiffLine{
			Text: fmt.Sprintf("... diff truncated at %s", formatHumanBytes(maxChangesDiffReadBytes)),
			Kind: DiffLineMeta,
		})
	}
	return lines, plus, minus, false
}

func buildAddedFileDiff(content string) ([]DiffLine, int, int) {
	if content == "" {
		return nil, 0, 0
	}
	rawLines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	plus := len(rawLines)
	limit := min(plus, maxChangesDiffLines)
	lines := make([]DiffLine, 0, limit+1)
	for i := range limit {
		lines = append(lines, DiffLine{
			NewLine: i + 1,
			Sign:    "+",
			Text:    strings.TrimRight(rawLines[i], "\r"),
			Kind:    DiffLineAdd,
		})
	}
	if plus > limit {
		lines = append(lines, DiffLine{
			Text: fmt.Sprintf("... %d more lines (truncated at %d lines)", plus-limit, limit),
			Kind: DiffLineMeta,
		})
	}
	return lines, plus, 0
}

func parseUnifiedDiff(raw string) ([]DiffLine, int, int) {
	var lines []DiffLine
	oldNo, newNo := 0, 0
	plus, minus := 0, 0
	inHunk := false
	omitted := 0

	appendLine := func(dl DiffLine) {
		if len(lines) < maxChangesDiffLines {
			lines = append(lines, dl)
		} else {
			omitted++
		}
	}

	for _, rawLine := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, "@@ ") {
			inHunk = true
			o, n := parseHunkStart(line)
			oldNo, newNo = o, n
			appendLine(DiffLine{Text: line, Kind: DiffLineHunk})
			continue
		}
		if !inHunk {
			continue
		}
		if strings.HasPrefix(line, `\ No newline`) {
			appendLine(DiffLine{Text: line, Kind: DiffLineMeta})
			continue
		}
		if len(line) == 0 {
			appendLine(DiffLine{OldLine: oldNo, NewLine: newNo, Text: "", Kind: DiffLineContext})
			oldNo++
			newNo++
			continue
		}
		switch line[0] {
		case '+':
			plus++
			appendLine(DiffLine{NewLine: newNo, Sign: "+", Text: line[1:], Kind: DiffLineAdd})
			newNo++
		case '-':
			minus++
			appendLine(DiffLine{OldLine: oldNo, Sign: "-", Text: line[1:], Kind: DiffLineDel})
			oldNo++
		case ' ':
			appendLine(DiffLine{OldLine: oldNo, NewLine: newNo, Sign: "", Text: line[1:], Kind: DiffLineContext})
			oldNo++
			newNo++
		default:
			appendLine(DiffLine{Text: line, Kind: DiffLineMeta})
		}
	}
	if omitted > 0 {
		lines = append(lines, DiffLine{
			Text: fmt.Sprintf("... %d more diff lines omitted (truncated at %d lines)", omitted, maxChangesDiffLines),
			Kind: DiffLineMeta,
		})
	}
	return lines, plus, minus
}

func parseHunkStart(hunk string) (oldLine, newLine int) {
	// Format: @@ -oldStart[,oldCount] +newStart[,newCount] @@ ...
	fields := strings.Fields(hunk)
	for _, f := range fields {
		if strings.HasPrefix(f, "-") && len(f) > 1 {
			num, _, _ := strings.Cut(f[1:], ",")
			if v, err := strconv.Atoi(num); err == nil {
				oldLine = v
			}
		} else if strings.HasPrefix(f, "+") && len(f) > 1 {
			num, _, _ := strings.Cut(f[1:], ",")
			if v, err := strconv.Atoi(num); err == nil {
				newLine = v
			}
		}
	}
	return oldLine, newLine
}

func (e *Editor) compareSceneWithHEAD(repoRoot, projectPrefix, relPath, oldPath string, status ChangesFileStatus, hasHead bool) *SceneComparison {
	comp := &SceneComparison{}

	if hasHead && status != ChangesStatusUntracked && status != ChangesStatusAdded {
		headPath := relPath
		if status == ChangesStatusRenamed && oldPath != "" {
			headPath = oldPath
		}
		ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
		headBytes, truncated, err := e.runGitRawBoundedCtx(ctx, repoRoot, maxChangesSceneBytes, "show", "HEAD:"+headPath)
		cancel()
		if err == nil && len(bytes.TrimSpace(headBytes)) > 0 {
			if truncated {
				comp.HeadErr = fmt.Sprintf("HEAD scene exceeds %s limit", formatHumanBytes(maxChangesSceneBytes))
			} else {
				doc, perr := sceneformat.ParseWith(headBytes, sceneformat.ParseOptions{File: headPath + " (HEAD)", Components: e.components})
				if perr != nil {
					comp.HeadErr = perr.Error()
				} else {
					comp.HeadDoc = doc
					if built, berr := sceneformat.Build(doc, e.Resolver); berr == nil {
						comp.HeadBuilt = built
					}
				}
			}
		}
	}

	if status != ChangesStatusDeleted {
		absPath, pathErr := resolveRepoFilePath(repoRoot, projectPrefix, relPath)
		if pathErr != nil {
			comp.NowErr = pathErr.Error()
		} else if nowBytes, truncated, _, err := readFileBounded(absPath, maxChangesSceneBytes); err == nil && len(bytes.TrimSpace(nowBytes)) > 0 {
			if truncated {
				comp.NowErr = fmt.Sprintf("scene exceeds %s limit", formatHumanBytes(maxChangesSceneBytes))
			} else {
				doc, perr := sceneformat.ParseWith(nowBytes, sceneformat.ParseOptions{File: relPath, Components: e.components})
				if perr != nil {
					comp.NowErr = perr.Error()
				} else {
					comp.NowDoc = doc
					if built, berr := sceneformat.Build(doc, e.Resolver); berr == nil {
						comp.NowBuilt = built
					}
				}
			}
		}
	}

	diffSceneDocs(comp)
	return comp
}

type flatSceneObj struct {
	layer  string
	parent string
	obj    *sceneformat.ObjectDoc
}

func collectFlatSceneObjects(doc *sceneformat.SceneDoc) (map[string]flatSceneObj, []string) {
	byID := make(map[string]flatSceneObj)
	var order []string
	if doc == nil {
		return byID, order
	}
	for i := range doc.Layers {
		layer := &doc.Layers[i]
		var walk func(objs []sceneformat.ObjectDoc, parent string)
		walk = func(objs []sceneformat.ObjectDoc, parent string) {
			for j := range objs {
				o := &objs[j]
				if o.ID != "" {
					if _, exists := byID[o.ID]; !exists {
						order = append(order, o.ID)
					}
					byID[o.ID] = flatSceneObj{layer: layer.Name, parent: parent, obj: o}
				}
				walk(o.Children, o.ID)
			}
		}
		walk(layer.Objects, "")
	}
	return byID, order
}

func diffSceneDocs(comp *SceneComparison) {
	headObjs, headOrder := collectFlatSceneObjects(comp.HeadDoc)
	nowObjs, nowOrder := collectFlatSceneObjects(comp.NowDoc)

	for _, id := range nowOrder {
		n := nowObjs[id]
		h, existed := headObjs[id]
		display := nonEmpty(n.obj.Name, n.obj.ID)
		if !existed {
			comp.Added = append(comp.Added, SceneObjectChange{
				ID:      id,
				Name:    display,
				Layer:   n.layer,
				Kind:    SceneObjectAdded,
				Summary: "added",
				NowObj:  n.obj,
			})
			continue
		}
		if summary, changed := summarizeObjectDiff(h, n); changed {
			comp.Changed = append(comp.Changed, SceneObjectChange{
				ID:      id,
				Name:    display,
				Layer:   n.layer,
				Kind:    SceneObjectChanged,
				Summary: summary,
				HeadObj: h.obj,
				NowObj:  n.obj,
			})
		}
	}

	for _, id := range headOrder {
		if _, stillThere := nowObjs[id]; !stillThere {
			h := headObjs[id]
			comp.Removed = append(comp.Removed, SceneObjectChange{
				ID:      id,
				Name:    nonEmpty(h.obj.Name, h.obj.ID),
				Layer:   h.layer,
				Kind:    SceneObjectRemoved,
				Summary: "removed",
				HeadObj: h.obj,
			})
		}
	}
}

func summarizeObjectDiff(before, after flatSceneObj) (string, bool) {
	a, b := before.obj, after.obj
	if a == nil || b == nil {
		return "changed", a != b
	}
	var parts []string
	if before.layer != after.layer {
		parts = append(parts, fmt.Sprintf("layer %s → %s", before.layer, after.layer))
	}
	if before.parent != after.parent {
		parts = append(parts, "reparented")
	}
	if a.Name != b.Name {
		parts = append(parts, fmt.Sprintf("name %q → %q", a.Name, b.Name))
	}
	if a.X != b.X && a.Y != b.Y {
		parts = append(parts, fmt.Sprintf("moved (%g,%g) → (%g,%g)", a.X, a.Y, b.X, b.Y))
	} else if a.X != b.X {
		parts = append(parts, fmt.Sprintf("x %g → %g", a.X, b.X))
	} else if a.Y != b.Y {
		parts = append(parts, fmt.Sprintf("y %g → %g", a.Y, b.Y))
	}
	if a.Z != b.Z {
		parts = append(parts, fmt.Sprintf("z %d → %d", a.Z, b.Z))
	}
	if a.PosZ != b.PosZ {
		parts = append(parts, fmt.Sprintf("posZ %g → %g", a.PosZ, b.PosZ))
	}
	if a.Rotation != b.Rotation {
		parts = append(parts, fmt.Sprintf("rot %g → %g", a.Rotation, b.Rotation))
	}
	if a.Prefab != b.Prefab {
		parts = append(parts, "prefab changed")
	}
	if a.Material != b.Material {
		parts = append(parts, "material changed")
	}
	if a.Model != b.Model {
		parts = append(parts, "model changed")
	}
	if !reflect.DeepEqual(a.Sprite, b.Sprite) {
		parts = append(parts, "sprite changed")
	}
	if !reflect.DeepEqual(a.Components, b.Components) {
		parts = append(parts, "components changed")
	}
	if !reflect.DeepEqual(a.Properties, b.Properties) {
		parts = append(parts, "properties changed")
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", "), true
	}
	// Fallback canonical YAML comparison (excluding Children, which are compared individually by ID).
	ac := *a
	bc := *b
	ac.Children = nil
	bc.Children = nil
	ab, errA := yaml.Marshal(ac)
	bb, errB := yaml.Marshal(bc)
	if errA == nil && errB == nil && !bytes.Equal(ab, bb) {
		return "changed", true
	}
	return "", false
}

func formatSceneChangeDetail(sc *SceneComparison, status ChangesFileStatus, plus, minus int) string {
	if sc == nil {
		return formatTextChangeDetail(status, plus, minus, nil)
	}
	var parts []string
	if n := len(sc.Added); n > 0 {
		parts = append(parts, fmt.Sprintf("%d added", n))
	}
	if n := len(sc.Changed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d changed", n))
	}
	if n := len(sc.Removed); n > 0 {
		parts = append(parts, fmt.Sprintf("%d removed", n))
	}
	if len(parts) > 0 {
		suffix := "objects"
		if len(sc.Added)+len(sc.Changed)+len(sc.Removed) == 1 {
			suffix = "object"
		}
		return strings.Join(parts, " · ") + " " + suffix
	}
	if status == ChangesStatusAdded || status == ChangesStatusUntracked {
		return "new scene"
	}
	if status == ChangesStatusDeleted {
		return "deleted scene"
	}
	return formatTextChangeDetail(status, plus, minus, nil)
}

func formatTextChangeDetail(status ChangesFileStatus, plus, minus int, diff []DiffLine) string {
	switch status {
	case ChangesStatusDeleted:
		if minus > 0 {
			return fmt.Sprintf("deleted · -%d lines", minus)
		}
		return "deleted"
	case ChangesStatusUntracked, ChangesStatusAdded:
		if plus > 0 {
			return fmt.Sprintf("new · +%d lines", plus)
		}
		return "new file"
	}
	if plus > 0 && minus > 0 {
		return fmt.Sprintf("+%d -%d lines", plus, minus)
	}
	if plus > 0 {
		return fmt.Sprintf("+%d lines", plus)
	}
	if minus > 0 {
		return fmt.Sprintf("-%d lines", minus)
	}
	if len(diff) == 0 {
		return string(status)
	}
	return "modified"
}

func formatAudioChangeDetail(info *AudioChangeInfo, status ChangesFileStatus) string {
	if info == nil {
		return string(status)
	}
	var parts []string
	if status == ChangesStatusAdded || status == ChangesStatusUntracked {
		parts = append(parts, "new")
	} else if status == ChangesStatusDeleted {
		parts = append(parts, "deleted")
	}
	if info.Duration > 0 {
		parts = append(parts, fmt.Sprintf("%.1f s", info.Duration.Seconds()))
	}
	if info.SizeBytes > 0 {
		parts = append(parts, formatHumanBytes(info.SizeBytes))
	}
	if len(parts) == 0 {
		return string(status)
	}
	return strings.Join(parts, " · ")
}

func formatBinaryChangeDetail(size int64, status ChangesFileStatus) string {
	prefix := "binary"
	if status == ChangesStatusDeleted {
		return "deleted · binary"
	}
	if size > 0 {
		return prefix + " · " + formatHumanBytes(size)
	}
	return prefix
}

func formatAssetChangeDetail(size int64, status ChangesFileStatus) string {
	prefix := string(status)
	if status == ChangesStatusUntracked || status == ChangesStatusAdded {
		prefix = "new"
	}
	if size > 0 {
		return prefix + " · " + formatHumanBytes(size)
	}
	return prefix
}

func formatHumanBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%d KB", max(1, (n+512)/1024))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func (e *Editor) inspectAudioChange(absPath string, pathErr error, usedBy []string) *AudioChangeInfo {
	info := &AudioChangeInfo{
		Format: strings.ToUpper(strings.TrimPrefix(path.Ext(absPath), ".")),
		UsedBy: slices.Clone(usedBy),
	}
	if pathErr != nil {
		return info
	}
	if data, truncated, size, err := readFileBounded(absPath, maxChangesAudioInspectBytes); err == nil {
		info.SizeBytes = size
		ext := strings.ToLower(path.Ext(absPath))
		switch ext {
		case ".wav":
			info.Format = "WAV"
			if !truncated {
				if samples, sr, ch, derr := sound.DecodeWAV(data); derr == nil && sr > 0 && ch > 0 {
					info.SampleRate = sr
					info.Channels = ch
					frames := len(samples) / ch
					info.Duration = time.Duration(float64(frames) / float64(sr) * float64(time.Second))
					info.Waveform = computeWaveformBars(samples, 60)
				}
			}
		case ".ogg":
			info.Format = "Vorbis"
			sr, ch, dur := probeOggVorbis(data)
			info.SampleRate = sr
			info.Channels = ch
			if !truncated {
				info.Duration = dur
			}
		case ".mp3":
			info.Format = "MP3"
		case ".flac":
			info.Format = "FLAC"
		}
		if len(info.Waveform) == 0 {
			info.Waveform = syntheticWaveformBars(data, 60)
		}
	}
	return info
}

func computeWaveformBars(samples []float64, bars int) []float32 {
	if bars <= 0 || len(samples) == 0 {
		return nil
	}
	out := make([]float32, bars)
	chunk := max(1, len(samples)/bars)
	var peak float64
	for i := range bars {
		start := i * chunk
		if start >= len(samples) {
			break
		}
		end := min(len(samples), start+chunk)
		var sumSq float64
		for j := start; j < end; j++ {
			v := samples[j]
			sumSq += v * v
		}
		rms := math.Sqrt(sumSq / float64(end-start))
		if rms > peak {
			peak = rms
		}
		out[i] = float32(rms)
	}
	if peak > 0 {
		for i := range out {
			v := float32(float64(out[i]) / peak)
			if v < 0.08 {
				v = 0.08
			}
			out[i] = v
		}
	}
	return out
}

func syntheticWaveformBars(data []byte, bars int) []float32 {
	out := make([]float32, bars)
	for i := range bars {
		env := 1.0
		if i < 6 {
			env = float64(i+1) / 6.0
		} else {
			env = math.Max(0.10, 1.0-float64(i-6)/float64(bars))
		}
		mod := 0.55 + 0.45*math.Abs(math.Sin(float64(i)*1.7))
		if len(data) > 0 {
			b := data[(i*len(data))/bars]
			mod = 0.35 + 0.65*(float64(b)/255.0)
		}
		v := float32(env * mod)
		if v < 0.08 {
			v = 0.08
		}
		out[i] = v
	}
	return out
}

func probeOggVorbis(data []byte) (sampleRate, channels int, duration time.Duration) {
	// Parse Vorbis identification header: "\x01vorbis" + version(4) + channels(1) + sampleRate(4).
	idx := bytes.Index(data, []byte("\x01vorbis"))
	if idx >= 0 && idx+16 <= len(data) {
		channels = int(data[idx+11])
		sampleRate = int(binary.LittleEndian.Uint32(data[idx+12 : idx+16]))
	}
	// Scan backwards for the last "OggS" page header to extract granule position (offset +6, 8-byte LE int64).
	if sampleRate > 0 {
		last := bytes.LastIndex(data, []byte("OggS"))
		if last >= 0 && last+14 <= len(data) {
			granule := int64(binary.LittleEndian.Uint64(data[last+6 : last+14]))
			if granule > 0 {
				duration = time.Duration(float64(granule) / float64(sampleRate) * float64(time.Second))
			}
		}
	}
	return sampleRate, channels, duration
}

// scanAudioIncomingReferences scans the project once for all changed audio target paths,
// enforcing symlink containment and bounded scene reads.
func (e *Editor) scanAudioIncomingReferences(projectDir string, targets map[string]bool) map[string][]string {
	if len(targets) == 0 {
		return nil
	}
	out := make(map[string][]string, len(targets))
	seen := make(map[string]map[string]bool, len(targets))
	add := func(target, label string) {
		if target == "" || label == "" || !targets[target] {
			return
		}
		if seen[target] == nil {
			seen[target] = make(map[string]bool)
		}
		if !seen[target][label] {
			seen[target][label] = true
			out[target] = append(out[target], label)
		}
	}

	// 1. Query the project's refs index once.
	if e.Project != nil && e.ProjectPath != "" {
		for target := range targets {
			if found, err := e.FindReferences(target); err == nil {
				for _, r := range found {
					add(target, formatIncomingRef(r))
				}
			}
		}
	} else if projectDir != "" {
		if scan, err := refs.Scan(os.DirFS(projectDir)); err == nil {
			for _, r := range scan.Refs {
				if targets[r.Target] {
					add(r.Target, formatIncomingRef(r))
				}
			}
		}
	}

	// 2. Walk scene YAML files once to pick up custom gameplay audio fields in properties/components.
	if projectDir != "" {
		absProj, err := filepath.Abs(projectDir)
		if err != nil {
			absProj = filepath.Clean(projectDir)
		}
		realProj := absProj
		if eval, err := filepath.EvalSymlinks(absProj); err == nil {
			realProj = eval
		}
		targetList := make([]string, 0, len(targets))
		for t := range targets {
			targetList = append(targetList, t)
		}
		_ = fs.WalkDir(os.DirFS(realProj), ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if p != "." && skippedProjectDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			lower := strings.ToLower(p)
			if !strings.HasSuffix(lower, ".scene.yaml") && !strings.HasSuffix(lower, ".scene.yml") {
				return nil
			}
			_, fullPath, rerr := resolvePathWithinRoot(realProj, filepath.Join(realProj, filepath.FromSlash(p)))
			if rerr != nil {
				return nil
			}
			raw, truncated, _, ferr := readFileBounded(fullPath, maxChangesSceneBytes)
			if ferr != nil || truncated {
				return nil
			}
			matched := false
			for _, t := range targetList {
				if bytes.Contains(raw, []byte(t)) {
					matched = true
					break
				}
			}
			if !matched {
				return nil
			}
			doc, perr := sceneformat.ParseWith(raw, sceneformat.ParseOptions{File: p, Components: e.components})
			if perr != nil {
				return nil
			}
			sceneLabel := nonEmpty(doc.Name, trimSceneFilename(p))
			for i := range doc.Layers {
				sceneformat.WalkObjects(doc.Layers[i].Objects, func(o, _ *sceneformat.ObjectDoc) bool {
					objLabel := nonEmpty(o.Name, o.ID)
					for k, v := range o.Properties {
						if s, ok := v.(string); ok {
							sl := filepath.ToSlash(s)
							if targets[sl] {
								add(sl, fmt.Sprintf("Used by %s in %s as %s", objLabel, sceneLabel, k))
							}
						}
					}
					for cname, cv := range o.Components {
						if cm, ok := cv.(map[string]any); ok {
							for k, v := range cm {
								if s, ok := v.(string); ok {
									sl := filepath.ToSlash(s)
									if targets[sl] {
										add(sl, fmt.Sprintf("Used by %s in %s as %s.%s", objLabel, sceneLabel, cname, k))
									}
								}
							}
						}
					}
					return true
				})
			}
			return nil
		})
	}

	for target := range out {
		slices.Sort(out[target])
	}
	return out
}

func formatIncomingRef(r refs.Ref) string {
	if r.Field != "" {
		return fmt.Sprintf("Used by %s (%s)", r.File, r.Field)
	}
	return fmt.Sprintf("Used by %s", r.File)
}

// SelectChangedFile selects the changed file at repo-relative path for preview in the Review layout.
func (e *Editor) SelectChangedFile(filePath string) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	for _, f := range sess.Files {
		if f.Path == filePath {
			if sess.SelectedPath != filePath {
				e.stopChangedAudioPlayback()
			}
			sess.SelectedPath = filePath
			e.changes.rev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: changed file %q not found", filePath)
}

// SelectedChangedFile returns a pointer to the currently selected ChangedFile in the active session, or nil.
func (s *ChangesSession) SelectedChangedFile() *ChangedFile {
	if s == nil || len(s.Files) == 0 {
		return nil
	}
	for i := range s.Files {
		if s.Files[i].Path == s.SelectedPath {
			return &s.Files[i]
		}
	}
	return &s.Files[0]
}

// IncludedSummary returns the included file count, total file count, included added lines, and included deleted lines.
func (s *ChangesSession) IncludedSummary() (included, total, plus, minus int) {
	if s == nil {
		return 0, 0, 0, 0
	}
	total = len(s.Files)
	for _, f := range s.Files {
		if f.Included {
			included++
			plus += f.PlusLines
			minus += f.MinusLines
		}
	}
	return included, total, plus, minus
}

// SetChangeIncluded includes or excludes the changed file at path from the next commit.
func (e *Editor) SetChangeIncluded(filePath string, included bool) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	for i := range sess.Files {
		if sess.Files[i].Path == filePath {
			if sess.Files[i].Included == included {
				return nil
			}
			sess.Files[i].Included = included
			if !sess.draftEdited && !sess.Committed {
				e.applyCommitDraft(sess)
			}
			e.changes.rev++
			e.changes.draftRev++
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: changed file %q not found", filePath)
}

// ToggleChangeIncluded toggles whether the changed file at path is included in the next commit.
func (e *Editor) ToggleChangeIncluded(filePath string) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	for i := range sess.Files {
		if sess.Files[i].Path == filePath {
			return e.SetChangeIncluded(filePath, !sess.Files[i].Included)
		}
	}
	return fmt.Errorf("editor: changed file %q not found", filePath)
}

// SetChangesSceneView switches the scene diff preview mode between side-by-side, HEAD, and Now.
func (e *Editor) SetChangesSceneView(mode ChangesSceneViewMode) {
	if !e.ChangesOpen() {
		return
	}
	switch mode {
	case ChangesSceneViewSideBySide, ChangesSceneViewHead, ChangesSceneViewNow:
	default:
		return
	}
	if e.changes.session.SceneView == mode {
		return
	}
	e.changes.session.SceneView = mode
	e.changes.rev++
	e.notify()
}

// SetCommitSummary updates the editable commit summary field in the Changes panel without rebuilding the input widgets.
func (e *Editor) SetCommitSummary(summary string) {
	if !e.ChangesOpen() {
		return
	}
	sess := e.changes.session
	sess.DraftSummary = summary
	sess.draftEdited = true
	hadErr := sess.LastErr != ""
	sess.LastErr = ""
	e.changes.draftRev++
	if hadErr {
		e.changes.rev++
	}
	e.notify()
}

// SetCommitDescription updates the editable commit description field in the Changes panel without rebuilding the input widgets.
func (e *Editor) SetCommitDescription(description string) {
	if !e.ChangesOpen() {
		return
	}
	sess := e.changes.session
	sess.DraftDescription = description
	sess.draftEdited = true
	hadErr := sess.LastErr != ""
	sess.LastErr = ""
	e.changes.draftRev++
	if hadErr {
		e.changes.rev++
	}
	e.notify()
}

// SetChangesContext records review, playtest, and agent-request provenance metadata used when drafting commit messages and PR descriptions.
func (e *Editor) SetChangesContext(reviews int, playtestSummary string, agentRequests int) {
	if e == nil || e.changes.session == nil {
		return
	}
	sess := e.changes.session
	sess.ReviewCount = max(0, reviews)
	sess.PlaytestSummary = strings.TrimSpace(playtestSummary)
	sess.AgentRequestCount = max(0, agentRequests)
	if !sess.draftEdited {
		e.applyCommitDraft(sess)
	}
	e.changes.rev++
	e.changes.draftRev++
	e.notify()
}

// RedraftCommitMessage regenerates the commit summary and description from the currently included files using Editor.CommitMessageDrafter or DefaultCommitDraft.
func (e *Editor) RedraftCommitMessage() error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	sess.draftEdited = false
	sess.Committed = false
	sess.LastErr = ""
	if err := e.applyCommitDraft(sess); err != nil {
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	e.changes.rev++
	e.changes.draftRev++
	e.notify()
	return nil
}

func (e *Editor) applyCommitDraft(sess *ChangesSession) error {
	var included []ChangedFile
	for _, f := range sess.Files {
		if f.Included {
			included = append(included, f)
		}
	}
	req := CommitDraftRequest{
		RepoDir:         sess.RepoDir,
		Branch:          sess.Branch,
		HeadSHA:         sess.HeadSHA,
		Files:           included,
		Reviews:         sess.ReviewCount,
		PlaytestSummary: sess.PlaytestSummary,
		AgentRequests:   sess.AgentRequestCount,
		Checks:          sess.Checks,
	}
	var draft CommitDraft
	if e.CommitMessageDrafter != nil {
		d, err := e.CommitMessageDrafter(context.Background(), req)
		if err != nil {
			return err
		}
		draft = d
	} else {
		draft = DefaultCommitDraft(req)
	}
	sess.DraftSummary = draft.Summary
	sess.DraftDescription = draft.Description
	sess.DraftProvenance = draft.Provenance
	sess.DraftAttribution = draft.Attribution
	return nil
}

// DefaultCommitDraft builds a deterministic, human-readable commit summary and description from the included files.
func DefaultCommitDraft(req CommitDraftRequest) CommitDraft {
	n := len(req.Files)
	provenance := fmt.Sprintf("Drafted from %d changed files", n)
	if n == 1 {
		provenance = "Drafted from 1 changed file"
	}
	switch {
	case req.Reviews > 0 && req.PlaytestSummary != "":
		unit := "reviews"
		if req.Reviews == 1 {
			unit = "review"
		}
		provenance = fmt.Sprintf("Drafted from %d %s and a playtest", req.Reviews, unit)
	case req.Reviews > 0:
		unit := "reviews"
		if req.Reviews == 1 {
			unit = "review"
		}
		provenance = fmt.Sprintf("Drafted from %d %s", req.Reviews, unit)
	case req.PlaytestSummary != "":
		provenance = "Drafted from a playtest"
	}

	attribution := ""
	if req.AgentRequests > 0 {
		unit := "agent requests"
		if req.AgentRequests == 1 {
			unit = "agent request"
		}
		attribution = fmt.Sprintf("Made from %d %s, reviewed and kept", req.AgentRequests, unit)
	}

	if n == 0 {
		return CommitDraft{
			Provenance:  "Select files to include in the commit",
			Attribution: attribution,
		}
	}

	// Build a concise subject line.
	var summary string
	var primaryScene *ChangedFile
	for i := range req.Files {
		if req.Files[i].Category == ChangesCategoryScenes {
			primaryScene = &req.Files[i]
			break
		}
	}
	if primaryScene != nil {
		sceneName := trimSceneFilename(primaryScene.DisplayName)
		if primaryScene.Scene != nil && primaryScene.Scene.NowDoc != nil && primaryScene.Scene.NowDoc.Name != "" {
			sceneName = primaryScene.Scene.NowDoc.Name
		}
		if n == 1 {
			summary = fmt.Sprintf("Update %s", sceneName)
		} else {
			summary = fmt.Sprintf("Update %s and %d related files", sceneName, n-1)
		}
	} else if n == 1 {
		f := req.Files[0]
		verb := "Update"
		if f.Status == ChangesStatusAdded || f.Status == ChangesStatusUntracked {
			verb = "Add"
		} else if f.Status == ChangesStatusDeleted {
			verb = "Remove"
		}
		summary = fmt.Sprintf("%s %s", verb, f.DisplayName)
	} else {
		summary = fmt.Sprintf("Update %s and %d other files", req.Files[0].DisplayName, n-1)
	}

	var bullets []string
	for _, f := range req.Files {
		if f.Category == ChangesCategoryScenes && f.Scene != nil {
			var sceneParts []string
			if len(f.Scene.Added) > 0 {
				sceneParts = append(sceneParts, "added "+joinObjectNames(f.Scene.Added, 4))
			}
			if len(f.Scene.Changed) > 0 {
				sceneParts = append(sceneParts, "changed "+joinObjectNames(f.Scene.Changed, 4))
			}
			if len(f.Scene.Removed) > 0 {
				sceneParts = append(sceneParts, "removed "+joinObjectNames(f.Scene.Removed, 4))
			}
			if len(sceneParts) > 0 {
				bullets = append(bullets, fmt.Sprintf("- %s: %s", f.DisplayName, strings.Join(sceneParts, "; ")))
				continue
			}
		}
		if f.Detail != "" {
			bullets = append(bullets, fmt.Sprintf("- %s (%s)", f.DisplayName, f.Detail))
		} else {
			bullets = append(bullets, fmt.Sprintf("- %s (%s)", f.DisplayName, f.Status))
		}
	}

	var bodyParts []string
	bodyParts = append(bodyParts, strings.Join(bullets, "\n"))

	var footer []string
	if req.PlaytestSummary != "" {
		footer = append(footer, "Playtest: "+req.PlaytestSummary)
	}
	if req.Checks.Ran {
		checkWord := "failed"
		if req.Checks.CheckPassed {
			checkWord = "passed"
		}
		testWord := "failed"
		if req.Checks.TestPassed {
			if req.Checks.TestCount > 0 {
				testWord = fmt.Sprintf("%d passed", req.Checks.TestCount)
			} else {
				testWord = "passed"
			}
		}
		footer = append(footer, fmt.Sprintf("Checks: nsgo check %s, nsgo test %s", checkWord, testWord))
	}
	if attribution != "" {
		footer = append(footer, attribution)
	}
	if len(footer) > 0 {
		bodyParts = append(bodyParts, strings.Join(footer, "\n"))
	}

	return CommitDraft{
		Summary:     summary,
		Description: strings.Join(bodyParts, "\n\n"),
		Provenance:  provenance,
		Attribution: attribution,
	}
}

func joinObjectNames(items []SceneObjectChange, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit <= 0 || len(items) <= limit {
		names := make([]string, len(items))
		for i, it := range items {
			names[i] = it.Name
		}
		return strings.Join(names, ", ")
	}
	names := make([]string, limit)
	for i := range limit {
		names[i] = items[i].Name
	}
	return fmt.Sprintf("%s +%d more", strings.Join(names, ", "), len(items)-limit)
}

// RunChangesChecks runs nsgo check and nsgo test (or Editor.ChangesCheckRunner when set) and records the result and timestamp.
func (e *Editor) RunChangesChecks() error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	dir := nonEmpty(sess.ProjectDir, sess.RepoDir)
	ctx, cancel := context.WithTimeout(context.Background(), defaultChangesChecksTimeout)
	defer cancel()

	res, err := executeChangesChecks(ctx, dir, e.ChangesCheckRunner, e.Now)
	return e.applyChangesChecksResult(res, err)
}

// RunChangesChecksAsync runs nsgo check and nsgo test off the UI thread and posts the result back to the editor scope.
func (e *Editor) RunChangesChecksAsync(cx *core.Scope) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	scope := e.scope
	if scope == nil {
		scope = cx
	}
	if scope == nil {
		return e.RunChangesChecks()
	}
	sess := e.changes.session
	dir := nonEmpty(sess.ProjectDir, sess.RepoDir)
	runner := e.ChangesCheckRunner
	nowFn := e.Now
	ctx, gen, done, cancel, err := e.beginChangesOp("Running checks...", defaultChangesChecksTimeout)
	if err != nil {
		return err
	}
	go func() {
		defer close(done)
		res, runErr := executeChangesChecks(ctx, dir, runner, nowFn)
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		scope.Post(func() {
			if !e.finishChangesOp(gen, cancel) {
				return
			}
			e.report(e.applyChangesChecksResult(res, runErr))
		})
	}()
	return nil
}

func executeChangesChecks(ctx context.Context, dir string, runner func(string) (ChangesCheckStatus, error), nowFn func() time.Time) (ChangesCheckStatus, error) {
	now := func() time.Time {
		if nowFn != nil {
			return nowFn()
		}
		return time.Now()
	}
	if runner != nil {
		res, err := runner(dir)
		if res.RanAt.IsZero() {
			res.RanAt = now()
		}
		res.Ran = true
		return res, err
	}
	return runDefaultChangesChecksCtx(ctx, dir, now()), nil
}

func (e *Editor) applyChangesChecksResult(res ChangesCheckStatus, err error) error {
	if !e.ChangesOpen() {
		return nil
	}
	sess := e.changes.session
	if err != nil {
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	sess.Checks = res
	sess.LastErr = ""
	if !sess.draftEdited && !sess.Committed {
		_ = e.applyCommitDraft(sess)
	}
	e.changes.rev++
	e.changes.draftRev++
	e.notify()
	return nil
}

func runDefaultChangesChecksCtx(ctx context.Context, dir string, now time.Time) ChangesCheckStatus {
	status := ChangesCheckStatus{
		Ran:   true,
		RanAt: now,
	}
	env := gotoolchain.Env(gitBaseEnv(nil))
	checkCmd := gotoolchain.CommandContext(ctx, "tool", "nsgo", "check", "./...")
	checkCmd.Dir = dir
	checkCmd.Env = env
	checkCmd.Stdin = nil
	checkOut, checkErr := checkCmd.CombinedOutput()
	status.CheckOutput = strings.TrimSpace(string(checkOut))
	if checkErr == nil {
		status.CheckPassed = true
		status.CheckSummary = "nsgo check"
	} else {
		status.CheckPassed = false
		status.CheckSummary = "nsgo check failed"
	}

	testCmd := gotoolchain.CommandContext(ctx, "tool", "nsgo", "test", "./...")
	testCmd.Dir = dir
	testCmd.Env = env
	testCmd.Stdin = nil
	testOut, testErr := testCmd.CombinedOutput()
	status.TestOutput = strings.TrimSpace(string(testOut))
	passed := countGoTestPasses(status.TestOutput)
	status.TestCount = passed
	if testErr == nil {
		status.TestPassed = true
		if passed > 0 {
			status.TestSummary = fmt.Sprintf("nsgo test · %d passed", passed)
		} else {
			status.TestSummary = "nsgo test · passed"
		}
	} else {
		status.TestPassed = false
		status.TestSummary = "nsgo test · failed"
	}
	return status
}

// countGoTestPasses parses test pass counts from either `go tool nsgo test` output
// (`✓ TestName`, `N passed, 0 failed`, `PASS  1 package, N passed`) or `go test -v` (`--- PASS:`).
func countGoTestPasses(out string) int {
	summaryPassed := 0
	goVPassed := 0
	checkMarkPassed := 0
	for _, rawLine := range strings.Split(out, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "--- PASS:") {
			goVPassed++
			continue
		}
		if strings.HasPrefix(line, "✓ ") || strings.HasPrefix(line, "PASS: ") {
			checkMarkPassed++
			continue
		}
		if n, ok := parseNSGoPassedSummary(line); ok {
			summaryPassed = n
		}
	}
	if summaryPassed > 0 {
		return summaryPassed
	}
	if goVPassed > 0 {
		return goVPassed
	}
	return checkMarkPassed
}

func parseNSGoPassedSummary(line string) (int, bool) {
	// Examples:
	//   "570 passed, 0 failed, 0 skipped"
	//   "PASS  1 package, 61 passed (0 skipped)  55.87s"
	fields := strings.Fields(line)
	for i := 1; i < len(fields); i++ {
		tok := strings.TrimRight(fields[i], ",;:")
		if tok != "passed" {
			continue
		}
		prev := strings.TrimRight(fields[i-1], ",")
		if n, err := strconv.Atoi(prev); err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

func validateGitBranchName(branch string) error {
	if branch == "" {
		return errors.New("editor: empty branch name")
	}
	if branch == "HEAD" {
		return errors.New("editor: cannot use detached HEAD as a branch name")
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("editor: invalid branch name %q (must not start with '-')", branch)
	}
	if strings.ContainsAny(branch, " \t\r\n\x00~^:?*[\\") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.HasSuffix(branch, ".lock") || strings.HasPrefix(branch, "/") || strings.HasSuffix(branch, "/") {
		return fmt.Errorf("editor: invalid branch name %q", branch)
	}
	return nil
}

// GitSwitchBranch checks out branch in the active Changes repository and reloads open scenes and project state.
func (e *Editor) GitSwitchBranch(branch string) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return ErrMergeConflictInProgress
	}
	branch = strings.TrimSpace(branch)
	if err := validateGitBranchName(branch); err != nil {
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	if branch == sess.Branch {
		e.changes.rev++
		e.notify()
		return nil
	}
	if e.HasUnsavedChanges() {
		sess.LastErr = ErrUnsavedChanges.Error()
		e.changes.rev++
		e.notify()
		return ErrUnsavedChanges
	}
	// Verify the target is a genuine local branch ref before switching to prevent flag or pathspec injection.
	if _, err := e.runGit(sess.RepoDir, "show-ref", "--verify", "--quiet", "--", "refs/heads/"+branch); err != nil {
		err = fmt.Errorf("editor: local branch %q does not exist", branch)
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	if out, err := e.runGit(sess.RepoDir, "switch", "--", branch); err != nil {
		msg := nonEmpty(out, err.Error())
		sess.LastErr = "Switch branch failed: " + msg
		e.changes.rev++
		e.notify()
		return fmt.Errorf("editor: git switch %s: %w (%s)", branch, err, out)
	}
	e.reloadOpenStateAfterGitWorktreeChange()
	sess.LastNotice = "Switched to " + branch
	e.Status = "Switched branch to " + branch
	e.refreshDetectedMergeConflicts()
	return e.refreshChangesSession(false)
}

// GitPull pulls from the upstream branch using fast-forward only (never creating an unintended merge commit),
// reloads open scenes/project state from disk, and refreshes Changes.
func (e *Editor) GitPull() error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return ErrMergeConflictInProgress
	}
	if e.HasUnsavedChanges() {
		sess.LastErr = ErrUnsavedChanges.Error()
		e.changes.rev++
		e.notify()
		return ErrUnsavedChanges
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitNetworkTimeout)
	defer cancel()
	out, err := e.runGitCtx(ctx, sess.RepoDir, "pull", "--ff-only")
	return e.applyGitPullResult(out, err)
}

// GitPullAsync runs `git pull --ff-only` off the UI thread and posts the result back to the editor scope.
func (e *Editor) GitPullAsync(cx *core.Scope) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	scope := e.scope
	if scope == nil {
		scope = cx
	}
	if scope == nil {
		return e.GitPull()
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return ErrMergeConflictInProgress
	}
	if e.HasUnsavedChanges() {
		sess.LastErr = ErrUnsavedChanges.Error()
		e.changes.rev++
		e.notify()
		return ErrUnsavedChanges
	}
	repoDir := sess.RepoDir
	runner := e.GitRunner
	ctx, gen, done, cancel, err := e.beginChangesOp("Pulling...", defaultGitNetworkTimeout)
	if err != nil {
		return err
	}
	go func() {
		defer close(done)
		var out string
		var runErr error
		if runner != nil {
			out, runErr = runner(repoDir, "pull", "--ff-only")
		} else {
			out, runErr = runGitExecCtx(ctx, repoDir, nil, nil, "pull", "--ff-only")
		}
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		scope.Post(func() {
			if !e.finishChangesOp(gen, cancel) {
				return
			}
			e.report(e.applyGitPullResult(out, runErr))
		})
	}()
	return nil
}

func (e *Editor) applyGitPullResult(out string, err error) error {
	if !e.ChangesOpen() {
		return nil
	}
	sess := e.changes.session
	if err != nil {
		msg := nonEmpty(out, err.Error())
		sess.LastErr = "Pull failed: " + msg
		e.refreshDetectedMergeConflicts()
		e.changes.rev++
		e.notify()
		return fmt.Errorf("editor: git pull: %w (%s)", err, out)
	}
	e.reloadOpenStateAfterGitWorktreeChange()
	sess.LastNotice = nonEmpty(firstLine(out), "Pulled latest changes")
	e.Status = sess.LastNotice
	e.refreshDetectedMergeConflicts()
	return e.refreshChangesSession(false)
}

// reloadOpenStateAfterGitWorktreeChange reloads open project, open scene tabs (#1162), and animation clips
// from disk after a branch switch or pull, and synchronizes #1171 disk watch snapshots.
func (e *Editor) reloadOpenStateAfterGitWorktreeChange() {
	if e == nil {
		return
	}
	// Reload game.yaml when present.
	if e.Project != nil && e.ProjectPath != "" {
		if data, err := os.ReadFile(e.ProjectPath); err == nil {
			if f, perr := project.Parse(data); perr == nil {
				if saved, eerr := project.Encode(f); eerr == nil {
					e.Project = f
					e.projectSaved = saved
					e.recordProjectDiskSnapshot(e.ProjectPath, data)
				}
			}
		}
	}

	// Reload every open scene tab from disk so stale pre-switch documents cannot overwrite the new branch.
	e.syncActiveTab()
	for i, t := range e.tabs {
		if t.filePath == "" {
			continue
		}
		data, err := os.ReadFile(t.filePath)
		if err != nil {
			continue
		}
		doc, perr := sceneformat.ParseWith(data, sceneformat.ParseOptions{
			File:          t.filePath,
			Components:    e.components,
			PhysicsLayers: e.PhysicsLayers(),
		})
		if perr != nil {
			continue
		}
		saved, eerr := sceneformat.Encode(doc)
		if eerr != nil {
			continue
		}
		t.doc = doc
		t.built, t.buildErr = sceneformat.Build(doc, e.Resolver)
		t.sceneSaved = saved
		t.sceneDisk = makeDiskSnapshot(t.filePath, data)
		if i == e.activeTab {
			e.Doc = t.doc
			e.Built = t.built
			e.BuildErr = t.buildErr
			e.sceneSaved = saved
			e.recordSceneDiskSnapshot(t.filePath, data)
		}
	}

	// Reload open animation clip when present.
	if e.clip != nil && e.clipMeta.path != "" {
		if data, err := os.ReadFile(e.clipMeta.path); err == nil {
			if clip, perr := resource.ParseAs[*resource.Animation](data); perr == nil {
				if saved, eerr := resource.Encode(clip); eerr == nil {
					e.clip = clip
					e.clipSaved = saved
					e.recordClipDiskSnapshot(e.clipMeta.path, data)
				}
			}
		}
	}

	e.disk.conflicts = nil
	e.disk.saveConflict = nil
	e.disk.saveConflictTabID = 0
	e.snapshotGitHead()
	e.snapshotProjectTree()
	e.syncDiskWatchTargets()
	e.filesRev++
	e.scenesRev++
}

// CommitSelectedChanges stages ONLY the checked/included files and commits ONLY those paths,
// leaving every unchecked file untouched and uncommitted, then records a CommitMilestone in History.
func (e *Editor) CommitSelectedChanges() (CommitMilestone, error) {
	if !e.ChangesOpen() {
		return CommitMilestone{}, ErrNoChangesSession
	}
	if e.Playing() {
		return CommitMilestone{}, ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return CommitMilestone{}, ErrMergeConflictInProgress
	}
	summary := strings.TrimSpace(sess.DraftSummary)
	if summary == "" {
		err := errors.New("editor: commit summary cannot be empty")
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return CommitMilestone{}, err
	}

	var displayIncluded []string
	var stagePaths []string
	var commitPaths []string
	seenStage := make(map[string]bool)
	seenCommit := make(map[string]bool)
	addCommitPath := func(p string) error {
		if p == "" || seenCommit[p] {
			return nil
		}
		if _, err := resolveRepoFilePath(sess.RepoDir, sess.ProjectPrefix, p); err != nil {
			return err
		}
		seenCommit[p] = true
		commitPaths = append(commitPaths, p)
		return nil
	}
	addStagePath := func(p string) {
		if p == "" || seenStage[p] {
			return
		}
		seenStage[p] = true
		stagePaths = append(stagePaths, p)
	}

	for _, f := range sess.Files {
		if !f.Included {
			continue
		}
		if err := addCommitPath(f.Path); err != nil {
			sess.LastErr = err.Error()
			e.changes.rev++
			e.notify()
			return CommitMilestone{}, err
		}
		// Only stage f.Path if it exists on disk or is tracked in the index (skip already-staged deletions).
		if f.Status != ChangesStatusDeleted {
			addStagePath(f.Path)
		} else if _, err := e.runGit(sess.RepoDir, "ls-files", "--error-unmatch", "--", f.Path); err == nil {
			addStagePath(f.Path)
		}
		// Include the old path on renames in commitPaths so git commits the rename atomically;
		// stage OldPath only if it is still present in the index (not already removed by `git mv`).
		if f.OldPath != "" {
			if err := addCommitPath(f.OldPath); err != nil {
				sess.LastErr = err.Error()
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, err
			}
			if _, err := e.runGit(sess.RepoDir, "ls-files", "--error-unmatch", "--", f.OldPath); err == nil {
				addStagePath(f.OldPath)
			}
		}
		displayIncluded = append(displayIncluded, f.Path)
	}
	if len(commitPaths) == 0 {
		err := errors.New("editor: select at least one file to commit")
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return CommitMilestone{}, err
	}

	message := summary
	if desc := strings.TrimSpace(sess.DraftDescription); desc != "" {
		message += "\n\n" + desc
	}

	repoRoot := sess.RepoDir
	hasHead := sess.HasHead
	if !hasHead {
		if _, herr := e.runGit(repoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); herr == nil {
			hasHead = true
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultGitTimeout)
	defer cancel()

	if hasHead {
		// Stage ONLY the included paths that need staging (handling modified, untracked, renamed, and deleted files cleanly).
		if len(stagePaths) > 0 {
			addArgs := append([]string{"add", "-A", "--"}, stagePaths...)
			if out, err := e.runGitCtx(ctx, repoRoot, addArgs...); err != nil {
				msg := nonEmpty(out, err.Error())
				sess.LastErr = "Stage failed: " + msg
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: git add: %w (%s)", err, out)
			}
		}

		// Commit ONLY the included paths using --only so any previously staged unchecked files stay uncommitted.
		// Pass the message via stdin (-F -) when running real git to avoid OS argv limits and flag parsing edge cases.
		var out string
		var err error
		if e.GitRunner != nil {
			commitArgs := append([]string{"commit", "--only", "-m", message, "--"}, commitPaths...)
			out, err = e.runGitCtx(ctx, repoRoot, commitArgs...)
		} else {
			commitArgs := append([]string{"commit", "--only", "-F", "-", "--"}, commitPaths...)
			out, err = e.runGitEnvCtx(ctx, repoRoot, nil, []byte(message), commitArgs...)
		}
		if err != nil {
			msg := nonEmpty(out, err.Error())
			sess.LastErr = "Commit failed: " + msg
			e.changes.rev++
			e.notify()
			return CommitMilestone{}, fmt.Errorf("editor: git commit: %w (%s)", err, out)
		}
	} else {
		// Unborn HEAD: commit through a temporary GIT_INDEX_FILE containing ONLY commitPaths so any
		// previously staged unchecked files in the real index remain completely untouched and uncommitted.
		if e.GitRunner != nil {
			addArgs := append([]string{"add", "-A", "--"}, commitPaths...)
			if out, err := e.runGitCtx(ctx, repoRoot, addArgs...); err != nil {
				msg := nonEmpty(out, err.Error())
				sess.LastErr = "Stage failed: " + msg
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: git add: %w (%s)", err, out)
			}
			if out, err := e.runGitCtx(ctx, repoRoot, "commit", "-m", message); err != nil {
				msg := nonEmpty(out, err.Error())
				sess.LastErr = "Commit failed: " + msg
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: git commit: %w (%s)", err, out)
			}
		} else {
			tmpDir, derr := os.MkdirTemp("", "nsgo-git-idx-*")
			if derr != nil {
				sess.LastErr = "Stage failed: " + derr.Error()
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: temp git index: %w", derr)
			}
			defer os.RemoveAll(tmpDir)
			tmpIdxEnv := []string{"GIT_INDEX_FILE=" + filepath.Join(tmpDir, "index")}
			addArgs := append([]string{"add", "-A", "--"}, commitPaths...)
			if out, err := e.runGitEnvCtx(ctx, repoRoot, tmpIdxEnv, nil, addArgs...); err != nil {
				msg := nonEmpty(out, err.Error())
				sess.LastErr = "Stage failed: " + msg
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: git add: %w (%s)", err, out)
			}
			if out, err := e.runGitEnvCtx(ctx, repoRoot, tmpIdxEnv, []byte(message), "commit", "-F", "-"); err != nil {
				msg := nonEmpty(out, err.Error())
				sess.LastErr = "Commit failed: " + msg
				e.changes.rev++
				e.notify()
				return CommitMilestone{}, fmt.Errorf("editor: git commit: %w (%s)", err, out)
			}
			// Synchronize ONLY the committed paths into the real index so they match the new HEAD
			// while unchecked staged files remain untouched in the real index.
			_, _ = e.runGitCtx(ctx, repoRoot, addArgs...)
		}
	}

	sha, err := e.runGit(repoRoot, "rev-parse", "--short", "HEAD")
	if err != nil {
		sha = "HEAD"
	}

	milestone := CommitMilestone{
		SHA:             sha,
		Summary:         summary,
		Description:     strings.TrimSpace(sess.DraftDescription),
		Branch:          sess.Branch,
		Files:           slices.Clone(displayIncluded),
		Timestamp:       e.now(),
		Pushed:          false,
		AgentRequests:   sess.AgentRequestCount,
		PlaytestSummary: sess.PlaytestSummary,
	}
	e.changes.milestones = append([]CommitMilestone{milestone}, e.changes.milestones...)
	e.recordMilestoneAcrossTabs(milestone)

	sess.LastCommit = &milestone
	sess.Milestones = slices.Clone(e.changes.milestones)
	sess.Committed = true
	sess.draftEdited = false
	sess.LastErr = ""
	sess.LastNotice = fmt.Sprintf("Committed as %s", sha)
	e.Status = sess.LastNotice
	e.snapshotGitHead()

	if err := e.refreshChangesSession(true); err != nil {
		return milestone, err
	}
	return milestone, nil
}

func (e *Editor) recordMilestoneAcrossTabs(m CommitMilestone) {
	if e == nil {
		return
	}
	recorded := make(map[*History]bool)
	if e.History != nil {
		e.History.RecordMilestone(m)
		recorded[e.History] = true
	}
	for _, t := range e.tabs {
		if t.history != nil && !recorded[t.history] {
			t.history.RecordMilestone(m)
			recorded[t.history] = true
		}
	}
}

func (e *Editor) markMilestonesPushed(branch string) {
	if e == nil {
		return
	}
	for i := range e.changes.milestones {
		if e.changes.milestones[i].Branch == branch {
			e.changes.milestones[i].Pushed = true
		}
	}
	seen := make(map[*History]bool)
	if e.History != nil {
		e.History.MarkBranchPushed(branch)
		seen[e.History] = true
	}
	for _, t := range e.tabs {
		if t.history != nil && !seen[t.history] {
			t.history.MarkBranchPushed(branch)
			seen[t.history] = true
		}
	}
}

// CommitAndPushSelectedChanges commits the currently included files and immediately pushes the branch.
func (e *Editor) CommitAndPushSelectedChanges() (CommitMilestone, error) {
	m, err := e.CommitSelectedChanges()
	if err != nil {
		return m, err
	}
	if err := e.PushCommits(); err != nil {
		return m, err
	}
	m.Pushed = true
	return m, nil
}

// CommitAndPushSelectedChangesAsync commits locally and pushes the branch off the UI thread.
func (e *Editor) CommitAndPushSelectedChangesAsync(cx *core.Scope) error {
	if _, err := e.CommitSelectedChanges(); err != nil {
		return err
	}
	return e.PushCommitsAsync(cx)
}

func (e *Editor) buildPushArgs(sess *ChangesSession) ([]string, error) {
	if sess.Upstream != "" {
		return []string{"push"}, nil
	}
	if err := validateGitBranchName(sess.Branch); err != nil {
		return nil, err
	}
	return []string{"push", "-u", "--", "origin", "refs/heads/" + sess.Branch}, nil
}

// PushCommits pushes local commits on the current branch to its upstream (or sets upstream on origin when none is configured).
func (e *Editor) PushCommits() error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return ErrMergeConflictInProgress
	}
	args, err := e.buildPushArgs(sess)
	if err != nil {
		sess.LastErr = "Push failed: " + err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitNetworkTimeout)
	defer cancel()
	out, err := e.runGitCtx(ctx, sess.RepoDir, args...)
	return e.applyPushResult(sess.Branch, out, err)
}

// PushCommitsAsync pushes commits off the UI thread and posts the result back to the editor scope.
func (e *Editor) PushCommitsAsync(cx *core.Scope) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	scope := e.scope
	if scope == nil {
		scope = cx
	}
	if scope == nil {
		return e.PushCommits()
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return ErrMergeConflictInProgress
	}
	args, err := e.buildPushArgs(sess)
	if err != nil {
		sess.LastErr = "Push failed: " + err.Error()
		e.changes.rev++
		e.notify()
		return err
	}
	repoDir := sess.RepoDir
	branch := sess.Branch
	runner := e.GitRunner
	ctx, gen, done, cancel, err := e.beginChangesOp("Pushing...", defaultGitNetworkTimeout)
	if err != nil {
		return err
	}
	go func() {
		defer close(done)
		var out string
		var runErr error
		if runner != nil {
			out, runErr = runner(repoDir, args...)
		} else {
			out, runErr = runGitExecCtx(ctx, repoDir, nil, nil, args...)
		}
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		scope.Post(func() {
			if !e.finishChangesOp(gen, cancel) {
				return
			}
			e.report(e.applyPushResult(branch, out, runErr))
		})
	}()
	return nil
}

func (e *Editor) applyPushResult(branch, out string, err error) error {
	if !e.ChangesOpen() {
		return nil
	}
	sess := e.changes.session
	if err != nil {
		msg := nonEmpty(out, err.Error())
		sess.LastErr = "Push failed: " + msg
		e.changes.rev++
		e.notify()
		return fmt.Errorf("editor: git push: %w (%s)", err, out)
	}

	e.markMilestonesPushed(branch)
	if sess.LastCommit != nil {
		sess.LastCommit.Pushed = true
	}
	sess.Milestones = slices.Clone(e.changes.milestones)
	sess.Ahead = 0
	sess.LastErr = ""
	sess.LastNotice = "Pushed " + branch
	e.Status = sess.LastNotice
	return e.refreshChangesSession(sess.Committed)
}

// BackToCommitDraft switches the right pane from the post-commit milestone view back to the editable commit draft.
func (e *Editor) BackToCommitDraft() {
	if !e.ChangesOpen() {
		return
	}
	sess := e.changes.session
	sess.Committed = false
	sess.LastErr = ""
	if !sess.draftEdited && len(sess.Files) > 0 {
		_ = e.applyCommitDraft(sess)
	}
	e.changes.rev++
	e.changes.draftRev++
	e.notify()
}

// BuildPRDescription formats a structured Markdown pull request description from the active Changes session
// (summary, plan/description, changed files & scene object deltas, verification checks, and playtest numbers).
func (e *Editor) BuildPRDescription() string {
	if e == nil || e.changes.session == nil {
		return ""
	}
	sess := e.changes.session
	var b strings.Builder

	title := strings.TrimSpace(sess.DraftSummary)
	body := strings.TrimSpace(sess.DraftDescription)
	if sess.LastCommit != nil && (sess.Committed || title == "") {
		title = sess.LastCommit.Summary
		if body == "" {
			body = sess.LastCommit.Description
		}
	}
	if title != "" {
		fmt.Fprintf(&b, "## %s\n\n", title)
	}
	if body != "" {
		b.WriteString(body)
		b.WriteString("\n\n")
	}

	// Changed files section (included working-tree files or the last committed milestone's files).
	var fileLines []string
	for _, f := range sess.Files {
		if !f.Included {
			continue
		}
		line := fmt.Sprintf("- `%s` (%s)", f.DisplayName, f.Status)
		if f.Detail != "" {
			line = fmt.Sprintf("- `%s` — %s", f.DisplayName, f.Detail)
		}
		fileLines = append(fileLines, line)
	}
	if len(fileLines) == 0 && sess.LastCommit != nil && len(sess.LastCommit.Files) > 0 {
		for _, p := range sess.LastCommit.Files {
			fileLines = append(fileLines, fmt.Sprintf("- `%s`", p))
		}
	}
	if len(fileLines) > 0 {
		b.WriteString("### Changes\n")
		for _, l := range fileLines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	// Checks and playtest numbers.
	var verify []string
	if sess.Checks.Ran {
		checkState := "failed"
		if sess.Checks.CheckPassed {
			checkState = "passed"
		}
		verify = append(verify, "- `nsgo check`: "+checkState)
		if sess.Checks.TestPassed {
			if sess.Checks.TestCount > 0 {
				verify = append(verify, fmt.Sprintf("- `nsgo test`: %d passed", sess.Checks.TestCount))
			} else {
				verify = append(verify, "- `nsgo test`: passed")
			}
		} else {
			verify = append(verify, "- `nsgo test`: failed")
		}
	}
	if sess.PlaytestSummary != "" {
		verify = append(verify, "- Playtest: "+sess.PlaytestSummary)
	}
	if len(verify) > 0 {
		b.WriteString("### Verification\n")
		for _, l := range verify {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}

	if sess.DraftAttribution != "" {
		b.WriteString(sess.DraftAttribution)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// CopyPRDescription builds the structured PR description and places it on the clipboard (or Editor.ChangesClipboard / WriteClipboard when set).
func (e *Editor) CopyPRDescription() (string, error) {
	if !e.ChangesOpen() {
		return "", ErrNoChangesSession
	}
	desc := e.BuildPRDescription()
	if desc == "" {
		return "", errors.New("editor: nothing to copy for PR description")
	}
	sess := e.changes.session
	sess.LastPRDescription = desc
	switch {
	case e.ChangesClipboard != nil:
		e.ChangesClipboard(desc)
	case e.WriteClipboard != nil:
		e.WriteClipboard(desc)
	case e.scope != nil:
		e.scope.SetClipboard(desc)
	}
	sess.LastErr = ""
	sess.LastNotice = "Copied PR description to clipboard"
	e.Status = sess.LastNotice
	e.EmitToastEvent(ToastEvent{
		Kind:    ToastEventCopied,
		Target:  "pr-description",
		Message: sess.LastNotice,
	})
	e.changes.rev++
	e.notify()
	return desc, nil
}

func (e *Editor) detectGHAvailable() bool {
	if e != nil && e.GHRunner != nil {
		return true
	}
	_, err := exec.LookPath("gh")
	return err == nil
}

// ChangesGHAvailable reports whether the GitHub CLI (`gh`) is available (or Editor.GHRunner is injected),
// using the cached value from the active session when open.
func (e *Editor) ChangesGHAvailable() bool {
	if e != nil && e.GHRunner != nil {
		return true
	}
	if e != nil && e.changes.session != nil {
		return e.changes.session.GHAvailable
	}
	_, err := exec.LookPath("gh")
	return err == nil
}

func (e *Editor) prepareOpenPullRequest() (repoDir, title, body, base string, err error) {
	if !e.ChangesOpen() {
		return "", "", "", "", ErrNoChangesSession
	}
	if e.Playing() {
		return "", "", "", "", ErrPlaying
	}
	sess := e.changes.session
	if e.hasGitMergeInProgress(sess.RepoDir) {
		sess.LastErr = ErrMergeConflictInProgress.Error()
		e.changes.rev++
		e.notify()
		return "", "", "", "", ErrMergeConflictInProgress
	}
	if !e.ChangesGHAvailable() {
		err := errors.New("editor: GitHub CLI (gh) is not installed on PATH")
		sess.LastErr = err.Error()
		e.changes.rev++
		e.notify()
		return "", "", "", "", err
	}
	title = strings.TrimSpace(sess.DraftSummary)
	if sess.LastCommit != nil && (sess.Committed || title == "") {
		title = sess.LastCommit.Summary
	}
	if title == "" {
		title = "Changes on " + sess.Branch
	}
	body = e.BuildPRDescription()
	base = strings.TrimSpace(nonEmpty(sess.BaseBranch, "main"))
	if err := validateGitBranchName(base); err != nil {
		sess.LastErr = "Open PR failed: " + err.Error()
		e.changes.rev++
		e.notify()
		return "", "", "", "", err
	}
	return sess.RepoDir, title, body, base, nil
}

func runGHCreatePRCtx(ctx context.Context, repoDir, title, body, base string, runner func(string, ...string) (string, error)) (string, error) {
	if runner != nil {
		args := []string{"pr", "create", "--title", title, "--body", body, "--base", base}
		return runner(repoDir, args...)
	}
	// Pass PR body via stdin (--body-file -) so long descriptions never exceed OS argv limits or trigger flag parsing edge cases.
	args := []string{"pr", "create", "--title", title, "--body-file", "-", "--base", base}
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoDir
	cmd.Env = gitBaseEnv(nil)
	cmd.Stdin = strings.NewReader(body)
	raw, cerr := cmd.CombinedOutput()
	return strings.TrimSpace(string(raw)), cerr
}

func (e *Editor) applyOpenPullRequestResult(base, out string, err error) (string, error) {
	if !e.ChangesOpen() {
		return "", nil
	}
	sess := e.changes.session
	if err != nil {
		msg := nonEmpty(out, err.Error())
		sess.LastErr = "Open PR failed: " + msg
		e.changes.rev++
		e.notify()
		return "", fmt.Errorf("editor: gh pr create: %w (%s)", err, out)
	}
	sess.LastPRURL = strings.TrimSpace(out)
	sess.LastErr = ""
	sess.LastNotice = "Opened pull request: " + nonEmpty(sess.LastPRURL, base)
	e.Status = sess.LastNotice
	e.changes.rev++
	e.notify()
	return sess.LastPRURL, nil
}

// OpenPullRequest creates a pull request with `gh pr create` (or Editor.GHRunner) using the drafted title and PR description.
func (e *Editor) OpenPullRequest() (string, error) {
	repoDir, title, body, base, err := e.prepareOpenPullRequest()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), defaultGitNetworkTimeout)
	defer cancel()
	out, runErr := runGHCreatePRCtx(ctx, repoDir, title, body, base, e.GHRunner)
	return e.applyOpenPullRequestResult(base, out, runErr)
}

// OpenPullRequestAsync runs `gh pr create` off the UI thread and posts the result back to the editor scope.
func (e *Editor) OpenPullRequestAsync(cx *core.Scope) error {
	scope := e.scope
	if scope == nil {
		scope = cx
	}
	if scope == nil {
		_, err := e.OpenPullRequest()
		return err
	}
	repoDir, title, body, base, err := e.prepareOpenPullRequest()
	if err != nil {
		return err
	}
	runner := e.GHRunner
	ctx, gen, done, cancel, err := e.beginChangesOp("Opening PR...", defaultGitNetworkTimeout)
	if err != nil {
		return err
	}
	go func() {
		defer close(done)
		out, runErr := runGHCreatePRCtx(ctx, repoDir, title, body, base, runner)
		if runErr == nil && ctx.Err() != nil {
			runErr = ctx.Err()
		}
		scope.Post(func() {
			if !e.finishChangesOp(gen, cancel) {
				return
			}
			_, applyErr := e.applyOpenPullRequestResult(base, out, runErr)
			e.report(applyErr)
		})
	}()
	return nil
}

// AuditionChangedAudio plays the selected sound asset from the Changes panel (reusing sound.Mixer when a UI scope is attached).
func (e *Editor) AuditionChangedAudio(filePath string) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	if e.Playing() {
		return ErrPlaying
	}
	sess := e.changes.session
	var target *ChangedFile
	for i := range sess.Files {
		if sess.Files[i].Path == filePath {
			target = &sess.Files[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("editor: changed file %q not found", filePath)
	}
	if !isAudioPath(target.Path) {
		return fmt.Errorf("editor: %q is not a playable audio asset", target.Path)
	}
	if e.changes.auditionPath == filePath {
		e.StopChangedAudio()
		return nil
	}
	absPath, err := resolveRepoFilePath(sess.RepoDir, sess.ProjectPrefix, target.Path)
	if err != nil {
		return err
	}
	raw, truncated, _, err := readFileBounded(absPath, maxChangesAuditionBytes)
	if err != nil {
		return fmt.Errorf("editor: read audio %q: %w", target.Path, err)
	}
	if truncated {
		return fmt.Errorf("editor: audio %q exceeds %s audition limit", target.Path, formatHumanBytes(maxChangesAuditionBytes))
	}

	e.stopChangedAudioPlayback()
	if e.scope != nil {
		if e.Project != nil && e.Project.Audio != nil && e.ProjectPath != "" && len(e.Project.Audio.Buses) > 0 && target.ProjectPath != "" {
			bus := e.Project.Audio.Buses[0].Name
			if err := e.AuditionAudioBus(bus, target.ProjectPath); err == nil {
				e.changes.auditionPath = filePath
				e.changes.rev++
				e.notify()
				return nil
			}
		}
		if e.changes.audioMixer == nil || e.changes.audioMixerScope != e.scope {
			var m *sound.Mixer
			e.scope.Run(func() {
				m = sound.NewMixer(e.scope, "editor.changesAudio")
			})
			e.changes.audioMixer = m
			e.changes.audioMixerScope = e.scope
			e.changes.audioMusic = nil
		}
		if e.changes.audioMusic == nil && e.changes.audioMixer != nil {
			e.changes.audioMusic = e.changes.audioMixer.Music("sfx")
		}
		if e.changes.audioMusic != nil {
			e.changes.audioMusic.Loop = false
			e.changes.audioMusic.Play(audio.Source{Data: raw}, 0)
		}
	}
	e.changes.auditionPath = filePath
	e.Status = "Playing " + target.DisplayName
	e.changes.rev++
	e.notify()
	return nil
}

// StopChangedAudio stops any audio asset audition playing from the Changes panel.
func (e *Editor) StopChangedAudio() {
	if e == nil || e.changes.auditionPath == "" {
		return
	}
	e.stopChangedAudioPlayback()
	e.changes.rev++
	e.notify()
}

func (e *Editor) stopChangedAudioPlayback() {
	if e == nil {
		return
	}
	if e.changes.audioMusic != nil {
		e.changes.audioMusic.Stop(0)
	}
	if e.changes.auditionPath != "" && e.audioMixer.auditioning {
		e.stopAudioAudition()
	}
	e.changes.auditionPath = ""
}

func (e *Editor) cleanupChangedAudioScope(cx *core.Scope) {
	if e == nil {
		return
	}
	e.stopChangedAudioPlayback()
	if e.changes.audioMixerScope == cx {
		e.changes.audioMixer = nil
		e.changes.audioMixerScope = nil
		e.changes.audioMusic = nil
	}
}

// OpenChangedFileInEditor closes the Changes panel and opens the changed project file in the editor when supported.
func (e *Editor) OpenChangedFileInEditor(filePath string) error {
	if !e.ChangesOpen() {
		return ErrNoChangesSession
	}
	sess := e.changes.session
	for _, f := range sess.Files {
		if f.Path == filePath {
			absPath, err := resolveRepoFilePath(sess.RepoDir, sess.ProjectPrefix, f.Path)
			if err != nil {
				return err
			}
			projPath := nonEmpty(f.ProjectPath, f.Path)
			e.CloseChangesPanel()
			if e.Project != nil && e.ProjectPath != "" {
				return e.OpenProjectFile(projPath)
			}
			if f.Category == ChangesCategoryScenes {
				return e.OpenFile(absPath)
			}
			e.Status = "Opened " + f.DisplayName
			e.notify()
			return nil
		}
	}
	return fmt.Errorf("editor: changed file %q not found", filePath)
}

func (e *Editor) resetChangesState() {
	if e == nil {
		return
	}
	e.cancelChangesOps()
	e.stopChangedAudioPlayback()
	e.changes.open = false
	e.changes.session = nil
	e.changes.count = 0
	e.changes.hasRepo = false
	e.changes.milestones = nil
	e.changes.rev++
	e.changes.draftRev++
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func formatCheckRelativeTime(t, now time.Time) string {
	if t.IsZero() {
		return "not run yet"
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d / time.Minute)
		return fmt.Sprintf("%d min ago", m)
	case d < 24*time.Hour:
		h := int(d / time.Hour)
		return fmt.Sprintf("%d h ago", h)
	default:
		days := int(d / (24 * time.Hour))
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
