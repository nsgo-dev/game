package editor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"go.yaml.in/yaml/v3"
)

// ReviewConflictChoice selects how an overlapping edit between the person and the agent merges.
type ReviewConflictChoice string

const (
	// ReviewConflictUnresolved indicates the person has not yet chosen Mine, Agent's, or Both.
	ReviewConflictUnresolved ReviewConflictChoice = ""
	// ReviewConflictMine keeps the person's edit for the conflicting field or list.
	ReviewConflictMine ReviewConflictChoice = "mine"
	// ReviewConflictAgent keeps the agent's edit for the conflicting field or list.
	ReviewConflictAgent ReviewConflictChoice = "agent"
	// ReviewConflictBoth keeps both sides' entries for a conflicting list (or map), yours first.
	ReviewConflictBoth ReviewConflictChoice = "both"
)

// ReviewLineAuthor tags who wrote a line in the review YAML pane or conflict card.
type ReviewLineAuthor string

const (
	ReviewAuthorNone  ReviewLineAuthor = ""
	ReviewAuthorAgent ReviewLineAuthor = "agent"
	ReviewAuthorYou   ReviewLineAuthor = "you"
)

// ReviewConflictLine is one side's preview line inside a conflict card ("you" or "agent").
type ReviewConflictLine struct {
	Author ReviewLineAuthor `json:"author"`
	Sign   string           `json:"sign"`
	Text   string           `json:"text"`
}

// ReviewConflict describes one overlapping field or list edit on a reviewed object.
type ReviewConflict struct {
	Field      string               `json:"field"`
	IsList     bool                 `json:"isList,omitempty"`
	Base       any                  `json:"base,omitempty"`
	Mine       any                  `json:"mine,omitempty"`
	Agent      any                  `json:"agent,omitempty"`
	MineLines  []ReviewConflictLine `json:"mineLines,omitempty"`
	AgentLines []ReviewConflictLine `json:"agentLines,omitempty"`
}

// ReviewYAMLFix is a mechanical one-step fix offered when a YAML parse error names what is missing.
type ReviewYAMLFix struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	PatchedYAML string `json:"patchedYaml"`
}

// ReviewYAMLError describes a syntax or validation error while hand-editing YAML in a review.
type ReviewYAMLError struct {
	Line        int            `json:"line"`
	LocalLine   int            `json:"localLine"`
	Title       string         `json:"title"`
	Message     string         `json:"message"`
	Fix         *ReviewYAMLFix `json:"fix,omitempty"`
	RawParseErr string         `json:"rawParseErr,omitempty"`
}

// CanEditYAML reports whether this change can be hand-edited in place in the review's right YAML pane.
func (c *ReviewChange) CanEditYAML() bool {
	if c == nil || c.Reverted || c.FileKind != ReviewFileScene {
		return false
	}
	return c.Kind == ReviewChangeAdded || c.Kind == ReviewChangeChanged
}

// HasUnresolvedConflict reports whether this non-reverted change has overlapping edits awaiting a choice.
func (c *ReviewChange) HasUnresolvedConflict() bool {
	if c == nil || c.Reverted || len(c.Conflicts) == 0 {
		return false
	}
	return c.ConflictChoice == ReviewConflictUnresolved
}

// findReviewChange resolves a change either by its exact ReviewChange.ID or (when unique / unambiguous)
// by its ObjectID, preferring the currently focused file when multiple files share an object ID.
func (e *Editor) findReviewChange(idOrObj string) *ReviewChange {
	if e == nil || idOrObj == "" {
		return nil
	}
	if ch := e.review.changesByID[idOrObj]; ch != nil {
		return ch
	}
	focusedFile := ""
	if cur := e.review.changesByID[e.review.focusedID]; cur != nil {
		focusedFile = cur.File
	}
	var firstMatch *ReviewChange
	for _, ch := range e.review.changes {
		if ch.ObjectID != idOrObj {
			continue
		}
		if focusedFile != "" && ch.File == focusedFile {
			return ch
		}
		if firstMatch == nil {
			firstMatch = ch
		}
	}
	return firstMatch
}

func (e *Editor) initReviewScenePersonDoc(fs *reviewFileState, fi ReviewFileInput) error {
	if fs == nil || fs.kind != ReviewFileScene {
		return nil
	}
	opts := sceneformat.ParseOptions{Components: e.components}
	if len(fi.PersonYAML) > 0 {
		pdoc, err := sceneformat.ParseWith(fi.PersonYAML, opts)
		if err != nil {
			return err
		}
		fs.personDoc = pdoc
		fs.personEdited = !reviewSceneDocsEqual(fs.beforeDoc, pdoc)
		return nil
	}
	openRel := e.openSceneProjectRelPath()
	if openRel != "" && fs.path == openRel && e.Doc != nil {
		fs.personDoc = e.Doc.Clone()
		fs.personEdited = !reviewSceneDocsEqual(fs.beforeDoc, fs.personDoc)
		return nil
	}
	fs.personDoc = cloneSceneDoc(fs.beforeDoc)
	fs.personEdited = false
	return nil
}

func reviewSceneDocsEqual(a, b *sceneformat.SceneDoc) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	ba, ea := sceneformat.Encode(a)
	bb, eb := sceneformat.Encode(b)
	return ea == nil && eb == nil && bytes.Equal(ba, bb)
}

func (e *Editor) syncAllReviewConflicts() {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return
	}
	for _, fs := range e.review.files {
		if fs.kind == ReviewFileScene {
			e.syncReviewConflictsForFile(fs)
		}
	}
}

// syncReviewAfterMutate keeps a pending turn review synchronized whenever the person edits
// the open scene through normal editor commands (MoveObject, SetProperty, SetComponentField, etc.).
func (e *Editor) syncReviewAfterMutate() {
	if e == nil || e.review.applyingCmd || e.agentActorDepth.Load() > 0 || !e.HasPendingReview() || e.Doc == nil {
		return
	}
	openRel := e.openSceneProjectRelPath()
	if openRel == "" {
		return
	}
	fs := e.review.filesByPath[openRel]
	if fs == nil || fs.kind != ReviewFileScene {
		return
	}
	fs.personDoc = e.Doc.Clone()
	fs.personEdited = !reviewSceneDocsEqual(fs.beforeDoc, fs.personDoc)
	e.syncReviewConflictsForFile(fs)
	fs.markAllRoundsDirty()
	e.bumpReviewUIRev()
}

// SyncReviewPersonEdits explicitly synchronizes the pending review turn against the editor's open scene Doc
// (useful after direct in-memory edits or external disk reloads).
func (e *Editor) SyncReviewPersonEdits() {
	if e == nil || !e.HasPendingReview() || e.Doc == nil {
		return
	}
	openRel := e.openSceneProjectRelPath()
	if openRel == "" {
		return
	}
	fs := e.review.filesByPath[openRel]
	if fs == nil || fs.kind != ReviewFileScene {
		return
	}
	fs.personDoc = e.Doc.Clone()
	fs.personEdited = !reviewSceneDocsEqual(fs.beforeDoc, fs.personDoc)
	e.syncReviewConflictsForFile(fs)
	fs.markAllRoundsDirty()
	e.bumpReviewUIRev()
	e.notify()
}

