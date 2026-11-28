package editor

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"github.com/nsgo-dev/game/pkg/tooling/refs"
	"go.yaml.in/yaml/v3"
)

// ReferencesScreenMode selects which workflow stage the full-window References screen displays.
type ReferencesScreenMode string

const (
	// ReferencesModeUsages lists grouped usages of a prefab, scene, or project file.
	ReferencesModeUsages ReferencesScreenMode = "usages"
	// ReferencesModeRename previews a safe YAML-only rename with exact line diffs and non-rewritten warnings.
	ReferencesModeRename ReferencesScreenMode = "rename"
	// ReferencesModeDone displays the post-rename summary with one-step Undo and Back to the scene.
	ReferencesModeDone ReferencesScreenMode = "done"
)

// UsageGroupKind classifies a usage into one of the four primary References screen categories (plus Other).
type UsageGroupKind string

const (
	// UsageGroupScenes lists scene instances referencing the target file, with override counts.
	UsageGroupScenes UsageGroupKind = "scenes"
	// UsageGroupVariants lists prefab or scene variants that inherit the target file.
	UsageGroupVariants UsageGroupKind = "variants"
	// UsageGroupDataTables lists data table rows referencing the target file.
	UsageGroupDataTables UsageGroupKind = "data"
	// UsageGroupGoCode lists Go string literals or constants holding the target path (not rewritten).
	UsageGroupGoCode UsageGroupKind = "go"
	// UsageGroupOther lists references in other YAML/JSON project files (such as game.yaml, cutscenes, or screens).
	UsageGroupOther UsageGroupKind = "other"
)

// movedFocusSentinel identifies the virtual MOVED row in the left list during rename review.
const movedFocusSentinel = "__moved__"

// UsageItem is one occurrence of a reference to the target file.
type UsageItem struct {
	File          string         `json:"file"`
	Line          int            `json:"line"`
	Column        int            `json:"column,omitempty"`
	Group         UsageGroupKind `json:"group"`
	Label         string         `json:"label"`
	Field         string         `json:"field"`
	Value         string         `json:"value"`
	ObjectID      string         `json:"objectId,omitempty"`
	TopObjectID   string         `json:"topObjectId,omitempty"`
	RowID         string         `json:"rowId,omitempty"`
	GoIdent       string         `json:"goIdent,omitempty"`
	X             float32        `json:"x,omitempty"`
	Y             float32        `json:"y,omitempty"`
	HasPos        bool           `json:"hasPos,omitempty"`
	OverrideCount int            `json:"overrideCount,omitempty"`
	Overrides     []string       `json:"overrides,omitempty"`
	NotRewritten  bool           `json:"notRewritten,omitempty"`
}

// UsageFileGroup groups all usages inside one project file.
type UsageFileGroup struct {
	File            string         `json:"file"`
	Name            string         `json:"name"`
	Group           UsageGroupKind `json:"group"`
	NotRewritten    bool           `json:"notRewritten,omitempty"`
	VirtualWidth    float32        `json:"virtualWidth,omitempty"`
	VirtualHeight   float32        `json:"virtualHeight,omitempty"`
	BackgroundColor string         `json:"backgroundColor,omitempty"`
	Refs            []UsageItem    `json:"refs"`
}

// UsagesReport is the grouped result of Editor.FindUsages across scenes, prefab variants, data tables, Go code, and other files.
type UsagesReport struct {
	Target      string           `json:"target"`
	TotalUsages int              `json:"totalUsages"`
	TotalFiles  int              `json:"totalFiles"`
	Scenes      []UsageFileGroup `json:"scenes,omitempty"`
	Variants    []UsageFileGroup `json:"variants,omitempty"`
	DataTables  []UsageFileGroup `json:"dataTables,omitempty"`
	GoCode      []UsageFileGroup `json:"goCode,omitempty"`
	Other       []UsageFileGroup `json:"other,omitempty"`
}

// AllFiles returns all grouped files in display order: Scenes, Prefab Variants, Data Tables, Go Code, Other.
func (r *UsagesReport) AllFiles() []UsageFileGroup {
	if r == nil {
		return nil
	}
	out := make([]UsageFileGroup, 0, len(r.Scenes)+len(r.Variants)+len(r.DataTables)+len(r.GoCode)+len(r.Other))
	out = append(out, r.Scenes...)
	out = append(out, r.Variants...)
	out = append(out, r.DataTables...)
	out = append(out, r.GoCode...)
	out = append(out, r.Other...)
	return out
}

// FileByPath looks up a UsageFileGroup by its slash-separated project path.
func (r *UsagesReport) FileByPath(p string) *UsageFileGroup {
	if r == nil {
		return nil
	}
	for _, list := range [][]UsageFileGroup{r.Scenes, r.Variants, r.DataTables, r.GoCode, r.Other} {
		for i := range list {
			if list[i].File == p {
				return &list[i]
			}
		}
	}
	return nil
}

// CodePreviewLine is one rendered line in the right-hand code or diff preview pane.
type CodePreviewLine struct {
	LineNumber int    `json:"lineNumber,omitempty"`
	Sign       string `json:"sign,omitempty"` // "", "-", "+"
	Kind       string `json:"kind,omitempty"` // "", "hit", "del", "add", "warn", "gap", "note"
	Text       string `json:"text"`
	Focused    bool   `json:"focused,omitempty"`
}

