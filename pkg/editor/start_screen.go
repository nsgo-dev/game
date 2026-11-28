package editor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/game/internal/gotoolchain"
	"github.com/nsgo-dev/nsgo/core"
)

// ErrCreateProjectRunning is returned by StartCreateProject when a project creation command is already running.
var ErrCreateProjectRunning = errors.New("editor: project creation is already running")

// ProjectTemplate describes one project template offered on the editor's Start Screen,
// populated dynamically from `game new`'s template registry.
type ProjectTemplate struct {
	// Name is the template identifier passed to `game new -template` (such as "starter").
	Name string
	// Title is the human-readable card title; derived from Name when empty.
	Title string
	// Summary is the one-line description shown on the template card.
	Summary string
	// Dimension classifies the template for Start Screen filter chips: "2d" or "3d" (inferred when empty).
	Dimension   string
	Genre       string
	Orientation string
	Inputs      []string
	PlayersMin  int
	PlayersMax  int
	// Default marks the template selected initially.
	Default bool
}

// DisplayTitle returns Title when set, or a readable title derived from Name.
func (t ProjectTemplate) DisplayTitle() string {
	if strings.TrimSpace(t.Title) != "" {
		return t.Title
	}
	if t.Name == "" {
		return "Template"
	}
	parts := strings.Split(t.Name, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		lower := strings.ToLower(p)
		switch lower {
		case "2d", "3d", "rpg", "fps", "rts":
			parts[i] = strings.ToUpper(lower)
		default:
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

// EffectiveDimension returns "3d" or "2d" for Start Screen template filtering.
func (t ProjectTemplate) EffectiveDimension() string {
	d := strings.ToLower(strings.TrimSpace(t.Dimension))
	if d == "2d" || d == "3d" {
		return d
	}
	return "2d"
}

// CreateProjectRequest describes a new project to scaffold from a template and open in the editor.
type CreateProjectRequest struct {
	// Name is the project name (also used as the directory name inside ParentDir when Dir is empty).
	Name string
	// ParentDir is the parent folder containing the new project folder (defaults to ~/games when Dir is empty).
	ParentDir string
	// Dir is the explicit target directory; when empty, filepath.Join(ParentDir, Name) is used.
	Dir string
	// Module is the optional Go module path passed to `game new -module`.
	Module string
	// Template is the template identifier passed to `game new -template` (defaults to the first registered template).
	Template string
}

// TargetDir returns the full directory path where the new project will be created.
func (r CreateProjectRequest) TargetDir() string {
	if strings.TrimSpace(r.Dir) != "" {
		return filepath.Clean(expandHomePath(strings.TrimSpace(r.Dir)))
	}
	parent := strings.TrimSpace(r.ParentDir)
	if parent == "" {
		parent = defaultProjectsParentDir()
	} else {
		parent = expandHomePath(parent)
	}
	return filepath.Join(filepath.Clean(parent), strings.TrimSpace(r.Name))
}

// StartNameValidation summarizes live validation of the Start Screen's New Project name, folder, and module inputs.
type StartNameValidation struct {
	// OK reports whether the current inputs are valid and ready to create a project.
	OK bool
	// TargetDir is the resolved full target directory path.
	TargetDir string
	// DisplayPath is TargetDir shortened with "~" when inside the user's home directory.
	DisplayPath string
	// Exists reports whether TargetDir already exists on disk.
	Exists bool
	// SuggestedName is the next free sibling folder name (such as "dungeon-run-2") when Exists is true.
	SuggestedName string
	// InvalidChars reports whether Name violates the lowercase a-z, 0-9, and - rule.
	InvalidChars bool
	// NotWritable reports whether the target parent directory is not writable.
	NotWritable bool
	// Message is the human-readable validation status or error text.
	Message string
}

// PendingProjectAction describes a project switch waiting for confirmation because the open session has unsaved edits.
type PendingProjectAction struct {
	// Kind is "open", "create", or "demo".
	Kind string
	// Target is the path or name being opened (project directory or "Demo Room").
	Target string
	// Summary is a human-readable description shown in the confirmation dialog.
	Summary string
}

type unsavedPromptState struct {
	open    bool
	pending PendingProjectAction
	run     func() error
}

type createProjectRun struct {
	cancel        context.CancelFunc
	done          chan struct{}
	once          sync.Once
	finishOnce    sync.Once
	pendingFinish func()
}

func (r *createProjectRun) finish() {
	r.once.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		close(r.done)
	})
}

func (r *createProjectRun) runPendingFinish() {
	if r == nil || r.pendingFinish == nil {
		return
	}
	r.finishOnce.Do(func() {
		defer r.finish()
		r.pendingFinish()
	})
}

type startScreenState struct {
	mu                 sync.Mutex
	open               bool
	hasEnteredEditor   bool
	templates          []ProjectTemplate
	filter             string // "all", "2d", "3d"
	selectedTemplate   string
	idea               string
	aiNotice           string
	name               string
	parentDir          string
	module             string
	validation         StartNameValidation
	writableCheckedDir string
	writableResult     bool
	recentRev          uint64
	createOutput       []string
	createErr          error
	creating           bool
	run                *createProjectRun
	scope              *core.Scope
	initialized        bool
}

func (s *startScreenState) setScope(cx *core.Scope) {
	s.mu.Lock()
	s.scope = cx
	var run *createProjectRun
	if cx == nil && s.run != nil && s.run.pendingFinish != nil {
		run = s.run
	}
	s.mu.Unlock()
	if run != nil {
		run.runPendingFinish()
	}
}

func defaultProjectsParentDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "games"
	}
	return filepath.Join(home, "games")
}