// syncReviewConflictsForFile recomputes 3-way merge conflicts and auto-merged fields
// between fs.beforeDoc, fs.personDoc, and fs.afterDoc for every change in fs.
func (e *Editor) syncReviewConflictsForFile(fs *reviewFileState) {
	if e == nil || fs == nil || fs.kind != ReviewFileScene || fs.beforeDoc == nil || fs.afterDoc == nil {
		return
	}
	if !fs.personEdited || fs.personDoc == nil {
		for _, ch := range e.review.changes {
			if ch.File != fs.path {
				continue
			}
			hadConflicts := len(ch.Conflicts) > 0
			ch.personObj = nil
			ch.Conflicts = nil
			ch.ConflictChoice = ReviewConflictUnresolved
			ch.AutoMergedFields = nil
			if hadConflicts {
				e.refreshReviewChangeYAMLLines(fs, ch)
			}
		}
		return
	}

	res, err := sceneformat.Merge(fs.beforeDoc, fs.personDoc, fs.afterDoc)
	if err != nil {
		return
	}

	personIdx := indexObjects(fs.personDoc)
	conflictsByObj := make(map[string][]sceneformat.MergeConflict)
	for _, c := range res.Conflicts {
		if c.Scope == "object" && c.Object != "" {
			conflictsByObj[c.Object] = append(conflictsByObj[c.Object], c)
		}
	}

	for _, ch := range e.review.changes {
		if ch.File != fs.path {
			continue
		}
		if pInfo, ok := personIdx[ch.ObjectID]; ok {
			ch.personObj = mergeCloneObjectDocPtr(pInfo.obj)
		} else {
			ch.personObj = nil
		}

		rawConflicts := conflictsByObj[ch.ObjectID]
		prevSig := reviewConflictSignature(ch.Conflicts)
		ch.Conflicts = nil
		for _, rc := range rawConflicts {
			ch.Conflicts = append(ch.Conflicts, buildReviewConflict(rc))
		}
		if len(ch.Conflicts) == 0 {
			ch.ConflictChoice = ReviewConflictUnresolved
		} else if reviewConflictSignature(ch.Conflicts) != prevSig {
			ch.ConflictChoice = ReviewConflictUnresolved
		}

		// Record any fields where person and agent edited different fields and merged cleanly.
		ch.AutoMergedFields = nil
		if len(ch.Conflicts) == 0 && ch.beforeObj != nil && ch.personObj != nil && ch.afterObj != nil {
			personDiff := diffObjectFieldPaths(ch.beforeObj, ch.personObj)
			agentDiff := diffObjectFieldPaths(ch.beforeObj, ch.afterObj)
			if len(personDiff) > 0 && len(agentDiff) > 0 {
				var merged []string
				for _, f := range personDiff {
					if !slices.Contains(agentDiff, f) {
						merged = append(merged, f)
					}
				}
				slices.Sort(merged)
				ch.AutoMergedFields = merged
			}
		}
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
}

// ResolveReviewConflict sets how a conflicting change merges (Mine, Agent's, Both, or Unresolved).
func (e *Editor) ResolveReviewConflict(changeID string, choice ReviewConflictChoice) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	switch choice {
	case ReviewConflictUnresolved, ReviewConflictMine, ReviewConflictAgent, ReviewConflictBoth:
	default:
		return fmt.Errorf("editor: invalid review conflict choice %q", choice)
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	if _, err := e.validateReviewRelPath(ch.File); err != nil {
		return err
	}
	if len(ch.Conflicts) == 0 {
		return fmt.Errorf("editor: review change %q has no conflict to resolve", ch.ObjectID)
	}
	ch.ConflictChoice = choice
	ch.Seen = true
	if fs := e.review.filesByPath[ch.File]; fs != nil {
		fs.markAllRoundsDirty()
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// BeginReviewYAMLEdit switches the review's right YAML pane into in-place editing for an added or changed scene object.
func (e *Editor) BeginReviewYAMLEdit(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	if _, err := e.validateReviewRelPath(ch.File); err != nil {
		return err
	}
	if !ch.CanEditYAML() {
		return fmt.Errorf("editor: cannot edit YAML for %s (%s)", ch.Title, ch.Kind)
	}
	if ch.EditingYAML {
		return nil
	}
	fs := e.review.filesByPath[ch.File]
	effObj := e.effectiveReviewObjectForChange(fs, ch)
	if effObj == nil {
		return fmt.Errorf("editor: object %q has no YAML to edit", ch.ObjectID)
	}
	if ch.handEditBaseObj == nil {
		ch.handEditBaseObj = mergeCloneObjectDocPtr(effObj)
	}
	currentYAML := encodeReviewObjectYAML(effObj)
	if ch.HandEdited && strings.TrimSpace(ch.HandEditYAML) != "" && ch.YAMLError == nil {
		currentYAML = ch.HandEditYAML
	}
	ch.handEditPreYAML = currentYAML
	ch.HandEditYAML = currentYAML
	ch.hadHandEditBeforeSession = ch.HandEdited
	ch.preSessionHandObj = mergeCloneObjectDocPtr(ch.handEditObj)
	ch.preSessionHandFields = slices.Clone(ch.HandEditFields)
	ch.preSessionHandLineNums = slices.Clone(ch.handEditLineNums)
	ch.YAMLError = nil
	ch.EditingYAML = true
	ch.Seen = true
	e.review.focusedID = ch.ID
	e.review.focusedRowKey = reviewCardRowKey(ch.ID)
	e.review.yamlEditSessionRev++
	e.refreshReviewChangeYAMLLines(fs, ch)
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// SetReviewYAMLDraft updates the in-place YAML text for changeID, parses and validates it live
// (refusing unknown fields and duplicate keys), updates the Review viewport immediately when valid,
// or records a line-specific error with a one-step fix when the parser reports what is missing.
func (e *Editor) SetReviewYAMLDraft(changeID, raw string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	if _, err := e.validateReviewRelPath(ch.File); err != nil {
		return err
	}
	if !ch.CanEditYAML() {
		return fmt.Errorf("editor: cannot edit YAML for %s", ch.Title)
	}
	if !ch.EditingYAML {
		if err := e.BeginReviewYAMLEdit(ch.ID); err != nil {
			return err
		}
	}
	fs := e.review.filesByPath[ch.File]
	startLine := ch.Line
	if startLine <= 0 {
		startLine = 1
	}

	if len(raw) > maxReviewYAMLEditBytes {
		ch.YAMLError = &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   fmt.Sprintf("YAML edit exceeds %d KiB limit. Keep stays off until the file parses.", maxReviewYAMLEditBytes/1024),
		}
		e.refreshReviewChangeYAMLLines(fs, ch)
		e.bumpReviewUIRev()
		e.notify()
		return errors.New(ch.YAMLError.Message)
	}
	if strings.ContainsRune(raw, 0) {
		ch.YAMLError = &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   "YAML cannot contain NUL bytes. Keep stays off until the file parses.",
		}
		e.refreshReviewChangeYAMLLines(fs, ch)
		e.bumpReviewUIRev()
		e.notify()
		return errors.New(ch.YAMLError.Message)
	}

	ch.HandEditYAML = raw
	var baseDoc *sceneformat.SceneDoc
	if fs != nil {
		baseDoc = fs.beforeDoc
	}

	parsedObj, yamlErr := parseReviewObjectYAML(raw, startLine, ch.ObjectID, ch.handEditBaseObj, e.components, baseDoc)
	if yamlErr != nil {
		ch.YAMLError = yamlErr
		e.refreshReviewChangeYAMLLines(fs, ch)
		e.bumpReviewUIRev()
		e.notify()
		return errors.New(yamlErr.Message)
	}

	// Dry-run into the full effective scene to guarantee sceneformat.Validate passes with this edit.
	prevObj := ch.handEditObj
	prevEdited := ch.HandEdited
	ch.handEditObj = parsedObj
	ch.HandEdited = true
	ch.YAMLError = nil
	var derr error
	if fs != nil {
		doc := e.effectiveReviewSceneDoc(fs)
		derr = sceneformat.Validate(doc)
	}
	if derr != nil {
		ch.handEditObj = prevObj
		ch.HandEdited = prevEdited
		ch.YAMLError = &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   derr.Error() + ". Keep stays off until the file parses.",
		}
		e.refreshReviewChangeYAMLLines(fs, ch)
		e.bumpReviewUIRev()
		e.notify()
		return derr
	}

	// Compute which fields and 1-based scene lines were edited by "you".
	baseObj := ch.handEditBaseObj
	if baseObj == nil {
		baseObj = ch.afterObj
	}
	ch.HandEditFields = diffObjectFieldPaths(baseObj, parsedObj)
	ch.handEditLineNums = computeHandEditedLines(baseObj, parsedObj, startLine)
	ch.HandEdited = len(ch.HandEditFields) > 0 || len(ch.handEditLineNums) > 0
	if !ch.HandEdited {
		ch.handEditObj = nil
	}
	if ch.Kind == ReviewChangeAdded {
		ch.Detail = formatAddedObjectDetail(parsedObj)
	} else if ch.Kind == ReviewChangeChanged && ch.beforeObj != nil {
		ch.Detail = formatChangedObjectDetail(ch.beforeObj, parsedObj, ch.beforeLayer, ch.afterLayer)
	}

	if fs != nil {
		fs.markAllRoundsDirty()
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// ApplyReviewYAMLFix applies the offered one-step fix (such as "Add the colon") to the failing YAML draft.
func (e *Editor) ApplyReviewYAMLFix(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	if _, err := e.validateReviewRelPath(ch.File); err != nil {
		return err
	}
	if ch.YAMLError == nil || ch.YAMLError.Fix == nil {
		return errors.New("editor: no one-step YAML fix is available")
	}
	patched := ch.YAMLError.Fix.PatchedYAML
	e.review.yamlEditSessionRev++
	return e.SetReviewYAMLDraft(ch.ID, patched)
}

// UndoReviewYAMLTyping discards the in-progress YAML typing and restores the pre-edit snapshot.
func (e *Editor) UndoReviewYAMLTyping(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	ch.HandEditYAML = ch.handEditPreYAML
	ch.YAMLError = nil
	ch.EditingYAML = false
	ch.HandEdited = ch.hadHandEditBeforeSession
	ch.handEditObj = mergeCloneObjectDocPtr(ch.preSessionHandObj)
	ch.HandEditFields = slices.Clone(ch.preSessionHandFields)
	ch.handEditLineNums = slices.Clone(ch.preSessionHandLineNums)
	fs := e.review.filesByPath[ch.File]
	if obj := e.effectiveReviewObjectForChange(fs, ch); obj != nil {
		if ch.Kind == ReviewChangeAdded {
			ch.Detail = formatAddedObjectDetail(obj)
		} else if ch.Kind == ReviewChangeChanged && ch.beforeObj != nil {
			ch.Detail = formatChangedObjectDetail(ch.beforeObj, obj, ch.beforeLayer, ch.afterLayer)
		}
	}
	if fs != nil {
		fs.markAllRoundsDirty()
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
	e.review.yamlEditSessionRev++
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// FinishReviewYAMLEdit exits in-place YAML editing (Done). Refused with ErrReviewYAMLInvalid while the YAML does not parse.
func (e *Editor) FinishReviewYAMLEdit(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.findReviewChange(changeID)
	if ch == nil {
		return fmt.Errorf("editor: unknown review change %q", changeID)
	}
	if ch.YAMLError != nil {
		return fmt.Errorf("%w: %s", ErrReviewYAMLInvalid, ch.YAMLError.Title)
	}
	ch.EditingYAML = false
	e.review.yamlEditSessionRev++
	if fs := e.review.filesByPath[ch.File]; fs != nil {
		e.refreshReviewChangeYAMLLines(fs, ch)
	}
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// ReviewEffectiveSceneDoc returns a cloned copy of the live merged SceneDoc for filePath in the active review
// (applying non-reverted agent changes, auto-merged person edits, resolved conflicts, and valid hand edits).
func (e *Editor) ReviewEffectiveSceneDoc(filePath string) *sceneformat.SceneDoc {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return nil
	}
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind != ReviewFileScene {
		return nil
	}
	return cloneSceneDoc(e.effectiveReviewSceneDoc(fs))
}

func (e *Editor) effectiveReviewSceneDocWithConflicts(fs *reviewFileState) *sceneformat.SceneDoc {
	if fs == nil || fs.beforeDoc == nil || fs.afterDoc == nil {
		return nil
	}
	personDoc := fs.personDoc
	if personDoc == nil {
		personDoc = fs.beforeDoc
	}
	mergeRes, err := sceneformat.Merge(fs.beforeDoc, personDoc, fs.afterDoc)
	if err != nil {
		return nil
	}
	doc := mergeRes.Doc.Clone()

	for _, ch := range e.review.changes {
		if ch.File != fs.path {
			continue
		}
		if ch.Reverted {
			// Restore person's object (or base object if person did not touch it), or remove if added.
			_, _ = removeObjectFromDoc(doc, ch.ObjectID)
			if ch.personObj != nil {
				insertObjectInDoc(doc, mergeCloneObjectDocPtr(ch.personObj), ch.beforeLayer, ch.beforeParentID)
			} else if ch.beforeObj != nil {
				insertObjectInDoc(doc, mergeCloneObjectDocPtr(ch.beforeObj), ch.beforeLayer, ch.beforeParentID)
			}
			continue
		}

		// Apply conflict choices first (before any hand edit on top).
		if len(ch.Conflicts) > 0 {
			obj, _, _ := findObjectInDoc(doc, ch.ObjectID)
			if obj != nil {
				for _, c := range ch.Conflicts {
					switch ch.ConflictChoice {
					case ReviewConflictMine:
						applyObjectFieldValue(obj, c.Field, cloneAnyValue(c.Mine))
					case ReviewConflictAgent:
						applyObjectFieldValue(obj, c.Field, cloneAnyValue(c.Agent))
					case ReviewConflictBoth:
						merged := mergeConflictValueBoth(c.Base, c.Mine, c.Agent)
						applyObjectFieldValue(obj, c.Field, merged)
					default:
						// Unresolved preview preserves person's side until chosen.
						applyObjectFieldValue(obj, c.Field, cloneAnyValue(c.Mine))
					}
				}
			}
		}

		// Apply valid hand-edited YAML on top of the change.
		if ch.HandEdited && ch.handEditObj != nil {
			existing, _, _ := findObjectInDoc(doc, ch.ObjectID)
			if existing != nil {
				replacement := mergeCloneObjectDocPtr(ch.handEditObj)
				if len(replacement.Children) == 0 && len(existing.Children) > 0 {
					replacement.Children = cloneObjectSlice(existing.Children)
				}
				*existing = *replacement
			} else {
				insertObjectInDoc(doc, mergeCloneObjectDocPtr(ch.handEditObj), ch.afterLayer, ch.ParentID)
			}
		}
	}
	return doc
}

func (e *Editor) applyReviewHandEditsToDoc(fs *reviewFileState, doc *sceneformat.SceneDoc) {
	if fs == nil || doc == nil {
		return
	}
	for _, ch := range e.review.changes {
		if ch.File != fs.path || ch.Reverted || !ch.HandEdited || ch.handEditObj == nil {
			continue
		}
		existing, _, _ := findObjectInDoc(doc, ch.ObjectID)
		if existing != nil {
			replacement := mergeCloneObjectDocPtr(ch.handEditObj)
			if len(replacement.Children) == 0 && len(existing.Children) > 0 {
				replacement.Children = cloneObjectSlice(existing.Children)
			}
			*existing = *replacement
		} else {
			insertObjectInDoc(doc, mergeCloneObjectDocPtr(ch.handEditObj), ch.afterLayer, ch.ParentID)
		}
	}
}

func (e *Editor) effectiveReviewObjectForChange(fs *reviewFileState, ch *ReviewChange) *sceneformat.ObjectDoc {
	if ch == nil {
		return nil
	}
	if fs != nil && fs.kind == ReviewFileScene {
		if doc := e.effectiveReviewSceneDoc(fs); doc != nil {
			if obj, _, _ := findObjectInDoc(doc, ch.ObjectID); obj != nil {
				return mergeCloneObjectDocPtr(obj)
			}
		}
	}
	if ch.HandEdited && ch.handEditObj != nil {
		return mergeCloneObjectDocPtr(ch.handEditObj)
	}
	if ch.afterObj != nil {
		return mergeCloneObjectDocPtr(ch.afterObj)
	}
	return mergeCloneObjectDocPtr(ch.beforeObj)
}

func (e *Editor) refreshReviewChangeYAMLLines(fs *reviewFileState, ch *ReviewChange) {
	if ch == nil || ch.FileKind != ReviewFileScene {
		return
	}
	start := ch.Line
	if start <= 0 {
		start = 1
	}

	// If currently editing and broken, render the raw draft lines with the error line marked.
	if ch.EditingYAML && ch.YAMLError != nil {
		rawLines := strings.Split(strings.TrimSuffix(ch.HandEditYAML, "\n"), "\n")
		out := make([]ReviewYAMLLine, 0, len(rawLines))
		for i, text := range rawLines {
			lineNum := start + i
			isErr := lineNum == ch.YAMLError.Line || (i+1) == ch.YAMLError.LocalLine
			sign := "+"
			if ch.Kind == ReviewChangeChanged {
				sign = ""
			}
			tag := ""
			if isErr {
				tag = fmt.Sprintf("line %d", lineNum)
			}
			out = append(out, ReviewYAMLLine{
				Line:  lineNum,
				Sign:  sign,
				Text:  text,
				Tag:   tag,
				Error: isErr,
			})
		}
		ch.YAMLLines = out
		return
	}

	// Special conflict rendering showing both "you" and "agent" lines when unresolved or Both.
	if len(ch.Conflicts) > 0 && !ch.HandEdited {
		ch.YAMLLines = buildConflictYAMLLines(ch, start)
		return
	}

	// If hand-edited, render lines with edited lines tagged "you".
	if ch.HandEdited && ch.handEditObj != nil {
		editedSet := make(map[int]bool, len(ch.handEditLineNums))
		for _, ln := range ch.handEditLineNums {
			editedSet[ln] = true
		}
		switch ch.Kind {
		case ReviewChangeAdded:
			raw := encodeReviewObjectYAML(ch.handEditObj)
			lines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
			out := make([]ReviewYAMLLine, 0, len(lines))
			for i, l := range lines {
				ln := start + i
				author := ReviewAuthorNone
				tag := ""
				if editedSet[ln] {
					author = ReviewAuthorYou
					tag = string(ReviewAuthorYou)
				}
				out = append(out, ReviewYAMLLine{Line: ln, Sign: "+", Text: l, Tag: tag, Author: author})
			}
			ch.YAMLLines = out
			return
		case ReviewChangeChanged:
			baseRaw := encodeReviewObjectYAML(ch.beforeObj)
			effRaw := encodeReviewObjectYAML(ch.handEditObj)
			baseLines := strings.Split(strings.TrimSuffix(baseRaw, "\n"), "\n")
			effLines := strings.Split(strings.TrimSuffix(effRaw, "\n"), "\n")
			ch.YAMLLines = diffReviewHandEditYAMLLines(baseLines, effLines, start, true, editedSet)
			return
		}
	}

	// Restore standard diff lines when neither conflict nor hand-edit is active.
	if fs != nil {
		switch ch.Kind {
		case ReviewChangeAdded:
			if ch.afterObj != nil {
				_, ch.YAMLLines = buildAddedObjectYAMLDiff(fs.afterYAML, ch.afterObj)
			}
		case ReviewChangeChanged:
			if ch.beforeObj != nil && ch.afterObj != nil {
				_, ch.YAMLLines = buildChangedObjectYAMLDiff(fs.beforeYAML, fs.afterYAML, ch.beforeObj, ch.afterObj)
			}
		case ReviewChangeRemoved:
			if ch.beforeObj != nil {
				_, ch.YAMLLines = buildRemovedObjectYAMLDiff(fs.beforeYAML, ch.beforeObj)
			}
		}
	}
}

func buildConflictYAMLLines(ch *ReviewChange, start int) []ReviewYAMLLine {
	var out []ReviewYAMLLine
	ln := start
	out = append(out, ReviewYAMLLine{Line: ln, Text: "  - id: " + ch.ObjectID})
	ln++
	for _, c := range ch.Conflicts {
		shortField := c.Field
		if idx := strings.LastIndex(shortField, "."); idx >= 0 {
			shortField = shortField[idx+1:]
		}
		showMine := ch.ConflictChoice == ReviewConflictUnresolved || ch.ConflictChoice == ReviewConflictMine || ch.ConflictChoice == ReviewConflictBoth
		showAgent := ch.ConflictChoice == ReviewConflictUnresolved || ch.ConflictChoice == ReviewConflictAgent || ch.ConflictChoice == ReviewConflictBoth
		if c.IsList {
			out = append(out, ReviewYAMLLine{Line: ln, Text: "    " + shortField + ":"})
			ln++
			if baseSlice, ok := toAnySlice(c.Base); ok {
				for _, item := range baseSlice {
					out = append(out, ReviewYAMLLine{Line: ln, Text: "      - " + formatConflictListItem(item)})
					ln++
				}
			}
			if showMine {
				for _, ml := range c.MineLines {
					out = append(out, ReviewYAMLLine{
						Line:   ln,
						Sign:   "+",
						Text:   "      - " + ml.Text,
						Tag:    string(ReviewAuthorYou),
						Author: ReviewAuthorYou,
					})
					ln++
				}
			}
			if showAgent {
				for _, al := range c.AgentLines {
					out = append(out, ReviewYAMLLine{
						Line:   ln,
						Sign:   "+",
						Text:   "      - " + al.Text,
						Tag:    string(ReviewAuthorAgent),
						Author: ReviewAuthorAgent,
					})
					ln++
				}
			}
		} else {
			if c.Base != nil {
				out = append(out, ReviewYAMLLine{Line: ln, Sign: "-", Text: "    " + formatConflictScalar(c.Field, c.Base)})
				ln++
			}
			if showMine {
				for _, ml := range c.MineLines {
					out = append(out, ReviewYAMLLine{
						Line:   ln,
						Sign:   "+",
						Text:   "    " + ml.Text,
						Tag:    string(ReviewAuthorYou),
						Author: ReviewAuthorYou,
					})
					ln++
				}
			}
			if showAgent {
				for _, al := range c.AgentLines {
					out = append(out, ReviewYAMLLine{
						Line:   ln,
						Sign:   "+",
						Text:   "    " + al.Text,
						Tag:    string(ReviewAuthorAgent),
						Author: ReviewAuthorAgent,
					})
					ln++
				}
			}
		}
	}
	return out
}

func diffReviewHandEditYAMLLines(baseLines, effLines []string, start int, handEdited bool, editedSet map[int]bool) []ReviewYAMLLine {
	baseSet := make(map[string]int, len(baseLines))
	for _, l := range baseLines {
		baseSet[l]++
	}
	effSet := make(map[string]int, len(effLines))
	for _, l := range effLines {
		effSet[l]++
	}
	var out []ReviewYAMLLine
	emittedDel := make(map[int]bool)
	for i, l := range effLines {
		ln := start + i
		if baseSet[l] > 0 {
			baseSet[l]--
			out = append(out, ReviewYAMLLine{Line: ln, Text: l})
			continue
		}
		keyPrefix := yamlLineKeyPrefix(l)
		if keyPrefix != "" {
			for bi, bl := range baseLines {
				if emittedDel[bi] || effSet[bl] > 0 {
					continue
				}
				if yamlLineKeyPrefix(bl) == keyPrefix {
					emittedDel[bi] = true
					out = append(out, ReviewYAMLLine{Line: ln, Sign: "-", Text: bl})
					break
				}
			}
		}
		author := ReviewAuthorNone
		tag := ""
		if handEdited && editedSet[ln] {
			author = ReviewAuthorYou
			tag = string(ReviewAuthorYou)
		}
		out = append(out, ReviewYAMLLine{Line: ln, Sign: "+", Text: l, Tag: tag, Author: author})
	}
	for bi, bl := range baseLines {
		if emittedDel[bi] || effSet[bl] > 0 {
			continue
		}
		out = append(out, ReviewYAMLLine{Line: start + len(effLines), Sign: "-", Text: bl})
	}
	return out
}

func yamlLineKeyPrefix(line string) string {
	idx := strings.IndexByte(line, ':')
	if idx <= 0 {
		return ""
	}
	return line[:idx]
}

// cloneReviewChangeForSnapshot produces a deep, immutable snapshot copy of ch for ReviewStatus and MCP.
func cloneReviewChangeForSnapshot(ch *ReviewChange, viewRound int, readOnly bool) ReviewChange {
	cp := *ch
	cp.Versions = cloneReviewChangeVersions(ch.Versions)
	if readOnly && viewRound > 0 {
		ver := ch.VersionFor(viewRound)
		cp.Detail = ver.Detail
		cp.Why = ver.Why
		cp.OldValue = ver.OldValue
		cp.NewValue = ver.NewValue
		cp.DeltaPct = ver.DeltaPct
		cp.Line = ver.Line
		cp.YAMLLines = slices.Clone(ver.YAMLLines)
		cp.EditingYAML = false
		cp.YAMLError = nil
	} else {
		cp.YAMLLines = slices.Clone(ch.YAMLLines)
	}
	cp.AutoMergedFields = slices.Clone(ch.AutoMergedFields)
	cp.HandEditFields = slices.Clone(ch.HandEditFields)

	author := ReviewAuthorAgent
	var handBy ReviewLineAuthor
	if ch.HandEdited {
		author = ReviewAuthorYou
		handBy = ReviewAuthorYou
	} else if len(ch.Conflicts) > 0 && ch.ConflictChoice == ReviewConflictMine {
		author = ReviewAuthorYou
	}
	cp.Author = author
	cp.HandEditBy = handBy

	var handLines []ReviewYAMLLine
	for _, ln := range cp.YAMLLines {
		if ln.Author == ReviewAuthorYou || ln.Tag == string(ReviewAuthorYou) {
			handLines = append(handLines, ln)
		}
	}
	cp.HandEditLines = handLines

	if !readOnly && (ch.HandEdited || ch.EditingYAML) {
		if ch.handEditObj != nil && ch.YAMLError == nil {
			cp.HandEditYAML = encodeReviewObjectYAML(ch.handEditObj)
		} else {
			cp.HandEditYAML = ch.HandEditYAML
		}
	} else {
		cp.HandEditYAML = ""
	}

	if len(ch.Conflicts) > 0 {
		cp.Conflicts = make([]ReviewConflict, len(ch.Conflicts))
		for ci, c := range ch.Conflicts {
			cc := c
			cc.Base = cloneAnyValue(c.Base)
			cc.Mine = cloneAnyValue(c.Mine)
			cc.Agent = cloneAnyValue(c.Agent)
			cc.MineLines = slices.Clone(c.MineLines)
			cc.AgentLines = slices.Clone(c.AgentLines)
			cp.Conflicts[ci] = cc
		}
	}
	if !readOnly && ch.YAMLError != nil {
		errCp := *ch.YAMLError
		if ch.YAMLError.Fix != nil {
			fixCp := *ch.YAMLError.Fix
			errCp.Fix = &fixCp
		}
		cp.YAMLError = &errCp
	}
	return cp
}

func cloneReviewChangesSlice(in []ReviewChange) []ReviewChange {
	if len(in) == 0 {
		return nil
	}
	out := make([]ReviewChange, len(in))
	for i := range in {
		cp := in[i]
		cp.YAMLLines = slices.Clone(in[i].YAMLLines)
		cp.Versions = cloneReviewChangeVersions(in[i].Versions)
		cp.AutoMergedFields = slices.Clone(in[i].AutoMergedFields)
		cp.HandEditFields = slices.Clone(in[i].HandEditFields)
		cp.HandEditLines = slices.Clone(in[i].HandEditLines)
		if len(in[i].Conflicts) > 0 {
			cp.Conflicts = make([]ReviewConflict, len(in[i].Conflicts))
			for ci, c := range in[i].Conflicts {
				cc := c
				cc.Base = cloneAnyValue(c.Base)
				cc.Mine = cloneAnyValue(c.Mine)
				cc.Agent = cloneAnyValue(c.Agent)
				cc.MineLines = slices.Clone(c.MineLines)
				cc.AgentLines = slices.Clone(c.AgentLines)
				cp.Conflicts[ci] = cc
			}
		}
		if in[i].YAMLError != nil {
			errCp := *in[i].YAMLError
			if in[i].YAMLError.Fix != nil {
				fixCp := *in[i].YAMLError.Fix
				errCp.Fix = &fixCp
			}
			cp.YAMLError = &errCp
		}
		out[i] = cp
	}
	return out
}

// --- Conflict metadata formatting ---

func reviewConflictSignature(cs []ReviewConflict) string {
	if len(cs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range cs {
		mineB, _ := yaml.Marshal(c.Mine)
		agentB, _ := yaml.Marshal(c.Agent)
		fmt.Fprintf(&b, "%s|%s|%s;", c.Field, bytes.TrimSpace(mineB), bytes.TrimSpace(agentB))
	}
	return b.String()
}

func buildReviewConflict(rc sceneformat.MergeConflict) ReviewConflict {
	c := ReviewConflict{
		Field: rc.Field,
		Base:  cloneAnyValue(rc.Base),
		Mine:  cloneAnyValue(rc.Ours),
		Agent: cloneAnyValue(rc.Theirs),
	}
	mineSlice, mineIsList := toAnySlice(rc.Ours)
	agentSlice, agentIsList := toAnySlice(rc.Theirs)
	baseSlice, _ := toAnySlice(rc.Base)
	if mineIsList && agentIsList {
		c.IsList = true
		for _, item := range sliceAdditions(baseSlice, mineSlice) {
			c.MineLines = append(c.MineLines, ReviewConflictLine{
				Author: ReviewAuthorYou,
				Sign:   "+",
				Text:   formatConflictListItem(item),
			})
		}
		for _, item := range sliceAdditions(baseSlice, agentSlice) {
			c.AgentLines = append(c.AgentLines, ReviewConflictLine{
				Author: ReviewAuthorAgent,
				Sign:   "+",
				Text:   formatConflictListItem(item),
			})
		}
		if len(c.MineLines) == 0 {
			c.MineLines = []ReviewConflictLine{{Author: ReviewAuthorYou, Sign: "+", Text: formatConflictScalar(rc.Field, rc.Ours)}}
		}
		if len(c.AgentLines) == 0 {
			c.AgentLines = []ReviewConflictLine{{Author: ReviewAuthorAgent, Sign: "+", Text: formatConflictScalar(rc.Field, rc.Theirs)}}
		}
		return c
	}

	c.MineLines = []ReviewConflictLine{{
		Author: ReviewAuthorYou,
		Sign:   "+",
		Text:   formatConflictScalar(rc.Field, rc.Ours),
	}}
	c.AgentLines = []ReviewConflictLine{{
		Author: ReviewAuthorAgent,
		Sign:   "+",
		Text:   formatConflictScalar(rc.Field, rc.Theirs),
	}}
	return c
}

func formatConflictListItem(item any) string {
	if m, ok := item.(map[string]any); ok && len(m) == 1 {
		for k, v := range m {
			return fmt.Sprintf("%s: %s", k, formatSimpleYAMLVal(v))
		}
	}
	return formatSimpleYAMLVal(item)
}

func formatConflictScalar(field string, v any) string {
	short := field
	if idx := strings.LastIndex(field, "."); idx >= 0 {
		short = field[idx+1:]
	}
	if v == nil {
		return short + ": (removed)"
	}
	return fmt.Sprintf("%s: %s", short, formatSimpleYAMLVal(v))
}

func formatSimpleYAMLVal(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		raw, err := yaml.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return strings.TrimSpace(string(raw))
	}
}

// encodeReviewObjectYAML encodes obj as clean 2-space indented object YAML without repeating children.
func encodeReviewObjectYAML(obj *sceneformat.ObjectDoc) string {
	if obj == nil {
		return ""
	}
	cp := mergeCloneObjectDocPtr(obj)
	cp.Children = nil
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	_ = enc.Encode(cp)
	_ = enc.Close()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	for i, l := range lines {
		if i == 0 {
			lines[i] = "  - " + l
		} else {
			lines[i] = "    " + l
		}
	}
	return strings.Join(lines, "\n")
}

// computeHandEditedLines returns the 1-based scene line numbers of lines in newObj's YAML
// that differ from baseObj's YAML.
func computeHandEditedLines(baseObj, newObj *sceneformat.ObjectDoc, startLine int) []int {
	if newObj == nil {
		return nil
	}
	baseLines := strings.Split(strings.TrimSuffix(encodeReviewObjectYAML(baseObj), "\n"), "\n")
	newLines := strings.Split(strings.TrimSuffix(encodeReviewObjectYAML(newObj), "\n"), "\n")
	baseCount := make(map[string]int, len(baseLines))
	for _, l := range baseLines {
		baseCount[l]++
	}
	var out []int
	for i, l := range newLines {
		if baseCount[l] > 0 {
			baseCount[l]--
			continue
		}
		out = append(out, startLine+i)
	}
	return out
}

// --- Strict YAML parsing & mechanical one-step fix detection ---

var (
	reReviewYAMLLineErr   = regexp.MustCompile(`line (\d+):`)
	reReviewMissingColon  = regexp.MustCompile(`^(\s*(?:-\s+)?)([A-Za-z_][A-Za-z0-9_.-]*)(\s+)([^:#\s].*)$`)
	reReviewLeadingTabErr = regexp.MustCompile(`^\t+`)
)

func parseReviewObjectYAML(raw string, startLine int, expectedID string, baseObj *sceneformat.ObjectDoc, reg *sceneformat.ComponentRegistry, baseDoc *sceneformat.SceneDoc) (*sceneformat.ObjectDoc, *ReviewYAMLError) {
	if strings.TrimSpace(raw) == "" {
		return nil, &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   "Object YAML cannot be empty. Keep stays off until the file parses.",
		}
	}

	rawLines := strings.Split(strings.TrimSuffix(raw, "\n"), "\n")

	// Check for leading tabs first (yaml/v3 rejects tabs for indentation).
	for i, l := range rawLines {
		if reReviewLeadingTabErr.MatchString(l) {
			sceneLine := startLine + i
			patched := slices.Clone(rawLines)
			for j := range patched {
				for strings.HasPrefix(patched[j], "\t") {
					patched[j] = "  " + strings.TrimPrefix(patched[j], "\t")
				}
			}
			return nil, &ReviewYAMLError{
				Line:      sceneLine,
				LocalLine: i + 1,
				Title:     fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
				Message:   "YAML indentation uses spaces, not tabs. Keep stays off until the file parses.",
				Fix: &ReviewYAMLFix{
					Label:       "Replace tabs with spaces",
					Description: "Replace leading tabs with two spaces each.",
					PatchedYAML: strings.Join(patched, "\n"),
				},
			}
		}
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, diagnoseReviewYAMLParseError(raw, rawLines, startLine, err)
	}
	if root.Kind == 0 || len(root.Content) == 0 {
		return nil, &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   "Expected a YAML mapping for the object. Keep stays off until the file parses.",
		}
	}
	docNode := root.Content[0]
	if docNode.Kind == yaml.SequenceNode {
		if len(docNode.Content) != 1 {
			return nil, &ReviewYAMLError{
				Line:      startLine,
				LocalLine: 1,
				Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
				Message:   "Expected a single object entry. Keep stays off until the file parses.",
			}
		}
		docNode = docNode.Content[0]
	}
	if docNode.Kind != yaml.MappingNode {
		if fixErr := detectReviewMissingColonInLines(rawLines, startLine, 0); fixErr != nil {
			return nil, fixErr
		}
		return nil, &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   "Expected key: value fields for the object. Keep stays off until the file parses.",
		}
	}

	// Refuse duplicate mapping keys deterministically.
	if dupErr := checkDuplicateYAMLMappingKeys(docNode, startLine); dupErr != nil {
		return nil, dupErr
	}

	// Guard against YAML folding a colon-less line (`x 444`) into the preceding plain string scalar!
	if fixErr := detectReviewFoldedMissingColon(&root, rawLines, startLine); fixErr != nil {
		return nil, fixErr
	}

	// Re-encode the mapping node and decode strictly with KnownFields(true) so unknown fields are refused.
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(docNode); err != nil {
		return nil, diagnoseReviewYAMLParseError(raw, rawLines, startLine, err)
	}
	_ = enc.Close()

	dec := yaml.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.KnownFields(true)
	var obj sceneformat.ObjectDoc
	if err := dec.Decode(&obj); err != nil && !errors.Is(err, io.EOF) {
		localLine := extractReviewYAMLErrorLine(err.Error(), len(rawLines))
		sceneLine := startLine + localLine - 1
		if fixErr := detectReviewMissingColonInLines(rawLines, startLine, localLine-1); fixErr != nil {
			return nil, fixErr
		}
		return nil, &ReviewYAMLError{
			Line:        sceneLine,
			LocalLine:   localLine,
			Title:       fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
			Message:     cleanReviewYAMLErrorText(err.Error()) + ". Keep stays off until the file parses.",
			RawParseErr: err.Error(),
		}
	}

	if obj.ID == "" {
		obj.ID = expectedID
	}
	if obj.ID != expectedID {
		return nil, &ReviewYAMLError{
			Line:      startLine,
			LocalLine: 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
			Message:   fmt.Sprintf("Object id %q must stay %q. Keep stays off until the file parses.", obj.ID, expectedID),
		}
	}

	// Validate component fields if a component registry is available.
	if reg != nil && len(obj.Components) > 0 {
		testScene := &sceneformat.SceneDoc{
			Version: sceneformat.CurrentVersion,
			Layers:  []sceneformat.LayerDoc{{Name: "check", Objects: []sceneformat.ObjectDoc{obj}}},
		}
		if baseDoc != nil && baseDoc.Version > 0 {
			testScene.Version = baseDoc.Version
		}
		if encoded, err := sceneformat.Encode(testScene); err == nil {
			if _, perr := sceneformat.ParseWith(encoded, sceneformat.ParseOptions{Components: reg}); perr != nil {
				return nil, &ReviewYAMLError{
					Line:      startLine,
					LocalLine: 1,
					Title:     fmt.Sprintf("Line %d isn't valid YAML", startLine),
					Message:   perr.Error() + ". Keep stays off until the file parses.",
				}
			}
		}
	}

	// Preserve existing children if not explicitly edited.
	if len(obj.Children) == 0 && baseObj != nil && len(baseObj.Children) > 0 {
		obj.Children = cloneObjectSlice(baseObj.Children)
	}
	return &obj, nil
}

func checkDuplicateYAMLMappingKeys(n *yaml.Node, startLine int) *ReviewYAMLError {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			v := n.Content[i+1]
			if k.Kind == yaml.ScalarNode {
				if seen[k.Value] {
					localLine := max(k.Line, 1)
					sceneLine := startLine + localLine - 1
					return &ReviewYAMLError{
						Line:      sceneLine,
						LocalLine: localLine,
						Title:     fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
						Message:   fmt.Sprintf("Duplicate key %q on line %d. Keep stays off until the file parses.", k.Value, sceneLine),
					}
				}
				seen[k.Value] = true
			}
			if err := checkDuplicateYAMLMappingKeys(v, startLine); err != nil {
				return err
			}
		}
		return nil
	}
	for _, child := range n.Content {
		if err := checkDuplicateYAMLMappingKeys(child, startLine); err != nil {
			return err
		}
	}
	return nil
}