// SafeRenameLineDiff records one rewritten line pair (old -> new) inside a YAML file.
type SafeRenameLineDiff struct {
	Line    int    `json:"line"`
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

// SafeRenameFileDiff describes the planned changes to one YAML file in a safe rename review.
type SafeRenameFileDiff struct {
	Path               string               `json:"path"`
	OldPath            string               `json:"oldPath"`
	Group              UsageGroupKind       `json:"group"`
	ChangeCount        int                  `json:"changeCount"`
	Diffs              []SafeRenameLineDiff `json:"diffs"`
	PreviewLines       []CodePreviewLine    `json:"previewLines,omitempty"`
	PreservedOverrides []string             `json:"preservedOverrides,omitempty"`
}

// SafeRenameProblem describes a non-YAML reference (such as a Go string constant) that safe rename intentionally does not rewrite.
type SafeRenameProblem struct {
	File         string `json:"file"`
	Line         int    `json:"line"`
	Ident        string `json:"ident,omitempty"`
	OldPath      string `json:"oldPath"`
	NewPath      string `json:"newPath,omitempty"`
	Message      string `json:"message"`
	NotRewritten bool   `json:"notRewritten"`
	AskSent      bool   `json:"askSent,omitempty"`
}

// SafeRenameAgentRequest is passed to Editor.OnAskAgentUpdateRef when the author asks the agent to update a non-rewritten reference.
type SafeRenameAgentRequest struct {
	File    string
	Line    int
	Ident   string
	OldPath string
	NewPath string
}

// SafeRenamePlan is the dry-run result of PreviewSafeRename before any file is written to disk.
type SafeRenamePlan struct {
	From               string               `json:"from"`
	To                 string               `json:"to"`
	FolderPrefix       string               `json:"folderPrefix"`
	OldStem            string               `json:"oldStem"`
	NewStem            string               `json:"newStem"`
	Suffix             string               `json:"suffix"`
	Valid              bool                 `json:"valid"`
	ValidationMsg      string               `json:"validationMsg"`
	MovedContentLines  []CodePreviewLine    `json:"movedContentLines,omitempty"`
	Files              []SafeRenameFileDiff `json:"files,omitempty"`
	Problems           []SafeRenameProblem  `json:"problems,omitempty"`
	RewrittenFileCount int                  `json:"rewrittenFileCount"`
	TotalTouchedFiles  int                  `json:"totalTouchedFiles"`
	RewrittenRefCount  int                  `json:"rewrittenRefCount"`
	IndexUpdated       bool                 `json:"indexUpdated,omitempty"`
	PreservedObjects   []string             `json:"preservedObjects,omitempty"`
}

// FileDiff returns the SafeRenameFileDiff for the given old or new project path, or nil.
func (p *SafeRenamePlan) FileDiff(filePath string) *SafeRenameFileDiff {
	if p == nil {
		return nil
	}
	for i := range p.Files {
		if p.Files[i].OldPath == filePath || p.Files[i].Path == filePath {
			return &p.Files[i]
		}
	}
	return nil
}

// ReferencesScreenState is a read-only snapshot of the full-window Find Usages and Safe Rename screen.
type ReferencesScreenState struct {
	Open            bool                 `json:"open"`
	Mode            ReferencesScreenMode `json:"mode"`
	Target          string               `json:"target"`
	SourceObjectID  string               `json:"sourceObjectId,omitempty"`
	ReturnSceneName string               `json:"returnSceneName,omitempty"`
	Report          *UsagesReport        `json:"report,omitempty"`
	FocusedFile     string               `json:"focusedFile,omitempty"`
	FocusedRefIdx   int                  `json:"focusedRefIdx"`
	RenameInput     string               `json:"renameInput,omitempty"`
	FolderPrefix    string               `json:"folderPrefix,omitempty"`
	OldStem         string               `json:"oldStem,omitempty"`
	Suffix          string               `json:"suffix,omitempty"`
	Plan            *SafeRenamePlan      `json:"plan,omitempty"`
	AppliedFrom     string               `json:"appliedFrom,omitempty"`
	AppliedTo       string               `json:"appliedTo,omitempty"`
	AppliedFiles    int                  `json:"appliedFiles,omitempty"`
	CheckProblems   []SafeRenameProblem  `json:"checkProblems,omitempty"`
	AskSentFiles    map[string]bool      `json:"askSentFiles,omitempty"`
}

type usagePreviewCacheKey struct {
	projectPath  string
	file         string
	group        UsageGroupKind
	notRewritten bool
	refCount     int
	focusLine    int
	filesRev     uint64
	report       *UsagesReport
}

type usagePreviewCache struct {
	valid bool
	key   usagePreviewCacheKey
	title string
	meta  string
	lines []CodePreviewLine
}

type referencesScreenState struct {
	open            bool
	mode            ReferencesScreenMode
	target          string
	sourceObjectID  string
	returnSceneName string
	report          *UsagesReport
	focusedFile     string
	focusedRefIdx   int
	renameInput     string
	folderPrefix    string
	oldStem         string
	suffix          string
	plan            *SafeRenamePlan
	appliedFrom     string
	appliedTo       string
	appliedFiles    int
	checkProblems   []SafeRenameProblem
	askSentFiles    map[string]bool
	previewCache    usagePreviewCache
	rev             uint64
}

// resetUsagesForProject closes and clears the Find Usages / Safe Rename screen when switching projects or opening the demo.
func (e *Editor) resetUsagesForProject() {
	if e == nil {
		return
	}
	e.usages = referencesScreenState{rev: e.usages.rev + 1}
}

func (e *Editor) usagesBlockedErr() error {
	switch {
	case e.Playing():
		return ErrPlaying
	case e.StartScreenOpen():
		return errors.New("editor: close the Start Screen before finding usages")
	case e.MergeConflictOpen():
		return errors.New("editor: resolve the active merge conflict before finding usages")
	case e.PlaytestReportOpen():
		return errors.New("editor: close the Playtest Report screen before finding usages")
	default:
		return nil
	}
}

// resolveProjectFileWithinRoot normalizes rel (rejecting traversal, colons, backslashes, control chars, and absolute paths)
// and verifies via filepath.EvalSymlinks that the target (or its nearest existing ancestor when allowMissing is true)
// remains inside root.
func resolveProjectFileWithinRoot(root, rel string, allowMissing bool) (cleanRel, abs string, err error) {
	if root == "" {
		return "", "", ErrNoProject
	}
	raw := strings.TrimSpace(rel)
	if raw == "" || strings.ContainsAny(raw, "\\:") || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", "", fmt.Errorf("editor: invalid project path %q", rel)
	}
	if strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) {
		return "", "", fmt.Errorf("editor: invalid project path %q", rel)
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || !fs.ValidPath(cleaned) {
		return "", "", fmt.Errorf("editor: invalid project path %q", rel)
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	resolvedRoot := rootAbs
	if r, rerr := filepath.EvalSymlinks(rootAbs); rerr == nil {
		resolvedRoot = r
	}

	abs = filepath.Join(rootAbs, filepath.FromSlash(cleaned))
	if _, lerr := os.Lstat(abs); lerr == nil {
		resolved, serr := filepath.EvalSymlinks(abs)
		if serr != nil {
			return "", "", fmt.Errorf("editor: path %q cannot be resolved inside project root", cleaned)
		}
		if !isSubpath(resolvedRoot, resolved) {
			return "", "", fmt.Errorf("editor: path %q escapes project root via symlink", cleaned)
		}
		return cleaned, abs, nil
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return "", "", lerr
	}
	if !allowMissing {
		return "", "", os.ErrNotExist
	}
	cur := filepath.Dir(abs)
	for {
		if resolvedCur, serr := filepath.EvalSymlinks(cur); serr == nil {
			if !isSubpath(resolvedRoot, resolvedCur) {
				return "", "", fmt.Errorf("editor: path %q escapes project root via symlink", cleaned)
			}
			break
		} else if !errors.Is(serr, fs.ErrNotExist) {
			return "", "", serr
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", "", fmt.Errorf("editor: path %q has no ancestor inside project root", cleaned)
		}
		cur = parent
	}
	return cleaned, abs, nil
}

func isSubpath(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func (e *Editor) usagesStateRev() uint64 {
	if e == nil {
		return 0
	}
	return e.usages.rev
}

// UsagesScreenOpen reports whether the full-window Find Usages / Safe Rename screen is open.
func (e *Editor) UsagesScreenOpen() bool {
	return e != nil && e.usages.open
}

// ReferencesScreen returns a snapshot of the active References screen state, or nil when closed.
func (e *Editor) ReferencesScreen() *ReferencesScreenState {
	if e == nil || !e.usages.open {
		return nil
	}
	cp := ReferencesScreenState{
		Open:            e.usages.open,
		Mode:            e.usages.mode,
		Target:          e.usages.target,
		SourceObjectID:  e.usages.sourceObjectID,
		ReturnSceneName: e.usages.returnSceneName,
		Report:          cloneUsagesReport(e.usages.report),
		FocusedFile:     e.usages.focusedFile,
		FocusedRefIdx:   e.usages.focusedRefIdx,
		RenameInput:     e.usages.renameInput,
		FolderPrefix:    e.usages.folderPrefix,
		OldStem:         e.usages.oldStem,
		Suffix:          e.usages.suffix,
		Plan:            cloneSafeRenamePlan(e.usages.plan),
		AppliedFrom:     e.usages.appliedFrom,
		AppliedTo:       e.usages.appliedTo,
		AppliedFiles:    e.usages.appliedFiles,
		CheckProblems:   slices.Clone(e.usages.checkProblems),
	}
	if len(e.usages.askSentFiles) > 0 {
		cp.AskSentFiles = make(map[string]bool, len(e.usages.askSentFiles))
		for k, v := range e.usages.askSentFiles {
			cp.AskSentFiles[k] = v
		}
	}
	return &cp
}

// findUsagesTarget resolves the default project-relative path to inspect for Find Usages:
// the selected object's instanced scene or path-valued prefab, then SelectedFile, then ScenePath.
func (e *Editor) findUsagesTarget() (target, sourceObjectID string) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return "", ""
	}
	if obj, _ := e.SelectedObject(); obj != nil {
		if obj.Scene != "" {
			if clean := e.normalizeProjectPath(obj.Scene); clean != "" {
				return clean, obj.ID
			}
		}
		if obj.Prefab != "" && isPathLikePrefab(obj.Prefab) {
			if clean := e.normalizeProjectPath(obj.Prefab); clean != "" {
				return clean, obj.ID
			}
		}
	}
	if e.SelectedFile != "" {
		if clean := e.normalizeProjectPath(e.SelectedFile); clean != "" {
			return clean, ""
		}
	}
	if e.ScenePath != "" {
		if clean := e.normalizeProjectPath(e.ScenePath); clean != "" {
			return clean, ""
		}
	}
	return "", ""
}

