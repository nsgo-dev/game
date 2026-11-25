package editor

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/packages/game/loaderr"
	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
)

// problemState holds the asynchronous project-check state for an Editor.
// Its fields are guarded by mu so background check goroutines and callers on
// any goroutine can read or publish results without racing with UI state.
type problemState struct {
	mu          sync.Mutex
	gen         uint64
	running     bool
	hasRun      bool
	projectPath string
	problems    []assetcheck.Problem
	checkErr    error
	done        chan struct{}
	scope       *core.Scope
	// checkFn overrides runProjectCheck in internal tests; nil uses runProjectCheck.
	checkFn func(dir string, reg *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error)
}

func (p *problemState) setScope(cx *core.Scope) {
	p.mu.Lock()
	p.scope = cx
	p.mu.Unlock()
}

// CheckProblems runs assetcheck.CheckWith (using the editor's component registry)
// and project.Check over the open project asynchronously off the UI goroutine.
// Each call supersedes any in-flight check so stale results never overwrite newer
// ones. It is a no-op when no project is open.
func (e *Editor) CheckProblems() {
	if e.Project == nil || e.ProjectPath == "" {
		e.problems.mu.Lock()
		e.problems.gen++
		e.problems.running = false
		e.problems.hasRun = false
		e.problems.projectPath = ""
		e.problems.problems = nil
		e.problems.checkErr = nil
		e.problems.done = nil
		e.problems.mu.Unlock()
		return
	}

	projectPath := e.ProjectPath
	dir := filepath.Dir(projectPath)
	reg := e.components

	myDone := make(chan struct{})
	e.problems.mu.Lock()
	e.problems.gen++
	gen := e.problems.gen
	if e.problems.projectPath != projectPath {
		e.problems.projectPath = projectPath
		e.problems.problems = nil
		e.problems.checkErr = nil
		e.problems.hasRun = false
	}
	e.problems.running = true
	e.problems.done = myDone
	fn := e.problems.checkFn
	e.problems.mu.Unlock()
	if fn == nil {
		fn = runProjectCheck
	}

	go func() {
		probs, err := fn(dir, reg)

		e.problems.mu.Lock()
		if e.problems.gen != gen {
			e.problems.mu.Unlock()
			close(myDone)
			return
		}
		e.problems.problems = probs
		e.problems.checkErr = err
		e.problems.running = false
		e.problems.hasRun = true
		scope := e.problems.scope
		e.problems.mu.Unlock()

		if scope != nil {
			scope.Post(func() {
				e.notify()
			})
		}
		close(myDone)
	}()
}

// Problems returns a copy of the problems found by the last completed project
// check, sorted by file, line and message. It is safe to call from any goroutine.
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

// ProblemCount returns the number of problems found by the last completed project
// check. It is safe to call from any goroutine.
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

// CheckingProblems reports whether a background project check is in flight.
// It is safe to call from any goroutine.
func (e *Editor) CheckingProblems() bool {
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return e.problems.running
}

// WaitForProblems blocks until any in-flight project check (including newer
// checks started while waiting) finishes. It returns immediately when idle.
func (e *Editor) WaitForProblems() {
	for {
		e.problems.mu.Lock()
		running := e.problems.running
		done := e.problems.done
		e.problems.mu.Unlock()
		if !running || done == nil {
			return
		}
		<-done
	}
}

// SelectFile selects relPath, a slash-separated path from the open project's
// root, in the file browser.
func (e *Editor) SelectFile(relPath string) {
	e.selectFile(relPath)
	e.notify()
}

func (e *Editor) selectFile(relPath string) {
	e.SelectedFile = relPath
	e.selectedFileRev++
}

// OpenProblem navigates to problem p in the open project: if p.File names a scene file (*.scene.yaml, *.scene.yml, or a scene listed under Project.Autoload.Scenes), it opens that scene with OpenScene (and updates ScenePath and SelectedFile, and clears OnSave), unless it is the scene already open (ScenePath), which keeps its edits and undo history and is only selected; if opening the scene fails (for example because the YAML does not parse), it still selects p.File in the file browser and returns the error. For any non-scene file, it selects p.File in the file browser. It returns ErrNoProject when no project is open. Opening another scene loses the open one's unsaved edits; ProblemsView asks first (HasUnsavedChanges).
func (e *Editor) OpenProblem(p assetcheck.Problem) error {
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if p.File == "" {
		return nil
	}
	if isProjectScene(e.Project, p.File) && p.File != e.ScenePath {
		// OpenScene, as the file browser opens scenes, so OnSave, which persisted the scene open before, does not get this one.
		if err := e.OpenScene(p.File); err != nil {
			e.selectFile(p.File)
			e.notify()
			return err
		}
		e.selectFile(p.File)
		if p.Line > 0 {
			e.Status = fmt.Sprintf("Opened %s:%d", p.File, p.Line)
		} else {
			e.Status = "Opened " + p.File
		}
		e.notify()
		return nil
	}
	e.selectFile(p.File)
	if p.Line > 0 {
		e.Status = fmt.Sprintf("Selected %s:%d", p.File, p.Line)
	} else {
		e.Status = "Selected " + p.File
	}
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
	running := e.problems.running
	hasRun := e.problems.hasRun
	err := e.problems.checkErr
	e.problems.mu.Unlock()
	if err != nil {
		return "Problems: error"
	}
	if running && !hasRun {
		return "Problems: checking…"
	}
	return "Problems: " + strconv.Itoa(count)
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

// runProjectCheck runs assetcheck.CheckWith (with reg) and project.Check over
// dir, combining and sorting their problems without duplicating faults that
// assetcheck already reported with file and line.
func runProjectCheck(dir string, reg *sceneformat.ComponentRegistry) ([]assetcheck.Problem, error) {
	fsys := os.DirFS(dir)
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
		return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Message, b.Message))
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
			out = append(out, assetcheck.Problem{
				File:    project.FileName,
				Line:    errorLine(sub),
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
		out = append(out, assetcheck.Problem{
			File:    file,
			Line:    errorLine(sub),
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
			return assetcheck.Problem{
				File:    s.Path,
				Line:    errorLine(err),
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

func errorLine(err error) int {
	if le, ok := errors.AsType[*loaderr.Error](err); ok && le.Line > 0 {
		return le.Line
	}
	return 0
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