func diagnoseReviewYAMLParseError(raw string, rawLines []string, startLine int, err error) *ReviewYAMLError {
	errStr := err.Error()
	localLine := extractReviewYAMLErrorLine(errStr, len(rawLines))
	hintIdx := localLine - 1

	// 1. Check for missing colon on the reported line or the preceding line.
	if strings.Contains(errStr, "could not find expected ':'") || strings.Contains(errStr, "mapping values are not allowed") {
		if fixErr := detectReviewMissingColonInLines(rawLines, startLine, hintIdx); fixErr != nil {
			fixErr.RawParseErr = errStr
			return fixErr
		}
	}

	// 2. Check for unclosed quote.
	if strings.Contains(errStr, "found unexpected end of stream") || strings.Contains(errStr, "quoted scalar") {
		for _, idx := range reviewCandidateLineIndices(len(rawLines), hintIdx) {
			line := rawLines[idx]
			for _, quote := range []byte{'"', '\''} {
				if strings.Count(line, string(quote))%2 == 1 {
					patched := slices.Clone(rawLines)
					patched[idx] = line + string(quote)
					sceneLine := startLine + idx
					return &ReviewYAMLError{
						Line:        sceneLine,
						LocalLine:   idx + 1,
						Title:       fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
						Message:     fmt.Sprintf("Unclosed %c quote; close the string on line %d. Keep stays off until the file parses.", quote, sceneLine),
						RawParseErr: errStr,
						Fix: &ReviewYAMLFix{
							Label:       "Close the quote",
							Description: fmt.Sprintf("Add the closing %c quote on line %d.", quote, sceneLine),
							PatchedYAML: strings.Join(patched, "\n"),
						},
					}
				}
			}
		}
	}

	// 3. Check for unclosed inline brace or bracket.
	for _, pair := range [][3]string{
		{"{", "}", "Add the closing brace"},
		{"[", "]", "Add the closing bracket"},
	} {
		openCh, closeCh, label := pair[0], pair[1], pair[2]
		if strings.Count(raw, openCh) > strings.Count(raw, closeCh) {
			for _, idx := range reviewCandidateLineIndices(len(rawLines), hintIdx) {
				if strings.Count(rawLines[idx], openCh) > strings.Count(rawLines[idx], closeCh) {
					patched := slices.Clone(rawLines)
					patched[idx] = rawLines[idx] + " " + closeCh
					sceneLine := startLine + idx
					return &ReviewYAMLError{
						Line:        sceneLine,
						LocalLine:   idx + 1,
						Title:       fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
						Message:     fmt.Sprintf("Missing %s on line %d. Keep stays off until the file parses.", closeCh, sceneLine),
						RawParseErr: errStr,
						Fix: &ReviewYAMLFix{
							Label:       label,
							Description: fmt.Sprintf("Append %s to line %d.", closeCh, sceneLine),
							PatchedYAML: strings.Join(patched, "\n"),
						},
					}
				}
			}
		}
	}

	// Fallback: check for missing colon anywhere near the error line.
	if fixErr := detectReviewMissingColonInLines(rawLines, startLine, hintIdx); fixErr != nil {
		fixErr.RawParseErr = errStr
		return fixErr
	}

	sceneLine := startLine + localLine - 1
	return &ReviewYAMLError{
		Line:        sceneLine,
		LocalLine:   localLine,
		Title:       fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
		Message:     cleanReviewYAMLErrorText(errStr) + ". Keep stays off until the file parses.",
		RawParseErr: errStr,
	}
}