func isPathLikePrefab(v string) bool {
	lower := strings.ToLower(strings.TrimSpace(v))
	return strings.ContainsRune(v, '/') || strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

// FindUsages scans the open project for every usage of target across scenes, prefab variants, data tables, Go source files (via go/parser), and other project files.
func (e *Editor) FindUsages(target string) (*UsagesReport, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	clean := e.normalizeProjectPath(target)
	if clean == "" {
		return nil, fmt.Errorf("editor: invalid project path %q", target)
	}
	root := filepath.Dir(e.ProjectPath)
	if _, _, err := resolveProjectFileWithinRoot(root, clean, true); err != nil {
		return nil, err
	}
	return scanUsagesInDir(root, clean)
}

// OpenUsagesScreen opens the full-window Find Usages screen for targetPath (or the current selection/file/scene when empty).
func (e *Editor) OpenUsagesScreen(targetPath string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if err := e.usagesBlockedErr(); err != nil {
		return err
	}
	var clean, srcObj string
	if strings.TrimSpace(targetPath) == "" {
		clean, srcObj = e.findUsagesTarget()
		if clean == "" {
			return errors.New("editor: select a prefab, scene, or project file to find usages")
		}
	} else {
		clean = e.normalizeProjectPath(targetPath)
		if clean == "" {
			return fmt.Errorf("editor: invalid project path %q", targetPath)
		}
	}
	rep, err := e.FindUsages(clean)
	if err != nil {
		return err
	}
	folder, stem, suffix := splitRenameParts(clean)
	returnName := e.currentReturnSceneName()

	e.usages = referencesScreenState{
		open:            true,
		mode:            ReferencesModeUsages,
		target:          clean,
		sourceObjectID:  srcObj,
		returnSceneName: returnName,
		report:          rep,
		renameInput:     stem,
		folderPrefix:    folder,
		oldStem:         stem,
		suffix:          suffix,
		askSentFiles:    make(map[string]bool),
		rev:             e.usages.rev + 1,
	}
	e.ensureValidUsageFocus()
	e.Status = fmt.Sprintf("Usages of %s: %d in %d files", path.Base(clean), rep.TotalUsages, rep.TotalFiles)
	e.notify()
	return nil
}

// CloseUsagesScreen closes the full-window Find Usages / Safe Rename screen and returns to the editor.
func (e *Editor) CloseUsagesScreen() {
	if e == nil || !e.usages.open {
		return
	}
	e.usages.open = false
	e.usages.rev++
	e.scheduleAutoSaveIfNeeded()
	e.notify()
}

// StartSafeRename switches the open References screen into safe-rename mode (F2).
func (e *Editor) StartSafeRename() error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if err := e.usagesBlockedErr(); err != nil {
		return err
	}
	if !e.usages.open || e.usages.target == "" {
		if err := e.OpenUsagesScreen(""); err != nil {
			return err
		}
	}
	folder, stem, suffix := splitRenameParts(e.usages.target)
	e.usages.mode = ReferencesModeRename
	e.usages.folderPrefix = folder
	e.usages.oldStem = stem
	e.usages.suffix = suffix
	if e.usages.renameInput == "" || e.usages.renameInput == stem {
		e.usages.renameInput = stem
	}
	plan, _ := e.computeSafeRenamePlan(e.usages.target, e.usages.renameInput)
	e.usages.plan = plan
	e.usages.rev++
	e.notify()
	return nil
}

// SetSafeRenameInput updates the NEW NAME field in safe-rename mode, validating live and computing the dry-run plan without writing any file.
func (e *Editor) SetSafeRenameInput(newName string) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if !e.usages.open || e.usages.target == "" {
		return errors.New("editor: references screen is not open")
	}
	e.usages.renameInput = newName
	plan, err := e.computeSafeRenamePlan(e.usages.target, newName)
	e.usages.plan = plan
	e.usages.rev++
	e.notify()
	return err
}

// PreviewSafeRename opens the References screen in safe-rename review mode for oldRel -> newName (either a bare stem such as "slime_green" or a full slash-separated project path), computing the exact dry-run line diffs and non-rewritten Go warnings WITHOUT writing any file to disk.
func (e *Editor) PreviewSafeRename(oldRel, newName string) (*SafeRenamePlan, error) {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	if err := e.usagesBlockedErr(); err != nil {
		return nil, err
	}
	cleanOld := e.normalizeProjectPath(oldRel)
	if cleanOld == "" {
		return nil, fmt.Errorf("editor: invalid project path %q", oldRel)
	}
	rep, err := e.FindUsages(cleanOld)
	if err != nil {
		return nil, err
	}
	folder, oldStem, suffix := splitRenameParts(cleanOld)
	plan, planErr := e.computeSafeRenamePlanWithReport(cleanOld, newName, rep)
	if e.AgentMutating() {
		if dom, ok := e.CheckYouOnlyTarget(cleanOld); ok {
			return nil, fmt.Errorf("editor: refused (You only): %s is protected under %s and cannot be renamed by the agent", cleanOld, dom.DisplayName())
		}
		if plan != nil && plan.To != "" {
			if dom, ok := e.CheckYouOnlyTarget(plan.To); ok {
				return nil, fmt.Errorf("editor: refused (You only): %s is protected under %s and cannot be renamed to by the agent", plan.To, dom.DisplayName())
			}
		}
	}

	inputStem := newName
	if plan != nil && plan.NewStem != "" {
		inputStem = plan.NewStem
	}
	e.usages = referencesScreenState{
		open:            true,
		mode:            ReferencesModeRename,
		target:          cleanOld,
		returnSceneName: e.currentReturnSceneName(),
		report:          rep,
		renameInput:     inputStem,
		folderPrefix:    folder,
		oldStem:         oldStem,
		suffix:          suffix,
		plan:            plan,
		askSentFiles:    make(map[string]bool),
		rev:             e.usages.rev + 1,
	}
	e.ensureValidUsageFocus()
	if plan != nil && plan.Valid {
		e.Status = fmt.Sprintf("Preview rename %s -> %s (%d files)", cleanOld, plan.To, plan.TotalTouchedFiles)
	} else if plan != nil && plan.ValidationMsg != "" {
		e.Status = plan.ValidationMsg
	}
	e.notify()
	return cloneSafeRenamePlan(plan), planErr
}

// CancelSafeRename leaves safe-rename review mode without modifying any file on disk.
func (e *Editor) CancelSafeRename() {
	if e == nil || !e.usages.open {
		return
	}
	e.usages.mode = ReferencesModeUsages
	e.usages.plan = nil
	e.usages.renameInput = e.usages.oldStem
	if e.usages.focusedFile == movedFocusSentinel {
		e.usages.focusedFile = ""
		e.usages.focusedRefIdx = 0
		e.ensureValidUsageFocus()
	}
	e.usages.rev++
	e.notify()
}

// ApplySafeRename applies the current validated safe-rename plan through MoveFile as one undoable History step and enters the Done review state.
func (e *Editor) ApplySafeRename() error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if err := e.usagesBlockedErr(); err != nil {
		return err
	}
	if !e.usages.open || e.usages.plan == nil {
		return errors.New("editor: no safe rename preview is active")
	}
	plan := e.usages.plan
	if !plan.Valid {
		if plan.ValidationMsg != "" {
			return errors.New(plan.ValidationMsg)
		}
		return errors.New("editor: safe rename target is not valid")
	}
	from, to := plan.From, plan.To
	touched := plan.TotalTouchedFiles
	askSent := e.usages.askSentFiles

	if err := e.MoveFile(from, to); err != nil {
		return err
	}

	// Re-scan usages for the new path, plus scan any remaining Go code still pointing at the old path.
	root := filepath.Dir(e.ProjectPath)
	rep, err := scanUsagesInDir(root, to)
	if err != nil {
		rep = &UsagesReport{Target: to}
	}
	staleGo := scanGoUsages(root, from, false)
	var checkProbs []SafeRenameProblem
	for _, gf := range staleGo {
		// Append stale Go usages to the report so the author can still click and inspect them in Done mode.
		rep.GoCode = append(rep.GoCode, gf)
		rep.TotalFiles++
		rep.TotalUsages += len(gf.Refs)
		for _, r := range gf.Refs {
			checkProbs = append(checkProbs, SafeRenameProblem{
				File:         r.File,
				Line:         r.Line,
				Ident:        r.GoIdent,
				OldPath:      from,
				NewPath:      to,
				Message:      fmt.Sprintf("%s loads the old path and is not rewritten", r.File),
				NotRewritten: true,
				AskSent:      askSent[r.File],
			})
		}
	}
	// Also check YAML references via refs.Scan.Check to ensure no YAML reference points at the old path.
	if scan, serr := refs.Scan(os.DirFS(root)); serr == nil {
		for _, prob := range scan.Check() {
			if strings.Contains(prob.Message, from) {
				checkProbs = append(checkProbs, SafeRenameProblem{
					File:    prob.File,
					Line:    prob.Line,
					OldPath: from,
					NewPath: to,
					Message: prob.Message,
				})
			}
		}
	}

	folder, stem, suffix := splitRenameParts(to)
	e.usages.mode = ReferencesModeDone
	e.usages.target = to
	e.usages.report = rep
	e.usages.folderPrefix = folder
	e.usages.oldStem = stem
	e.usages.suffix = suffix
	e.usages.renameInput = stem
	e.usages.plan = nil
	e.usages.appliedFrom = from
	e.usages.appliedTo = to
	e.usages.appliedFiles = touched
	e.usages.checkProblems = checkProbs
	if e.usages.focusedFile == movedFocusSentinel || e.usages.focusedFile == from {
		e.usages.focusedFile = ""
		e.usages.focusedRefIdx = 0
	}
	e.ensureValidUsageFocus()
	e.usages.rev++

	if len(checkProbs) > 0 {
		e.Status = fmt.Sprintf("Renamed to %s · %d files rewritten · %d problem(s)", path.Base(to), touched, len(checkProbs))
	} else {
		e.Status = fmt.Sprintf("Renamed to %s · %d files rewritten", path.Base(to), touched)
	}
	e.notify()
	return nil
}

// UndoSafeRename undoes the applied safe rename in one History step, restoring every moved and rewritten file on disk and in open tabs.
func (e *Editor) UndoSafeRename() bool {
	if e == nil || !e.usages.open || e.usages.mode != ReferencesModeDone {
		return false
	}
	return e.Undo()
}

