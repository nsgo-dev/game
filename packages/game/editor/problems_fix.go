package editor

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/game/packages/game/tooling/assetcheck"
	"github.com/nsgo-dev/nsgo/core"
)

// Supported ProblemFixEdit.Kind values for agent-proposed problem fixes.
const (
	FixKindUpdateConnection  = "update_connection"
	FixKindRemoveConnection  = "remove_connection"
	FixKindSetSprite         = "set_sprite"
	FixKindSetMaterial       = "set_material"
	FixKindSetObjectID       = "set_object_id"
	FixKindSetComponentField = "set_component_field"
	FixKindRemoveComponent   = "remove_component"
	FixKindReplaceLine       = "replace_line"
)

// ProblemFixEdit describes one structured edit inside an agent's proposed fix
// for a problem. Proposals are validated on a cloned document without mutating
// the editor or disk; only ApplyProposedFix applies them as one undoable step.
type ProblemFixEdit struct {
	// Kind selects the edit operation (update_connection, remove_connection,
	// set_sprite, set_material, set_object_id, set_component_field,
	// remove_component, or replace_line).
	Kind string `json:"kind"`
	// File is the slash-separated project path to edit; defaults to Problem.File.
	File string `json:"file,omitempty"`

	// Connection edit fields (update_connection, remove_connection).
	ConnectionIndex int            `json:"connectionIndex,omitempty"`
	From            string         `json:"from,omitempty"`
	Event           string         `json:"event,omitempty"`
	To              string         `json:"to,omitempty"`
	Handler         string         `json:"handler,omitempty"`
	Once            *bool          `json:"once,omitempty"`
	Args            map[string]any `json:"args,omitempty"`

	// Object/component edit fields.
	ObjectID  string `json:"objectId,omitempty"`
	NewID     string `json:"newId,omitempty"`
	Atlas     string `json:"atlas,omitempty"`
	Frame     string `json:"frame,omitempty"`
	Component string `json:"component,omitempty"`
	Field     string `json:"field,omitempty"`
	Value     any    `json:"value,omitempty"`

	// Line-level YAML replacement (replace_line), validated against sceneformat.
	Line        int    `json:"line,omitempty"`
	OldText     string `json:"oldText,omitempty"`
	Replacement string `json:"replacement,omitempty"`
}

// ProblemDiffLine is one line of the before/after YAML diff shown on a proposed fix card.
type ProblemDiffLine struct {
	Kind string `json:"kind"` // "removed", "added", or "context"
	Line int    `json:"line,omitempty"`
	Text string `json:"text"`
}

// ProposedProblemFix is a fix proposed by an agent (for example via MCP
// problem_propose_fix) for a live problem. Creating a proposal never mutates
// any document or file; only ApplyProposedFix applies it as one undoable step.
type ProposedProblemFix struct {
	ID          string             `json:"id"`
	Problem     assetcheck.Problem `json:"problem"`
	Explanation string             `json:"explanation"`
	Edit        ProblemFixEdit     `json:"edit"`
	Diff        []ProblemDiffLine  `json:"diff"`
	ObjectID    string             `json:"objectId,omitempty"`
	Summary     string             `json:"summary,omitempty"`

	// Internal cross-project and staleness guards (never serialized to JSON).
	projectPath string
	baseHash    string
}

func (e *Editor) problemActionBlockedErr(action string) error {
	switch {
	case e == nil:
		return errors.New("editor: nil editor")
	case e.Playing():
		return ErrPlaying
	case e.StartScreenOpen():
		return fmt.Errorf("editor: close the Start Screen before %s", action)
	case e.MergeConflictOpen():
		return fmt.Errorf("editor: resolve the active merge conflict before %s", action)
	default:
		return nil
	}
}

// ProblemQuestionRequest is delivered to Editor.OnAskAboutProblem when the
// person submits a question from "Ask about this problem…" with the problem attached.
type ProblemQuestionRequest struct {
	Problem  assetcheck.Problem
	Question string
	ObjectID string
	Fix      *ProposedProblemFix
}

// ProblemQuestionRecord stores a submitted "Ask about this problem…" question
// shown in the Agent problem panel.
type ProblemQuestionRecord struct {
	Problem  assetcheck.Problem
	Question string
	ObjectID string
}