func detectReviewFoldedMissingColon(root *yaml.Node, rawLines []string, startLine int) *ReviewYAMLError {
	var found *ReviewYAMLError
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n == nil || found != nil {
			return
		}
		if n.Kind == yaml.ScalarNode && n.Style == 0 && n.Line > 0 && n.Line < len(rawLines) {
			nextIdx := n.Line // 0-based index of line (n.Line + 1)
			if nextIdx < len(rawLines) {
				if m := reReviewMissingColon.FindStringSubmatch(rawLines[nextIdx]); m != nil && !strings.Contains(rawLines[nextIdx], ":") {
					key := m[2]
					if strings.Contains(n.Value, key) {
						patched := slices.Clone(rawLines)
						patched[nextIdx] = m[1] + m[2] + ":" + m[3] + m[4]
						sceneLine := startLine + nextIdx
						found = &ReviewYAMLError{
							Line:      sceneLine,
							LocalLine: nextIdx + 1,
							Title:     fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
							Message:   fmt.Sprintf("Expected key: value; add a colon after %s. Keep stays off until the file parses.", key),
							Fix: &ReviewYAMLFix{
								Label:       "Add the colon",
								Description: fmt.Sprintf("Add a colon after %s.", key),
								PatchedYAML: strings.Join(patched, "\n"),
							},
						}
						return
					}
				}
			}
		}
		for _, child := range n.Content {
			walk(child)
		}
	}
	walk(root)
	return found
}