// followUsagesMove keeps the open References screen synchronized when a MoveFile executes, undoes, or redoes.
func (e *Editor) followUsagesMove(from, to string) {
	if e == nil || !e.usages.open {
		return
	}
	// If this move is the Undo of our applied rename (from == appliedTo && to == appliedFrom), restore Usages mode on the original target.
	if e.usages.mode == ReferencesModeDone && e.usages.appliedTo == from && e.usages.appliedFrom == to {
		rep := &UsagesReport{Target: to}
		if e.ProjectPath != "" {
			if scanned, err := scanUsagesInDir(filepath.Dir(e.ProjectPath), to); err == nil {
				rep = scanned
			}
		}
		folder, stem, suffix := splitRenameParts(to)
		e.usages.mode = ReferencesModeUsages
		e.usages.target = to
		e.usages.report = rep
		e.usages.folderPrefix = folder
		e.usages.oldStem = stem
		e.usages.suffix = suffix
		e.usages.renameInput = stem
		e.usages.plan = nil
		e.usages.appliedFrom = ""
		e.usages.appliedTo = ""
		e.usages.appliedFiles = 0
		e.usages.checkProblems = nil
		e.ensureValidUsageFocus()
		e.usages.rev++
		return
	}
	// Otherwise, follow the target path if it moved externally.
	forth := movedPath(from, to)
	if next := forth(e.usages.target); next != e.usages.target {
		e.usages.target = next
		folder, stem, suffix := splitRenameParts(next)
		e.usages.folderPrefix = folder
		e.usages.oldStem = stem
		e.usages.suffix = suffix
		if e.ProjectPath != "" {
			if rep, err := scanUsagesInDir(filepath.Dir(e.ProjectPath), next); err == nil {
				e.usages.report = rep
			}
		}
		e.ensureValidUsageFocus()
		e.usages.rev++
	}
}

// FocusUsage selects a file (or the MOVED entry during rename review) and reference index in the References screen.
func (e *Editor) FocusUsage(file string, refIdx int) {
	if e == nil || !e.usages.open {
		return
	}
	if file == movedFocusSentinel && e.usages.mode == ReferencesModeRename {
		e.usages.focusedFile = movedFocusSentinel
		e.usages.focusedRefIdx = 0
		e.usages.rev++
		e.notify()
		return
	}
	fg := e.usages.report.FileByPath(file)
	if fg == nil {
		return
	}
	if refIdx < 0 {
		refIdx = 0
	}
	if len(fg.Refs) > 0 && refIdx >= len(fg.Refs) {
		refIdx = len(fg.Refs) - 1
	}
	e.usages.focusedFile = fg.File
	e.usages.focusedRefIdx = refIdx
	e.usages.rev++
	e.notify()
}

type usageCursor struct {
	file   string
	refIdx int
}

func (e *Editor) usageCursors() []usageCursor {
	var out []usageCursor
	if e.usages.mode == ReferencesModeRename && e.usages.plan != nil && e.usages.plan.Valid {
		out = append(out, usageCursor{file: movedFocusSentinel, refIdx: 0})
	}
	if e.usages.report != nil {
		for _, fg := range e.usages.report.AllFiles() {
			if len(fg.Refs) == 0 {
				out = append(out, usageCursor{file: fg.File, refIdx: 0})
				continue
			}
			for i := range fg.Refs {
				out = append(out, usageCursor{file: fg.File, refIdx: i})
			}
		}
	}
	return out
}

// StepUsageFocus moves keyboard focus forward (+1) or backward (-1) through all usages in the References screen.
func (e *Editor) StepUsageFocus(delta int) {
	if e == nil || !e.usages.open || delta == 0 {
		return
	}
	cursors := e.usageCursors()
	if len(cursors) == 0 {
		return
	}
	curIdx := 0
	for i, c := range cursors {
		if c.file == e.usages.focusedFile && c.refIdx == e.usages.focusedRefIdx {
			curIdx = i
			break
		}
	}
	next := (curIdx + delta) % len(cursors)
	if next < 0 {
		next += len(cursors)
	}
	e.usages.focusedFile = cursors[next].file
	e.usages.focusedRefIdx = cursors[next].refIdx
	e.usages.rev++
	e.notify()
}

// OpenFocusedUsage activates the currently focused usage (Enter): closes the References screen, opens the scene with the instanced object selected and framed (or opens the data table / Go source file).
func (e *Editor) OpenFocusedUsage() error {
	if e == nil || !e.usages.open {
		return nil
	}
	if e.usages.focusedFile == "" || e.usages.focusedFile == movedFocusSentinel {
		return nil
	}
	fg := e.usages.report.FileByPath(e.usages.focusedFile)
	if fg == nil {
		return nil
	}
	var item UsageItem
	if len(fg.Refs) > 0 {
		idx := min(max(e.usages.focusedRefIdx, 0), len(fg.Refs)-1)
		item = fg.Refs[idx]
	} else {
		item = UsageItem{File: fg.File, Group: fg.Group}
	}
	return e.OpenUsageItem(item)
}

// OpenUsageItem navigates to a specific UsageItem: scenes open with the referencing object selected and framed; data tables open in the Data Table panel; Go files invoke OnOpenSourceFile and select the file.
func (e *Editor) OpenUsageItem(item UsageItem) error {
	if e == nil || e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if item.File == "" {
		return nil
	}
	cleanFile, abs, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), item.File, false)
	if err != nil {
		return err
	}
	item.File = cleanFile
	switch item.Group {
	case UsageGroupGoCode:
		e.selectFile(item.File)
		if e.OnOpenSourceFile != nil {
			if err := e.OnOpenSourceFile(item.File, item.Line); err != nil {
				return err
			}
		}
		if item.Line > 0 {
			e.Status = fmt.Sprintf("Opened %s:%d", item.File, item.Line)
		} else {
			e.Status = "Opened " + item.File
		}
		e.notify()
		return nil
	case UsageGroupDataTables:
		e.CloseUsagesScreen()
		e.selectFile(item.File)
		if err := e.OpenDataTableProjectFile(item.File); err != nil {
			// Fall back to selecting the file if it is a custom YAML table outside datatable's strict version/rows schema.
			e.Status = fmt.Sprintf("Selected %s:%d", item.File, item.Line)
			e.notify()
			return nil
		}
		e.Status = fmt.Sprintf("Opened %s:%d", item.File, item.Line)
		e.notify()
		return nil
	case UsageGroupScenes:
		e.CloseUsagesScreen()
		if !e.isOpenScene(item.File) {
			if err := e.OpenScene(item.File); err != nil {
				e.selectFile(item.File)
				e.notify()
				return err
			}
		} else if e.ScenePath == "" {
			e.ScenePath = item.File
		}
		e.selectFile(item.File)
		selected := false
		if item.ObjectID != "" {
			selected = e.SelectObject(item.ObjectID)
		}
		if !selected && item.TopObjectID != "" && item.TopObjectID != item.ObjectID {
			selected = e.SelectObject(item.TopObjectID)
		}
		if !selected && item.Line > 0 {
			if data, err := os.ReadFile(abs); err == nil {
				if objID := sceneObjectAtLine(data, item.Line); objID != "" {
					selected = e.SelectObject(objID)
				}
			}
		}
		e.FrameSelection()
		if selected {
			e.Status = fmt.Sprintf("Opened %s (%s)", item.File, e.SelectedID)
		} else {
			e.Status = "Opened " + item.File
		}
		e.notify()
		return nil
	default:
		e.CloseUsagesScreen()
		if e.isKnownSceneFile(item.File) {
			if !e.isOpenScene(item.File) {
				_ = e.OpenScene(item.File)
			}
		}
		e.selectFile(item.File)
		if item.Line > 0 {
			e.Status = fmt.Sprintf("Opened %s:%d", item.File, item.Line)
		} else {
			e.Status = "Selected " + item.File
		}
		e.notify()
		return nil
	}
}

