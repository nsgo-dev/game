package editor

import (
	"context"
	"errors"
	"fmt"
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
	"unicode"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// CodeReviewDiffLayout selects how Go diffs are displayed in CodeReviewView.
type CodeReviewDiffLayout string

const (
	// CodeReviewDiffUnified shows old and new lines in a single stream with two line-number columns.
	CodeReviewDiffUnified CodeReviewDiffLayout = "unified"
	// CodeReviewDiffSplit shows old and new lines side by side.
	CodeReviewDiffSplit CodeReviewDiffLayout = "split"
)

// CodeReviewFileKind separates Keep-included data/YAML files from per-file confirmed Go files.
type CodeReviewFileKind string

const (
	// CodeReviewFileYAML is a scene, prefab, data table, or other text file included in Keep.
	CodeReviewFileYAML CodeReviewFileKind = "yaml"
	// CodeReviewFileGo is a Go source or test file that requires passing checks and per-file confirmation.
	CodeReviewFileGo CodeReviewFileKind = "go"
)

// CodeReviewCheckPhase tracks the lifecycle of nsgo check and nsgo test for pending Go files.
type CodeReviewCheckPhase string

const (
	CodeReviewChecksIdle    CodeReviewCheckPhase = "idle"
	CodeReviewChecksRunning CodeReviewCheckPhase = "running"
	CodeReviewChecksFixing  CodeReviewCheckPhase = "fixing"
	CodeReviewChecksPassed  CodeReviewCheckPhase = "passed"
	CodeReviewChecksFailed  CodeReviewCheckPhase = "failed"
)

// CodeReviewLineTag annotates notable diff lines (failing line, agent edit, round 2).
type CodeReviewLineTag string

const (
	CodeReviewTagNone        CodeReviewLineTag = ""
	CodeReviewTagFailsTest   CodeReviewLineTag = "fails test"
	CodeReviewTagFailedHere  CodeReviewLineTag = "failed here"
	CodeReviewTagAgentEdited CodeReviewLineTag = "agent edited"
	CodeReviewTagRound2      CodeReviewLineTag = "round 2"
)

const (
	defaultCodeReviewChecksTimeout = 5 * time.Minute
	maxCodeReviewFiles             = 64
	maxCodeReviewFileBytes         = 2 * 1024 * 1024
)

var (
	// ErrNoCodeReview is returned when a Go code review action runs with no active review session.
	ErrNoCodeReview = errors.New("editor: no active Go code review")
	// ErrCodeReviewChecksNotPassed is returned when attempting to confirm a Go file before nsgo check and nsgo test pass for the current pending code.
	ErrCodeReviewChecksNotPassed = errors.New("editor: cannot confirm Go file until nsgo check and nsgo test pass")
	// ErrCodeReviewNotGoFile is returned when ConfirmCodeReviewFile targets a non-Go file.
	ErrCodeReviewNotGoFile = errors.New("editor: only Go files require per-file confirmation")
	// ErrCodeReviewNothingToKeep is returned when KeepCodeReview has no pending YAML files and no confirmed Go files to write.
	ErrCodeReviewNothingToKeep = errors.New("editor: no YAML changes or confirmed Go files ready to keep")
	// ErrCodeReviewPathOutsideProject is returned when a proposed file path escapes the open project root.
	ErrCodeReviewPathOutsideProject = errors.New("editor: path escapes the project root")
)

// CodeReviewDiffLine is one line in a unified diff computed between Original and Pending.
type CodeReviewDiffLine struct {
	OldLine int               `json:"oldLine,omitempty"`
	NewLine int               `json:"newLine,omitempty"`
	Sign    string            `json:"sign,omitempty"`
	Text    string            `json:"text"`
	Kind    DiffLineKind      `json:"kind"`
	Tag     CodeReviewLineTag `json:"tag,omitempty"`
}

// CodeReviewSplitRow pairs one left (old) slot and one right (new) slot for Split diff layout.
type CodeReviewSplitRow struct {
	OldLine   int
	LeftText  string
	LeftKind  DiffLineKind
	NewLine   int
	RightText string
	RightKind DiffLineKind
	RightTag  CodeReviewLineTag
}

// CodeReviewFile holds one pending file proposed during an agent turn.
// Until confirmed (for Go files) and kept, Pending is held strictly in memory so disk and Play keep the old code.
type CodeReviewFile struct {
	Path          string               `json:"path"`
	Kind          CodeReviewFileKind   `json:"kind"`
	Original      string               `json:"-"`
	Existed       bool                 `json:"existed"`
	PreviousRound string               `json:"-"`
	Pending       string               `json:"-"`
	Delete        bool                 `json:"delete,omitempty"`
	Note          string               `json:"note,omitempty"`
	AddedLines    int                  `json:"addedLines"`
	RemovedLines  int                  `json:"removedLines"`
	Confirmed     bool                 `json:"confirmed"`
	Applied       bool                 `json:"applied"`
	EditedInRound int                  `json:"editedInRound"`
	Diff          []CodeReviewDiffLine `json:"diff,omitempty"`
}

// BaseName returns the file's base name (for example "patrol.go").
func (f *CodeReviewFile) BaseName() string {
	if f == nil {
		return ""
	}
	return path.Base(f.Path)
}

// CodeReviewTestFailure describes a failing Go test (or check diagnostic) surfaced in the failure card.
type CodeReviewTestFailure struct {
	Package     string `json:"package,omitempty"`
	TestName    string `json:"testName"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Message     string `json:"message"`
	Explanation string `json:"explanation,omitempty"`
	SuspectFile string `json:"suspectFile,omitempty"`
	SuspectLine int    `json:"suspectLine,omitempty"`
}

// CodeReviewCheckProgress reports live test execution progress while nsgo test runs.
type CodeReviewCheckProgress struct {
	Completed   int    `json:"completed"`
	Total       int    `json:"total"`
	CurrentTest string `json:"currentTest,omitempty"`
}

// CodeReviewCheckResult records the latest nsgo check and nsgo test outcomes against the pending Go overlay.
type CodeReviewCheckResult struct {
	Phase          CodeReviewCheckPhase    `json:"phase"`
	CheckPassed    bool                    `json:"checkPassed"`
	CheckOutput    string                  `json:"checkOutput,omitempty"`
	CheckError     string                  `json:"checkError,omitempty"`
	TestPassed     bool                    `json:"testPassed"`
	PassedCount    int                     `json:"passedCount"`
	FailedCount    int                     `json:"failedCount"`
	TotalCount     int                     `json:"totalCount"`
	Duration       time.Duration           `json:"duration"`
	Packages       []string                `json:"packages,omitempty"`
	TestOutput     string                  `json:"testOutput,omitempty"`
	Failures       []CodeReviewTestFailure `json:"failures,omitempty"`
	Completed      int                     `json:"completed"`
	ExpectedTotal  int                     `json:"expectedTotal"`
	AgentEditBadge string                  `json:"agentEditBadge,omitempty"`
	Generation     uint64                  `json:"generation"`
}

// AllPassed reports whether both nsgo check and nsgo test passed for editGen.
func (r CodeReviewCheckResult) AllPassed(editGen uint64) bool {
	return r.Phase == CodeReviewChecksPassed &&
		r.CheckPassed &&
		r.TestPassed &&
		r.Generation == editGen
}

// PrimaryFailure returns the first failing test, or nil when none failed.
func (r CodeReviewCheckResult) PrimaryFailure() *CodeReviewTestFailure {
	if len(r.Failures) == 0 {
		return nil
	}
	cp := r.Failures[0]
	return &cp
}

// PendingGoOverlayFile represents one pending Go file included in a Go build overlay JSON.
type PendingGoOverlayFile struct {
	Content string
	Delete  bool
}

// CodeReviewCheckRequest supplies the pending Go overlay and target packages to Editor.CodeReviewCheckRunner.
type CodeReviewCheckRequest struct {
	ProjectDir    string
	Packages      []string
	PendingGo     map[string]PendingGoOverlayFile
	OverlayPath   string
	PreviousTotal int
}

// CodeReviewFixRequest is delivered to Editor.OnAskCodeReviewFix when the author clicks "Ask the agent to fix".
type CodeReviewFixRequest struct {
	TurnID     string
	Round      int
	Prompt     string
	Failure    *CodeReviewTestFailure
	CheckError string
	Files      []string
}

// CodeReviewProposedFile is one file staged into the Go code review via ProposeCodeReviewFiles or MCP.
type CodeReviewProposedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Delete  bool   `json:"delete,omitempty"`
	Note    string `json:"note,omitempty"`
}

// CodeReviewProposeRequest stages or updates pending Go (and optional companion YAML) files for review.
type CodeReviewProposeRequest struct {
	TurnID             string
	Prompt             string
	Round              int
	RoundSummary       string
	FailureExplanation string
	SuspectFile        string
	SuspectLine        int
	PlayImpact         string
	Files              []CodeReviewProposedFile
	Open               bool
	// PreserveTurn keeps the existing review's TurnID and advances Round when updating an active review.
	PreserveTurn bool
}

// CodeReviewSession holds the live state of reviewing an agent turn's Go and companion YAML changes.
type CodeReviewSession struct {
	TurnID             string
	Prompt             string
	Round              int
	RoundSummary       string
	PlayImpact         string
	FailureExplanation string
	FailureSuspectFile string
	FailureSuspectLine int
	Files              []*CodeReviewFile
	SelectedPath       string
	Layout             CodeReviewDiffLayout
	Checks             CodeReviewCheckResult
	EditGen            uint64
	Open               bool
	Paused             bool
	KeptYAML           bool
	FixRequested       bool
	LastErr            string
	LastKeepSummary    string
	LastRevertSummary  string
}

// YAMLFiles returns the pending/unapplied YAML/prefab/data files included in Keep.
func (s *CodeReviewSession) YAMLFiles() []*CodeReviewFile {
	if s == nil {
		return nil
	}
	var out []*CodeReviewFile
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileYAML && !f.Applied {
			out = append(out, f)
		}
	}
	return out
}

// GoFiles returns all Go files in the review that have not yet been applied to disk.
func (s *CodeReviewSession) GoFiles() []*CodeReviewFile {
	if s == nil {
		return nil
	}
	var out []*CodeReviewFile
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileGo && !f.Applied {
			out = append(out, f)
		}
	}
	return out
}

// WaitingGoFiles returns Go files that are neither confirmed nor applied.
func (s *CodeReviewSession) WaitingGoFiles() []*CodeReviewFile {
	if s == nil {
		return nil
	}
	var out []*CodeReviewFile
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileGo && !f.Applied && !f.Confirmed {
			out = append(out, f)
		}
	}
	return out
}

// ConfirmedGoFiles returns Go files confirmed by the author and ready to write on Keep.
func (s *CodeReviewSession) ConfirmedGoFiles() []*CodeReviewFile {
	if s == nil {
		return nil
	}
	var out []*CodeReviewFile
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileGo && !f.Applied && f.Confirmed {
			out = append(out, f)
		}
	}
	return out
}

// SelectedFile returns the currently focused file in the review, falling back to the first Go or YAML file.
func (s *CodeReviewSession) SelectedFile() *CodeReviewFile {
	if s == nil {
		return nil
	}
	for _, f := range s.Files {
		if f.Path == s.SelectedPath && !f.Applied {
			return f
		}
	}
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileGo && !f.Applied {
			return f
		}
	}
	for _, f := range s.Files {
		if !f.Applied {
			return f
		}
	}
	return nil
}

// FindFile returns the file matching relPath, or nil.
func (s *CodeReviewSession) FindFile(relPath string) *CodeReviewFile {
	if s == nil {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(relPath, `\`, "/"))
	for _, f := range s.Files {
		if f.Path == clean {
			return f
		}
	}
	return nil
}

type codeReviewState struct {
	session *CodeReviewSession
	rev     uint64

	checkMu        sync.Mutex
	checkGen       uint64
	checkCancel    context.CancelFunc
	checkDone      chan struct{}
	checkDoneClose func()
}

func (e *Editor) codeReviewStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.codeReview.rev
}

func (e *Editor) resetCodeReviewForProject() {
	if e == nil {
		return
	}
	e.cancelCodeReviewChecks()
	e.codeReview.session = nil
	e.codeReview.rev++
}

func (e *Editor) cancelCodeReviewChecks() {
	if e == nil {
		return
	}
	e.codeReview.checkMu.Lock()
	e.codeReview.checkGen++
	cancel := e.codeReview.checkCancel
	closeDone := e.codeReview.checkDoneClose
	e.codeReview.checkCancel = nil
	e.codeReview.checkDone = nil
	e.codeReview.checkDoneClose = nil
	e.codeReview.checkMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if closeDone != nil {
		closeDone()
	}
}

// WaitCodeReviewChecks blocks until any in-flight asynchronous code review check completes (primarily for tests).
func (e *Editor) WaitCodeReviewChecks() {
	if e == nil {
		return
	}
	e.codeReview.checkMu.Lock()
	done := e.codeReview.checkDone
	e.codeReview.checkMu.Unlock()
	if done != nil {
		<-done
	}
}

// CodeReviewOpen reports whether the full-window Go code review screen is currently visible.
func (e *Editor) CodeReviewOpen() bool {
	return e != nil && e.codeReview.session != nil && e.codeReview.session.Open
}

// HasPendingCodeReview reports whether a Go code review is open, paused, or waiting on unconfirmed Go files after Keep.
func (e *Editor) HasPendingCodeReview() bool {
	if e == nil || e.codeReview.session == nil {
		return false
	}
	s := e.codeReview.session
	return len(s.YAMLFiles()) > 0 || len(s.GoFiles()) > 0
}

// CodeReviewSession returns the active Go code review session, or nil when none is active.
func (e *Editor) CodeReviewSession() *CodeReviewSession {
	if e == nil {
		return nil
	}
	return e.codeReview.session
}

// CodeReviewWaitingGoFiles returns the project-relative paths of Go files still waiting for confirmation.
func (e *Editor) CodeReviewWaitingGoFiles() []string {
	if e == nil || e.codeReview.session == nil {
		return nil
	}
	waiting := e.codeReview.session.WaitingGoFiles()
	out := make([]string, 0, len(waiting))
	for _, f := range waiting {
		out = append(out, f.Path)
	}
	return out
}

// CodeReviewHasWaitingCard reports whether the Agent panel should display the unconfirmed-Go waiting card
// (after Keep wrote YAML changes while Go files remained unconfirmed) or paused-review card.
func (e *Editor) CodeReviewHasWaitingCard() bool {
	if e == nil || e.codeReview.session == nil {
		return false
	}
	s := e.codeReview.session
	if s.Open {
		return false
	}
	if s.KeptYAML && len(s.WaitingGoFiles()) > 0 {
		return true
	}
	return s.Paused && (len(s.YAMLFiles()) > 0 || len(s.GoFiles()) > 0)
}

func (e *Editor) codeReviewProjectRoot() string {
	if e == nil {
		return ""
	}
	if e.ProjectPath != "" {
		return filepath.Dir(e.ProjectPath)
	}
	if e.FilePath != "" {
		return filepath.Dir(e.FilePath)
	}
	return ""
}

// cleanCodeReviewPath normalizes and validates a project-relative slash-separated path.
func cleanCodeReviewPath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("editor: empty file path")
	}
	if strings.ContainsRune(raw, 0) || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", errors.New("editor: invalid file path")
	}
	slashed := strings.ReplaceAll(raw, `\`, "/")
	if path.IsAbs(slashed) || filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", fmt.Errorf("%w: %s", ErrCodeReviewPathOutsideProject, raw)
	}
	clean := path.Clean(slashed)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || !fs.ValidPath(clean) {
		return "", fmt.Errorf("%w: %s", ErrCodeReviewPathOutsideProject, raw)
	}
	return clean, nil
}

func isCodeReviewGitPath(cleanRel string) bool {
	lower := strings.ToLower(cleanRel)
	return lower == ".git" || strings.HasPrefix(lower, ".git/")
}

// resolveCodeReviewAbsPath resolves cleanRel inside projectDir and refuses symlink escapes (including broken symlinks) outside projectDir.
func resolveCodeReviewAbsPath(projectDir, cleanRel string) (string, error) {
	if projectDir == "" {
		return "", errors.New("editor: no project directory is open")
	}
	absRoot, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	realRoot := absRoot
	if eval, err := filepath.EvalSymlinks(absRoot); err == nil {
		realRoot = eval
	}
	target := filepath.Join(absRoot, filepath.FromSlash(cleanRel))

	// Check the target itself first: if it exists (including as a broken symlink), EvalSymlinks must succeed and remain inside realRoot.
	if _, lerr := os.Lstat(target); lerr == nil {
		eval, serr := filepath.EvalSymlinks(target)
		if serr != nil || !isSubpath(realRoot, eval) {
			return "", fmt.Errorf("%w: %s", ErrCodeReviewPathOutsideProject, cleanRel)
		}
		return target, nil
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return "", lerr
	}

	// For a new file, walk upward to the deepest existing ancestor; if any existing ancestor is a broken or escaping symlink, refuse it.
	probe := filepath.Dir(target)
	for {
		if _, lerr := os.Lstat(probe); lerr == nil {
			eval, serr := filepath.EvalSymlinks(probe)
			if serr != nil || !isSubpath(realRoot, eval) {
				return "", fmt.Errorf("%w: %s", ErrCodeReviewPathOutsideProject, cleanRel)
			}
			break
		} else if !errors.Is(lerr, fs.ErrNotExist) {
			return "", lerr
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		probe = parent
	}
	return target, nil
}

func classifyCodeReviewFile(cleanRel string) CodeReviewFileKind {
	if strings.HasSuffix(strings.ToLower(cleanRel), ".go") {
		return CodeReviewFileGo
	}
	return CodeReviewFileYAML
}

// ProposeCodeReviewFiles stages or updates pending Go and companion YAML files for review without writing them to disk.
// Calling it while a review is already active updates the existing turn in place (advancing Round for round 2 fixes)
// rather than creating a second turn, resets Go confirmations for modified code, and recomputes line-tagged diffs.
func (e *Editor) ProposeCodeReviewFiles(req CodeReviewProposeRequest) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if len(req.Files) == 0 {
		return errors.New("editor: at least one file is required for code review")
	}
	if len(req.Files) > maxCodeReviewFiles {
		return fmt.Errorf("editor: too many files (%d) in code review proposal (max %d)", len(req.Files), maxCodeReviewFiles)
	}
	projectDir := e.codeReviewProjectRoot()

	// Validate and normalize every path before mutating state.
	type resolvedItem struct {
		rel     string
		abs     string
		kind    CodeReviewFileKind
		content string
		del     bool
		note    string
	}
	items := make([]resolvedItem, 0, len(req.Files))
	seen := make(map[string]bool, len(req.Files))
	for _, pf := range req.Files {
		if len(pf.Content) > maxCodeReviewFileBytes {
			return fmt.Errorf("editor: proposed file %q exceeds %d-byte limit", pf.Path, maxCodeReviewFileBytes)
		}
		rel, err := cleanCodeReviewPath(pf.Path)
		if err != nil {
			return err
		}
		if isCodeReviewGitPath(rel) {
			return fmt.Errorf("editor: refusing to stage Git metadata path %q in code review", rel)
		}
		if dom, ok := e.CheckYouOnlyTarget(rel); ok {
			return fmt.Errorf("editor: %s is protected under %s (You only) and cannot be staged in code review", rel, dom.DisplayName())
		} else if dom, ok := MatchYouOnlyPath(rel); ok {
			return fmt.Errorf("editor: %s is protected under %s (You only) and cannot be staged in code review", rel, dom.DisplayName())
		}
		if seen[rel] {
			return fmt.Errorf("editor: duplicate file %q in code review proposal", rel)
		}
		seen[rel] = true
		var abs string
		if projectDir != "" {
			abs, err = resolveCodeReviewAbsPath(projectDir, rel)
			if err != nil {
				return err
			}
		}
		items = append(items, resolvedItem{
			rel:     rel,
			abs:     abs,
			kind:    classifyCodeReviewFile(rel),
			content: pf.Content,
			del:     pf.Delete,
			note:    pf.Note,
		})
	}

	sess := e.codeReview.session
	updating := sess != nil && (req.PreserveTurn || req.Prompt == "" || sess.FixRequested || sess.TurnID == req.TurnID)
	if !updating {
		e.cancelCodeReviewChecks()
		turnID := req.TurnID
		if turnID == "" {
			if n := len(e.agent.turns); n > 0 {
				turnID = e.agent.turns[n-1].ID
			} else {
				e.agent.turnSeq++
				turnID = fmt.Sprintf("turn-%d", e.agent.turnSeq)
				e.agent.turns = append(e.agent.turns, AgentTurn{
					ID:         turnID,
					Prompt:     req.Prompt,
					Done:       true,
					StartedAt:  e.nowTime(),
					FinishedAt: e.nowTime(),
				})
			}
		}
		round := req.Round
		if round <= 0 {
			round = 1
		}
		sess = &CodeReviewSession{
			TurnID:       turnID,
			Prompt:       req.Prompt,
			Round:        round,
			RoundSummary: req.RoundSummary,
			PlayImpact:   req.PlayImpact,
			Layout:       CodeReviewDiffUnified,
			Checks: CodeReviewCheckResult{
				Phase: CodeReviewChecksIdle,
			},
		}
		e.codeReview.session = sess
	} else {
		e.cancelCodeReviewChecks()
		if req.Prompt != "" && sess.Prompt == "" {
			sess.Prompt = req.Prompt
		}
		if req.Round > sess.Round {
			sess.Round = req.Round
		} else if sess.FixRequested || sess.Checks.Phase == CodeReviewChecksFailed || sess.Checks.Phase == CodeReviewChecksPassed {
			sess.Round++
		}
		if req.RoundSummary != "" {
			sess.RoundSummary = req.RoundSummary
		}
		if req.PlayImpact != "" {
			sess.PlayImpact = req.PlayImpact
		}
	}

	if req.FailureExplanation != "" {
		sess.FailureExplanation = req.FailureExplanation
	}
	if req.SuspectFile != "" {
		if cleanSuspect, err := cleanCodeReviewPath(req.SuspectFile); err == nil {
			sess.FailureSuspectFile = cleanSuspect
		}
	}
	if req.SuspectLine > 0 {
		sess.FailureSuspectLine = req.SuspectLine
	}

	touchedGo := false
	var editedBadge string
	for _, it := range items {
		existing := sess.FindFile(it.rel)
		if existing != nil {
			if existing.Applied && it.abs != "" {
				// File was already written to disk by an earlier Keep; refresh Original from disk so a subsequent Undo restores the post-Keep content.
				if data, err := os.ReadFile(it.abs); err == nil {
					existing.Original = string(data)
					existing.Existed = true
				} else {
					existing.Original = ""
					existing.Existed = false
				}
			}
			existing.PreviousRound = existing.Pending
			existing.Pending = it.content
			existing.Delete = it.del
			if it.note != "" {
				existing.Note = it.note
			}
			existing.Applied = false
			existing.EditedInRound = sess.Round
			if existing.Kind == CodeReviewFileGo {
				touchedGo = true
				existing.Confirmed = false
				if line := firstChangedNewLine(existing.PreviousRound, existing.Pending); line > 0 {
					editedBadge = fmt.Sprintf("Agent edited %s:%d", existing.BaseName(), line)
				} else {
					editedBadge = fmt.Sprintf("Agent edited %s", existing.BaseName())
				}
			}
			continue
		}

		// First time seeing this file: snapshot original content from disk without mutating disk.
		var orig string
		var existed bool
		if it.abs != "" {
			if data, err := os.ReadFile(it.abs); err == nil {
				orig = string(data)
				existed = true
			}
		}
		f := &CodeReviewFile{
			Path:          it.rel,
			Kind:          it.kind,
			Original:      orig,
			Existed:       existed,
			Pending:       it.content,
			Delete:        it.del,
			Note:          it.note,
			EditedInRound: sess.Round,
		}
		if f.Kind == CodeReviewFileGo {
			touchedGo = true
		}
		sess.Files = append(sess.Files, f)
	}

	// Keep file list stably ordered: YAML files first (alphabetical), then Go files (alphabetical, non-test before test).
	slices.SortStableFunc(sess.Files, func(a, b *CodeReviewFile) int {
		if a.Kind != b.Kind {
			if a.Kind == CodeReviewFileYAML {
				return -1
			}
			return 1
		}
		aTest := strings.HasSuffix(a.Path, "_test.go")
		bTest := strings.HasSuffix(b.Path, "_test.go")
		if aTest != bTest {
			if !aTest {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})

	if touchedGo || sess.EditGen == 0 {
		sess.EditGen++
	}
	if touchedGo {
		// Any Go change invalidates prior confirmations and check passes across all Go files in the review.
		for _, f := range sess.Files {
			if f.Kind == CodeReviewFileGo && !f.Applied {
				f.Confirmed = false
			}
		}
		prevTotal := sess.Checks.TotalCount
		if prevTotal == 0 {
			prevTotal = sess.Checks.ExpectedTotal
		}
		phase := CodeReviewChecksIdle
		if sess.FixRequested {
			phase = CodeReviewChecksFixing
		}
		sess.Checks = CodeReviewCheckResult{
			Phase:          phase,
			Packages:       sess.goPackages(),
			ExpectedTotal:  prevTotal,
			AgentEditBadge: editedBadge,
			Generation:     0, // not passed for current EditGen yet
		}
	}
	sess.FixRequested = false
	sess.Paused = false
	sess.LastErr = ""
	if req.Open {
		sess.Open = true
		e.closeComposerPopovers()
	}
	if sess.SelectedPath == "" || sess.FindFile(sess.SelectedPath) == nil {
		if goFiles := sess.GoFiles(); len(goFiles) > 0 {
			sess.SelectedPath = goFiles[0].Path
		} else if len(sess.Files) > 0 {
			sess.SelectedPath = sess.Files[0].Path
		}
	}

	e.recomputeCodeReviewDiffs()
	e.recordCodeReviewTurnStep(sess, items[0].rel)
	e.codeReview.rev++
	e.notify()
	return nil
}

func (e *Editor) recordCodeReviewTurnStep(sess *CodeReviewSession, primaryRel string) {
	if sess == nil || sess.TurnID == "" {
		return
	}
	turn := e.findAgentTurn(sess.TurnID)
	if turn == nil {
		return
	}
	title := fmt.Sprintf("Proposed %s", path.Base(primaryRel))
	if sess.Round > 1 {
		title = fmt.Sprintf("Edited %s (round %d)", path.Base(primaryRel), sess.Round)
	}
	turn.Steps = append(turn.Steps, AgentStep{
		ID:       fmt.Sprintf("%s-code-review-%d", turn.ID, len(turn.Steps)+1),
		Title:    title,
		ToolName: "code_review_propose",
		Status:   AgentStepDone,
	})
	turn.FinishedAt = e.nowTime()
}

// SetCodeReviewFailureExplanation attaches or updates the agent's explanation and optional suspect source line
// for the current failing check/test.
func (e *Editor) SetCodeReviewFailureExplanation(explanation, suspectFile string, suspectLine int) {
	if e == nil || e.codeReview.session == nil {
		return
	}
	sess := e.codeReview.session
	sess.FailureExplanation = explanation
	if suspectFile != "" {
		if clean, err := cleanCodeReviewPath(suspectFile); err == nil {
			sess.FailureSuspectFile = clean
		}
	}
	if suspectLine > 0 {
		sess.FailureSuspectLine = suspectLine
	}
	if len(sess.Checks.Failures) > 0 {
		sess.Checks.Failures[0].Explanation = explanation
		if sess.FailureSuspectFile != "" {
			sess.Checks.Failures[0].SuspectFile = sess.FailureSuspectFile
		}
		if sess.FailureSuspectLine > 0 {
			sess.Checks.Failures[0].SuspectLine = sess.FailureSuspectLine
		}
	}
	e.recomputeCodeReviewDiffs()
	e.codeReview.rev++
	e.notify()
}

// OpenCodeReview opens or resumes the full-window Go code review screen.
// When selectPath is non-empty and exists in the session, it focuses that file; otherwise it focuses the first unconfirmed Go file.
func (e *Editor) OpenCodeReview(selectPath string) {
	if e == nil || e.codeReview.session == nil {
		return
	}
	sess := e.codeReview.session
	sess.Open = true
	sess.Paused = false
	e.closeComposerPopovers()
	if selectPath != "" {
		if clean, err := cleanCodeReviewPath(selectPath); err == nil && sess.FindFile(clean) != nil {
			sess.SelectedPath = clean
		}
	} else if sel := sess.SelectedFile(); sel == nil || sel.Applied {
		if waiting := sess.WaitingGoFiles(); len(waiting) > 0 {
			sess.SelectedPath = waiting[0].Path
		} else if goFiles := sess.GoFiles(); len(goFiles) > 0 {
			sess.SelectedPath = goFiles[0].Path
		}
	}
	e.codeReview.rev++
	e.notify()
}

// CloseCodeReview pauses the Go code review and returns to the editor (Esc), keeping pending changes and leaving a card in the Agent panel.
func (e *Editor) CloseCodeReview() {
	if e == nil || e.codeReview.session == nil || !e.codeReview.session.Open {
		return
	}
	sess := e.codeReview.session
	sess.Open = false
	if !sess.KeptYAML || len(sess.WaitingGoFiles()) == 0 {
		sess.Paused = true
	}
	waiting := len(sess.WaitingGoFiles())
	msg := "Review paused"
	if waiting == 1 {
		msg = "Review paused · 1 Go file not confirmed"
	} else if waiting > 1 {
		msg = fmt.Sprintf("Review paused · %d Go files not confirmed", waiting)
	}
	e.ShowToast(Toast{
		ID:          "code-review-paused",
		Kind:        ToastInfo,
		Message:     msg,
		ActionLabel: "Resume",
		OnAction: func() {
			e.OpenCodeReview("")
		},
	})
	e.codeReview.rev++
	e.notify()
}

// SelectCodeReviewFile switches the focused file in the Go code review screen.
func (e *Editor) SelectCodeReviewFile(relPath string) {
	if e == nil || e.codeReview.session == nil {
		return
	}
	clean, err := cleanCodeReviewPath(relPath)
	if err != nil {
		return
	}
	if f := e.codeReview.session.FindFile(clean); f == nil || f.Applied {
		return
	}
	if e.codeReview.session.SelectedPath == clean {
		return
	}
	e.codeReview.session.SelectedPath = clean
	e.codeReview.rev++
	e.notify()
}

// StepCodeReviewFile moves selection forward (+1) or backward (-1) across visible files in the Go review sidebar.
func (e *Editor) StepCodeReviewFile(delta int) {
	if e == nil || e.codeReview.session == nil {
		return
	}
	sess := e.codeReview.session
	visible := append(sess.YAMLFiles(), sess.GoFiles()...)
	if len(visible) == 0 {
		return
	}
	idx := 0
	for i, f := range visible {
		if f.Path == sess.SelectedPath {
			idx = i
			break
		}
	}
	next := max(0, min(len(visible)-1, idx+delta))
	e.SelectCodeReviewFile(visible[next].Path)
}

// SetCodeReviewLayout switches the Go diff layout between Unified and Split.
func (e *Editor) SetCodeReviewLayout(layout CodeReviewDiffLayout) {
	if e == nil || e.codeReview.session == nil {
		return
	}
	if layout != CodeReviewDiffSplit {
		layout = CodeReviewDiffUnified
	}
	if e.codeReview.session.Layout == layout {
		return
	}
	e.codeReview.session.Layout = layout
	e.codeReview.rev++
	e.notify()
}

// CanConfirmCodeReviewFile reports whether relPath is a Go file whose checks have passed for the current pending code.
func (e *Editor) CanConfirmCodeReviewFile(relPath string) bool {
	if e == nil || e.codeReview.session == nil {
		return false
	}
	sess := e.codeReview.session
	f := sess.FindFile(relPath)
	if f == nil || f.Kind != CodeReviewFileGo || f.Applied {
		return false
	}
	return sess.Checks.AllPassed(sess.EditGen)
}

// ConfirmCodeReviewFile confirms or unconfirms one Go file in the review.
// Confirming is refused with ErrCodeReviewChecksNotPassed until both nsgo check and nsgo test pass for the latest pending code.
// A confirmed file is not written to disk until KeepCodeReview runs, so it lands and undoes with the rest of the review.
func (e *Editor) ConfirmCodeReviewFile(relPath string, confirmed bool) error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	sess := e.codeReview.session
	clean, err := cleanCodeReviewPath(relPath)
	if err != nil {
		return err
	}
	f := sess.FindFile(clean)
	if f == nil || f.Applied {
		return fmt.Errorf("editor: file %q is not pending in code review", clean)
	}
	if f.Kind != CodeReviewFileGo {
		return ErrCodeReviewNotGoFile
	}
	if confirmed && !sess.Checks.AllPassed(sess.EditGen) {
		return ErrCodeReviewChecksNotPassed
	}
	if f.Confirmed == confirmed {
		return nil
	}
	f.Confirmed = confirmed
	e.codeReview.rev++
	e.notify()
	return nil
}

// AskAgentToFixCodeReview asks the agent to fix the failing Go test/check within the same turn (round 2).
// It switches the review into the fixing state, focuses the suspect Go file, notifies Editor.OnAskCodeReviewFix
// (or starts a fix prompt via Editor.AgentRunner if configured), and awaits the agent's updated files.
func (e *Editor) AskAgentToFixCodeReview() error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	sess := e.codeReview.session
	sess.FixRequested = true
	sess.Checks.Phase = CodeReviewChecksFixing
	sess.Checks.Completed = 0
	if sess.Checks.TotalCount > 0 {
		sess.Checks.ExpectedTotal = sess.Checks.TotalCount
	}

	// Focus the suspect Go source file (or first non-test Go file).
	if failure := sess.Checks.PrimaryFailure(); failure != nil && failure.SuspectFile != "" {
		if f := sess.FindFile(failure.SuspectFile); f != nil && !f.Applied {
			sess.SelectedPath = f.Path
		}
	} else if goFiles := sess.GoFiles(); len(goFiles) > 0 {
		sess.SelectedPath = goFiles[0].Path
	}

	req := CodeReviewFixRequest{
		TurnID:     sess.TurnID,
		Round:      sess.Round + 1,
		Prompt:     sess.Prompt,
		Failure:    sess.Checks.PrimaryFailure(),
		CheckError: sess.Checks.CheckError,
	}
	for _, f := range sess.GoFiles() {
		req.Files = append(req.Files, f.Path)
	}

	if turn := e.findAgentTurn(sess.TurnID); turn != nil {
		detail := sess.Checks.CheckError
		if req.Failure != nil {
			detail = req.Failure.TestName + ": " + req.Failure.Message
		}
		turn.Steps = append(turn.Steps, AgentStep{
			ID:       fmt.Sprintf("%s-fix-round-%d", turn.ID, req.Round),
			Title:    fmt.Sprintf("Fixing failing Go checks (round %d)", req.Round),
			Detail:   detail,
			ToolName: "code_review_fix",
			Status:   AgentStepRunning,
		})
	}

	e.recomputeCodeReviewDiffs()
	e.codeReview.rev++
	e.notify()

	if e.OnAskCodeReviewFix != nil {
		e.OnAskCodeReviewFix(req)
	}
	return nil
}

// OpenCodeReviewTestFile focuses the failing test file in the review if present, or opens it in the external editor.
func (e *Editor) OpenCodeReviewTestFile() error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	sess := e.codeReview.session
	if f := sess.Checks.PrimaryFailure(); f != nil && f.File != "" {
		if matched := sess.matchFailureFile(f.File, f.Package); matched != nil && !matched.Applied {
			sess.SelectedPath = matched.Path
			e.codeReview.rev++
			e.notify()
			return nil
		}
		return e.OpenCodeReviewInExternalEditor(f.File, f.Line)
	}
	for _, f := range sess.GoFiles() {
		if strings.HasSuffix(f.Path, "_test.go") {
			sess.SelectedPath = f.Path
			e.codeReview.rev++
			e.notify()
			return nil
		}
	}
	return nil
}

// ExternalEditorLabel returns the button label for opening a file externally, such as "Open in GoLand".
func (e *Editor) ExternalEditorLabel() string {
	if e != nil && strings.TrimSpace(e.ExternalEditorName) != "" {
		return "Open in " + strings.TrimSpace(e.ExternalEditorName)
	}
	return e.T("codeReview.openExternalDefault")
}

// OpenCodeReviewInExternalEditor opens relPath (at 1-based line when > 0) in the author's external IDE/editor.
// Containment inside the project root is enforced.
func (e *Editor) OpenCodeReviewInExternalEditor(relPath string, line int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	clean, err := cleanCodeReviewPath(relPath)
	if err != nil {
		return err
	}
	projectDir := e.codeReviewProjectRoot()
	target := clean
	if projectDir != "" {
		abs, err := resolveCodeReviewAbsPath(projectDir, clean)
		if err != nil {
			return err
		}
		target = abs
	}
	if line < 1 {
		line = 1
	}
	switch {
	case e.OnOpenSourceFile != nil:
		if err := e.OnOpenSourceFile(target, line); err != nil {
			return err
		}
	case e.OnOpenFileAtLine != nil:
		e.OnOpenFileAtLine(target, line)
	default:
		if err := openExternalSourceFileOS(target); err != nil {
			return err
		}
	}
	e.Status = fmt.Sprintf("Opened %s:%d", clean, line)
	e.notify()
	return nil
}

// CodeReviewCheckRun captures a snapshot of a pending Go code review so callers (such as editormcp)
// can execute nsgo check and nsgo test off the editor's UI goroutine and apply the result back on it.
type CodeReviewCheckRun struct {
	sess       *CodeReviewSession
	editGen    uint64
	projectDir string
	pkgs       []string
	pendingGo  map[string]PendingGoOverlayFile
	prevTotal  int
	runner     func(context.Context, CodeReviewCheckRequest, func(CodeReviewCheckProgress)) (CodeReviewCheckResult, error)
}

// PrepareCodeReviewCheckRun snapshots the active code review and marks checks running on the editor goroutine.
func (e *Editor) PrepareCodeReviewCheckRun() (*CodeReviewCheckRun, error) {
	if e == nil || e.codeReview.session == nil {
		return nil, ErrNoCodeReview
	}
	// Cancel any earlier in-flight async UI check so stale results cannot race with this run.
	e.cancelCodeReviewChecks()
	sess := e.codeReview.session
	prevTotal := sess.Checks.TotalCount
	if prevTotal == 0 {
		prevTotal = sess.Checks.ExpectedTotal
	}
	pkgs := sess.goPackages()
	if sess.Checks.Phase != CodeReviewChecksFixing {
		sess.Checks.Phase = CodeReviewChecksRunning
	}
	sess.Checks.Completed = 0
	sess.Checks.ExpectedTotal = prevTotal
	sess.Checks.Packages = pkgs
	e.codeReview.rev++
	e.notify()
	return &CodeReviewCheckRun{
		sess:       sess,
		editGen:    sess.EditGen,
		projectDir: e.codeReviewProjectRoot(),
		pkgs:       pkgs,
		pendingGo:  sess.pendingGoMap(),
		prevTotal:  prevTotal,
		runner:     e.CodeReviewCheckRunner,
	}, nil
}

// ExecuteCodeReviewCheckRun runs nsgo check and nsgo test against the pending Go overlay for run.
// Safe to call off the editor's UI goroutine.
func (e *Editor) ExecuteCodeReviewCheckRun(ctx context.Context, run *CodeReviewCheckRun) (CodeReviewCheckResult, error) {
	if e == nil || run == nil {
		return CodeReviewCheckResult{}, ErrNoCodeReview
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultCodeReviewChecksTimeout)
		defer cancel()
	}
	return executeCodeReviewChecks(ctx, run.runner, run.projectDir, run.pkgs, run.pendingGo, run.prevTotal, func(prog CodeReviewCheckProgress) {
		e.postOrRun(func() {
			if e.codeReview.session == run.sess && run.sess.EditGen == run.editGen {
				run.sess.Checks.Completed = prog.Completed
				if prog.Total > 0 {
					run.sess.Checks.ExpectedTotal = prog.Total
				}
				e.codeReview.rev++
				e.notify()
			}
		})
	})
}

// ApplyCodeReviewCheckRun commits the check result onto the editor goroutine if the review has not been edited since PrepareCodeReviewCheckRun.
func (e *Editor) ApplyCodeReviewCheckRun(run *CodeReviewCheckRun, res CodeReviewCheckResult, runErr error) error {
	if e == nil || run == nil {
		return ErrNoCodeReview
	}
	return e.applyCodeReviewChecksResult(run.sess, run.editGen, res, runErr)
}

// RunCodeReviewChecks synchronously runs nsgo check and nsgo test against the pending Go files via a Go build overlay,
// leaving every file on disk untouched.
func (e *Editor) RunCodeReviewChecks(ctx context.Context) error {
	run, err := e.PrepareCodeReviewCheckRun()
	if err != nil {
		return err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), defaultCodeReviewChecksTimeout)
		defer cancel()
	}
	res, runErr := executeCodeReviewChecks(ctx, run.runner, run.projectDir, run.pkgs, run.pendingGo, run.prevTotal, func(prog CodeReviewCheckProgress) {
		if e.codeReview.session == run.sess && run.sess.EditGen == run.editGen {
			run.sess.Checks.Completed = prog.Completed
			if prog.Total > 0 {
				run.sess.Checks.ExpectedTotal = prog.Total
			}
			e.codeReview.rev++
			e.notify()
		}
	})
	return e.applyCodeReviewChecksResult(run.sess, run.editGen, res, runErr)
}

// RunCodeReviewChecksAsync runs nsgo check and nsgo test off the UI thread and posts progress and final results back to the UI scope.
func (e *Editor) RunCodeReviewChecksAsync() error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	sess := e.codeReview.session
	editGen := sess.EditGen
	projectDir := e.codeReviewProjectRoot()
	pendingGo := sess.pendingGoMap()
	pkgs := sess.goPackages()
	runner := e.CodeReviewCheckRunner
	prevTotal := sess.Checks.TotalCount
	if prevTotal == 0 {
		prevTotal = sess.Checks.ExpectedTotal
	}

	e.cancelCodeReviewChecks()
	ctx, cancel := context.WithTimeout(context.Background(), defaultCodeReviewChecksTimeout)
	done := make(chan struct{})
	var doneOnce sync.Once
	finishDone := func() {
		doneOnce.Do(func() { close(done) })
	}
	e.codeReview.checkMu.Lock()
	e.codeReview.checkGen++
	checkGen := e.codeReview.checkGen
	e.codeReview.checkCancel = cancel
	e.codeReview.checkDone = done
	e.codeReview.checkDoneClose = finishDone
	e.codeReview.checkMu.Unlock()

	if sess.Checks.Phase != CodeReviewChecksFixing {
		sess.Checks.Phase = CodeReviewChecksRunning
	}
	sess.Checks.Completed = 0
	sess.Checks.ExpectedTotal = prevTotal
	sess.Checks.Packages = pkgs
	e.codeReview.rev++
	e.notify()

	go func() {
		res, runErr := executeCodeReviewChecks(ctx, runner, projectDir, pkgs, pendingGo, prevTotal, func(prog CodeReviewCheckProgress) {
			e.postOrRun(func() {
				e.codeReview.checkMu.Lock()
				stale := e.codeReview.checkGen != checkGen
				e.codeReview.checkMu.Unlock()
				if stale || e.codeReview.session != sess || sess.EditGen != editGen {
					return
				}
				sess.Checks.Completed = prog.Completed
				if prog.Total > 0 {
					sess.Checks.ExpectedTotal = prog.Total
				}
				e.codeReview.rev++
				e.notify()
			})
		})
		e.postOrRun(func() {
			defer finishDone()
			cancel()
			e.codeReview.checkMu.Lock()
			stale := e.codeReview.checkGen != checkGen
			if !stale {
				e.codeReview.checkCancel = nil
				e.codeReview.checkDone = nil
				e.codeReview.checkDoneClose = nil
			}
			e.codeReview.checkMu.Unlock()
			if stale {
				return
			}
			_ = e.applyCodeReviewChecksResult(sess, editGen, res, runErr)
		})
	}()
	return nil
}

func executeCodeReviewChecks(
	ctx context.Context,
	runner func(context.Context, CodeReviewCheckRequest, func(CodeReviewCheckProgress)) (CodeReviewCheckResult, error),
	projectDir string,
	pkgs []string,
	pendingGo map[string]PendingGoOverlayFile,
	prevTotal int,
	onProgress func(CodeReviewCheckProgress),
) (CodeReviewCheckResult, error) {
	var overlayPath string
	var cleanup func()
	if projectDir != "" && len(pendingGo) > 0 {
		var err error
		overlayPath, cleanup, err = BuildGoOverlay(projectDir, pendingGo)
		if err != nil {
			return CodeReviewCheckResult{
				Phase:      CodeReviewChecksFailed,
				CheckError: err.Error(),
			}, err
		}
		defer cleanup()
	}

	req := CodeReviewCheckRequest{
		ProjectDir:    projectDir,
		Packages:      pkgs,
		PendingGo:     pendingGo,
		OverlayPath:   overlayPath,
		PreviousTotal: prevTotal,
	}
	if runner != nil {
		return runner(ctx, req, onProgress)
	}
	return runDefaultCodeReviewChecks(ctx, req, onProgress)
}

func (e *Editor) applyCodeReviewChecksResult(sess *CodeReviewSession, editGen uint64, res CodeReviewCheckResult, runErr error) error {
	if e == nil || e.codeReview.session != sess || sess.EditGen != editGen {
		return nil
	}
	if runErr != nil && res.CheckError == "" && len(res.Failures) == 0 {
		res.CheckError = runErr.Error()
		res.Phase = CodeReviewChecksFailed
	}
	if len(res.Packages) == 0 {
		res.Packages = sess.goPackages()
	}
	if res.TotalCount == 0 {
		res.TotalCount = res.PassedCount + res.FailedCount
	}
	res.Generation = editGen
	if res.Phase == "" || res.Phase == CodeReviewChecksRunning || res.Phase == CodeReviewChecksFixing {
		if res.CheckPassed && res.TestPassed && res.FailedCount == 0 && res.CheckError == "" {
			res.Phase = CodeReviewChecksPassed
		} else {
			res.Phase = CodeReviewChecksFailed
		}
	}
	// Attach stored agent explanation & suspect line to the first failure if not already populated by runner.
	if res.Phase == CodeReviewChecksFailed && len(res.Failures) > 0 {
		if res.Failures[0].Explanation == "" && sess.FailureExplanation != "" {
			res.Failures[0].Explanation = sess.FailureExplanation
		}
		if res.Failures[0].SuspectFile == "" && sess.FailureSuspectFile != "" {
			res.Failures[0].SuspectFile = sess.FailureSuspectFile
		}
		if res.Failures[0].SuspectLine == 0 && sess.FailureSuspectLine > 0 {
			res.Failures[0].SuspectLine = sess.FailureSuspectLine
		}
	}
	sess.Checks = res
	sess.FixRequested = false

	// Complete any running fix step in the turn.
	if turn := e.findAgentTurn(sess.TurnID); turn != nil {
		for i := range turn.Steps {
			if turn.Steps[i].ToolName == "code_review_fix" && turn.Steps[i].Status == AgentStepRunning {
				if res.Phase == CodeReviewChecksPassed {
					turn.Steps[i].Status = AgentStepDone
				} else {
					turn.Steps[i].Status = AgentStepFailed
				}
			}
		}
	}

	e.recomputeCodeReviewDiffs()
	e.codeReview.rev++
	e.notify()
	return runErr
}

// KeepCodeReview writes all unapplied YAML files plus all confirmed Go files to disk atomically as one undo step.
// Unconfirmed Go files are NEVER included in Keep: if any Go files remain unconfirmed, Keep writes only the YAML
// (and any confirmed Go) files, closes the review screen, and leaves a waiting card in the Agent panel.
func (e *Editor) KeepCodeReview() error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	if e.playing {
		return ErrPlaying
	}
	if e.HistoryPreviewActive() {
		return errors.New("editor: exit history preview before keeping code review changes")
	}
	sess := e.codeReview.session
	projectDir := e.codeReviewProjectRoot()
	if projectDir == "" {
		return errors.New("editor: no project directory is open")
	}

	// Gather unapplied YAML files and confirmed unapplied Go files.
	var batch []*CodeReviewFile
	var yamlNames []string
	var confirmedGoNames []string
	totalPending := 0
	for _, f := range sess.Files {
		if f.Applied {
			continue
		}
		totalPending++
		if f.Kind == CodeReviewFileYAML {
			batch = append(batch, f)
			yamlNames = append(yamlNames, f.BaseName())
		} else if f.Kind == CodeReviewFileGo && f.Confirmed {
			batch = append(batch, f)
			confirmedGoNames = append(confirmedGoNames, f.BaseName())
		}
	}
	if len(batch) == 0 {
		return ErrCodeReviewNothingToKeep
	}

	entries := make([]codeReviewFileWrite, 0, len(batch))
	touchedFiles := make([]string, 0, len(batch))
	beforeFiles := make(map[string]historyFileSnapshot, len(batch))
	afterFiles := make(map[string]historyFileSnapshot, len(batch))
	for _, f := range batch {
		abs, err := resolveCodeReviewAbsPath(projectDir, f.Path)
		if err != nil {
			return err
		}
		entries = append(entries, codeReviewFileWrite{
			relPath:  f.Path,
			absPath:  abs,
			existed:  f.Existed,
			original: []byte(f.Original),
			pending:  []byte(f.Pending),
			delete:   f.Delete,
			file:     f,
		})
		touchedFiles = append(touchedFiles, f.Path)
		beforeFiles[f.Path] = historyFileSnapshot{
			exists: f.Existed,
			data:   []byte(f.Original),
		}
		if f.Delete {
			afterFiles[f.Path] = historyFileSnapshot{exists: false}
		} else {
			afterFiles[f.Path] = historyFileSnapshot{
				exists: true,
				data:   []byte(f.Pending),
			}
		}
	}

	label := buildCodeReviewKeepLabel(yamlNames, confirmedGoNames)
	summary := label
	if p := strings.TrimSpace(sess.Prompt); p != "" {
		summary = p
	}
	cmd := &codeReviewApplyCmd{
		ed:      e,
		label:   label,
		entries: entries,
	}
	prevMeta := e.historyPanel.pendingMeta
	e.historyPanel.pendingMeta = &HistoryMeta{
		Author:      HistoryAuthorAgent,
		Summary:     summary,
		Detail:      formatAgentTurnDetail("", totalPending, len(batch)),
		Files:       touchedFiles,
		ChangeCount: totalPending,
		KeptCount:   len(batch),
		BeforeFiles: beforeFiles,
		AfterFiles:  afterFiles,
	}
	execErr := e.History.execute(cmd)
	e.historyPanel.pendingMeta = prevMeta
	if execErr != nil {
		sess.LastErr = execErr.Error()
		e.codeReview.rev++
		e.notify()
		return execErr
	}

	waitingGo := sess.WaitingGoFiles()
	toastMsg := buildCodeReviewKeepToast(yamlNames, confirmedGoNames, len(waitingGo))
	sess.LastKeepSummary = toastMsg

	if turn := e.findAgentTurn(sess.TurnID); turn != nil {
		summary := toastMsg + " · one undo step"
		if turn.Summary != "" {
			turn.Summary += "\n" + summary
		} else {
			turn.Summary = summary
		}
	}

	if len(waitingGo) > 0 {
		sess.Open = false
		sess.Paused = false
		sess.KeptYAML = true
		sess.SelectedPath = waitingGo[0].Path
	} else {
		e.cancelCodeReviewChecks()
		e.codeReview.session = nil
	}

	e.ShowToast(Toast{
		ID:             "code-review-kept",
		Kind:           ToastSuccess,
		Message:        toastMsg,
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction: func() {
			e.Undo()
		},
	})
	e.codeReview.rev++
	e.notify()
	return nil
}

// RevertCodeReview discards all unapplied pending YAML and Go changes and closes the review without touching disk.
func (e *Editor) RevertCodeReview() error {
	if e == nil || e.codeReview.session == nil {
		return ErrNoCodeReview
	}
	sess := e.codeReview.session
	var names []string
	for _, f := range sess.Files {
		if !f.Applied {
			names = append(names, f.BaseName())
		}
	}
	e.cancelCodeReviewChecks()
	e.codeReview.session = nil

	msg := "Reverted " + joinHumanList(names)
	if len(names) == 0 {
		msg = "Reverted review"
	}
	if turn := e.findAgentTurn(sess.TurnID); turn != nil {
		if turn.Summary != "" {
			turn.Summary += "\n" + msg
		} else {
			turn.Summary = msg
		}
	}
	e.ShowToast(Toast{
		ID:      "code-review-reverted",
		Kind:    ToastInfo,
		Message: msg,
	})
	e.codeReview.rev++
	e.notify()
	return nil
}

// codeReviewFileWrite records one file written atomically by KeepCodeReview so Undo restores disk cleanly.
type codeReviewFileWrite struct {
	relPath     string
	absPath     string
	existed     bool
	original    []byte
	pending     []byte
	delete      bool
	file        *CodeReviewFile
	createdDirs []string
}

type codeReviewApplyCmd struct {
	ed      *Editor
	label   string
	entries []codeReviewFileWrite
}

func (c *codeReviewApplyCmd) Label() string { return c.label }
func (c *codeReviewApplyCmd) Do()           { _ = c.do() }
func (c *codeReviewApplyCmd) Undo()         { _ = c.undo() }

func (c *codeReviewApplyCmd) do() error {
	written := 0
	for i := range c.entries {
		en := &c.entries[i]
		if en.delete {
			if err := os.Remove(en.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				c.rollbackDo(written)
				return err
			}
		} else {
			created, err := mkdirAllTracking(filepath.Dir(en.absPath))
			if err != nil {
				c.rollbackDo(written)
				return err
			}
			en.createdDirs = created
			if err := writeFileAtomic(en.absPath, en.pending); err != nil {
				removeEmptyDirsReverse(en.createdDirs)
				en.createdDirs = nil
				c.rollbackDo(written)
				return err
			}
		}
		if en.file != nil {
			en.file.Applied = true
		}
		written++
	}
	c.syncOpenEditorFiles()
	return nil
}

func (c *codeReviewApplyCmd) rollbackDo(count int) {
	for i := count - 1; i >= 0; i-- {
		en := &c.entries[i]
		if en.existed {
			_ = writeFileAtomic(en.absPath, en.original)
		} else {
			_ = os.Remove(en.absPath)
			removeEmptyDirsReverse(en.createdDirs)
		}
		en.createdDirs = nil
		if en.file != nil {
			en.file.Applied = false
		}
	}
}

func (c *codeReviewApplyCmd) undo() error {
	for i := len(c.entries) - 1; i >= 0; i-- {
		en := &c.entries[i]
		if en.existed {
			if err := os.MkdirAll(filepath.Dir(en.absPath), 0o755); err != nil {
				return err
			}
			if err := writeFileAtomic(en.absPath, en.original); err != nil {
				return err
			}
		} else {
			if err := os.Remove(en.absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			removeEmptyDirsReverse(en.createdDirs)
		}
		if en.file != nil {
			en.file.Applied = false
		}
	}
	if c.ed != nil && c.ed.codeReview.session != nil {
		sess := c.ed.codeReview.session
		if len(sess.YAMLFiles()) > 0 {
			sess.KeptYAML = false
			if !sess.Open {
				sess.Paused = true
			}
		}
	}
	c.syncOpenEditorFiles()
	return nil
}

func (c *codeReviewApplyCmd) syncOpenEditorFiles() {
	if c.ed == nil {
		return
	}
	if c.ed.FilePath != "" {
		if openAbs, err := filepath.Abs(c.ed.FilePath); err == nil {
			for _, en := range c.entries {
				if en.absPath == openAbs {
					if data, readErr := os.ReadFile(openAbs); readErr == nil {
						if doc, parseErr := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: c.ed.components}); parseErr == nil {
							c.ed.Doc = doc
							c.ed.Rebuild()
						}
					}
					break
				}
			}
		}
	}
	if c.ed.ProjectPath != "" {
		if projAbs, err := filepath.Abs(c.ed.ProjectPath); err == nil {
			for _, en := range c.entries {
				if en.absPath == projAbs {
					if data, readErr := os.ReadFile(projAbs); readErr == nil {
						if proj, parseErr := project.Parse(data); parseErr == nil {
							c.ed.Project = proj
						}
					}
					break
				}
			}
		}
	}
	for _, tab := range c.ed.tabs {
		if tab == nil || tab.filePath == "" || tab.filePath == c.ed.FilePath {
			continue
		}
		tabAbs, err := filepath.Abs(tab.filePath)
		if err != nil {
			continue
		}
		for _, en := range c.entries {
			if en.absPath == tabAbs {
				if data, readErr := os.ReadFile(tabAbs); readErr == nil {
					if doc, parseErr := sceneformat.ParseWith(data, sceneformat.ParseOptions{Components: c.ed.components}); parseErr == nil {
						tab.doc = doc
					}
				}
				break
			}
		}
	}
	c.ed.filesRev++
	c.ed.assetsRev++
	c.ed.invalidateProjectIndex()
	c.ed.snapshotProjectTree()
	c.ed.syncDiskWatchTargets()
	c.ed.codeReview.rev++
	c.ed.notify()
}

func mkdirAllTracking(dir string) ([]string, error) {
	if dir == "" || dir == "." {
		return nil, nil
	}
	var missing []string
	cur := dir
	for {
		if _, err := os.Stat(cur); err == nil {
			break
		}
		missing = append(missing, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	if len(missing) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	slices.Reverse(missing)
	return missing, nil
}

func removeEmptyDirsReverse(dirs []string) {
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i])
	}
}

func buildCodeReviewKeepLabel(yamlNames, goNames []string) string {
	var parts []string
	if len(yamlNames) > 0 {
		parts = append(parts, "Keep "+joinHumanList(yamlNames))
	}
	if len(goNames) > 0 {
		parts = append(parts, "confirm "+joinHumanList(goNames))
	}
	if len(parts) == 0 {
		return "Keep review"
	}
	return strings.Join(parts, " · ")
}

func buildCodeReviewKeepToast(yamlNames, goNames []string, waitingGo int) string {
	var parts []string
	if len(yamlNames) > 0 {
		parts = append(parts, "Kept "+joinHumanList(yamlNames))
	}
	if len(goNames) > 0 {
		parts = append(parts, "confirmed "+joinHumanList(goNames))
	}
	if waitingGo == 1 {
		parts = append(parts, "1 Go file still needs your OK")
	} else if waitingGo > 1 {
		parts = append(parts, fmt.Sprintf("%d Go files still need your OK", waitingGo))
	}
	return strings.Join(parts, " · ")
}

func joinHumanList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

// --- Pending Go overlay helpers & diff engine ---

func (s *CodeReviewSession) pendingGoMap() map[string]PendingGoOverlayFile {
	out := make(map[string]PendingGoOverlayFile)
	for _, f := range s.Files {
		if f.Kind == CodeReviewFileGo && !f.Applied {
			out[f.Path] = PendingGoOverlayFile{
				Content: f.Pending,
				Delete:  f.Delete,
			}
		}
	}
	return out
}

func (s *CodeReviewSession) goPackages() []string {
	seen := make(map[string]bool)
	var pkgs []string
	for _, f := range s.Files {
		if f.Kind != CodeReviewFileGo || f.Applied {
			continue
		}
		dir := path.Dir(f.Path)
		pkg := "."
		if dir != "." && dir != "" {
			pkg = "./" + dir
		}
		if !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	if len(pkgs) == 0 {
		return []string{"."}
	}
	slices.Sort(pkgs)
	return pkgs
}

func (s *CodeReviewSession) matchFailureFile(file, pkg string) *CodeReviewFile {
	if s == nil || file == "" {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(file, `\`, "/"))
	for _, f := range s.Files {
		if f.Path == clean {
			return f
		}
	}
	base := path.Base(clean)
	pkgSuffix := strings.TrimPrefix(pkg, "./")
	for _, f := range s.Files {
		if f.BaseName() == base {
			if pkgSuffix == "" || pkgSuffix == "." || strings.HasSuffix(path.Dir(f.Path), pkgSuffix) {
				return f
			}
		}
	}
	for _, f := range s.Files {
		if f.BaseName() == base {
			return f
		}
	}
	return nil
}

func (e *Editor) recomputeCodeReviewDiffs() {
	if e == nil || e.codeReview.session == nil {
		return
	}
	sess := e.codeReview.session
	failure := sess.Checks.PrimaryFailure()
	var failedTestFile *CodeReviewFile
	var suspectFile *CodeReviewFile
	var suspectLine int
	if sess.Checks.Phase == CodeReviewChecksFailed && failure != nil {
		failedTestFile = sess.matchFailureFile(failure.File, failure.Package)
		if failure.SuspectFile != "" {
			suspectFile = sess.matchFailureFile(failure.SuspectFile, failure.Package)
			suspectLine = failure.SuspectLine
		} else if sess.FailureSuspectFile != "" {
			suspectFile = sess.matchFailureFile(sess.FailureSuspectFile, failure.Package)
			suspectLine = sess.FailureSuspectLine
		}
		if suspectFile == nil && failure.Explanation != "" {
			if ln := extractMentionedLineNumber(failure.Explanation); ln > 0 {
				for _, gf := range sess.GoFiles() {
					if !strings.HasSuffix(gf.Path, "_test.go") {
						suspectFile = gf
						suspectLine = ln
						break
					}
				}
			}
		}
	}

	for _, f := range sess.Files {
		lines, added, removed := computeCodeReviewDiff(f.Original, f.Pending, f.Existed, f.Delete)
		// Compute lines modified in the latest round relative to PreviousRound.
		var roundChanged map[int]bool
		if sess.Round > 1 && f.EditedInRound == sess.Round && f.PreviousRound != "" {
			roundChanged = changedNewLineSet(f.PreviousRound, f.Pending)
		}
		for i := range lines {
			l := &lines[i]
			l.Tag = CodeReviewTagNone
			if l.NewLine > 0 {
				if sess.Checks.Phase == CodeReviewChecksFailed && failure != nil {
					if failedTestFile == f && failure.Line == l.NewLine {
						l.Tag = CodeReviewTagFailedHere
						continue
					}
					if suspectFile == f && suspectLine == l.NewLine {
						l.Tag = CodeReviewTagFailsTest
						continue
					}
				}
				if roundChanged[l.NewLine] {
					if sess.Checks.Phase == CodeReviewChecksFixing {
						l.Tag = CodeReviewTagAgentEdited
					} else if sess.Round == 2 {
						l.Tag = CodeReviewTagRound2
					} else {
						l.Tag = CodeReviewLineTag(fmt.Sprintf("round %d", sess.Round))
					}
				}
			}
		}
		f.Diff = lines
		f.AddedLines = added
		f.RemovedLines = removed
	}
}

var lineNumberMentionRe = regexp.MustCompile(`\bline\s+(\d+)\b`)

func extractMentionedLineNumber(text string) int {
	m := lineNumberMentionRe.FindStringSubmatch(strings.ToLower(text))
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(text, "\n")
	lines := strings.Split(trimmed, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

// computeCodeReviewDiff produces a deterministic unified diff with 1-based OldLine and NewLine numbers.
// For brand-new files, every line is returned as DiffLineAdd; for modified files, hunks include 3 lines of context.
func computeCodeReviewDiff(oldText, newText string, existed, deleted bool) ([]CodeReviewDiffLine, int, int) {
	if deleted {
		oldLines := splitDiffLines(oldText)
		out := make([]CodeReviewDiffLine, len(oldLines))
		for i, line := range oldLines {
			out[i] = CodeReviewDiffLine{
				OldLine: i + 1,
				Sign:    "−",
				Text:    line,
				Kind:    DiffLineDel,
			}
		}
		return out, 0, len(oldLines)
	}
	if !existed {
		newLines := splitDiffLines(newText)
		out := make([]CodeReviewDiffLine, len(newLines))
		for i, line := range newLines {
			out[i] = CodeReviewDiffLine{
				NewLine: i + 1,
				Sign:    "+",
				Text:    line,
				Kind:    DiffLineAdd,
			}
		}
		return out, len(newLines), 0
	}

	a := splitDiffLines(oldText)
	b := splitDiffLines(newText)
	full := lcsDiffLines(a, b)
	added, removed := 0, 0
	for _, l := range full {
		switch l.Kind {
		case DiffLineAdd:
			added++
		case DiffLineDel:
			removed++
		}
	}
	return trimDiffContext(full, 3), added, removed
}

func lcsDiffLines(a, b []string) []CodeReviewDiffLine {
	n, m := len(a), len(b)
	// Fast prefix/suffix trim so large files diff quickly.
	prefix := 0
	for prefix < n && prefix < m && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < n-prefix && suffix < m-prefix && a[n-1-suffix] == b[m-1-suffix] {
		suffix++
	}

	var out []CodeReviewDiffLine
	for i := 0; i < prefix; i++ {
		out = append(out, CodeReviewDiffLine{
			OldLine: i + 1,
			NewLine: i + 1,
			Text:    a[i],
			Kind:    DiffLineContext,
		})
	}

	midA := a[prefix : n-suffix]
	midB := b[prefix : m-suffix]
	na, nb := len(midA), len(midB)

	// Guard DP table size for very large files: fall back to direct del/add block if na*nb > 2_000_000.
	if na*nb > 2_000_000 {
		for i, line := range midA {
			out = append(out, CodeReviewDiffLine{OldLine: prefix + i + 1, Sign: "−", Text: line, Kind: DiffLineDel})
		}
		for j, line := range midB {
			out = append(out, CodeReviewDiffLine{NewLine: prefix + j + 1, Sign: "+", Text: line, Kind: DiffLineAdd})
		}
	} else {
		dp := make([][]int, na+1)
		for i := range dp {
			dp[i] = make([]int, nb+1)
		}
		for i := na - 1; i >= 0; i-- {
			for j := nb - 1; j >= 0; j-- {
				if midA[i] == midB[j] {
					dp[i][j] = dp[i+1][j+1] + 1
				} else if dp[i+1][j] >= dp[i][j+1] {
					dp[i][j] = dp[i+1][j]
				} else {
					dp[i][j] = dp[i][j+1]
				}
			}
		}
		i, j := 0, 0
		for i < na && j < nb {
			if midA[i] == midB[j] {
				out = append(out, CodeReviewDiffLine{
					OldLine: prefix + i + 1,
					NewLine: prefix + j + 1,
					Text:    midA[i],
					Kind:    DiffLineContext,
				})
				i++
				j++
			} else if dp[i+1][j] >= dp[i][j+1] {
				out = append(out, CodeReviewDiffLine{
					OldLine: prefix + i + 1,
					Sign:    "−",
					Text:    midA[i],
					Kind:    DiffLineDel,
				})
				i++
			} else {
				out = append(out, CodeReviewDiffLine{
					NewLine: prefix + j + 1,
					Sign:    "+",
					Text:    midB[j],
					Kind:    DiffLineAdd,
				})
				j++
			}
		}
		for ; i < na; i++ {
			out = append(out, CodeReviewDiffLine{
				OldLine: prefix + i + 1,
				Sign:    "−",
				Text:    midA[i],
				Kind:    DiffLineDel,
			})
		}
		for ; j < nb; j++ {
			out = append(out, CodeReviewDiffLine{
				NewLine: prefix + j + 1,
				Sign:    "+",
				Text:    midB[j],
				Kind:    DiffLineAdd,
			})
		}
	}

	for k := 0; k < suffix; k++ {
		oi := n - suffix + k
		ni := m - suffix + k
		out = append(out, CodeReviewDiffLine{
			OldLine: oi + 1,
			NewLine: ni + 1,
			Text:    a[oi],
			Kind:    DiffLineContext,
		})
	}
	return out
}

func trimDiffContext(lines []CodeReviewDiffLine, radius int) []CodeReviewDiffLine {
	if len(lines) == 0 {
		return nil
	}
	keep := make([]bool, len(lines))
	anyChange := false
	for i, l := range lines {
		if l.Kind != DiffLineContext {
			anyChange = true
			lo := max(0, i-radius)
			hi := min(len(lines)-1, i+radius)
			for k := lo; k <= hi; k++ {
				keep[k] = true
			}
		}
	}
	if !anyChange {
		if len(lines) > 20 {
			return lines[:20]
		}
		return lines
	}
	out := make([]CodeReviewDiffLine, 0, len(lines))
	for i, l := range lines {
		if keep[i] {
			out = append(out, l)
		}
	}
	return out
}

func changedNewLineSet(prevText, curText string) map[int]bool {
	diff := lcsDiffLines(splitDiffLines(prevText), splitDiffLines(curText))
	out := make(map[int]bool)
	for _, l := range diff {
		if l.Kind == DiffLineAdd && l.NewLine > 0 {
			out[l.NewLine] = true
		}
	}
	return out
}

func firstChangedNewLine(prevText, curText string) int {
	diff := lcsDiffLines(splitDiffLines(prevText), splitDiffLines(curText))
	for _, l := range diff {
		if l.Kind == DiffLineAdd && l.NewLine > 0 {
			return l.NewLine
		}
	}
	return 0
}

// BuildCodeReviewSplitRows transforms unified diff lines into side-by-side Split rows matching ReviewCode.dc.html.
func BuildCodeReviewSplitRows(lines []CodeReviewDiffLine) []CodeReviewSplitRow {
	var out []CodeReviewSplitRow
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if l.Kind == DiffLineContext {
			out = append(out, CodeReviewSplitRow{
				OldLine:   l.OldLine,
				LeftText:  l.Text,
				LeftKind:  DiffLineContext,
				NewLine:   l.NewLine,
				RightText: l.Text,
				RightKind: DiffLineContext,
				RightTag:  l.Tag,
			})
			continue
		}
		var dels, adds []CodeReviewDiffLine
		for i < len(lines) && lines[i].Kind == DiffLineDel {
			dels = append(dels, lines[i])
			i++
		}
		for i < len(lines) && lines[i].Kind == DiffLineAdd {
			adds = append(adds, lines[i])
			i++
		}
		i--
		n := max(len(dels), len(adds))
		for k := 0; k < n; k++ {
			var row CodeReviewSplitRow
			if k < len(dels) {
				row.OldLine = dels[k].OldLine
				row.LeftText = dels[k].Text
				row.LeftKind = DiffLineDel
			}
			if k < len(adds) {
				row.NewLine = adds[k].NewLine
				row.RightText = adds[k].Text
				row.RightKind = DiffLineAdd
				row.RightTag = adds[k].Tag
			}
			out = append(out, row)
		}
	}
	return out
}