func detectReviewMissingColonInLines(rawLines []string, startLine int, hintIdx int) *ReviewYAMLError {
	for _, idx := range reviewCandidateLineIndices(len(rawLines), hintIdx) {
		line := rawLines[idx]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, ":") {
			continue
		}
		m := reReviewMissingColon.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[2]
		patched := slices.Clone(rawLines)
		patched[idx] = m[1] + m[2] + ":" + m[3] + m[4]
		sceneLine := startLine + idx
		return &ReviewYAMLError{
			Line:      sceneLine,
			LocalLine: idx + 1,
			Title:     fmt.Sprintf("Line %d isn't valid YAML", sceneLine),
			Message:   fmt.Sprintf("Expected key: value; add a colon after %s. Keep stays off until the file parses.", key),
			Fix: &ReviewYAMLFix{
				Label:       "Add the colon",
				Description: fmt.Sprintf("Add a colon after %s.", key),
				PatchedYAML: strings.Join(patched, "\n"),
			},
		}
	}
	return nil
}

func reviewCandidateLineIndices(n, hint int) []int {
	if n <= 0 {
		return nil
	}
	if hint < 0 {
		hint = 0
	}
	if hint >= n {
		hint = n - 1
	}
	seen := make(map[int]bool, n)
	var out []int
	add := func(i int) {
		if i >= 0 && i < n && !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	add(hint - 1)
	add(hint)
	add(hint + 1)
	for i := 0; i < n; i++ {
		add(i)
	}
	return out
}

func extractReviewYAMLErrorLine(msg string, maxLine int) int {
	if m := reReviewYAMLLineErr.FindStringSubmatch(msg); len(m) == 2 {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 1 {
			if maxLine > 0 && n > maxLine {
				return maxLine
			}
			return n
		}
	}
	return 1
}

func cleanReviewYAMLErrorText(msg string) string {
	s := strings.TrimSpace(msg)
	s = strings.TrimPrefix(s, "yaml: ")
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	if s == "" {
		return "Invalid YAML"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// --- Field-level value merging & diff helpers ---

func toAnySlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.([]any); ok {
		return s, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	}
	return nil, false
}

// sliceAdditions returns items in side that are not accounted for by base, preserving side's order.
func sliceAdditions(base, side []any) []any {
	used := make([]bool, len(base))
	var added []any
	for _, item := range side {
		matched := false
		for j, bItem := range base {
			if !used[j] && yamlDeepEqual(bItem, item) {
				used[j] = true
				matched = true
				break
			}
		}
		if !matched {
			added = append(added, cloneAnyValue(item))
		}
	}
	return added
}

// mergeConflictValueBoth merges a conflicting list or map keeping both entries, yours first.
func mergeConflictValueBoth(base, mine, agent any) any {
	mineSlice, mineIsList := toAnySlice(mine)
	agentSlice, agentIsList := toAnySlice(agent)
	if mineIsList && agentIsList {
		baseSlice, _ := toAnySlice(base)
		mineAdded := sliceAdditions(baseSlice, mineSlice)
		agentAdded := sliceAdditions(baseSlice, agentSlice)
		var merged []any
		for _, bItem := range baseSlice {
			if sliceContainsDeep(mineSlice, bItem) && sliceContainsDeep(agentSlice, bItem) {
				merged = append(merged, cloneAnyValue(bItem))
			}
		}
		for _, item := range mineAdded {
			merged = append(merged, cloneAnyValue(item))
		}
		for _, item := range agentAdded {
			if !sliceContainsDeep(mineAdded, item) {
				merged = append(merged, cloneAnyValue(item))
			}
		}
		return merged
	}
	if mineMap, okM := mine.(map[string]any); okM {
		if agentMap, okA := agent.(map[string]any); okA {
			out := make(map[string]any, len(mineMap)+len(agentMap))
			for k, v := range agentMap {
				out[k] = cloneAnyValue(v)
			}
			for k, v := range mineMap {
				out[k] = cloneAnyValue(v)
			}
			return out
		}
	}
	return cloneAnyValue(mine)
}

func sliceContainsDeep(s []any, target any) bool {
	for _, item := range s {
		if yamlDeepEqual(item, target) {
			return true
		}
	}
	return false
}

func yamlDeepEqual(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	ba, ea := yaml.Marshal(a)
	bb, eb := yaml.Marshal(b)
	return ea == nil && eb == nil && bytes.Equal(ba, bb)
}

func cloneAnyValue(v any) any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneAnyValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneAnyValue(val)
		}
		return out
	default:
		return v
	}
}