// AskAgentToUpdateUsage triggers the optional OnAskAgentUpdateRef callback for a non-rewritten reference (such as a Go string constant) and marks it as requested in the review UI.
func (e *Editor) AskAgentToUpdateUsage(file string, line int) error {
	if e == nil || !e.usages.open {
		return nil
	}
	oldPath := e.usages.target
	newPath := ""
	if e.usages.plan != nil {
		oldPath = e.usages.plan.From
		newPath = e.usages.plan.To
	} else if e.usages.appliedFrom != "" {
		oldPath = e.usages.appliedFrom
		newPath = e.usages.appliedTo
	}
	var ident string
	if fg := e.usages.report.FileByPath(file); fg != nil {
		for _, r := range fg.Refs {
			if line == 0 || r.Line == line {
				ident = r.GoIdent
				if line == 0 {
					line = r.Line
				}
				break
			}
		}
	}
	req := SafeRenameAgentRequest{
		File:    file,
		Line:    line,
		Ident:   ident,
		OldPath: oldPath,
		NewPath: newPath,
	}
	if e.OnAskAgentUpdateRef != nil {
		if err := e.OnAskAgentUpdateRef(req); err != nil {
			return err
		}
	}
	if e.usages.askSentFiles == nil {
		e.usages.askSentFiles = make(map[string]bool)
	}
	e.usages.askSentFiles[file] = true
	if e.usages.plan != nil {
		for i := range e.usages.plan.Problems {
			if e.usages.plan.Problems[i].File == file {
				e.usages.plan.Problems[i].AskSent = true
			}
		}
	}
	for i := range e.usages.checkProblems {
		if e.usages.checkProblems[i].File == file {
			e.usages.checkProblems[i].AskSent = true
		}
	}
	e.usages.rev++
	e.Status = fmt.Sprintf("Asked agent to update %s", file)
	e.notify()
	return nil
}

func (e *Editor) ensureValidUsageFocus() {
	if e.usages.focusedFile == movedFocusSentinel && e.usages.mode == ReferencesModeRename {
		return
	}
	if e.usages.report != nil && e.usages.focusedFile != "" {
		if fg := e.usages.report.FileByPath(e.usages.focusedFile); fg != nil {
			if len(fg.Refs) > 0 && e.usages.focusedRefIdx >= len(fg.Refs) {
				e.usages.focusedRefIdx = len(fg.Refs) - 1
			}
			return
		}
	}
	if e.usages.report != nil {
		all := e.usages.report.AllFiles()
		if len(all) > 0 {
			e.usages.focusedFile = all[0].File
			e.usages.focusedRefIdx = 0
			return
		}
	}
	e.usages.focusedFile = ""
	e.usages.focusedRefIdx = 0
}

func (e *Editor) currentReturnSceneName() string {
	if e.Doc != nil && strings.TrimSpace(e.Doc.Name) != "" {
		return e.Doc.Name
	}
	if e.ScenePath != "" {
		_, stem, _ := splitRenameParts(e.ScenePath)
		if stem != "" {
			return stem
		}
	}
	return "scene"
}

// --- Core scanning & grouping ---

func scanUsagesInDir(root, target string) (*UsagesReport, error) {
	fsys := os.DirFS(root)
	scan, err := refs.Scan(fsys)
	if err != nil {
		return nil, err
	}
	isDir := false
	if info, err := fs.Stat(fsys, target); err == nil && info.IsDir() {
		isDir = true
	}

	kindByFile := make(map[string]refs.FileKind, len(scan.Files))
	for _, f := range scan.Files {
		kindByFile[f.Path] = f.Kind
	}

	// Collect YAML/JSON references matching target.
	prefix := target + "/"
	byFile := make(map[string][]refs.Ref)
	for _, r := range scan.Refs {
		if r.Target == target || (isDir && strings.HasPrefix(r.Target, prefix)) {
			byFile[r.File] = append(byFile[r.File], r)
		}
	}

	rep := &UsagesReport{Target: target}

	filePaths := make([]string, 0, len(byFile))
	for fp := range byFile {
		filePaths = append(filePaths, fp)
	}
	slices.Sort(filePaths)

	for _, fp := range filePaths {
		fileRefs := byFile[fp]
		slices.SortFunc(fileRefs, func(a, b refs.Ref) int {
			return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
		})
		var raw []byte
		if _, abs, rerr := resolveProjectFileWithinRoot(root, fp, false); rerr == nil {
			raw, _ = os.ReadFile(abs)
		}
		kind := kindByFile[fp]
		group := classifyUsageFile(fp, kind, fileRefs)
		fg := buildUsageFileGroup(fp, group, raw, fileRefs)
		switch group {
		case UsageGroupScenes:
			rep.Scenes = append(rep.Scenes, fg)
		case UsageGroupVariants:
			rep.Variants = append(rep.Variants, fg)
		case UsageGroupDataTables:
			rep.DataTables = append(rep.DataTables, fg)
		default:
			rep.Other = append(rep.Other, fg)
		}
		rep.TotalFiles++
		rep.TotalUsages += len(fg.Refs)
	}

	// Scan Go source files using go/parser + go/ast.
	goGroups := scanGoUsages(root, target, isDir)
	for _, gg := range goGroups {
		rep.GoCode = append(rep.GoCode, gg)
		rep.TotalFiles++
		rep.TotalUsages += len(gg.Refs)
	}

	return rep, nil
}

func classifyUsageFile(filePath string, kind refs.FileKind, fileRefs []refs.Ref) UsageGroupKind {
	allInherits := len(fileRefs) > 0
	for _, r := range fileRefs {
		if r.Field != refs.FieldInherits {
			allInherits = false
			break
		}
	}
	if allInherits {
		return UsageGroupVariants
	}
	lower := strings.ToLower(filePath)
	switch {
	case kind == refs.KindScene || strings.HasSuffix(lower, ".scene.yaml") || strings.HasSuffix(lower, ".scene.yml"):
		return UsageGroupScenes
	case kind == refs.KindPrefab || strings.HasSuffix(lower, ".prefab.yaml") || strings.HasSuffix(lower, ".prefab.yml"):
		return UsageGroupVariants
	case kind == refs.KindDataTable || (len(fileRefs) > 0 && fileRefs[0].Field == refs.FieldDataTable):
		return UsageGroupDataTables
	default:
		return UsageGroupOther
	}
}

func buildUsageFileGroup(filePath string, group UsageGroupKind, raw []byte, fileRefs []refs.Ref) UsageFileGroup {
	_, stem, _ := splitRenameParts(filePath)
	fg := UsageFileGroup{
		File:  filePath,
		Name:  stem,
		Group: group,
	}

	switch group {
	case UsageGroupScenes:
		meta := parseSceneUsageMeta(raw)
		if meta.name != "" {
			fg.Name = meta.name
		}
		fg.VirtualWidth = meta.virtualWidth
		fg.VirtualHeight = meta.virtualHeight
		fg.BackgroundColor = meta.bgColor
		for _, r := range fileRefs {
			objMeta := findSceneObjectUsageMeta(meta.objects, r.Line)
			label := objMeta.id
			if label == "" {
				label = fmt.Sprintf("%s: %s", r.Field, r.Value)
			}
			fg.Refs = append(fg.Refs, UsageItem{
				File:          filePath,
				Line:          r.Line,
				Column:        r.Column,
				Group:         group,
				Label:         label,
				Field:         r.Field,
				Value:         r.Value,
				ObjectID:      objMeta.id,
				TopObjectID:   objMeta.topID,
				X:             objMeta.x,
				Y:             objMeta.y,
				HasPos:        objMeta.hasPos,
				OverrideCount: len(objMeta.overrides),
				Overrides:     slices.Clone(objMeta.overrides),
			})
		}
	case UsageGroupVariants:
		for _, r := range fileRefs {
			fg.Refs = append(fg.Refs, UsageItem{
				File:   filePath,
				Line:   r.Line,
				Column: r.Column,
				Group:  group,
				Label:  fmt.Sprintf("%s: %s", r.Field, r.Value),
				Field:  r.Field,
				Value:  r.Value,
			})
		}
	case UsageGroupDataTables:
		rows := parseDataTableRowSpans(raw)
		for _, r := range fileRefs {
			rowID := findDataTableRowID(rows, r.Line)
			label := rowID
			if label == "" {
				label = fmt.Sprintf("line %d", r.Line)
			}
			fg.Refs = append(fg.Refs, UsageItem{
				File:   filePath,
				Line:   r.Line,
				Column: r.Column,
				Group:  group,
				Label:  label,
				Field:  r.Field,
				Value:  r.Value,
				RowID:  rowID,
			})
		}
	default:
		for _, r := range fileRefs {
			fg.Refs = append(fg.Refs, UsageItem{
				File:   filePath,
				Line:   r.Line,
				Column: r.Column,
				Group:  group,
				Label:  fmt.Sprintf("%s: %s", r.Field, r.Value),
				Field:  r.Field,
				Value:  r.Value,
			})
		}
	}
	return fg
}

// --- Scene YAML metadata extraction for usages & scene preview cards ---

type sceneUsageMeta struct {
	name          string
	virtualWidth  float32
	virtualHeight float32
	bgColor       string
	objects       []sceneObjUsageMeta
}

type sceneObjUsageMeta struct {
	id        string
	topID     string
	minLine   int
	maxLine   int
	x, y      float32
	hasPos    bool
	overrides []string
}