// ProblemAgentPanelOpen reports whether the right-hand Agent problem panel is visible.
func (e *Editor) ProblemAgentPanelOpen() bool {
	if e == nil {
		return false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return e.problems.agentPanelOpen
}

// SetProblemAgentPanelOpen shows or hides the right-hand Agent problem panel.
func (e *Editor) SetProblemAgentPanelOpen(open bool) {
	if e == nil {
		return
	}
	if open && (e.Playing() || e.StartScreenOpen() || e.MergeConflictOpen()) {
		return
	}
	if open && e.HistoryPanelOpen() {
		e.SetHistoryPanelOpen(false)
	}
	e.problems.mu.Lock()
	if e.problems.agentPanelOpen == open {
		e.problems.mu.Unlock()
		return
	}
	e.problems.agentPanelOpen = open
	if open && e.problems.activeProblem == nil {
		if len(e.problems.proposals) > 0 {
			p := e.problems.proposals[0].Problem
			e.problems.activeProblem = &p
		} else if len(e.problems.problems) > 0 {
			p := e.problems.problems[0]
			e.problems.activeProblem = &p
		}
	}
	e.problems.mu.Unlock()
	e.notify()
}

// ProblemOverlayVisible reports whether the Spec Problem overlay is drawn on
// live objects in the 2D viewport (true by default).
func (e *Editor) ProblemOverlayVisible() bool {
	if e == nil {
		return false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return !e.problems.problemOverlayHidden
}

// SetProblemOverlayVisible toggles the Spec Problem overlay in the 2D viewport.
func (e *Editor) SetProblemOverlayVisible(visible bool) {
	if e == nil {
		return
	}
	e.problems.mu.Lock()
	hidden := !visible
	if e.problems.problemOverlayHidden == hidden {
		e.problems.mu.Unlock()
		return
	}
	e.problems.problemOverlayHidden = hidden
	e.problems.mu.Unlock()
	e.notify()
}

// ActiveProblem returns the problem currently focused in the Agent problem panel.
func (e *Editor) ActiveProblem() (assetcheck.Problem, bool) {
	if e == nil {
		return assetcheck.Problem{}, false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	if e.problems.activeProblem != nil {
		return *e.problems.activeProblem, true
	}
	if len(e.problems.proposals) > 0 {
		return e.problems.proposals[0].Problem, true
	}
	if len(e.problems.problems) > 0 {
		return e.problems.problems[0], true
	}
	return assetcheck.Problem{}, false
}

// HighlightedProblemLine returns the file:line:col label and YAML line snippet
// highlighted by ShowProblem, or empty strings when none is highlighted.
func (e *Editor) HighlightedProblemLine() (loc, snippet string) {
	if e == nil {
		return "", ""
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return e.problems.highlightedLine, e.problems.highlightedSnippet
}

// ProblemQuestions returns a copy of questions asked via AskAboutProblem.
func (e *Editor) ProblemQuestions() []ProblemQuestionRecord {
	if e == nil {
		return nil
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	return slices.Clone(e.problems.questions)
}

// ProposedFixes returns a copy of all pending agent-proposed fixes.
func (e *Editor) ProposedFixes() []ProposedProblemFix {
	if e == nil {
		return nil
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	if len(e.problems.proposals) == 0 {
		return nil
	}
	out := make([]ProposedProblemFix, len(e.problems.proposals))
	for i, p := range e.problems.proposals {
		p.Diff = slices.Clone(p.Diff)
		out[i] = p
	}
	return out
}

// ProposedFixFor returns the pending proposed fix matching p, if any.
func (e *Editor) ProposedFixFor(p assetcheck.Problem) (ProposedProblemFix, bool) {
	if e == nil {
		return ProposedProblemFix{}, false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	for _, prop := range e.problems.proposals {
		if prop.Problem == p {
			prop.Diff = slices.Clone(prop.Diff)
			return prop, true
		}
	}
	return ProposedProblemFix{}, false
}

// ActiveProposedFix returns the pending proposed fix for the currently active
// problem, or the first pending proposed fix if any.
func (e *Editor) ActiveProposedFix() (ProposedProblemFix, bool) {
	if e == nil {
		return ProposedProblemFix{}, false
	}
	e.problems.mu.Lock()
	defer e.problems.mu.Unlock()
	if e.problems.activeProblem != nil {
		for _, prop := range e.problems.proposals {
			if prop.Problem == *e.problems.activeProblem {
				prop.Diff = slices.Clone(prop.Diff)
				return prop, true
			}
		}
	}
	if len(e.problems.proposals) > 0 {
		prop := e.problems.proposals[0]
		prop.Diff = slices.Clone(prop.Diff)
		return prop, true
	}
	return ProposedProblemFix{}, false
}

// DismissProposedFix removes a pending proposed fix without applying it.
func (e *Editor) DismissProposedFix(fixID string) {
	if e == nil {
		return
	}
	e.problems.mu.Lock()
	idx := slices.IndexFunc(e.problems.proposals, func(p ProposedProblemFix) bool { return p.ID == fixID })
	if idx < 0 {
		e.problems.mu.Unlock()
		return
	}
	e.problems.proposals = slices.Delete(e.problems.proposals, idx, idx+1)
	e.problems.mu.Unlock()
	e.notify()
}

// ProposeProblemFix stages a validated, non-mutating proposed fix from an agent
// for a live problem. It simulates the edit on a clone of the target scene to
// verify validity and compute the exact before/after YAML diff, then opens the
// Agent problem card. It NEVER modifies any open document, history, or file.
func (e *Editor) ProposeProblemFix(prob assetcheck.Problem, explanation string, edit ProblemFixEdit) (ProposedProblemFix, error) {
	if err := e.problemActionBlockedErr("proposing a problem fix"); err != nil {
		return ProposedProblemFix{}, err
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ProposedProblemFix{}, ErrNoProject
	}
	explanation = strings.TrimSpace(explanation)
	if explanation == "" {
		return ProposedProblemFix{}, errors.New("editor: proposed fix requires an explanation")
	}
	if strings.TrimSpace(edit.Kind) == "" {
		return ProposedProblemFix{}, errors.New("editor: proposed fix requires an edit kind")
	}

	matched, err := e.matchLiveProblem(prob)
	if err != nil {
		return ProposedProblemFix{}, err
	}

	targetFile := edit.File
	if targetFile == "" {
		targetFile = matched.File
	}
	root := filepath.Dir(e.ProjectPath)
	cleanTarget, _, err := resolveProjectFileWithinRoot(root, targetFile, true)
	if err != nil {
		return ProposedProblemFix{}, fmt.Errorf("editor: invalid fix target file %q: %w", targetFile, err)
	}
	targetFile = cleanTarget
	edit.File = targetFile

	beforeBytes, targetDoc, err := e.loadLiveSceneForFix(targetFile)
	if err != nil {
		return ProposedProblemFix{}, err
	}
	baseHash := sha256Hex(beforeBytes)

	afterDoc, summary, err := e.simulateFixEdit(targetDoc, beforeBytes, edit)
	if err != nil {
		return ProposedProblemFix{}, err
	}

	afterBytes, err := sceneformat.Encode(afterDoc)
	if err != nil {
		return ProposedProblemFix{}, fmt.Errorf("editor: proposed fix produces invalid scene: %w", err)
	}
	canonicalBefore, encErr := encodeSceneDocLive(targetDoc)
	if encErr != nil {
		canonicalBefore = beforeBytes
	}
	diff := computeProblemDiff(canonicalBefore, afterBytes)
	if len(diff) == 0 {
		diff = computeProblemDiff(beforeBytes, afterBytes)
	}
	if len(diff) == 0 {
		return ProposedProblemFix{}, errors.New("editor: proposed fix makes no change")
	}

	objID := ""
	if targetFile == e.ScenePath {
		objID = e.attributeProblemObject(matched, func() []yamlObjectSpan {
			return extractYAMLObjectSpans(beforeBytes)
		})
	} else {
		objID = extractProblemObjectIDFromDoc(targetDoc, matched, beforeBytes)
	}

	e.problems.mu.Lock()
	e.problems.nextFixSeq++
	fix := ProposedProblemFix{
		ID:          fmt.Sprintf("problem-fix-%d", e.problems.nextFixSeq),
		Problem:     matched,
		Explanation: explanation,
		Edit:        cloneProblemFixEdit(edit),
		Diff:        diff,
		ObjectID:    objID,
		Summary:     summary,
		projectPath: e.ProjectPath,
		baseHash:    baseHash,
	}
	// Replace any existing proposal for the same problem.
	replaced := false
	for i := range e.problems.proposals {
		if e.problems.proposals[i].Problem == matched {
			e.problems.proposals[i] = fix
			replaced = true
			break
		}
	}
	if !replaced {
		e.problems.proposals = append(e.problems.proposals, fix)
	}
	pCopy := matched
	e.problems.activeProblem = &pCopy
	e.problems.agentPanelOpen = true
	e.problems.mu.Unlock()

	e.Status = fmt.Sprintf("Agent proposed a fix for %s", matched.Location())
	e.notify()
	return fix, nil
}

// ApplyProposedFix applies the pending proposed fix identified by fixID (or the
// active/first pending fix when fixID is empty) as ONE undoable History step,
// displays a Spec toast with an Undo action, and re-runs the project check.
// This method is called only by user actions in the editor UI — never by MCP.
func (e *Editor) ApplyProposedFix(fixID string) error {
	if err := e.problemActionBlockedErr("applying a proposed fix"); err != nil {
		return err
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}

	e.problems.mu.Lock()
	idx := -1
	if fixID == "" {
		if e.problems.activeProblem != nil {
			idx = slices.IndexFunc(e.problems.proposals, func(p ProposedProblemFix) bool {
				return p.Problem == *e.problems.activeProblem
			})
		}
		if idx < 0 && len(e.problems.proposals) > 0 {
			idx = 0
		}
	} else {
		idx = slices.IndexFunc(e.problems.proposals, func(p ProposedProblemFix) bool { return p.ID == fixID })
	}
	if idx < 0 {
		e.problems.mu.Unlock()
		return fmt.Errorf("editor: proposed fix %q not found", fixID)
	}
	fix := e.problems.proposals[idx]
	e.problems.mu.Unlock()

	if fix.projectPath != "" && fix.projectPath != e.ProjectPath {
		return fmt.Errorf("editor: proposed fix %q belongs to another project", fix.ID)
	}

	targetFile := fix.Edit.File
	if targetFile == "" {
		targetFile = fix.Problem.File
	}
	cleanTarget, _, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), targetFile, true)
	if err != nil {
		return fmt.Errorf("editor: invalid fix target file %q: %w", targetFile, err)
	}
	targetFile = cleanTarget

	if fix.baseHash != "" {
		curBytes, _, err := e.loadLiveSceneForFix(targetFile)
		if err != nil {
			return fmt.Errorf("editor: load %s to verify proposed fix: %w", targetFile, err)
		}
		if sha256Hex(curBytes) != fix.baseHash {
			return fmt.Errorf("editor: proposed fix %q is stale because %s changed since the fix was proposed", fix.ID, targetFile)
		}
	}

	if targetFile != e.ScenePath {
		if err := e.OpenScene(targetFile); err != nil {
			return fmt.Errorf("editor: open %s to apply fix: %w", targetFile, err)
		}
	}
	if e.Doc == nil {
		return errors.New("editor: no open scene")
	}

	if err := e.applyFixEditUndoable(fix); err != nil {
		return err
	}

	toastProject := e.ProjectPath
	toastScene := e.ScenePath
	toastTabID := e.activeTabID()
	toastHistoryLen := 0
	if e.History != nil {
		toastHistoryLen = e.History.Len()
	}

	e.problems.mu.Lock()
	if rm := slices.IndexFunc(e.problems.proposals, func(p ProposedProblemFix) bool { return p.ID == fix.ID }); rm >= 0 {
		e.problems.proposals = slices.Delete(e.problems.proposals, rm, rm+1)
	}
	if len(e.problems.proposals) == 0 {
		e.problems.agentPanelOpen = false
		e.problems.activeProblem = nil
		e.problems.highlightedLine = ""
		e.problems.highlightedSnippet = ""
	} else {
		next := e.problems.proposals[0].Problem
		e.problems.activeProblem = &next
	}
	e.problems.mu.Unlock()

	e.ShowToast(Toast{
		ID:             "problem-fix-applied",
		Kind:           ToastSuccess,
		Message:        fmt.Sprintf("Applied fix to %s", path.Base(targetFile)),
		ActionLabel:    "Undo",
		ActionShortcut: "⌘Z",
		OnAction: func() {
			if e.Playing() || e.StartScreenOpen() || e.MergeConflictOpen() {
				return
			}
			if e.ProjectPath != toastProject || e.activeTabID() != toastTabID || e.ScenePath != toastScene {
				return
			}
			if e.History == nil || e.History.Len() != toastHistoryLen {
				return
			}
			if e.Undo() {
				e.CheckProblems()
			}
		},
	})
	e.CheckProblems()
	e.notify()
	return nil
}

// ShowProblem navigates to p, opens its scene (keeping other tabs' unsaved
// edits intact), selects and frames the offending object in the viewport when
// known, and highlights the exact file:line:col and YAML line in the Agent card.
func (e *Editor) ShowProblem(p assetcheck.Problem) error {
	if err := e.problemActionBlockedErr("showing a problem"); err != nil {
		return err
	}
	if e.Project == nil || e.ProjectPath == "" {
		return ErrNoProject
	}
	if err := e.OpenProblem(p); err != nil {
		return err
	}

	objID := ""
	if p.File == e.ScenePath {
		if data, err := e.liveFileBytes(p.File); err == nil {
			objID = e.attributeProblemObject(p, func() []yamlObjectSpan {
				return extractYAMLObjectSpans(data)
			})
		} else {
			objID = e.attributeProblemObject(p, func() []yamlObjectSpan { return nil })
		}
	}
	if objID != "" {
		if e.SelectObject(objID) {
			e.FrameSelection()
		}
	}

	snippet := ""
	if p.Line > 0 {
		if data, err := e.liveFileBytes(p.File); err == nil {
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if p.Line >= 1 && p.Line <= len(lines) {
				snippet = strings.TrimRight(lines[p.Line-1], "\r")
			}
		}
	}

	pCopy := p
	e.problems.mu.Lock()
	e.problems.activeProblem = &pCopy
	e.problems.highlightedLine = p.Location()
	e.problems.highlightedSnippet = snippet
	e.problems.agentPanelOpen = true
	e.problems.mu.Unlock()

	e.Status = "Showing " + p.Location()
	e.notify()
	return nil
}

// AskAboutProblem submits a question from "Ask about this problem…" with the
// problem (and any attributed object / proposed fix) attached.
func (e *Editor) AskAboutProblem(p assetcheck.Problem, question string) error {
	if err := e.problemActionBlockedErr("asking about a problem"); err != nil {
		return err
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return errors.New("editor: question must not be empty")
	}
	objID := ""
	if p.File == e.ScenePath {
		objID = e.attributeProblemObject(p, func() []yamlObjectSpan {
			data, _ := e.liveFileBytes(p.File)
			return extractYAMLObjectSpans(data)
		})
	}
	var fixPtr *ProposedProblemFix
	if fix, ok := e.ProposedFixFor(p); ok {
		fixCopy := fix
		fixPtr = &fixCopy
	}
	rec := ProblemQuestionRecord{
		Problem:  p,
		Question: question,
		ObjectID: objID,
	}
	e.problems.mu.Lock()
	e.problems.questions = append(e.problems.questions, rec)
	e.problems.mu.Unlock()

	e.Status = fmt.Sprintf("Asked agent about %s", p.Location())
	if e.OnAskAboutProblem != nil {
		e.OnAskAboutProblem(ProblemQuestionRequest{
			Problem:  p,
			Question: question,
			ObjectID: objID,
			Fix:      fixPtr,
		})
	}
	e.notify()
	return nil
}

// --- Internal helpers for fix simulation, diffing, and application ---

func (e *Editor) matchLiveProblem(want assetcheck.Problem) (assetcheck.Problem, error) {
	if want.File == "" {
		return assetcheck.Problem{}, errors.New("editor: problem must name a file")
	}
	cleanFile := path.Clean(strings.ReplaceAll(want.File, `\`, "/"))
	want.File = cleanFile

	probs := e.Problems()
	// 1. Exact match.
	for _, p := range probs {
		if p == want {
			return p, nil
		}
	}
	// 2. Match file + line (+ optional column/message).
	var lineMatches []assetcheck.Problem
	for _, p := range probs {
		if p.File != cleanFile {
			continue
		}
		if want.Line > 0 && p.Line != want.Line {
			continue
		}
		if want.Column > 0 && p.Column > 0 && p.Column != want.Column {
			continue
		}
		if want.Message != "" && !strings.Contains(p.Message, want.Message) {
			continue
		}
		lineMatches = append(lineMatches, p)
	}
	if len(lineMatches) == 1 {
		return lineMatches[0], nil
	}
	if len(lineMatches) > 1 {
		return lineMatches[0], nil
	}
	// 3. Fallback: match by file + message substring when line shifted slightly.
	if want.Message != "" {
		for _, p := range probs {
			if p.File == cleanFile && strings.Contains(p.Message, want.Message) {
				return p, nil
			}
		}
	}
	return assetcheck.Problem{}, fmt.Errorf("editor: no live problem matching %s (%q)", want.Location(), want.Message)
}

func (e *Editor) liveFileBytes(relPath string) ([]byte, error) {
	if e == nil || e.ProjectPath == "" {
		return nil, ErrNoProject
	}
	root := filepath.Dir(e.ProjectPath)
	clean, _, err := resolveProjectFileWithinRoot(root, relPath, true)
	if err != nil {
		return nil, err
	}
	if overrides := e.unsavedProjectFiles(); overrides != nil {
		if data, ok := overrides[clean]; ok {
			return data, nil
		}
	}
	return readProjectFileBounded(root, clean, maxInspectorYAMLBytes)
}

func (e *Editor) loadLiveSceneForFix(relPath string) ([]byte, *sceneformat.SceneDoc, error) {
	if e == nil || e.ProjectPath == "" {
		return nil, nil, ErrNoProject
	}
	clean, _, err := resolveProjectFileWithinRoot(filepath.Dir(e.ProjectPath), relPath, true)
	if err != nil {
		return nil, nil, err
	}
	activeRel := e.ScenePath
	if activeRel == "" {
		activeRel = e.projectRelPathForFile(e.FilePath)
	}
	if clean == activeRel && e.Doc != nil {
		data, err := e.liveFileBytes(clean)
		if err != nil {
			data, err = encodeSceneDocLive(e.Doc)
			if err != nil {
				return nil, nil, err
			}
		}
		return data, e.Doc.Clone(), nil
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
			data, err := e.liveFileBytes(clean)
			if err != nil {
				data, err = encodeSceneDocLive(tab.doc)
				if err != nil {
					return nil, nil, err
				}
			}
			return data, tab.doc.Clone(), nil
		}
	}
	data, err := e.liveFileBytes(clean)
	if err != nil {
		return nil, nil, err
	}
	doc, err := sceneformat.Parse(data)
	if err != nil {
		return nil, nil, fmt.Errorf("editor: parse %s: %w", clean, err)
	}
	return data, doc, nil
}

func (e *Editor) simulateFixEdit(doc *sceneformat.SceneDoc, rawYAML []byte, edit ProblemFixEdit) (*sceneformat.SceneDoc, string, error) {
	next := doc.Clone()
	switch edit.Kind {
	case FixKindUpdateConnection:
		if edit.ConnectionIndex < 0 || edit.ConnectionIndex >= len(next.Connections) {
			return nil, "", fmt.Errorf("editor: connectionIndex %d out of range (scene has %d connections)", edit.ConnectionIndex, len(next.Connections))
		}
		c := cloneConnectionDoc(next.Connections[edit.ConnectionIndex])
		if edit.From != "" {
			c.From = edit.From
		}
		if edit.Event != "" {
			c.Event = edit.Event
		}
		if edit.To != "" {
			c.To = edit.To
		}
		if edit.Handler != "" {
			c.Handler = edit.Handler
		}
		if edit.Once != nil {
			c.Once = *edit.Once
		}
		if edit.Args != nil {
			c.Args = cloneMap(edit.Args)
		}
		next.Connections[edit.ConnectionIndex] = c
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Update connection %s.%s → %s.%s", c.From, c.Event, c.To, c.Handler), nil

	case FixKindRemoveConnection:
		if edit.ConnectionIndex < 0 || edit.ConnectionIndex >= len(next.Connections) {
			return nil, "", fmt.Errorf("editor: connectionIndex %d out of range", edit.ConnectionIndex)
		}
		old := next.Connections[edit.ConnectionIndex]
		next.Connections = slices.Delete(next.Connections, edit.ConnectionIndex, edit.ConnectionIndex+1)
		if len(next.Connections) == 0 {
			next.Connections = nil
		}
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Remove connection %s.%s → %s.%s", old.From, old.Event, old.To, old.Handler), nil

	case FixKindSetSprite:
		if edit.ObjectID == "" {
			return nil, "", errors.New("editor: set_sprite requires objectId")
		}
		loc := locateObject(next, edit.ObjectID)
		if loc.obj == nil {
			return nil, "", fmt.Errorf("editor: object %q not found", edit.ObjectID)
		}
		atlas := edit.Atlas
		if atlas == "" && loc.obj.Sprite != nil {
			atlas = loc.obj.Sprite.Atlas
		}
		frame := edit.Frame
		if frame == "" && loc.obj.Sprite != nil {
			frame = loc.obj.Sprite.Frame
		}
		if atlas == "" && frame == "" {
			loc.obj.Sprite = nil
		} else {
			loc.obj.Sprite = &sceneformat.SpriteRef{Atlas: atlas, Frame: frame}
		}
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Set %s sprite to %s/%s", edit.ObjectID, atlas, frame), nil

	case FixKindSetMaterial:
		if edit.ObjectID == "" {
			return nil, "", errors.New("editor: set_material requires objectId")
		}
		loc := locateObject(next, edit.ObjectID)
		if loc.obj == nil {
			return nil, "", fmt.Errorf("editor: object %q not found", edit.ObjectID)
		}
		mat, _ := edit.Value.(string)
		loc.obj.Material = mat
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Set %s material to %q", edit.ObjectID, mat), nil

	case FixKindSetObjectID:
		if edit.ObjectID == "" || edit.NewID == "" {
			return nil, "", errors.New("editor: set_object_id requires objectId and newId")
		}
		loc := locateObject(next, edit.ObjectID)
		if loc.obj == nil {
			return nil, "", fmt.Errorf("editor: object %q not found", edit.ObjectID)
		}
		if edit.NewID == edit.ObjectID {
			return next, fmt.Sprintf("Rename %s to %s", edit.ObjectID, edit.NewID), nil
		}
		if e.simulatedObjectIDInUse(next, edit.NewID) {
			return nil, "", fmt.Errorf("editor: duplicate object id %q", edit.NewID)
		}
		cmd := &objectIDCmd{
			oldID:      edit.ObjectID,
			newID:      edit.NewID,
			instancing: loc.obj.Scene != "",
		}
		for i, conn := range next.Connections {
			from, fromOK := renameRef(conn.From, edit.ObjectID, edit.NewID, cmd.instancing)
			to, toOK := renameRef(conn.To, edit.ObjectID, edit.NewID, cmd.instancing)
			if fromOK || toOK {
				cmd.conns = append(cmd.conns, connRename{
					index:   i,
					oldFrom: conn.From,
					oldTo:   conn.To,
					newFrom: from,
					newTo:   to,
				})
			}
		}
		for _, key := range slices.Sorted(maps.Keys(next.Overrides)) {
			if renamed, ok := renameRef(key, edit.ObjectID, edit.NewID, cmd.instancing); ok {
				cmd.overrides = append(cmd.overrides, [2]string{key, renamed})
			}
		}
		cmd.apply(next, false)
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Rename %s to %s", edit.ObjectID, edit.NewID), nil

	case FixKindSetComponentField:
		if edit.ObjectID == "" || edit.Component == "" || edit.Field == "" {
			return nil, "", errors.New("editor: set_component_field requires objectId, component, and field")
		}
		loc := locateObject(next, edit.ObjectID)
		if loc.obj == nil {
			return nil, "", fmt.Errorf("editor: object %q not found", edit.ObjectID)
		}
		if loc.obj.Components == nil {
			return nil, "", fmt.Errorf("editor: object %q has no component %q", edit.ObjectID, edit.Component)
		}
		rawComp, ok := loc.obj.Components[edit.Component]
		if !ok {
			return nil, "", fmt.Errorf("editor: object %q has no component %q", edit.ObjectID, edit.Component)
		}
		m, ok := rawComp.(map[string]any)
		if !ok {
			m = make(map[string]any)
		} else {
			m = cloneMap(m)
		}
		m[edit.Field] = cloneAny(edit.Value)
		loc.obj.Components[edit.Component] = m
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Set %s.%s.%s", edit.ObjectID, edit.Component, edit.Field), nil

	case FixKindRemoveComponent:
		if edit.ObjectID == "" || edit.Component == "" {
			return nil, "", errors.New("editor: remove_component requires objectId and component")
		}
		loc := locateObject(next, edit.ObjectID)
		if loc.obj == nil {
			return nil, "", fmt.Errorf("editor: object %q not found", edit.ObjectID)
		}
		if _, ok := loc.obj.Components[edit.Component]; !ok {
			return nil, "", fmt.Errorf("editor: object %q has no component %q", edit.ObjectID, edit.Component)
		}
		delete(loc.obj.Components, edit.Component)
		if len(loc.obj.Components) == 0 {
			loc.obj.Components = nil
		}
		if err := e.validateSimulatedScene(next); err != nil {
			return nil, "", err
		}
		return next, fmt.Sprintf("Remove %s from %s", edit.Component, edit.ObjectID), nil

	case FixKindReplaceLine:
		patched, err := applyReplaceLineYAML(rawYAML, edit.Line, edit.OldText, edit.Replacement)
		if err != nil {
			return nil, "", err
		}
		parsed, err := sceneformat.ParseWith(patched, sceneformat.ParseOptions{Components: e.components})
		if err != nil {
			return nil, "", fmt.Errorf("editor: replace_line yields invalid scene: %w", err)
		}
		if err := e.validateSimulatedScene(parsed); err != nil {
			return nil, "", err
		}
		return parsed, fmt.Sprintf("Replace line %d", edit.Line), nil

	default:
		return nil, "", fmt.Errorf("editor: unknown fix edit kind %q", edit.Kind)
	}
}

func (e *Editor) simulatedObjectIDInUse(doc *sceneformat.SceneDoc, id string) bool {
	if doc == nil || id == "" {
		return false
	}
	if locateObject(doc, id).obj != nil {
		return true
	}
	if doc.RefersToScenes() {
		if scenes, ok := e.liveResolver().(sceneformat.SceneResolver); ok {
			if resolved, err := sceneformat.Resolve(doc, scenes); err == nil {
				if locateObject(resolved, id).obj != nil {
					return true
				}
			}
		}
	}
	return false
}

func (e *Editor) validateSimulatedScene(doc *sceneformat.SceneDoc) error {
	if err := sceneformat.Validate(doc); err != nil {
		return err
	}
	if !doc.RefersToScenes() {
		if e.components != nil {
			if err := sceneformat.ValidateComponents(doc, e.components); err != nil {
				return err
			}
			if err := sceneformat.ValidateConnections(doc, e.components); err != nil {
				return err
			}
		}
		return nil
	}
	scenes, ok := e.liveResolver().(sceneformat.SceneResolver)
	if !ok {
		return nil
	}
	resolved, err := sceneformat.Resolve(doc, scenes)
	if err != nil {
		return err
	}
	if e.components != nil {
		if err := sceneformat.ValidateConnections(resolved, e.components); err != nil {
			return err
		}
	}
	return nil
}

func (e *Editor) applyFixEditUndoable(fix ProposedProblemFix) error {
	edit := fix.Edit
	switch edit.Kind {
	case FixKindUpdateConnection:
		if edit.ConnectionIndex < 0 || edit.ConnectionIndex >= len(e.Doc.Connections) {
			return fmt.Errorf("editor: connectionIndex %d out of range", edit.ConnectionIndex)
		}
		c := cloneConnectionDoc(e.Doc.Connections[edit.ConnectionIndex])
		if edit.From != "" {
			c.From = edit.From
		}
		if edit.Event != "" {
			c.Event = edit.Event
		}
		if edit.To != "" {
			c.To = edit.To
		}
		if edit.Handler != "" {
			c.Handler = edit.Handler
		}
		if edit.Once != nil {
			c.Once = *edit.Once
		}
		if edit.Args != nil {
			c.Args = cloneMap(edit.Args)
		}
		return e.UpdateConnection(edit.ConnectionIndex, c)

	case FixKindRemoveConnection:
		return e.RemoveConnection(edit.ConnectionIndex)

	case FixKindSetSprite:
		var ref *sceneformat.SpriteRef
		obj, _, _ := e.FindObject(edit.ObjectID)
		atlas, frame := edit.Atlas, edit.Frame
		if obj != nil && obj.Sprite != nil {
			if atlas == "" {
				atlas = obj.Sprite.Atlas
			}
			if frame == "" {
				frame = obj.Sprite.Frame
			}
		}
		if atlas != "" || frame != "" {
			ref = &sceneformat.SpriteRef{Atlas: atlas, Frame: frame}
		}
		return e.SetObjectSprite(edit.ObjectID, ref)

	case FixKindSetMaterial:
		mat, _ := edit.Value.(string)
		return e.SetObjectMaterial(edit.ObjectID, mat)

	case FixKindSetObjectID:
		return e.SetObjectID(edit.ObjectID, edit.NewID)

	case FixKindSetComponentField:
		return e.SetComponentField(edit.ObjectID, edit.Component, edit.Field, edit.Value)

	case FixKindRemoveComponent:
		return e.RemoveComponent(edit.ObjectID, edit.Component)

	case FixKindReplaceLine:
		raw, err := e.liveFileBytes(e.ScenePath)
		if err != nil {
			raw, err = encodeSceneDocLive(e.Doc)
			if err != nil {
				return err
			}
		}
		patched, err := applyReplaceLineYAML(raw, edit.Line, edit.OldText, edit.Replacement)
		if err != nil {
			return err
		}
		parsed, err := sceneformat.ParseWith(patched, sceneformat.ParseOptions{Components: e.components})
		if err != nil {
			return err
		}
		if err := e.validateSimulatedScene(parsed); err != nil {
			return err
		}
		e.History.Execute(&replaceSceneDocCmd{
			ed:     e,
			label:  "Apply fix: " + fix.Summary,
			before: e.Doc.Clone(),
			after:  parsed,
		})
		return nil

	default:
		return fmt.Errorf("editor: unknown fix edit kind %q", edit.Kind)
	}
}

type replaceSceneDocCmd struct {
	ed     *Editor
	label  string
	before *sceneformat.SceneDoc
	after  *sceneformat.SceneDoc
}

func (c *replaceSceneDocCmd) Label() string { return c.label }
func (c *replaceSceneDocCmd) Do() {
	c.ed.Doc = c.after.Clone()
	c.ed.Status = c.label
}
func (c *replaceSceneDocCmd) Undo() {
	c.ed.Doc = c.before.Clone()
	c.ed.Status = "Undid " + c.label
}

func applyReplaceLineYAML(raw []byte, line int, oldText, replacement string) ([]byte, error) {
	if line <= 0 {
		return nil, errors.New("editor: replace_line requires a 1-based line number")
	}
	text := string(raw)
	hadTrailingNL := strings.HasSuffix(text, "\n")
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if line > len(lines) {
		return nil, fmt.Errorf("editor: replace_line line %d out of range (file has %d lines)", line, len(lines))
	}
	cur := strings.TrimRight(lines[line-1], "\r")
	if oldText != "" && !strings.Contains(cur, oldText) {
		return nil, fmt.Errorf("editor: line %d (%q) does not contain %q", line, cur, oldText)
	}
	if oldText != "" {
		lines[line-1] = strings.Replace(cur, oldText, replacement, 1)
	} else {
		lines[line-1] = replacement
	}
	out := strings.Join(lines, "\n")
	if hadTrailingNL {
		out += "\n"
	}
	return []byte(out), nil
}

func computeProblemDiff(before, after []byte) []ProblemDiffLine {
	a := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	b := strings.Split(strings.TrimSuffix(string(after), "\n"), "\n")
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	endA, endB := len(a)-1, len(b)-1
	for endA >= start && endB >= start && a[endA] == b[endB] {
		endA--
		endB--
	}
	var out []ProblemDiffLine
	for i := start; i <= endA; i++ {
		out = append(out, ProblemDiffLine{Kind: "removed", Line: i + 1, Text: a[i]})
	}
	for i := start; i <= endB; i++ {
		out = append(out, ProblemDiffLine{Kind: "added", Line: i + 1, Text: b[i]})
	}
	return out
}

func cloneProblemFixEdit(edit ProblemFixEdit) ProblemFixEdit {
	out := edit
	if edit.Once != nil {
		v := *edit.Once
		out.Once = &v
	}
	if edit.Args != nil {
		out.Args = cloneMap(edit.Args)
	}
	out.Value = cloneAny(edit.Value)
	return out
}

func extractProblemObjectIDFromDoc(doc *sceneformat.SceneDoc, p assetcheck.Problem, raw []byte) string {
	if doc == nil {
		return ""
	}
	hasObj := func(id string) bool { return locateObject(doc, id).obj != nil }
	if m := connProblemRE.FindStringSubmatch(p.Message); len(m) == 6 {
		if hasObj(m[2]) {
			return m[2]
		}
		if hasObj(m[4]) {
			return m[4]
		}
	}
	if m := objectProblemRE.FindStringSubmatch(p.Message); len(m) == 2 && hasObj(m[1]) {
		return m[1]
	}
	if p.Line > 0 && len(raw) > 0 {
		for _, sp := range extractYAMLObjectSpans(raw) {
			if p.Line >= sp.startLine && p.Line <= sp.endLine && hasObj(sp.id) {
				return sp.id
			}
		}
	}
	return ""
}

// --- Viewport Problem overlay (Spec.dc.html Problem kind) ---

var missingTargetRE = regexp.MustCompile(`has no object "([^"]+)"`)
var missingFrameRE = regexp.MustCompile(`frame "([^"]+)" is not in atlas`)

// shortProblemLabel condenses a diagnostic message into a crisp viewport pill label
// (for example `target "door_3" not found` or `frame "ghost" not found`).
func shortProblemLabel(p assetcheck.Problem) string {
	if m := missingTargetRE.FindStringSubmatch(p.Message); len(m) == 2 {
		return fmt.Sprintf("target %q not found", m[1])
	}
	if m := missingFrameRE.FindStringSubmatch(p.Message); len(m) == 2 {
		return fmt.Sprintf("frame %q not found", m[1])
	}
	msg := strings.TrimPrefix(p.Message, "sceneformat: ")
	if idx := strings.Index(msg, ", counting those"); idx > 0 {
		msg = msg[:idx]
	}
	if len([]rune(msg)) > 44 {
		r := []rune(msg)
		msg = string(r[:43]) + "…"
	}
	return msg
}

// drawProblemOverlays2D draws the Spec Problem overlay (2 px solid Problem.Edge
// outline on the live object plus a filled Problem.Fg pill with the alert
// triangle icon and message in Problem.OnFill) for every live visible object in
// the open scene that has a non-ignored problem.
func (e *Editor) drawProblemOverlays2D(d *core.Drawing, scene *sceneformat.BuiltScene) {
	if e == nil || d == nil || scene == nil || e.playing || !e.ProblemOverlayVisible() {
		return
	}
	objProbs := e.openSceneObjectProblems()
	if len(objProbs) == 0 {
		return
	}
	spec := e.SpecPalette(nil)
	probRole := spec.Problem()
	font := core.Font{Family: core.FontMonospace, Size: 10.5}

	d.Save()
	for _, layer := range scene.Layers {
		if !layer.Visible {
			continue
		}
		layerM := e.layerAffineFor(layer.Space, layer.ParallaxX, layer.ParallaxY)
		var walk func(objs []*sceneformat.BuiltObject, hidden bool)
		walk = func(objs []*sceneformat.BuiltObject, hidden bool) {
			for _, bo := range objs {
				h := hidden || e.hidden[bo.ID]
				if !h && bo != nil && bo.Doc != nil {
					if probs := objProbs[bo.ID]; len(probs) > 0 {
						m := layerM.Mul(bo.World)
						rx, ry, rw, rh := objectOutline(bo)
						corners := [4]math2d.Vec2{
							m.TransformPoint(math2d.Vec2{X: rx, Y: ry}),
							m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry}),
							m.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry + rh}),
							m.TransformPoint(math2d.Vec2{X: rx, Y: ry + rh}),
						}
						minX, minY := corners[0].X, corners[0].Y
						maxX, maxY := corners[0].X, corners[0].Y
						for _, pt := range corners[1:] {
							minX = float32(math.Min(float64(minX), float64(pt.X)))
							minY = float32(math.Min(float64(minY), float64(pt.Y)))
							maxX = float32(math.Max(float64(maxX), float64(pt.X)))
							maxY = float32(math.Max(float64(maxY), float64(pt.Y)))
						}
						// 2 px solid Spec Problem outline with 3 px offset around the live object.
						const pad float32 = 3
						d.SetStrokeColor(probRole.Edge).SetLineWidth(2).SetLineDash()
						d.BeginPath().Rect(minX-pad, minY-pad, (maxX-minX)+2*pad, (maxY-minY)+2*pad).Stroke()

						// Filled Spec Problem pill above the object with alert triangle icon + message.
						label := shortProblemLabel(probs[0])
						if len(probs) > 1 {
							label = fmt.Sprintf("%s (+%d)", label, len(probs)-1)
						}
						const iconW float32 = 14
						const pillH float32 = 20
						pillW := float32(len([]rune(label)))*6.5 + iconW + 16
						cx := (minX + maxX) * 0.5
						px := cx - pillW*0.5
						if e.canvasW > 0 {
							if px+pillW > e.canvasW-6 {
								px = e.canvasW - pillW - 6
							}
							if px < 6 {
								px = 6
							}
						}
						py := minY - pad - pillH - 6
						if py < 6 {
							py = maxY + pad + 6
						}
						if e.canvasH > 0 {
							if py+pillH > e.canvasH-6 {
								py = e.canvasH - pillH - 6
							}
							if py < 6 {
								py = 6
							}
						}
						d.SetFillColor(probRole.Fg)
						d.BeginPath().Rect(px, py, pillW, pillH).Fill()

						// Alert triangle icon in Problem.OnFill (never color alone).
						ix, iy := px+6, py+4
						d.SetStrokeColor(probRole.OnFill).SetLineWidth(1.5).SetLineCap(core.CapRound).SetLineJoin(core.JoinRound)
						d.BeginPath().MoveTo(ix+6, iy+1).LineTo(ix+11, iy+10.5).LineTo(ix+1, iy+10.5).ClosePath().Stroke()
						d.BeginPath().MoveTo(ix+6, iy+4.5).LineTo(ix+6, iy+7.2).Stroke()
						d.SetFillColor(probRole.OnFill).BeginPath().Arc(ix+6, iy+9, 0.75, 0, float32(2*math.Pi), false).Fill()

						d.SetFillColor(probRole.OnFill).FillText(label, px+iconW+8, py+13.5, font)
					}
				}
				walk(bo.Children, h)
			}
		}
		walk(layer.Objects, false)
	}
	d.Restore()
}