func applyObjectFieldValue(obj *sceneformat.ObjectDoc, field string, val any) {
	if obj == nil || field == "" {
		return
	}
	if strings.HasPrefix(field, "properties.") {
		rest := strings.Split(strings.TrimPrefix(field, "properties."), ".")
		if obj.Properties == nil {
			obj.Properties = make(map[string]any)
		}
		setNestedMapValue(obj.Properties, rest, val)
		if len(obj.Properties) == 0 {
			obj.Properties = nil
		}
		return
	}
	if strings.HasPrefix(field, "components.") {
		rest := strings.Split(strings.TrimPrefix(field, "components."), ".")
		if obj.Components == nil {
			obj.Components = make(map[string]any)
		}
		setNestedMapValue(obj.Components, rest, val)
		if len(obj.Components) == 0 {
			obj.Components = nil
		}
		return
	}

	switch field {
	case "name":
		if s, ok := val.(string); ok {
			obj.Name = s
		}
	case "prefab":
		if s, ok := val.(string); ok {
			obj.Prefab = s
		}
	case "x":
		if f, ok := reviewAsFloat32(val); ok {
			obj.X = f
		}
	case "y":
		if f, ok := reviewAsFloat32(val); ok {
			obj.Y = f
		}
	case "posZ":
		if f, ok := reviewAsFloat32(val); ok {
			obj.PosZ = f
		}
	case "z":
		if n, ok := reviewAsInt(val); ok {
			obj.Z = n
		}
	case "rotation":
		if f, ok := reviewAsFloat32(val); ok {
			obj.Rotation = f
		}
	case "scaleX":
		if val == nil {
			obj.ScaleX = nil
		} else if f, ok := reviewAsFloat32(val); ok {
			obj.ScaleX = &f
		}
	case "scaleY":
		if val == nil {
			obj.ScaleY = nil
		} else if f, ok := reviewAsFloat32(val); ok {
			obj.ScaleY = &f
		}
	case "alpha":
		if val == nil {
			obj.Alpha = nil
		} else if f, ok := reviewAsFloat32(val); ok {
			obj.Alpha = &f
		}
	case "flipX":
		if b, ok := val.(bool); ok {
			obj.FlipX = b
		}
	case "flipY":
		if b, ok := val.(bool); ok {
			obj.FlipY = b
		}
	case "material":
		if s, ok := val.(string); ok {
			obj.Material = s
		}
	case "mesh":
		if s, ok := val.(string); ok {
			obj.Mesh = s
		}
	case "model":
		if s, ok := val.(string); ok {
			obj.Model = s
		}
	}
}