func parseSceneUsageMeta(raw []byte) sceneUsageMeta {
	meta := sceneUsageMeta{virtualWidth: 640, virtualHeight: 360}
	if len(raw) == 0 {
		return meta
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return meta
	}
	doc := yamlDeref(root.Content[0])
	if doc == nil || doc.Kind != yaml.MappingNode {
		return meta
	}
	meta.name = yamlFieldString(doc, "name")
	if vw := yamlFieldFloat(doc, "virtualWidth"); vw > 0 {
		meta.virtualWidth = vw
	}
	if vh := yamlFieldFloat(doc, "virtualHeight"); vh > 0 {
		meta.virtualHeight = vh
	}
	meta.bgColor = yamlFieldString(doc, "backgroundColor")

	layers := yamlField(doc, "layers")
	if layers != nil && layers.Kind == yaml.SequenceNode {
		for _, ln := range layers.Content {
			ln = yamlDeref(ln)
			if ln == nil || ln.Kind != yaml.MappingNode {
				continue
			}
			objs := yamlField(ln, "objects")
			if objs == nil || objs.Kind != yaml.SequenceNode {
				continue
			}
			for _, on := range objs.Content {
				on = yamlDeref(on)
				if on == nil || on.Kind != yaml.MappingNode {
					continue
				}
				topID := yamlFieldString(on, "id")
				collectSceneObjUsageMeta(on, topID, 0, &meta.objects)
			}
		}
	}
	return meta
}

func collectSceneObjUsageMeta(n *yaml.Node, topID string, depth int, out *[]sceneObjUsageMeta) {
	if n == nil || n.Kind != yaml.MappingNode || depth > 128 {
		return
	}
	minL, maxL := yamlLineBounds(n)
	id := yamlFieldString(n, "id")
	xNode := yamlField(n, "x")
	yNode := yamlField(n, "y")
	hasPos := xNode != nil || yNode != nil
	var x, y float32
	if xNode != nil {
		if f, err := strconv.ParseFloat(xNode.Value, 32); err == nil {
			x = float32(f)
		}
	}
	if yNode != nil {
		if f, err := strconv.ParseFloat(yNode.Value, 32); err == nil {
			y = float32(f)
		}
	}
	overrides := extractObjectOverrides(n)
	*out = append(*out, sceneObjUsageMeta{
		id:        id,
		topID:     topID,
		minLine:   minL,
		maxLine:   maxL,
		x:         x,
		y:         y,
		hasPos:    hasPos,
		overrides: overrides,
	})
	if children := yamlField(n, "children"); children != nil && children.Kind == yaml.SequenceNode {
		for _, ch := range children.Content {
			collectSceneObjUsageMeta(yamlDeref(ch), topID, depth+1, out)
		}
	}
}

func extractObjectOverrides(objNode *yaml.Node) []string {
	var out []string
	if ov := yamlField(objNode, "overrides"); ov != nil && ov.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(ov.Content); i += 2 {
			out = append(out, formatCompactYAMLOverride(ov.Content[i].Value, yamlDeref(ov.Content[i+1])))
		}
		return out
	}
	// Prefab instances may also carry per-instance component or property overrides directly.
	if yamlField(objNode, "prefab") != nil {
		if comps := yamlField(objNode, "components"); comps != nil && comps.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(comps.Content); i += 2 {
				out = append(out, formatCompactYAMLOverride(comps.Content[i].Value, yamlDeref(comps.Content[i+1])))
			}
		}
		if props := yamlField(objNode, "properties"); props != nil && props.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(props.Content); i += 2 {
				out = append(out, formatCompactYAMLOverride(props.Content[i].Value, yamlDeref(props.Content[i+1])))
			}
		}
	}
	return out
}

func formatCompactYAMLOverride(key string, val *yaml.Node) string {
	if val == nil {
		return key
	}
	switch val.Kind {
	case yaml.ScalarNode:
		return key + ": " + val.Value
	case yaml.MappingNode:
		parts := make([]string, 0, len(val.Content)/2)
		for i := 0; i+1 < len(val.Content); i += 2 {
			k := val.Content[i].Value
			v := yamlDeref(val.Content[i+1])
			if v != nil && v.Kind == yaml.ScalarNode {
				parts = append(parts, k+": "+v.Value)
			} else {
				parts = append(parts, k+": …")
			}
		}
		return fmt.Sprintf("%s: { %s }", key, strings.Join(parts, ", "))
	default:
		return key
	}
}

func findSceneObjectUsageMeta(objs []sceneObjUsageMeta, line int) sceneObjUsageMeta {
	// Prefer the narrowest (most specific nested) object span containing line.
	best := sceneObjUsageMeta{}
	bestSpan := int(^uint(0) >> 1)
	for _, o := range objs {
		if o.minLine <= line && line <= o.maxLine {
			sp := o.maxLine - o.minLine
			if sp < bestSpan {
				bestSpan = sp
				best = o
			}
		}
	}
	return best
}

func yamlFieldFloat(n *yaml.Node, key string) float32 {
	v := yamlField(n, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return 0
	}
	f, err := strconv.ParseFloat(v.Value, 32)
	if err != nil {
		return 0
	}
	return float32(f)
}

// --- Data table row span extraction ---

type dataTableRowSpan struct {
	id      string
	minLine int
	maxLine int
}

func parseDataTableRowSpans(raw []byte) []dataTableRowSpan {
	if len(raw) == 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	doc := yamlDeref(root.Content[0])
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	var out []dataTableRowSpan
	for i := 0; i+1 < len(doc.Content); i += 2 {
		seq := yamlDeref(doc.Content[i+1])
		if seq == nil || seq.Kind != yaml.SequenceNode {
			continue
		}
		for idx, item := range seq.Content {
			row := yamlDeref(item)
			if row == nil || row.Kind != yaml.MappingNode {
				continue
			}
			minL, maxL := yamlLineBounds(row)
			id := yamlFieldString(row, "id")
			if id == "" {
				id = yamlFieldString(row, "name")
			}
			if id == "" {
				id = yamlFieldString(row, "key")
			}
			if id == "" {
				id = fmt.Sprintf("row_%d", idx+1)
			}
			out = append(out, dataTableRowSpan{id: id, minLine: minL, maxLine: maxL})
		}
	}
	return out
}

func findDataTableRowID(rows []dataTableRowSpan, line int) string {
	for _, r := range rows {
		if r.minLine <= line && line <= r.maxLine {
			return r.id
		}
	}
	return ""
}

// --- Go AST scanner using go/parser + go/ast ---

func scanGoUsages(root, target string, isDir bool) []UsageFileGroup {
	fsys := os.DirFS(root)
	var goFiles []string
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == "." {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata" || name == "vendor" || name == "node_modules" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			goFiles = append(goFiles, p)
		}
		return nil
	})
	slices.Sort(goFiles)

	prefix := target + "/"
	var groups []UsageFileGroup

	for _, rel := range goFiles {
		_, abs, err := resolveProjectFileWithinRoot(root, rel, false)
		if err != nil {
			continue
		}
		src, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		fileAST, err := parser.ParseFile(fset, rel, src, 0)
		if err != nil {
			continue
		}

		// Record identifier names for string literals inside ValueSpec declarations (const / var).
		identByPos := make(map[token.Pos]string)
		ast.Inspect(fileAST, func(n ast.Node) bool {
			vs, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, val := range vs.Values {
				if i < len(vs.Names) {
					name := vs.Names[i].Name
					ast.Inspect(val, func(vn ast.Node) bool {
						if lit, ok := vn.(*ast.BasicLit); ok && lit.Kind == token.STRING {
							identByPos[lit.Pos()] = name
						}
						return true
					})
				}
			}
			return true
		})

		var items []UsageItem
		ast.Inspect(fileAST, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			unquoted, err := strconv.Unquote(lit.Value)
			if err != nil || unquoted == "" {
				return true
			}
			norm := path.Clean(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(unquoted)), "/"))
			if norm != target && !(isDir && strings.HasPrefix(norm, prefix)) {
				return true
			}
			pos := fset.Position(lit.Pos())
			ident := identByPos[lit.Pos()]
			label := fmt.Sprintf("%q", unquoted)
			if ident != "" {
				label = fmt.Sprintf("%s = %q", ident, unquoted)
			}
			items = append(items, UsageItem{
				File:         rel,
				Line:         pos.Line,
				Column:       pos.Column,
				Group:        UsageGroupGoCode,
				Label:        label,
				Field:        "go string",
				Value:        unquoted,
				GoIdent:      ident,
				NotRewritten: true,
			})
			return true
		})

		if len(items) > 0 {
			slices.SortFunc(items, func(a, b UsageItem) int {
				return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column))
			})
			groups = append(groups, UsageFileGroup{
				File:         rel,
				Name:         path.Base(rel),
				Group:        UsageGroupGoCode,
				NotRewritten: true,
				Refs:         items,
			})
		}
	}
	return groups
}

// --- Safe Rename validation & dry-run plan computation ---