func expandHomePath(p string) string {
	if p == "" {
		return ""
	}
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

func shortenHomePath(p string) string {
	if p == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	cleanHome := filepath.Clean(home)
	cleanP := filepath.Clean(p)
	if cleanP == cleanHome {
		return "~"
	}
	prefix := cleanHome + string(filepath.Separator)
	if strings.HasPrefix(cleanP, prefix) {
		return "~" + string(filepath.Separator) + strings.TrimPrefix(cleanP, prefix)
	}
	return p
}

func (e *Editor) ensureStartScreenInit() {
	if e == nil {
		return
	}
	e.startScreen.mu.Lock()
	if e.startScreen.initialized {
		e.startScreen.mu.Unlock()
		return
	}
	e.startScreen.initialized = true
	if e.startScreen.filter == "" {
		e.startScreen.filter = "all"
	}
	if e.startScreen.name == "" {
		e.startScreen.name = "my-game"
	}
	if e.startScreen.parentDir == "" {
		e.startScreen.parentDir = defaultProjectsParentDir()
	}
	if e.startScreen.selectedTemplate == "" && len(e.startScreen.templates) > 0 {
		e.startScreen.selectedTemplate = defaultTemplateName(e.startScreen.templates)
	}
	e.startScreen.mu.Unlock()
	e.refreshStartValidationLocked()
}

func defaultTemplateName(tmpls []ProjectTemplate) string {
	for _, t := range tmpls {
		if t.Default {
			return t.Name
		}
	}
	if len(tmpls) > 0 {
		return tmpls[0].Name
	}
	return ""
}

// StartScreenOpen reports whether the Start Screen is currently displayed in EditorView.
func (e *Editor) StartScreenOpen() bool {
	if e == nil {
		return false
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.open
}

// CanReturnToEditor reports whether the user can dismiss the Start Screen and return to an open project or demo session.
func (e *Editor) CanReturnToEditor() bool {
	if e == nil {
		return false
	}
	e.startScreen.mu.Lock()
	entered := e.startScreen.hasEnteredEditor
	e.startScreen.mu.Unlock()
	return e.Project != nil || e.FilePath != "" || entered
}

// setStartScreenEntered closes the Start Screen and records that the editor workspace has been entered.
func (e *Editor) setStartScreenEntered() {
	if e == nil {
		return
	}
	e.startScreen.mu.Lock()
	e.startScreen.open = false
	e.startScreen.hasEnteredEditor = true
	e.startScreen.mu.Unlock()
}

// SetStartScreenOpen shows or hides the Start Screen.
func (e *Editor) SetStartScreenOpen(open bool) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	changed := e.startScreen.open != open
	e.startScreen.open = open
	if open {
		e.startScreen.writableCheckedDir = ""
		e.startScreen.recentRev++
	} else {
		e.startScreen.hasEnteredEditor = true
	}
	e.startScreen.mu.Unlock()
	if open {
		e.refreshStartValidationLocked()
	}
	if changed {
		e.notify()
	}
}

// ProjectTemplates returns the project templates configured on the Start Screen (from `game new`'s template registry).
func (e *Editor) ProjectTemplates() []ProjectTemplate {
	if e == nil {
		return nil
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return slices.Clone(e.startScreen.templates)
}

// SetProjectTemplates sets the project templates displayed on the Start Screen.
func (e *Editor) SetProjectTemplates(tmpls []ProjectTemplate) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	e.startScreen.templates = slices.Clone(tmpls)
	if !slices.ContainsFunc(tmpls, func(t ProjectTemplate) bool { return t.Name == e.startScreen.selectedTemplate }) {
		e.startScreen.selectedTemplate = defaultTemplateName(tmpls)
	}
	e.startScreen.mu.Unlock()
	e.notify()
}

// FilteredProjectTemplates returns the templates matching the active Start Screen filter chip ("all", "2d", or "3d").
func (e *Editor) FilteredProjectTemplates() []ProjectTemplate {
	if e == nil {
		return nil
	}
	e.startScreen.mu.Lock()
	filter := e.startScreen.filter
	tmpls := slices.Clone(e.startScreen.templates)
	e.startScreen.mu.Unlock()
	if filter == "" || filter == "all" {
		return tmpls
	}
	var out []ProjectTemplate
	for _, t := range tmpls {
		if t.EffectiveDimension() == filter {
			out = append(out, t)
		}
	}
	return out
}

// StartTemplateFilter returns the active Start Screen template filter ("all", "2d", or "3d").
func (e *Editor) StartTemplateFilter() string {
	if e == nil {
		return "all"
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.filter
}

// SetStartTemplateFilter sets the Start Screen template filter ("all", "2d", or "3d").
func (e *Editor) SetStartTemplateFilter(filter string) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	f := strings.ToLower(strings.TrimSpace(filter))
	if f != "all" && f != "2d" && f != "3d" {
		f = "all"
	}
	e.startScreen.mu.Lock()
	changed := e.startScreen.filter != f
	e.startScreen.filter = f
	e.startScreen.mu.Unlock()
	if changed {
		e.notify()
	}
}

// SelectedProjectTemplate returns the currently selected template name on the Start Screen.
func (e *Editor) SelectedProjectTemplate() string {
	if e == nil {
		return ""
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.selectedTemplate
}

// SelectProjectTemplate selects the template named name on the Start Screen when it exists in ProjectTemplates.
func (e *Editor) SelectProjectTemplate(name string) bool {
	if e == nil {
		return false
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	found := slices.ContainsFunc(e.startScreen.templates, func(t ProjectTemplate) bool { return t.Name == name })
	if !found {
		e.startScreen.mu.Unlock()
		return false
	}
	changed := e.startScreen.selectedTemplate != name
	e.startScreen.selectedTemplate = name
	e.startScreen.mu.Unlock()
	if changed {
		e.notify()
	}
	return true
}

// StartProjectName returns the draft project name in the Start Screen form.
func (e *Editor) StartProjectName() string {
	if e == nil {
		return ""
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.name
}

// SetStartProjectName updates the draft project name and re-runs live validation.
func (e *Editor) SetStartProjectName(name string) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	e.startScreen.name = name
	e.startScreen.mu.Unlock()
	e.refreshStartValidationLocked()
	e.notify()
}

// StartProjectParentDir returns the draft parent folder in the Start Screen form.
func (e *Editor) StartProjectParentDir() string {
	if e == nil {
		return ""
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.parentDir
}

// SetStartProjectParentDir updates the draft parent folder and re-runs live validation.
func (e *Editor) SetStartProjectParentDir(dir string) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	e.startScreen.parentDir = dir
	e.startScreen.mu.Unlock()
	e.refreshStartValidationLocked()
	e.notify()
}

// StartProjectModule returns the draft Go module path in the Start Screen form.
func (e *Editor) StartProjectModule() string {
	if e == nil {
		return ""
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.module
}

// SetStartProjectModule updates the draft Go module path and re-runs live validation.
func (e *Editor) SetStartProjectModule(module string) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	e.startScreen.module = module
	e.startScreen.mu.Unlock()
	e.refreshStartValidationLocked()
	e.notify()
}

// StartProjectIdea returns the draft game idea text on the Start Screen.
func (e *Editor) StartProjectIdea() string {
	if e == nil {
		return ""
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.idea
}

// SetStartProjectIdea updates the draft game idea text on the Start Screen.
func (e *Editor) SetStartProjectIdea(idea string) {
	if e == nil {
		return
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	e.startScreen.idea = idea
	e.startScreen.mu.Unlock()
	e.notify()
}

// StartNameStatus returns the live validation result for the Start Screen's New Project inputs.
func (e *Editor) StartNameStatus() StartNameValidation {
	if e == nil {
		return StartNameValidation{}
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.validation
}

// ValidateNewProjectInputs checks name, parentDir, and optional module path according to the Start Screen rules:
// lowercase a-z, 0-9, and - starting with a letter; target folder does not exist (suggesting <name>-2 etc.);
// parent folder has write access; optional module path is a valid Go module path.
func ValidateNewProjectInputs(name, parentDir, module string) StartNameValidation {
	return validateNewProjectInputs(name, parentDir, module, isDirWritable)
}

func validateNewProjectInputs(name, parentDir, module string, checkWritable func(string) bool) StartNameValidation {
	trimmedParent := strings.TrimSpace(parentDir)
	if trimmedParent == "" {
		trimmedParent = defaultProjectsParentDir()
	} else {
		trimmedParent = expandHomePath(trimmedParent)
	}
	cleanParent := filepath.Clean(trimmedParent)
	targetDir := filepath.Join(cleanParent, name)
	displayTarget := shortenHomePath(targetDir)
	displayParent := shortenHomePath(cleanParent)

	if name == "" {
		return StartNameValidation{
			TargetDir:   targetDir,
			DisplayPath: displayTarget,
			Message:     "Enter a project name.",
		}
	}
	if !isValidStartProjectName(name) {
		return StartNameValidation{
			TargetDir:    targetDir,
			DisplayPath:  displayTarget,
			InvalidChars: true,
			Message:      "Use only a–z, 0–9 and -, starting with a–z.",
		}
	}
	if _, err := os.Stat(targetDir); err == nil {
		suggested := suggestFreeProjectName(cleanParent, name)
		return StartNameValidation{
			TargetDir:     targetDir,
			DisplayPath:   displayTarget,
			Exists:        true,
			SuggestedName: suggested,
			Message:       displayTarget + " already exists",
		}
	}
	if !checkWritable(cleanParent) {
		return StartNameValidation{
			TargetDir:   targetDir,
			DisplayPath: displayTarget,
			NotWritable: true,
			Message:     "No write access to " + displayParent,
		}
	}
	if trimmedMod := strings.TrimSpace(module); trimmedMod != "" {
		if err := validateStartModulePath(trimmedMod); err != nil {
			return StartNameValidation{
				TargetDir:   targetDir,
				DisplayPath: displayTarget,
				Message:     "Invalid module path: " + err.Error(),
			}
		}
	}
	return StartNameValidation{
		OK:          true,
		TargetDir:   targetDir,
		DisplayPath: displayTarget,
		Message:     "Creates " + displayTarget,
	}
}

func isValidStartProjectName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

func suggestFreeProjectName(parentDir, base string) string {
	for n := 2; n < 1000; n++ {
		cand := fmt.Sprintf("%s-%d", base, n)
		if _, err := os.Stat(filepath.Join(parentDir, cand)); err != nil {
			return cand
		}
	}
	return base + "-new"
}

func isDirWritable(dir string) bool {
	cur := filepath.Clean(dir)
	for {
		info, err := os.Stat(cur)
		if err == nil {
			if !info.IsDir() {
				return false
			}
			f, err := os.CreateTemp(cur, ".nsgo-write-check-*")
			if err != nil {
				return false
			}
			name := f.Name()
			_ = f.Close()
			_ = os.Remove(name)
			return true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

func validateStartModulePath(path string) error {
	if strings.HasPrefix(path, "-") {
		return errors.New("starts with -")
	}
	for elem := range strings.SplitSeq(path, "/") {
		if elem == "" {
			return errors.New("has an empty path element")
		}
		if strings.HasPrefix(elem, ".") || strings.HasSuffix(elem, ".") {
			return fmt.Errorf("element %q starts or ends with a dot", elem)
		}
		for _, r := range elem {
			if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' || strings.ContainsRune("-._~", r)) {
				return fmt.Errorf("element %q holds %q", elem, r)
			}
		}
	}
	return nil
}

func (e *Editor) checkParentDirWritableCached(dir string) bool {
	e.startScreen.mu.Lock()
	if e.startScreen.writableCheckedDir == dir {
		ok := e.startScreen.writableResult
		e.startScreen.mu.Unlock()
		return ok
	}
	e.startScreen.mu.Unlock()

	ok := isDirWritable(dir)
	e.startScreen.mu.Lock()
	e.startScreen.writableCheckedDir = dir
	e.startScreen.writableResult = ok
	e.startScreen.mu.Unlock()
	return ok
}

func (e *Editor) refreshStartValidationLocked() {
	e.startScreen.mu.Lock()
	name := e.startScreen.name
	parent := e.startScreen.parentDir
	module := e.startScreen.module
	e.startScreen.mu.Unlock()

	v := validateNewProjectInputs(name, parent, module, e.checkParentDirWritableCached)
	e.startScreen.mu.Lock()
	e.startScreen.validation = v
	e.startScreen.mu.Unlock()
}

// CreatingProject reports whether a Start Screen `game new` command is currently running.
func (e *Editor) CreatingProject() bool {
	if e == nil {
		return false
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.creating
}

// CreateProjectOutput returns the streamed output lines from the latest Start Screen project creation run.
func (e *Editor) CreateProjectOutput() []string {
	if e == nil {
		return nil
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return slices.Clone(e.startScreen.createOutput)
}

// CreateProjectErr returns any error from the latest Start Screen project creation run.
func (e *Editor) CreateProjectErr() error {
	if e == nil {
		return nil
	}
	e.startScreen.mu.Lock()
	defer e.startScreen.mu.Unlock()
	return e.startScreen.createErr
}

// WaitForCreateProject blocks until any active Start Screen project creation run finishes.
func (e *Editor) WaitForCreateProject() {
	if e == nil {
		return
	}
	e.startScreen.mu.Lock()
	run := e.startScreen.run
	e.startScreen.mu.Unlock()
	if run != nil {
		<-run.done
	}
}

func (e *Editor) cancelCreateProject() {
	if e == nil {
		return
	}
	e.startScreen.mu.Lock()
	run := e.startScreen.run
	e.startScreen.mu.Unlock()
	if run != nil {
		run.cancel()
	}
}

// StartCreateProject creates a new project using req via Editor.CreateProject (or `go tool game new` by default)
// off the UI goroutine, streaming its output to CreateProjectOutput, and opens the created project when it succeeds.
func (e *Editor) StartCreateProject(req CreateProjectRequest) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	e.ensureStartScreenInit()
	if err := e.goToolchainError(); err != nil {
		e.startScreen.mu.Lock()
		e.startScreen.createErr = err
		e.startScreen.mu.Unlock()
		e.reportStatusError(err.Error())
		return err
	}

	e.startScreen.mu.Lock()
	if e.startScreen.creating {
		e.startScreen.mu.Unlock()
		return ErrCreateProjectRunning
	}
	// Reserve the creation slot atomically under the lock so concurrent callers cannot race past validation.
	e.startScreen.creating = true
	if req.Name == "" {
		req.Name = e.startScreen.name
	}
	if req.ParentDir == "" && req.Dir == "" {
		req.ParentDir = e.startScreen.parentDir
	}
	if req.Module == "" {
		req.Module = strings.TrimSpace(e.startScreen.module)
	}
	if req.Template == "" {
		req.Template = e.startScreen.selectedTemplate
		if req.Template == "" {
			req.Template = defaultTemplateName(e.startScreen.templates)
		}
		if req.Template == "" {
			req.Template = "starter"
		}
	}
	e.startScreen.mu.Unlock()

	releaseCreating := func(err error, v *StartNameValidation) {
		e.startScreen.mu.Lock()
		e.startScreen.creating = false
		if v != nil {
			e.startScreen.validation = *v
		}
		e.startScreen.createErr = err
		e.startScreen.mu.Unlock()
		e.notify()
	}

	if req.Dir == "" {
		v := ValidateNewProjectInputs(req.Name, req.ParentDir, req.Module)
		if !v.OK {
			err := errors.New(v.Message)
			releaseCreating(err, &v)
			return err
		}
		e.startScreen.mu.Lock()
		e.startScreen.validation = v
		e.startScreen.mu.Unlock()
		req.Dir = v.TargetDir
	} else if req.Module != "" {
		if err := validateStartModulePath(req.Module); err != nil {
			releaseCreating(err, nil)
			return err
		}
	}

	targetDir := req.TargetDir()
	runner := e.CreateProject
	if runner == nil {
		runner = defaultCreateProject
	}

	ctx, cancel := context.WithCancel(context.Background())
	run := &createProjectRun{
		cancel: cancel,
		done:   make(chan struct{}),
	}

	e.startScreen.mu.Lock()
	e.startScreen.createErr = nil
	e.startScreen.createOutput = []string{formatCreateCommandLine(req, targetDir)}
	e.startScreen.run = run
	e.startScreen.mu.Unlock()
	e.Status = "Creating project " + req.Name + "..."
	e.notify()

	appendLine := func(line string) {
		e.startScreen.mu.Lock()
		e.startScreen.createOutput = append(e.startScreen.createOutput, line)
		scope := e.startScreen.scope
		e.startScreen.mu.Unlock()
		if scope != nil {
			scope.Post(func() {
				e.notify()
			})
		}
	}

	stdout := newOutputStreamWriter(appendLine)
	stderr := newOutputStreamWriter(appendLine)

	go func() {
		err := runner(ctx, req, stdout, stderr)
		stdout.Flush()
		stderr.Flush()

		finishOnMain := func() {
			e.startScreen.mu.Lock()
			e.startScreen.creating = false
			e.startScreen.writableCheckedDir = ""
			if e.startScreen.run == run {
				e.startScreen.run = nil
			}
			e.startScreen.mu.Unlock()

			if err == nil && ctx.Err() != nil {
				err = ctx.Err()
			}
			if err != nil {
				e.startScreen.mu.Lock()
				e.startScreen.createErr = err
				e.startScreen.mu.Unlock()
				e.refreshStartValidationLocked()
				e.reportStatusError("Create project failed: " + err.Error())
				return
			}

			if openErr := e.OpenProject(targetDir); openErr != nil {
				e.startScreen.mu.Lock()
				e.startScreen.createErr = openErr
				e.startScreen.mu.Unlock()
				e.refreshStartValidationLocked()
				e.reportStatusError("Created project, but failed to open: " + openErr.Error())
				return
			}
			e.startScreen.mu.Lock()
			e.startScreen.createErr = nil
			e.startScreen.mu.Unlock()
			e.refreshStartValidationLocked()
		}

		e.startScreen.mu.Lock()
		run.pendingFinish = finishOnMain
		e.startScreen.mu.Unlock()
		e.postOrRun(run.runPendingFinish)
	}()

	return nil
}

func formatCreateCommandLine(req CreateProjectRequest, targetDir string) string {
	args := []string{"game", "new"}
	if req.Template != "" {
		args = append(args, "-template", req.Template)
	}
	if req.Name != "" {
		args = append(args, "-name", req.Name)
	}
	if req.Module != "" {
		args = append(args, "-module", req.Module)
	}
	args = append(args, targetDir)
	return "$ " + strings.Join(args, " ")
}

func defaultCreateProject(ctx context.Context, req CreateProjectRequest, stdout, stderr io.Writer) error {
	if !canRunGameDesktop {
		return errors.New("editor: creating a project requires desktop support or Editor.CreateProject")
	}
	args := []string{"tool", "game", "new"}
	if req.Template != "" {
		args = append(args, "-template", req.Template)
	}
	if req.Name != "" {
		args = append(args, "-name", req.Name)
	}
	if req.Module != "" {
		args = append(args, "-module", req.Module)
	}
	args = append(args, "--", req.TargetDir())
	cmd := gotoolchain.CommandContext(ctx, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// CreateProjectFromStartScreen starts project creation using the current Start Screen form values,
// asking about unsaved edits first when another project or scene has unsaved changes.
func (e *Editor) CreateProjectFromStartScreen() error {
	if e == nil {
		return nil
	}
	e.ensureStartScreenInit()
	e.startScreen.mu.Lock()
	req := CreateProjectRequest{
		Name:      e.startScreen.name,
		ParentDir: e.startScreen.parentDir,
		Module:    strings.TrimSpace(e.startScreen.module),
		Template:  e.startScreen.selectedTemplate,
	}
	e.startScreen.mu.Unlock()
	return e.RequestCreateProject(req)
}

// UnsavedPromptOpen reports whether the unsaved-edits confirmation dialog is open before switching projects.
func (e *Editor) UnsavedPromptOpen() bool {
	return e != nil && e.unsavedPrompt.open
}

// PendingProjectAction returns the pending project switch awaiting unsaved-edits confirmation, if any.
func (e *Editor) PendingProjectAction() (PendingProjectAction, bool) {
	if e == nil || !e.unsavedPrompt.open {
		return PendingProjectAction{}, false
	}
	return e.unsavedPrompt.pending, true
}

func (e *Editor) requestSwitch(action PendingProjectAction, fn func() error) error {
	if e == nil {
		return nil
	}
	if e.HasPendingAgentTurn() {
		e.askPendingTurn(action.Summary, func() {
			if e.HasUnsavedChanges() {
				e.unsavedPrompt = unsavedPromptState{
					open:    true,
					pending: action,
					run:     fn,
				}
				e.notify()
				return
			}
			e.report(fn())
		}, nil)
		return nil
	}
	if e.HasUnsavedChanges() {
		e.unsavedPrompt = unsavedPromptState{
			open:    true,
			pending: action,
			run:     fn,
		}
		e.notify()
		return nil
	}
	return fn()
}

// RequestOpenProject opens the project at path, replacing the current project cleanly.
// When the current session has unsaved edits, it opens the unsaved-edits confirmation prompt instead of replacing immediately.
func (e *Editor) RequestOpenProject(path string) error {
	return e.requestSwitch(PendingProjectAction{
		Kind:    "open",
		Target:  path,
		Summary: "Open " + shortenHomePath(path),
	}, func() error {
		return e.OpenProject(path)
	})
}

// RequestOpenDemo opens the built-in Demo Room, asking first if there are unsaved edits.
func (e *Editor) RequestOpenDemo() error {
	return e.requestSwitch(PendingProjectAction{
		Kind:    "demo",
		Target:  "Demo Room",
		Summary: "Open Demo Room",
	}, func() error {
		return e.OpenDemo()
	})
}

// RequestCreateProject creates and opens the project described by req, asking first if there are unsaved edits.
func (e *Editor) RequestCreateProject(req CreateProjectRequest) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if err := e.goToolchainError(); err != nil {
		e.startScreen.mu.Lock()
		e.startScreen.createErr = err
		e.startScreen.mu.Unlock()
		e.reportStatusError(err.Error())
		return err
	}
	if req.Dir == "" {
		v := ValidateNewProjectInputs(req.Name, req.ParentDir, req.Module)
		if !v.OK {
			err := errors.New(v.Message)
			e.startScreen.mu.Lock()
			e.startScreen.validation = v
			e.startScreen.createErr = err
			e.startScreen.mu.Unlock()
			e.notify()
			return err
		}
	}
	return e.requestSwitch(PendingProjectAction{
		Kind:    "create",
		Target:  req.TargetDir(),
		Summary: "Create " + shortenHomePath(req.TargetDir()),
	}, func() error {
		return e.StartCreateProject(req)
	})
}

// ConfirmSaveAndSwitch saves all unsaved edits and, if saving succeeds, executes the pending project switch.
func (e *Editor) ConfirmSaveAndSwitch() error {
	if e == nil || !e.unsavedPrompt.open {
		return nil
	}
	if err := e.SaveAll(); err != nil {
		e.reportStatusError("Save failed: " + err.Error())
		return err
	}
	fn := e.unsavedPrompt.run
	e.unsavedPrompt = unsavedPromptState{}
	e.notify()
	if fn != nil {
		return fn()
	}
	return nil
}

// ConfirmDiscardAndSwitch discards unsaved edits and executes the pending project switch.
func (e *Editor) ConfirmDiscardAndSwitch() error {
	if e == nil || !e.unsavedPrompt.open {
		return nil
	}
	fn := e.unsavedPrompt.run
	e.unsavedPrompt = unsavedPromptState{}
	e.notify()
	if fn != nil {
		return fn()
	}
	return nil
}

// CancelPendingSwitch closes the unsaved-edits prompt without switching projects.
func (e *Editor) CancelPendingSwitch() {
	if e == nil || !e.unsavedPrompt.open {
		return
	}
	e.unsavedPrompt = unsavedPromptState{}
	e.notify()
}

// pickFolder invokes Editor.PickProjectFolder when set, or core.Scope.PickDirectories when a scope is attached.
func (e *Editor) pickFolder(title, startDir string, done func(dir string, ok bool, err error)) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.PickProjectFolder != nil {
		e.PickProjectFolder(title, startDir, done)
		return nil
	}
	if e.scope == nil {
		return errors.New("editor: no folder picker available (attach a UI scope or set Editor.PickProjectFolder)")
	}
	e.scope.PickDirectories(core.PickDirectoriesOptions{
		Title:  title,
		Button: "Open",
		Start:  startDir,
	}, func(dirs []core.Directory, err error) {
		if err != nil {
			done("", false, err)
			return
		}
		if len(dirs) == 0 || dirs[0].Path == "" {
			done("", false, nil)
			return
		}
		done(dirs[0].Path, true, nil)
	})
	return nil
}

// BrowseAndOpenProject opens the folder picker and switches to the chosen project (asking about unsaved edits first).
func (e *Editor) BrowseAndOpenProject() error {
	startDir := e.StartProjectParentDir()
	if e.ProjectPath != "" {
		startDir = filepath.Dir(filepath.Dir(e.ProjectPath))
	}
	return e.pickFolder("Open Project Folder", startDir, func(dir string, ok bool, err error) {
		if err != nil {
			e.report(err)
			return
		}
		if !ok || dir == "" {
			return
		}
		if openErr := e.RequestOpenProject(dir); openErr != nil {
			e.report(openErr)
		}
	})
}

// BrowseStartProjectParentDir opens the folder picker to choose the parent directory for a new project.
func (e *Editor) BrowseStartProjectParentDir() error {
	return e.pickFolder("Choose Projects Folder", e.StartProjectParentDir(), func(dir string, ok bool, err error) {
		if err != nil {
			e.report(err)
			return
		}
		if !ok || dir == "" {
			return
		}
		e.SetStartProjectParentDir(dir)
	})
}