func setNestedMapValue(m map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		if val == nil {
			delete(m, parts[0])
		} else {
			m[parts[0]] = cloneAnyValue(val)
		}
		return
	}
	sub, ok := m[parts[0]].(map[string]any)
	if !ok {
		if val == nil {
			return
		}
		sub = make(map[string]any)
		m[parts[0]] = sub
	}
	setNestedMapValue(sub, parts[1:], val)
	if len(sub) == 0 {
		delete(m, parts[0])
	}
}

func reviewAsFloat32(v any) (float32, bool) {
	switch x := v.(type) {
	case float32:
		return x, !math.IsNaN(float64(x)) && !math.IsInf(float64(x), 0)
	case float64:
		return float32(x), !math.IsNaN(x) && !math.IsInf(x, 0)
	case int:
		return float32(x), true
	case int64:
		return float32(x), true
	case uint64:
		return float32(x), true
	default:
		return 0, false
	}
}

func reviewAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case float32:
		return int(x), true
	default:
		return 0, false
	}
}

// diffObjectFieldPaths returns the sorted dot-separated field paths where a and b differ.
func diffObjectFieldPaths(a, b *sceneformat.ObjectDoc) []string {
	if a == nil && b == nil {
		return nil
	}
	ma := objectToGenericMap(a)
	mb := objectToGenericMap(b)
	var out []string
	diffGenericMaps("", ma, mb, &out)
	slices.Sort(out)
	return out
}

func objectToGenericMap(o *sceneformat.ObjectDoc) map[string]any {
	if o == nil {
		return nil
	}
	cp := mergeCloneObjectDocPtr(o)
	cp.Children = nil
	raw, err := yaml.Marshal(cp)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = yaml.Unmarshal(raw, &m)
	return m
}

func diffGenericMaps(prefix string, a, b map[string]any, out *[]string) {
	keys := make([]string, 0, len(a)+len(b))
	seen := make(map[string]bool, len(a)+len(b))
	for k := range a {
		seen[k] = true
		keys = append(keys, k)
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		p := k
		if prefix != "" {
			p = prefix + "." + k
		}
		va, hasA := a[k]
		vb, hasB := b[k]
		if !hasA || !hasB {
			*out = append(*out, p)
			continue
		}
		if yamlDeepEqual(va, vb) {
			continue
		}
		ma, okA := va.(map[string]any)
		mb, okB := vb.(map[string]any)
		if okA && okB {
			diffGenericMaps(p, ma, mb, out)
			continue
		}
		*out = append(*out, p)
	}
}