var compoundRenameSuffixes = []string{
	".prefab.yaml", ".prefab.yml",
	".scene.yaml", ".scene.yml",
	".material.yaml", ".material.yml",
	".tileset.yaml", ".tileset.yml",
	".terrain.yaml", ".terrain.yml",
	".cutscene.yaml", ".cutscene.yml",
	".screen.yaml", ".screen.yml",
	".states.yaml", ".states.yml",
	".gridmap.yaml", ".gridmap.yml",
	".particles.yaml", ".particles.yml",
	".animation.yaml", ".animation.yml",
	".skel.yaml", ".skel.yml",
	".rooms.yaml", ".rooms.yml",
}

func splitRenameParts(relPath string) (folderPrefix, stem, suffix string) {
	clean := path.Clean(filepath.ToSlash(strings.TrimSpace(relPath)))
	dir := path.Dir(clean)
	base := path.Base(clean)
	if dir != "." && dir != "/" {
		folderPrefix = dir + "/"
	}
	lower := strings.ToLower(base)
	for _, suf := range compoundRenameSuffixes {
		if strings.HasSuffix(lower, suf) {
			return folderPrefix, base[:len(base)-len(suf)], base[len(base)-len(suf):]
		}
	}
	ext := path.Ext(base)
	if ext != "" && ext != base {
		return folderPrefix, base[:len(base)-len(ext)], ext
	}
	return folderPrefix, base, ""
}

func (e *Editor) computeSafeRenamePlan(oldRel, newName string) (*SafeRenamePlan, error) {
	return e.computeSafeRenamePlanWithReport(oldRel, newName, e.usages.report)
}

func (e *Editor) computeSafeRenamePlanWithReport(oldRel, newName string, rep *UsagesReport) (*SafeRenamePlan, error) {
	root := filepath.Dir(e.ProjectPath)
	fsys := os.DirFS(root)
	folder, oldStem, suffix := splitRenameParts(oldRel)

	plan := &SafeRenamePlan{
		From:         oldRel,
		FolderPrefix: folder,
		OldStem:      oldStem,
		Suffix:       suffix,
	}

	if _, _, err := resolveProjectFileWithinRoot(root, oldRel, false); err != nil {
		plan.ValidationMsg = err.Error()
		return plan, err
	}

	// Read moved file contents for the "content unchanged" preview.
	if movedData, err := fs.ReadFile(fsys, oldRel); err == nil {
		plan.MovedContentLines = buildMovedPreviewLines(movedData)
	}

	// Populate non-rewritten Go problems up front so they are always visible during review.
	if rep != nil {
		for _, gf := range rep.GoCode {
			for _, r := range gf.Refs {
				plan.Problems = append(plan.Problems, SafeRenameProblem{
					File:         r.File,
					Line:         r.Line,
					Ident:        r.GoIdent,
					OldPath:      oldRel,
					Message:      fmt.Sprintf("%s loads the old path and is not rewritten", r.File),
					NotRewritten: true,
					AskSent:      e.usages.askSentFiles[r.File],
				})
			}
		}
	}

	raw := strings.TrimSpace(newName)
	if raw == "" {
		plan.ValidationMsg = "Enter a new name"
		return plan, errors.New(plan.ValidationMsg)
	}
	if strings.ContainsAny(raw, "\\:") || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsControl(r) }) >= 0 {
		plan.NewStem = raw
		plan.ValidationMsg = fmt.Sprintf("%q is not a valid project path", raw)
		return plan, errors.New(plan.ValidationMsg)
	}

	// Support either a bare stem ("slime_green") or a full relative path ("prefabs/slime_green.prefab.yaml").
	var targetRel, newStem string
	if strings.ContainsRune(raw, '/') || (suffix != "" && strings.HasSuffix(strings.ToLower(raw), strings.ToLower(suffix))) {
		norm := e.normalizeProjectPath(raw)
		if norm == "" {
			plan.NewStem = raw
			plan.ValidationMsg = fmt.Sprintf("%q must be a slash-separated path inside the project", raw)
			return plan, errors.New(plan.ValidationMsg)
		}
		targetRel = norm
		_, newStem, _ = splitRenameParts(norm)
	} else {
		if raw == "." || raw == ".." || strings.Contains(raw, "..") {
			plan.NewStem = raw
			plan.ValidationMsg = fmt.Sprintf("%q must be a safe file name", raw)
			return plan, errors.New(plan.ValidationMsg)
		}
		norm := e.normalizeProjectPath(folder + raw + suffix)
		if norm == "" {
			plan.NewStem = raw
			plan.ValidationMsg = fmt.Sprintf("%q must be a safe file name", raw)
			return plan, errors.New(plan.ValidationMsg)
		}
		newStem = raw
		targetRel = norm
	}

	plan.To = targetRel
	plan.NewStem = newStem
	for i := range plan.Problems {
		plan.Problems[i].NewPath = targetRel
	}

	if _, _, err := resolveProjectFileWithinRoot(root, targetRel, true); err != nil {
		plan.ValidationMsg = err.Error()
		return plan, err
	}

	if targetRel == oldRel {
		plan.ValidationMsg = "Enter a different name from " + oldStem
		return plan, errors.New(plan.ValidationMsg)
	}
	base := path.Base(targetRel)
	if base == project.FileName || base == refs.IndexFileName {
		plan.ValidationMsg = fmt.Sprintf("Cannot rename to reserved file %s", base)
		return plan, errors.New(plan.ValidationMsg)
	}

	// Run refs.PlanMove as a pure dry-run — it validates all `game mv` rules and computes every rewritten file in memory without touching disk.
	refPlan, err := refs.PlanMove(fsys, oldRel, targetRel)
	if err != nil {
		msg := strings.TrimPrefix(err.Error(), "refs: ")
		plan.ValidationMsg = msg
		return plan, err
	}

	back := movedPath(targetRel, oldRel)
	var preservedAll []string
	preservedSet := make(map[string]bool)

	for _, w := range refPlan.Writes {
		if path.Base(w.Path) == refs.IndexFileName {
			plan.IndexUpdated = true
			continue
		}
		oldPath := back(w.Path)
		_, oldAbs, oerr := resolveProjectFileWithinRoot(root, oldPath, false)
		if oerr != nil {
			continue
		}
		oldData, rerr := os.ReadFile(oldAbs)
		if rerr != nil {
			continue
		}
		if bytes.Equal(oldData, w.Data) {
			continue
		}
		diffs, previewLines := computeFileLineDiffs(oldData, w.Data)
		if len(diffs) == 0 {
			continue
		}
		group := UsageGroupOther
		var filePreserved []string
		if rep != nil {
			if fg := rep.FileByPath(oldPath); fg != nil {
				group = fg.Group
				for _, r := range fg.Refs {
					if r.OverrideCount > 0 && r.ObjectID != "" && !slices.Contains(filePreserved, r.ObjectID) {
						filePreserved = append(filePreserved, r.ObjectID)
						if !preservedSet[r.ObjectID] {
							preservedSet[r.ObjectID] = true
							preservedAll = append(preservedAll, r.ObjectID)
						}
					}
				}
			}
		}
		plan.Files = append(plan.Files, SafeRenameFileDiff{
			Path:               w.Path,
			OldPath:            oldPath,
			Group:              group,
			ChangeCount:        len(diffs),
			Diffs:              diffs,
			PreviewLines:       previewLines,
			PreservedOverrides: filePreserved,
		})
		plan.RewrittenRefCount += len(diffs)
	}

	plan.RewrittenFileCount = len(plan.Files)
	plan.TotalTouchedFiles = len(plan.Files) + 1
	plan.PreservedObjects = preservedAll
	plan.Valid = true
	plan.ValidationMsg = validationNoteForStem(newStem)
	return plan, nil
}

func validationNoteForStem(stem string) string {
	if isSnakeCase(stem) {
		return "Free name, snake_case"
	}
	return "Free name"
}

func isSnakeCase(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		if r == '_' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	return true
}

// --- Code & diff preview line builders ---

func buildMovedPreviewLines(data []byte) []CodePreviewLine {
	rawLines := splitTextLines(data)
	maxShow := min(len(rawLines), 24)
	out := make([]CodePreviewLine, 0, maxShow+2)
	for i := 0; i < maxShow; i++ {
		out = append(out, CodePreviewLine{LineNumber: i + 1, Text: rawLines[i]})
	}
	out = append(out,
		CodePreviewLine{Text: ""},
		CodePreviewLine{Kind: "note", Text: "# the file moves; the editor never edits its text"},
	)
	return out
}

func computeFileLineDiffs(oldData, newData []byte) ([]SafeRenameLineDiff, []CodePreviewLine) {
	oldLines := splitTextLines(oldData)
	newLines := splitTextLines(newData)
	n := max(len(oldLines), len(newLines))

	var diffs []SafeRenameLineDiff
	changedIdx := make([]int, 0)
	for i := 0; i < n; i++ {
		var o, nw string
		if i < len(oldLines) {
			o = oldLines[i]
		}
		if i < len(newLines) {
			nw = newLines[i]
		}
		if o != nw {
			diffs = append(diffs, SafeRenameLineDiff{Line: i + 1, OldText: o, NewText: nw})
			changedIdx = append(changedIdx, i)
		}
	}
	if len(diffs) == 0 {
		return nil, nil
	}

	// Build compact context hunks showing 1 line before and up to 5 lines after each changed line
	// (capturing x/y and any preserved overrides/components block right underneath).
	type hunk struct{ start, end int }
	var hunks []hunk
	for _, idx := range changedIdx {
		s := max(0, idx-1)
		e := min(len(oldLines)-1, idx+5)
		// Stop before an unrelated sibling object if we didn't hit overrides.
		for k := idx + 1; k <= e; k++ {
			trimmed := strings.TrimSpace(oldLines[k])
			if strings.HasPrefix(trimmed, "- id:") && k > idx+1 {
				e = k - 1
				break
			}
		}
		if len(hunks) > 0 && s <= hunks[len(hunks)-1].end+1 {
			if e > hunks[len(hunks)-1].end {
				hunks[len(hunks)-1].end = e
			}
		} else {
			hunks = append(hunks, hunk{start: s, end: e})
		}
	}

	diffByIdx := make(map[int]SafeRenameLineDiff, len(diffs))
	for _, d := range diffs {
		diffByIdx[d.Line-1] = d
	}

	var preview []CodePreviewLine
	for hi, h := range hunks {
		if hi > 0 {
			preview = append(preview, CodePreviewLine{Kind: "gap", Text: "⋯"})
		}
		for i := h.start; i <= h.end; i++ {
			if d, ok := diffByIdx[i]; ok {
				preview = append(preview,
					CodePreviewLine{LineNumber: d.Line, Sign: "−", Kind: "del", Text: d.OldText},
					CodePreviewLine{LineNumber: d.Line, Sign: "+", Kind: "add", Text: d.NewText},
				)
			} else if i < len(oldLines) {
				preview = append(preview, CodePreviewLine{LineNumber: i + 1, Text: oldLines[i]})
			}
		}
	}
	return diffs, preview
}

// buildUsagePreviewLines builds the right-pane code lines for the focused file in Usages / Done / Rename mode.
func (e *Editor) buildUsagePreviewLines(state *ReferencesScreenState) (title, meta string, lines []CodePreviewLine) {
	if state == nil || e.ProjectPath == "" {
		return "", "", nil
	}
	if state.FocusedFile == movedFocusSentinel && state.Plan != nil {
		return state.Plan.From + " → " + state.Plan.To, "content unchanged", state.Plan.MovedContentLines
	}
	fg := state.Report.FileByPath(state.FocusedFile)
	if fg == nil {
		return "", "", nil
	}

	// In Rename mode, show the exact YAML diff for rewritten files.
	if state.Mode == ReferencesModeRename && state.Plan != nil && state.Plan.Valid && !fg.NotRewritten {
		if fd := state.Plan.FileDiff(fg.File); fd != nil {
			return fg.File, fmt.Sprintf("−%d +%d", fd.ChangeCount, fd.ChangeCount), fd.PreviewLines
		}
	}

	focusLine := 0
	if len(fg.Refs) > 0 {
		idx := min(max(state.FocusedRefIdx, 0), len(fg.Refs)-1)
		focusLine = fg.Refs[idx].Line
	}

	cacheKey := usagePreviewCacheKey{
		projectPath:  e.ProjectPath,
		file:         fg.File,
		group:        fg.Group,
		notRewritten: fg.NotRewritten,
		refCount:     len(fg.Refs),
		focusLine:    focusLine,
		filesRev:     e.filesRev,
		report:       e.usages.report,
	}
	if e.usages.report != nil && e.usages.previewCache.valid && e.usages.previewCache.key == cacheKey {
		return e.usages.previewCache.title, e.usages.previewCache.meta, e.usages.previewCache.lines
	}

	_, abs, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), fg.File, false)
	if err != nil {
		return fg.File, "", []CodePreviewLine{{Kind: "note", Text: err.Error()}}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return fg.File, "", []CodePreviewLine{{Kind: "note", Text: err.Error()}}
	}
	rawLines := splitTextLines(data)

	refLines := make(map[int]bool, len(fg.Refs))
	for _, r := range fg.Refs {
		if r.Line > 0 {
			refLines[r.Line] = true
		}
	}

	// Build hunks around every reference line in this file.
	type hunk struct{ start, end int }
	var hunks []hunk
	beforeCtx, afterCtx := 1, 4
	if fg.Group == UsageGroupGoCode {
		beforeCtx, afterCtx = 2, 5
	}
	for _, r := range fg.Refs {
		if r.Line <= 0 || r.Line > len(rawLines) {
			continue
		}
		idx := r.Line - 1
		s := max(0, idx-beforeCtx)
		en := min(len(rawLines)-1, idx+afterCtx)
		if fg.Group == UsageGroupScenes {
			for k := idx + 1; k <= en; k++ {
				if strings.HasPrefix(strings.TrimSpace(rawLines[k]), "- id:") && k > idx+1 {
					en = k - 1
					break
				}
			}
		}
		if len(hunks) > 0 && s <= hunks[len(hunks)-1].end+1 {
			if en > hunks[len(hunks)-1].end {
				hunks[len(hunks)-1].end = en
			}
		} else {
			hunks = append(hunks, hunk{start: s, end: en})
		}
	}
	if len(hunks) == 0 && len(rawLines) > 0 {
		hunks = append(hunks, hunk{start: 0, end: min(len(rawLines)-1, 15)})
	}

	var out []CodePreviewLine
	for hi, h := range hunks {
		if hi > 0 {
			out = append(out, CodePreviewLine{Kind: "gap", Text: "⋯"})
		}
		for i := h.start; i <= h.end; i++ {
			ln := i + 1
			kind := ""
			focused := false
			if refLines[ln] {
				if fg.NotRewritten {
					kind = "warn"
				} else {
					kind = "hit"
				}
				focused = ln == focusLine
			}
			out = append(out, CodePreviewLine{
				LineNumber: ln,
				Kind:       kind,
				Text:       rawLines[i],
				Focused:    focused,
			})
		}
	}

	switch fg.Group {
	case UsageGroupScenes:
		if len(fg.Refs) == 1 {
			meta = "1 instance"
		} else {
			meta = fmt.Sprintf("%d instances", len(fg.Refs))
		}
	case UsageGroupDataTables:
		if len(fg.Refs) == 1 {
			meta = "1 row"
		} else {
			meta = fmt.Sprintf("%d rows", len(fg.Refs))
		}
	default:
		if focusLine > 0 {
			meta = fmt.Sprintf("line %d", focusLine)
		}
	}
	if e.usages.report != nil {
		e.usages.previewCache = usagePreviewCache{
			valid: true,
			key:   cacheKey,
			title: fg.File,
			meta:  meta,
			lines: out,
		}
	}
	return fg.File, meta, out
}

func splitTextLines(data []byte) []string {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// --- Deep clone helpers ---

func cloneUsagesReport(r *UsagesReport) *UsagesReport {
	if r == nil {
		return nil
	}
	cp := *r
	cp.Scenes = cloneUsageFileGroups(r.Scenes)
	cp.Variants = cloneUsageFileGroups(r.Variants)
	cp.DataTables = cloneUsageFileGroups(r.DataTables)
	cp.GoCode = cloneUsageFileGroups(r.GoCode)
	cp.Other = cloneUsageFileGroups(r.Other)
	return &cp
}

func cloneUsageFileGroups(src []UsageFileGroup) []UsageFileGroup {
	if len(src) == 0 {
		return nil
	}
	out := make([]UsageFileGroup, len(src))
	for i, g := range src {
		g.Refs = slices.Clone(g.Refs)
		for j := range g.Refs {
			g.Refs[j].Overrides = slices.Clone(g.Refs[j].Overrides)
		}
		out[i] = g
	}
	return out
}

func cloneSafeRenamePlan(p *SafeRenamePlan) *SafeRenamePlan {
	if p == nil {
		return nil
	}
	cp := *p
	cp.MovedContentLines = slices.Clone(p.MovedContentLines)
	cp.Problems = slices.Clone(p.Problems)
	cp.PreservedObjects = slices.Clone(p.PreservedObjects)
	if len(p.Files) > 0 {
		cp.Files = make([]SafeRenameFileDiff, len(p.Files))
		for i, f := range p.Files {
			f.Diffs = slices.Clone(f.Diffs)
			f.PreviewLines = slices.Clone(f.PreviewLines)
			f.PreservedOverrides = slices.Clone(f.PreservedOverrides)
			cp.Files[i] = f
		}
	}
	return &cp
}

// Verify sceneformat import is referenced for SceneDoc helpers.
var _ = sceneformat.CurrentVersion
