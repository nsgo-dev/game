package editor

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nsgo-dev/game/pkg/datatable"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/sceneformat"
	"go.yaml.in/yaml/v3"
)

// ReviewAgentNote is one change-scoped author note explicitly sent to the agent over MCP.
// Notes live strictly in memory on the active turn and are never written to project files.
type ReviewAgentNote struct {
	ChangeID    string           `json:"change_id"`
	File        string           `json:"file"`
	FileKind    ReviewFileKind   `json:"file_kind"`
	Kind        ReviewChangeKind `json:"kind"`
	Object      string           `json:"object,omitempty"`
	Field       string           `json:"field,omitempty"`
	Title       string           `json:"title"`
	Note        string           `json:"note"`
	RevertFirst bool             `json:"revert_first,omitempty"`
}

// ReviewNoteBatch is a batch of change-scoped notes explicitly dispatched via
// "Send N notes" or "Revert and redo with this note".
type ReviewNoteBatch struct {
	Round int               `json:"round"`
	Notes []ReviewAgentNote `json:"notes"`
}

// ReviewRedoChange describes the agent's second-round (or later) revision for one noted change.
type ReviewRedoChange struct {
	ChangeID    string
	Detail      string
	X           *float32
	Y           *float32
	ObjectDoc   *sceneformat.ObjectDoc
	LayerName   string
	FileContent []byte
	OldValue    string
	NewValue    string
	DeltaPct    string
	YAMLLines   []ReviewYAMLLine
}

// ReviewViewportPin represents a note pin positioned beside a noted scene object
// in the Review viewport or the paused 2D editor viewport.
type ReviewViewportPin struct {
	ChangeID string  `json:"changeId"`
	File     string  `json:"file"`
	ObjectID string  `json:"objectId"`
	Note     string  `json:"note"`
	Redone   bool    `json:"redone,omitempty"`
	X        float32 `json:"x"`
	Y        float32 `json:"y"`
}

func truncateReviewNote(s string) string {
	if utf8.RuneCountInString(s) <= maxReviewNoteLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxReviewNoteLen])
}

func validateReviewNoteText(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrReviewNoteEmpty
	}
	if utf8.RuneCountInString(trimmed) > maxReviewNoteLen {
		return "", ErrReviewNoteTooLong
	}
	return trimmed, nil
}

func cloneObjectDocPtr(o *sceneformat.ObjectDoc) *sceneformat.ObjectDoc {
	if o == nil {
		return nil
	}
	cp := cloneObjectDoc(*o)
	return &cp
}

func cloneYAMLNodePtr(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cp := *n
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			cp.Content[i] = cloneYAMLNodePtr(c)
		}
	}
	return &cp
}

func cloneReviewChangeVersion(v ReviewChangeVersion) ReviewChangeVersion {
	cp := v
	cp.YAMLLines = slices.Clone(v.YAMLLines)
	cp.afterObj = cloneObjectDocPtr(v.afterObj)
	cp.afterYAMLNode = cloneYAMLNodePtr(v.afterYAMLNode)
	cp.cellDiffs = slices.Clone(v.cellDiffs)
	return cp
}

func cloneReviewChangeVersions(vs []ReviewChangeVersion) []ReviewChangeVersion {
	if len(vs) == 0 {
		return nil
	}
	out := make([]ReviewChangeVersion, len(vs))
	for i, v := range vs {
		out[i] = cloneReviewChangeVersion(v)
	}
	return out
}

func (c *ReviewChange) initRound1() {
	if c == nil {
		return
	}
	var x, y float32
	if c.afterObj != nil {
		x, y = c.afterObj.X, c.afterObj.Y
	} else if c.beforeObj != nil {
		x, y = c.beforeObj.X, c.beforeObj.Y
	}
	c.Versions = []ReviewChangeVersion{{
		Round:         1,
		Detail:        c.Detail,
		Why:           c.Why,
		OldValue:      c.OldValue,
		NewValue:      c.NewValue,
		DeltaPct:      c.DeltaPct,
		Line:          c.Line,
		X:             x,
		Y:             y,
		YAMLLines:     slices.Clone(c.YAMLLines),
		afterObj:      cloneObjectDocPtr(c.afterObj),
		afterLayer:    c.afterLayer,
		parentID:      c.ParentID,
		afterYAMLNode: cloneYAMLNodePtr(c.afterYAMLNode),
		cellDiffs:     slices.Clone(c.cellDiffs),
	}}
}

// VersionFor returns the immutable snapshot of c for the given 1-based review round.
func (c ReviewChange) VersionFor(round int) ReviewChangeVersion {
	if len(c.Versions) == 0 {
		var x, y float32
		if c.afterObj != nil {
			x, y = c.afterObj.X, c.afterObj.Y
		} else if c.beforeObj != nil {
			x, y = c.beforeObj.X, c.beforeObj.Y
		}
		return ReviewChangeVersion{
			Round:         1,
			Detail:        c.Detail,
			Why:           c.Why,
			OldValue:      c.OldValue,
			NewValue:      c.NewValue,
			DeltaPct:      c.DeltaPct,
			Line:          c.Line,
			X:             x,
			Y:             y,
			YAMLLines:     c.YAMLLines,
			afterObj:      c.afterObj,
			afterLayer:    c.afterLayer,
			parentID:      c.ParentID,
			afterYAMLNode: c.afterYAMLNode,
			cellDiffs:     c.cellDiffs,
		}
	}
	if round <= 1 {
		return c.Versions[0]
	}
	if round > len(c.Versions) {
		return c.Versions[len(c.Versions)-1]
	}
	return c.Versions[round-1]
}

func (c *ReviewChange) syncFromVersion(v ReviewChangeVersion) {
	c.Detail = v.Detail
	c.Why = v.Why
	c.OldValue = v.OldValue
	c.NewValue = v.NewValue
	c.DeltaPct = v.DeltaPct
	c.Line = v.Line
	c.YAMLLines = slices.Clone(v.YAMLLines)
	c.afterObj = cloneObjectDocPtr(v.afterObj)
	c.afterLayer = v.afterLayer
	c.ParentID = v.parentID
	c.afterYAMLNode = cloneYAMLNodePtr(v.afterYAMLNode)
	c.cellDiffs = slices.Clone(v.cellDiffs)
}

func (fs *reviewFileState) initRound1() {
	if fs == nil {
		return
	}
	var afterTbl *datatable.Document
	if fs.afterTable != nil {
		afterTbl = fs.afterTable.Clone()
	}
	fs.rounds = []reviewFileRound{{
		afterYAML:  append([]byte(nil), fs.afterYAML...),
		afterDoc:   cloneSceneDoc(fs.afterDoc),
		afterTable: afterTbl,
		builtDirty: true,
	}}
}

func (fs *reviewFileState) roundState(round int) *reviewFileRound {
	if fs == nil || len(fs.rounds) == 0 {
		return nil
	}
	if round <= 1 {
		return &fs.rounds[0]
	}
	if round > len(fs.rounds) {
		return &fs.rounds[len(fs.rounds)-1]
	}
	return &fs.rounds[round-1]
}

func (fs *reviewFileState) markAllRoundsDirty() {
	if fs == nil {
		return
	}
	fs.builtDirty = true
	for i := range fs.rounds {
		fs.rounds[i].builtDirty = true
	}
}

func (e *Editor) reviewLatestRound() int {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return 0
	}
	if e.review.round <= 0 {
		return 1
	}
	return e.review.round
}

func (e *Editor) reviewActiveViewRound() int {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return 0
	}
	latest := e.reviewLatestRound()
	vr := e.review.viewRound
	if vr <= 0 || vr > latest {
		return latest
	}
	return vr
}

// ReviewRound returns the latest 1-based round number of the pending review (0 when none is pending).
func (e *Editor) ReviewRound() int {
	return e.reviewLatestRound()
}

// ReviewViewRound returns the 1-based round currently being inspected in the Review screen.
func (e *Editor) ReviewViewRound() int {
	return e.reviewActiveViewRound()
}

// ReviewViewingReadOnlyRound reports whether the author is currently inspecting an earlier
// read-only round (for example Round 1 once Round 2 exists).
func (e *Editor) ReviewViewingReadOnlyRound() bool {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return false
	}
	return e.reviewActiveViewRound() < e.reviewLatestRound()
}

// SetReviewViewRound switches the Review screen between Round 1 (read-only once Round 2+ exists)
// and the latest round. Keep always applies the latest round.
func (e *Editor) SetReviewViewRound(round int) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	latest := e.reviewLatestRound()
	if round < 1 || round > latest {
		return ErrReviewInvalidRound
	}
	if e.review.viewRound == round {
		return nil
	}
	e.review.viewRound = round
	if round < latest {
		e.review.composingID = ""
		e.review.noteDraft = ""
	}
	for _, ch := range e.review.changes {
		ch.syncFromVersion(ch.VersionFor(round))
		if round == latest {
			if ch.HandEdited && ch.handEditObj != nil {
				if ch.Kind == ReviewChangeAdded {
					ch.Detail = formatAddedObjectDetail(ch.handEditObj)
				} else if ch.Kind == ReviewChangeChanged && ch.beforeObj != nil {
					ch.Detail = formatChangedObjectDetail(ch.beforeObj, ch.handEditObj, ch.beforeLayer, ch.afterLayer)
				}
			}
			if ch.HandEdited || ch.EditingYAML || len(ch.Conflicts) > 0 || ch.YAMLError != nil {
				if fs := e.review.filesByPath[ch.File]; fs != nil {
					e.refreshReviewChangeYAMLLines(fs, ch)
				}
			}
		}
	}
	e.review.yamlEditSessionRev++
	e.frameFocusedReviewChange()
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// ReviewRoundBannerText returns the banner copy shown beneath the filter chips once Round 2+ exists.
func (e *Editor) ReviewRoundBannerText() string {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ""
	}
	latest := e.reviewLatestRound()
	if latest <= 1 {
		return ""
	}
	view := e.reviewActiveViewRound()
	if view < latest {
		if view == 1 {
			return "Round 1, as the agent first did it. Keep always applies the latest round."
		}
		return fmt.Sprintf("Round %d, as the agent did it then. Keep always applies the latest round.", view)
	}
	n := 0
	if e.review.roundRedone != nil {
		n = e.review.roundRedone[latest]
	}
	return fmt.Sprintf("Round %d · the agent redid only the %d %s you left notes on. Both rounds keep and undo as one step.",
		latest, n, pluralWord(n, "change", "changes"))
}

// ReviewDraftNoteCount returns how many changes currently have an unsent draft note.
func (e *Editor) ReviewDraftNoteCount() int {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return 0
	}
	n := 0
	for _, ch := range e.review.changes {
		if strings.TrimSpace(ch.Note) != "" {
			n++
		}
	}
	return n
}

// ReviewUnsentOrPendingNoteCount returns how many distinct changes currently have either an unsent
// draft note or a sent note awaiting redo (used by the Revert all warning dialog).
func (e *Editor) ReviewUnsentOrPendingNoteCount() int {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return 0
	}
	n := 0
	for _, ch := range e.review.changes {
		if strings.TrimSpace(ch.Note) != "" || strings.TrimSpace(ch.SentNote) != "" {
			n++
		}
	}
	return n
}

// ReviewNoteComposerChangeID returns the change ID whose inline note composer is currently open (or "").
func (e *Editor) ReviewNoteComposerChangeID() string {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ""
	}
	return e.review.composingID
}

// ReviewNoteDraft returns the live unsent draft text currently in the inline note composer.
func (e *Editor) ReviewNoteDraft() string {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ""
	}
	return e.review.noteDraft
}

// OpenReviewNoteComposer opens the inline note composer ("Note for the agent · not sent") under changeID.
func (e *Editor) OpenReviewNoteComposer(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.review.changesByID[changeID]
	if ch == nil {
		return ErrReviewChangeNotFound
	}
	e.review.focusedID = ch.ID
	e.review.focusedRowKey = reviewCardRowKey(ch.ID)
	ch.Seen = true
	e.review.collapsedFiles[ch.File] = false
	e.review.collapsedKinds[ch.File+":"+string(ch.Kind)] = false
	e.review.peekBefore = false
	e.review.composingID = ch.ID
	e.review.noteDraft = ch.Note
	e.frameFocusedReviewChange()
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// OpenFocusedReviewNoteComposer implements the C shortcut in Review scope.
func (e *Editor) OpenFocusedReviewNoteComposer() error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.review.focusedID == "" {
		return ErrReviewChangeNotFound
	}
	return e.OpenReviewNoteComposer(e.review.focusedID)
}

// SetReviewNoteDraft updates the live composer draft without trimming spaces or triggering
// a full Review UI remount so typing stays completely smooth and focus-stable.
func (e *Editor) SetReviewNoteDraft(text string) {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.review.composingID == "" {
		return
	}
	e.review.noteDraft = truncateReviewNote(text)
}

// CancelReviewNoteComposer closes the inline note composer without modifying any saved note.
func (e *Editor) CancelReviewNoteComposer() {
	if e == nil || e.review.composingID == "" {
		return
	}
	e.review.composingID = ""
	e.review.noteDraft = ""
	e.bumpReviewUIRev()
	e.notify()
}

// SetReviewNote saves an unsent turn-scoped note on changeID. Notes live only in memory on the
// active turn and are never written to any scene, prefab, or data file.
func (e *Editor) SetReviewNote(changeID, note string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.review.changesByID[changeID]
	if ch == nil {
		return ErrReviewChangeNotFound
	}
	trimmed, err := validateReviewNoteText(note)
	if err != nil {
		return err
	}
	if ch.Note == "" && e.ReviewDraftNoteCount() >= maxReviewNotesCount {
		return ErrReviewNotesLimit
	}
	ch.Note = trimmed
	if e.review.composingID == changeID {
		e.review.composingID = ""
		e.review.noteDraft = ""
	}
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// CommitReviewNote saves the open inline note composer's draft text onto its target change.
func (e *Editor) CommitReviewNote() error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.review.composingID == "" {
		return ErrReviewChangeNotFound
	}
	return e.SetReviewNote(e.review.composingID, e.review.noteDraft)
}

// RemoveReviewNote removes an unsent draft note from changeID. A note added and removed before
// SendReviewNotes never enters the pending batch and never reaches the agent.
func (e *Editor) RemoveReviewNote(changeID string) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ErrReviewReadOnlyRound
	}
	ch := e.review.changesByID[changeID]
	if ch == nil {
		return ErrReviewChangeNotFound
	}
	ch.Note = ""
	if e.review.composingID == changeID {
		e.review.composingID = ""
		e.review.noteDraft = ""
	}
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

// PendingReviewNotes returns a deep copy of the notes explicitly sent by the author and waiting
// for the agent's next round redo, or nil if no notes have been sent.
func (e *Editor) PendingReviewNotes() *ReviewNoteBatch {
	if e == nil || e.review.outcome != ReviewOutcomePending || e.review.pendingBatch == nil || len(e.review.pendingBatch.Notes) == 0 {
		return nil
	}
	cp := &ReviewNoteBatch{
		Round: e.review.pendingBatch.Round,
		Notes: slices.Clone(e.review.pendingBatch.Notes),
	}
	return cp
}

func reviewAgentNoteForChange(ch *ReviewChange, note string, revertFirst bool) ReviewAgentNote {
	obj := ch.ObjectID
	if obj == "" {
		obj = ch.RowID
	}
	if obj == "" {
		obj = ch.Title
	}
	field := ch.Field
	if field == "" && ch.FileKind == ReviewFileScene {
		if ch.Kind == ReviewChangeChanged && ch.beforeObj != nil && ch.afterObj != nil {
			var changed []string
			if ch.beforeObj.X != ch.afterObj.X {
				changed = append(changed, "x")
			}
			if ch.beforeObj.Y != ch.afterObj.Y {
				changed = append(changed, "y")
			}
			if ch.beforeObj.Z != ch.afterObj.Z {
				changed = append(changed, "z")
			}
			if ch.beforeObj.Rotation != ch.afterObj.Rotation {
				changed = append(changed, "rotation")
			}
			if ch.beforeObj.Name != ch.afterObj.Name {
				changed = append(changed, "name")
			}
			if len(changed) > 0 {
				field = strings.Join(changed, ",")
			} else {
				field = "object"
			}
		} else {
			field = "object"
		}
	}
	return ReviewAgentNote{
		ChangeID:    ch.ID,
		File:        ch.File,
		FileKind:    ch.FileKind,
		Kind:        ch.Kind,
		Object:      obj,
		Field:       field,
		Title:       ch.Title,
		Note:        note,
		RevertFirst: revertFirst,
	}
}

func (e *Editor) dispatchPendingReviewNotes(notes []ReviewAgentNote) ReviewNoteBatch {
	latest := e.reviewLatestRound()
	if e.review.pendingBatch == nil {
		e.review.pendingBatch = &ReviewNoteBatch{Round: latest}
	}
	e.review.pendingBatch.Round = latest
	idxByID := make(map[string]int, len(e.review.pendingBatch.Notes))
	for i, existing := range e.review.pendingBatch.Notes {
		idxByID[existing.ChangeID] = i
	}
	for _, n := range notes {
		if idx, ok := idxByID[n.ChangeID]; ok {
			e.review.pendingBatch.Notes[idx] = n
		} else {
			idxByID[n.ChangeID] = len(e.review.pendingBatch.Notes)
			e.review.pendingBatch.Notes = append(e.review.pendingBatch.Notes, n)
		}
	}
	return ReviewNoteBatch{
		Round: e.review.pendingBatch.Round,
		Notes: slices.Clone(e.review.pendingBatch.Notes),
	}
}

// SendReviewNotes dispatches all remaining unsent draft notes to the agent over MCP (review_notes_get).
// Notes added and removed before calling SendReviewNotes never appear in the sent batch.
func (e *Editor) SendReviewNotes() (ReviewNoteBatch, error) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ReviewNoteBatch{}, ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ReviewNoteBatch{}, ErrReviewReadOnlyRound
	}
	var sending []ReviewAgentNote
	for _, ch := range e.review.changes {
		if trimmed := strings.TrimSpace(ch.Note); trimmed != "" {
			sending = append(sending, reviewAgentNoteForChange(ch, trimmed, false))
		}
	}
	if len(sending) == 0 {
		return ReviewNoteBatch{}, ErrReviewNoNotesToSend
	}
	for _, ch := range e.review.changes {
		if trimmed := strings.TrimSpace(ch.Note); trimmed != "" {
			ch.SentNote = trimmed
			ch.Note = ""
		}
	}
	e.review.composingID = ""
	e.review.noteDraft = ""
	batch := e.dispatchPendingReviewNotes(sending)
	e.Status = fmt.Sprintf("Sent %d %s to the agent", len(sending), pluralWord(len(sending), "note", "notes"))
	e.bumpReviewUIRev()
	e.notify()
	return batch, nil
}

// RevertAndRedoReviewChange marks changeID reverted (for non-Go changes) and immediately sends
// this note (along with any other unsent draft notes) to the agent with RevertFirst=true.
func (e *Editor) RevertAndRedoReviewChange(changeID, note string) (ReviewNoteBatch, error) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ReviewNoteBatch{}, ErrNoReview
	}
	if e.ReviewViewingReadOnlyRound() {
		return ReviewNoteBatch{}, ErrReviewReadOnlyRound
	}
	ch := e.review.changesByID[changeID]
	if ch == nil {
		return ReviewNoteBatch{}, ErrReviewChangeNotFound
	}
	trimmed, err := validateReviewNoteText(note)
	if err != nil {
		return ReviewNoteBatch{}, err
	}
	if !ch.GoRequiresConfirm {
		ch.Reverted = true
		if fs := e.review.filesByPath[ch.File]; fs != nil {
			fs.markAllRoundsDirty()
		}
		e.refreshPrefabImpacts()
	}
	ch.Note = trimmed

	var sending []ReviewAgentNote
	for _, c := range e.review.changes {
		if t := strings.TrimSpace(c.Note); t != "" {
			sending = append(sending, reviewAgentNoteForChange(c, t, c.ID == changeID))
		}
	}
	for _, c := range e.review.changes {
		if t := strings.TrimSpace(c.Note); t != "" {
			c.SentNote = t
			c.Note = ""
		}
	}
	e.review.composingID = ""
	e.review.noteDraft = ""
	batch := e.dispatchPendingReviewNotes(sending)
	e.Status = fmt.Sprintf("Reverted %s and sent note to the agent", ch.Title)
	e.bumpReviewUIRev()
	e.notify()
	return batch, nil
}

// ReviewObjectDocForChange returns a deep clone of the change's scene ObjectDoc and layer for round
// (used by editormcp's review_redo to apply coordinate/field updates cleanly).
func (e *Editor) ReviewObjectDocForChange(changeID string, round int) (*sceneformat.ObjectDoc, string, bool) {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return nil, "", false
	}
	ch := e.review.changesByID[changeID]
	if ch == nil || ch.FileKind != ReviewFileScene {
		return nil, "", false
	}
	ver := ch.VersionFor(round)
	if ver.afterObj != nil {
		return cloneObjectDocPtr(ver.afterObj), ver.afterLayer, true
	}
	if ch.beforeObj != nil {
		return cloneObjectDocPtr(ch.beforeObj), ch.beforeLayer, true
	}
	return nil, "", false
}

// ApplyReviewRedo atomically validates and applies the agent's second-round (or later) revisions
// strictly for changes that were noted and sent by the author.
//
// Invariants enforced:
//   - Any change_id not in the sent pending batch is refused atomically (no partial state changes).
//   - Earlier rounds remain intact and read-only.
//   - Un-noted changes in the same file cannot be altered even if FileContent is supplied.
//   - Redone changes become unseen again with "Redone from your note: <note>" as their Why.
//   - Keep still applies the latest round as ONE History undo step restoring exact pre-request bytes.
func (e *Editor) ApplyReviewRedo(redos []ReviewRedoChange) error {
	if e == nil || e.review.outcome != ReviewOutcomePending {
		return ErrNoReview
	}
	if len(redos) == 0 {
		return fmt.Errorf("editor: review_redo requires at least one noted change")
	}
	curRound := e.reviewLatestRound()
	if curRound >= maxReviewRounds {
		return ErrReviewMaxRounds
	}
	if e.review.pendingBatch == nil || len(e.review.pendingBatch.Notes) == 0 {
		return ErrReviewChangeNotNoted
	}

	notedByID := make(map[string]ReviewAgentNote, len(e.review.pendingBatch.Notes))
	for _, n := range e.review.pendingBatch.Notes {
		notedByID[n.ChangeID] = n
	}

	// Phase 1: atomic validation & pre-parsing before touching any live review state.
	type preparedRedo struct {
		rc          ReviewRedoChange
		ch          *ReviewChange
		fs          *reviewFileState
		sentNote    ReviewAgentNote
		sceneObj    *sceneformat.ObjectDoc
		sceneLayer  string
		sceneParent string
		prefabNode  *yaml.Node
		prefabVal   string
		dataRow     *datatable.Row
	}
	seenIDs := make(map[string]bool, len(redos))
	prepared := make([]preparedRedo, 0, len(redos))

	for _, rc := range redos {
		id := strings.TrimSpace(rc.ChangeID)
		if id == "" {
			return ErrReviewChangeNotFound
		}
		if seenIDs[id] {
			return fmt.Errorf("editor: duplicate change_id %q in review_redo", id)
		}
		seenIDs[id] = true

		ch := e.review.changesByID[id]
		if ch == nil {
			return fmt.Errorf("%w: %s", ErrReviewChangeNotFound, id)
		}
		sentNote, noted := notedByID[id]
		if !noted {
			return fmt.Errorf("%w: %s", ErrReviewChangeNotNoted, id)
		}
		if _, err := e.validateReviewRelPath(ch.File); err != nil {
			return err
		}
		if len(rc.FileContent) > maxReviewFileBytes {
			return fmt.Errorf("editor: review_redo file_content for %q exceeds %d byte limit", ch.File, maxReviewFileBytes)
		}
		if len(rc.YAMLLines) > maxReviewRedoYAMLLines {
			return fmt.Errorf("editor: review_redo yaml_lines for %q exceeds %d line limit", id, maxReviewRedoYAMLLines)
		}
		if rc.X != nil && (math.IsNaN(float64(*rc.X)) || math.IsInf(float64(*rc.X), 0)) {
			return fmt.Errorf("editor: review_redo invalid x coordinate for %q", id)
		}
		if rc.Y != nil && (math.IsNaN(float64(*rc.Y)) || math.IsInf(float64(*rc.Y), 0)) {
			return fmt.Errorf("editor: review_redo invalid y coordinate for %q", id)
		}
		fs := e.review.filesByPath[ch.File]
		if fs == nil {
			return fmt.Errorf("editor: review file state missing for %q", ch.File)
		}

		pr := preparedRedo{rc: rc, ch: ch, fs: fs, sentNote: sentNote}
		prevVer := ch.VersionFor(curRound)

		switch fs.kind {
		case ReviewFileScene:
			// Extract ONLY the noted object so un-noted objects in the same scene can never be altered.
			var obj *sceneformat.ObjectDoc
			layer := prevVer.afterLayer
			parentID := prevVer.parentID
			if len(rc.FileContent) > 0 {
				parsed, err := sceneformat.ParseWith(rc.FileContent, sceneformat.ParseOptions{Components: e.components})
				if err != nil {
					return fmt.Errorf("editor: review_redo parse scene %s: %w", ch.File, err)
				}
				_, flatMap := flattenReviewSceneDocObjects(parsed)
				if found, ok := flatMap[ch.ObjectID]; ok {
					obj = found.obj
					layer = found.layer
					parentID = found.parentID
				}
			}
			if rc.ObjectDoc != nil {
				obj = cloneObjectDocPtr(rc.ObjectDoc)
				if obj.ID == "" {
					obj.ID = ch.ObjectID
				}
				if rc.LayerName != "" {
					layer = rc.LayerName
				}
			}
			if obj == nil {
				if prevVer.afterObj != nil {
					obj = cloneObjectDocPtr(prevVer.afterObj)
				} else if ch.beforeObj != nil {
					obj = cloneObjectDocPtr(ch.beforeObj)
					if layer == "" {
						layer = ch.beforeLayer
					}
				}
			}
			if obj != nil {
				if rc.X != nil {
					obj.X = *rc.X
				}
				if rc.Y != nil {
					obj.Y = *rc.Y
				}
			}
			pr.sceneObj = obj
			pr.sceneLayer = layer
			pr.sceneParent = parentID

		case ReviewFilePrefab:
			if len(rc.FileContent) > 0 {
				_, leafMap, err := collectYAMLLeaves(rc.FileContent)
				if err != nil {
					return fmt.Errorf("editor: review_redo parse prefab %s: %w", ch.File, err)
				}
				if ent, ok := leafMap[ch.Field]; ok {
					pr.prefabNode = cloneYAMLNodePtr(ent.node)
					pr.prefabVal = ent.value
				}
			}
			if pr.prefabNode == nil && rc.NewValue != "" {
				var parsed yaml.Node
				if err := yaml.Unmarshal([]byte(rc.NewValue), &parsed); err == nil && len(parsed.Content) > 0 {
					pr.prefabNode = cloneYAMLNodePtr(parsed.Content[0])
					pr.prefabVal = yamlScalarDisplay(pr.prefabNode)
				} else {
					pr.prefabNode = &yaml.Node{Kind: yaml.ScalarNode, Value: rc.NewValue}
					pr.prefabVal = rc.NewValue
				}
			}

		case ReviewFileData:
			if len(rc.FileContent) > 0 {
				doc, err := datatable.ParseDocument(ch.File, rc.FileContent)
				if err != nil {
					return fmt.Errorf("editor: review_redo parse data table %s: %w", ch.File, err)
				}
				for _, r := range doc.Rows() {
					if dataRowID(r) == ch.RowID {
						cloned := r.Clone()
						pr.dataRow = &cloned
						break
					}
				}
			} else if rc.NewValue != "" && ch.Field != "" {
				rs := fs.roundState(curRound)
				if rs != nil && rs.afterTable != nil {
					for _, r := range rs.afterTable.Rows() {
						if dataRowID(r) == ch.RowID {
							cloned := r.Clone()
							updated := false
							for ci := range cloned.Cells {
								if cloned.Cells[ci].Key == ch.Field {
									kind := cloned.Cells[ci].Value.Kind
									if kind == datatable.KindList || kind == datatable.KindNull {
										kind = datatable.KindString
									}
									cloned.Cells[ci].Value = datatable.Value{Kind: kind, Text: rc.NewValue}
									updated = true
									break
								}
							}
							if !updated {
								cloned.Cells = append(cloned.Cells, datatable.Cell{
									Key:   ch.Field,
									Value: datatable.StringValue(rc.NewValue),
								})
							}
							pr.dataRow = &cloned
							break
						}
					}
				}
			}
		}

		prepared = append(prepared, pr)
	}

	// Phase 2: build Round N+1 snapshots (preserving Round 1..N immutably).
	nextRound := curRound + 1

	// Clone all file rounds to nextRound first.
	nextFileRounds := make(map[string]*reviewFileRound, len(e.review.files))
	for _, fs := range e.review.files {
		prev := fs.roundState(curRound)
		var clonedTbl *datatable.Document
		if prev != nil && prev.afterTable != nil {
			clonedTbl = prev.afterTable.Clone()
		}
		var prevYAML []byte
		var prevDoc *sceneformat.SceneDoc
		if prev != nil {
			prevYAML = append([]byte(nil), prev.afterYAML...)
			prevDoc = cloneSceneDoc(prev.afterDoc)
		}
		nr := reviewFileRound{
			afterYAML:  prevYAML,
			afterDoc:   prevDoc,
			afterTable: clonedTbl,
			builtDirty: true,
		}
		nextFileRounds[fs.path] = &nr
	}

	// Build each redone file's updated nextRound content strictly for the noted changes.
	touchedFiles := make(map[string]bool)
	preparedByID := make(map[string]preparedRedo, len(prepared))
	for _, pr := range prepared {
		preparedByID[pr.ch.ID] = pr
		touchedFiles[pr.fs.path] = true
	}

	for _, fs := range e.review.files {
		if !touchedFiles[fs.path] {
			continue
		}
		nr := nextFileRounds[fs.path]
		switch fs.kind {
		case ReviewFileScene:
			if nr.afterDoc == nil {
				nr.afterDoc = cloneSceneDoc(fs.beforeDoc)
			}
			if nr.afterDoc != nil {
				for _, pr := range prepared {
					if pr.fs.path != fs.path || pr.sceneObj == nil {
						continue
					}
					upsertReviewSceneObject(nr.afterDoc, *pr.sceneObj, pr.sceneLayer, pr.sceneParent)
				}
				encoded, err := sceneformat.Encode(nr.afterDoc)
				if err != nil {
					return fmt.Errorf("editor: review_redo encode scene %s: %w", fs.path, err)
				}
				nr.afterYAML = encoded
			}

		case ReviewFilePrefab:
			var root yaml.Node
			if err := yaml.Unmarshal(nr.afterYAML, &root); err != nil {
				return fmt.Errorf("editor: review_redo parse current prefab %s: %w", fs.path, err)
			}
			for _, pr := range prepared {
				if pr.fs.path != fs.path || pr.prefabNode == nil {
					continue
				}
				setYAMLNodeAtPath(&root, strings.Split(pr.ch.Field, "."), pr.prefabNode)
			}
			var buf bytes.Buffer
			enc := yaml.NewEncoder(&buf)
			enc.SetIndent(2)
			if err := enc.Encode(&root); err != nil {
				return fmt.Errorf("editor: review_redo encode prefab %s: %w", fs.path, err)
			}
			if err := enc.Close(); err != nil {
				return fmt.Errorf("editor: review_redo encode prefab %s: %w", fs.path, err)
			}
			nr.afterYAML = buf.Bytes()

		case ReviewFileData:
			if nr.afterTable != nil {
				for _, pr := range prepared {
					if pr.fs.path != fs.path || pr.dataRow == nil {
						continue
					}
					replaceReviewDataTableRow(nr.afterTable, pr.ch.RowID, *pr.dataRow)
				}
				encoded, err := nr.afterTable.Marshal()
				if err != nil {
					return fmt.Errorf("editor: review_redo encode data table %s: %w", fs.path, err)
				}
				nr.afterYAML = encoded
			}

		default:
			for _, pr := range prepared {
				if pr.fs.path == fs.path && len(pr.rc.FileContent) > 0 {
					nr.afterYAML = append([]byte(nil), pr.rc.FileContent...)
				}
			}
		}
	}

	// Commit file rounds (all encodings succeeded, so mutation from here cannot fail).
	for _, fs := range e.review.files {
		nr := *nextFileRounds[fs.path]
		fs.rounds = append(fs.rounds, nr)
		fs.afterYAML = append([]byte(nil), nr.afterYAML...)
		fs.afterDoc = cloneSceneDoc(nr.afterDoc)
		if nr.afterTable != nil {
			fs.afterTable = nr.afterTable.Clone()
		}
		fs.markAllRoundsDirty()
	}

	// Advance every change's Versions slice to nextRound.
	for _, ch := range e.review.changes {
		prevVer := cloneReviewChangeVersion(ch.VersionFor(curRound))
		nextVer := prevVer
		nextVer.Round = nextRound

		pr, isRedone := preparedByID[ch.ID]
		if !isRedone {
			ch.Versions = append(ch.Versions, nextVer)
			ch.syncFromVersion(nextVer)
			continue
		}

		fs := pr.fs
		nr := fs.roundState(nextRound)
		nextVer.Why = "Redone from your note: " + pr.sentNote.Note

		switch fs.kind {
		case ReviewFileScene:
			if pr.sceneObj != nil {
				nextVer.afterObj = cloneObjectDocPtr(pr.sceneObj)
				nextVer.afterLayer = pr.sceneLayer
				nextVer.parentID = pr.sceneParent
				nextVer.X = pr.sceneObj.X
				nextVer.Y = pr.sceneObj.Y
			}
			if pr.rc.Detail != "" {
				nextVer.Detail = pr.rc.Detail
			} else if nextVer.afterObj != nil {
				if ch.Kind == ReviewChangeAdded {
					nextVer.Detail = formatAddedObjectDetail(nextVer.afterObj)
				} else if ch.beforeObj != nil {
					nextVer.Detail = formatChangedObjectDetail(ch.beforeObj, nextVer.afterObj, ch.beforeLayer, nextVer.afterLayer)
				}
			}
			if len(pr.rc.YAMLLines) > 0 {
				nextVer.YAMLLines = slices.Clone(pr.rc.YAMLLines)
			} else if nextVer.afterObj != nil && nr != nil {
				if ch.Kind == ReviewChangeAdded {
					ln, yl := buildAddedObjectYAMLDiff(nr.afterYAML, nextVer.afterObj)
					nextVer.Line, nextVer.YAMLLines = ln, yl
				} else if ch.beforeObj != nil {
					ln, yl := buildChangedObjectYAMLDiff(fs.beforeYAML, nr.afterYAML, ch.beforeObj, nextVer.afterObj)
					nextVer.Line, nextVer.YAMLLines = ln, yl
				}
			}

		case ReviewFilePrefab:
			if pr.prefabNode != nil {
				nextVer.afterYAMLNode = cloneYAMLNodePtr(pr.prefabNode)
				nextVer.NewValue = pr.prefabVal
			}
			if pr.rc.OldValue != "" {
				nextVer.OldValue = pr.rc.OldValue
			}
			if pr.rc.NewValue != "" {
				nextVer.NewValue = pr.rc.NewValue
			}
			if pr.rc.DeltaPct != "" {
				nextVer.DeltaPct = pr.rc.DeltaPct
			} else if nextVer.OldValue != "" && nextVer.NewValue != "" {
				nextVer.DeltaPct = formatDeltaPercent(nextVer.OldValue, nextVer.NewValue)
			}
			if pr.rc.Detail != "" {
				nextVer.Detail = pr.rc.Detail
			} else if nextVer.OldValue != "" && nextVer.NewValue != "" {
				nextVer.Detail = fmt.Sprintf("%s → %s", nextVer.OldValue, nextVer.NewValue)
				if nextVer.DeltaPct != "" {
					nextVer.Detail += " · " + nextVer.DeltaPct
				}
			}
			if len(pr.rc.YAMLLines) > 0 {
				nextVer.YAMLLines = slices.Clone(pr.rc.YAMLLines)
			} else if nr != nil {
				_, beforeLeaves, _ := collectYAMLLeaves(fs.beforeYAML)
				_, afterLeaves, _ := collectYAMLLeaves(nr.afterYAML)
				bl, al := beforeLeaves[ch.Field].line, afterLeaves[ch.Field].line
				nextVer.Line = al
				nextVer.YAMLLines = buildFieldYAMLDiff(fs.beforeYAML, nr.afterYAML, bl, al)
			}

		case ReviewFileData:
			if pr.dataRow != nil && fs.beforeTable != nil {
				for _, br := range fs.beforeTable.Rows() {
					if dataRowID(br) == ch.RowID {
						diffs := diffDataTableRows(br, *pr.dataRow)
						nextVer.cellDiffs = diffs
						if len(diffs) > 0 {
							nextVer.OldValue = diffs[0].OldValue
							nextVer.NewValue = diffs[0].NewValue
							nextVer.DeltaPct = diffs[0].DeltaPct
						}
						break
					}
				}
			}
			if pr.rc.OldValue != "" {
				nextVer.OldValue = pr.rc.OldValue
			}
			if pr.rc.NewValue != "" {
				nextVer.NewValue = pr.rc.NewValue
			}
			if pr.rc.DeltaPct != "" {
				nextVer.DeltaPct = pr.rc.DeltaPct
			}
			if pr.rc.Detail != "" {
				nextVer.Detail = pr.rc.Detail
			} else if len(nextVer.cellDiffs) > 0 {
				d0 := nextVer.cellDiffs[0]
				nextVer.Detail = fmt.Sprintf("%s %s → %s", d0.Column, d0.OldValue, d0.NewValue)
				if d0.DeltaPct != "" {
					nextVer.Detail += " · " + d0.DeltaPct
				}
				if len(nextVer.cellDiffs) > 1 {
					nextVer.Detail += fmt.Sprintf(" · +%d more", len(nextVer.cellDiffs)-1)
				}
			}
			if len(pr.rc.YAMLLines) > 0 {
				nextVer.YAMLLines = slices.Clone(pr.rc.YAMLLines)
			} else if nr != nil {
				ln, yl := buildDataRowYAMLDiff(fs.beforeYAML, nr.afterYAML, ch.RowID, ch.Kind)
				nextVer.Line, nextVer.YAMLLines = ln, yl
			}

		default:
			if pr.rc.Detail != "" {
				nextVer.Detail = pr.rc.Detail
			}
			if len(pr.rc.YAMLLines) > 0 {
				nextVer.YAMLLines = slices.Clone(pr.rc.YAMLLines)
			} else if nr != nil {
				yl, addCnt, delCnt := buildUnifiedTextDiff(fs.beforeYAML, nr.afterYAML)
				nextVer.YAMLLines = yl
				if pr.rc.Detail == "" {
					nextVer.Detail = fmt.Sprintf("+%d −%d", addCnt, delCnt)
				}
			}
		}

		ch.Versions = append(ch.Versions, nextVer)
		ch.syncFromVersion(nextVer)
		ch.EditingYAML = false
		ch.YAMLError = nil
		ch.HandEdited = false
		ch.handEditObj = nil
		ch.handEditBaseObj = nil
		ch.HandEditFields = nil
		ch.handEditLineNums = nil
		ch.HandEditYAML = ""
		ch.Seen = false
		ch.Reverted = false
		ch.RedoneInRound = nextRound
		ch.RedoneNote = pr.sentNote.Note
		ch.SentNote = ""
	}

	// Remove redone notes from pendingBatch (keeping any un-redone notes if the agent redid a subset).
	remaining := make([]ReviewAgentNote, 0, len(e.review.pendingBatch.Notes))
	for _, n := range e.review.pendingBatch.Notes {
		if !seenIDs[n.ChangeID] {
			remaining = append(remaining, n)
		}
	}
	if len(remaining) == 0 {
		e.review.pendingBatch = nil
	} else {
		e.review.pendingBatch.Notes = remaining
	}

	e.review.round = nextRound
	e.review.viewRound = nextRound
	e.review.yamlEditSessionRev++
	if e.review.roundRedone == nil {
		e.review.roundRedone = make(map[int]int)
	}
	e.review.roundRedone[nextRound] = len(prepared)
	e.review.composingID = ""
	e.review.noteDraft = ""

	e.syncAllReviewConflicts()
	for _, ch := range e.review.changes {
		if ch.HandEdited && ch.handEditObj != nil {
			if ch.Kind == ReviewChangeAdded {
				ch.Detail = formatAddedObjectDetail(ch.handEditObj)
			} else if ch.Kind == ReviewChangeChanged && ch.beforeObj != nil {
				ch.Detail = formatChangedObjectDetail(ch.beforeObj, ch.handEditObj, ch.beforeLayer, ch.afterLayer)
			}
			if fs := e.review.filesByPath[ch.File]; fs != nil {
				e.refreshReviewChangeYAMLLines(fs, ch)
			}
		}
	}
	e.refreshPrefabImpacts()
	e.frameFocusedReviewChange()
	e.Status = e.ReviewRoundBannerText()
	e.bumpReviewUIRev()
	e.notify()
	return nil
}

func upsertReviewSceneObject(doc *sceneformat.SceneDoc, obj sceneformat.ObjectDoc, targetLayer, parentID string) {
	if doc == nil {
		return
	}
	// Replace in place if already present anywhere in the scene.
	var replace func(objs []sceneformat.ObjectDoc) bool
	replace = func(objs []sceneformat.ObjectDoc) bool {
		for i := range objs {
			if objs[i].ID == obj.ID {
				children := objs[i].Children
				cp := cloneObjectDoc(obj)
				if len(cp.Children) == 0 {
					cp.Children = children
				}
				objs[i] = cp
				return true
			}
			if replace(objs[i].Children) {
				return true
			}
		}
		return false
	}
	for li := range doc.Layers {
		if replace(doc.Layers[li].Objects) {
			return
		}
	}
	cp := cloneObjectDoc(obj)
	if parentID != "" && insertChildByID(doc, parentID, cp) {
		return
	}
	if targetLayer == "" && len(doc.Layers) > 0 {
		targetLayer = doc.Layers[0].Name
	}
	li := findOrCreateReviewLayer(doc, targetLayer, nil)
	doc.Layers[li].Objects = append(doc.Layers[li].Objects, cp)
}

func replaceReviewDataTableRow(doc *datatable.Document, rowID string, updated datatable.Row) {
	if doc == nil {
		return
	}
	for i := 0; i < doc.RowCount(); i++ {
		r := doc.Row(i)
		if dataRowID(r) != rowID {
			continue
		}
		for _, c := range updated.Cells {
			_ = doc.SetCell(i, c.Key, c.Value.Clone())
		}
		for _, k := range r.Keys() {
			if _, exists := updated.Get(k); !exists {
				_ = doc.DeleteCell(i, k)
			}
		}
		return
	}
	_, _ = doc.AppendRow(updated.Clone().Cells...)
}

// ReviewViewportPins returns inspectable note pins for visible non-reverted scene objects in
// the focused review scene (when Review is open) or the open editor scene (when Review is paused).
func (e *Editor) ReviewViewportPins() []ReviewViewportPin {
	if e == nil || !e.HasPendingReview() {
		return nil
	}
	if e.review.open && e.review.peekBefore {
		return nil
	}
	var targetFile string
	if e.review.open {
		if ch := e.review.changesByID[e.review.focusedID]; ch != nil && ch.FileKind == ReviewFileScene {
			targetFile = ch.File
		}
	} else {
		targetFile = e.openSceneProjectRelPath()
	}
	if targetFile == "" {
		return nil
	}
	return e.reviewViewportPinsForFile(targetFile)
}

func (e *Editor) reviewViewportPinsForFile(filePath string) []ReviewViewportPin {
	fs := e.review.filesByPath[filePath]
	if fs == nil || fs.kind != ReviewFileScene {
		return nil
	}
	round := e.reviewActiveViewRound()
	viewingLatest := !e.ReviewViewingReadOnlyRound()
	effBuilt := e.reviewSceneBuiltForRound(fs, round, false)
	beforeBuilt := e.reviewSceneBuiltForRound(fs, round, true)

	var pins []ReviewViewportPin
	for _, ch := range e.review.changes {
		if ch.File != filePath || ch.Reverted {
			continue
		}
		noteText := strings.TrimSpace(ch.Note)
		redone := false
		if noteText == "" {
			noteText = strings.TrimSpace(ch.SentNote)
		}
		if noteText == "" && viewingLatest && ch.RedoneInRound > 1 && strings.TrimSpace(ch.RedoneNote) != "" {
			noteText = strings.TrimSpace(ch.RedoneNote)
			redone = true
		}
		if noteText == "" {
			continue
		}
		ver := ch.VersionFor(round)
		var bo *sceneformat.BuiltObject
		if ch.Kind == ReviewChangeRemoved && ver.afterObj == nil {
			if beforeBuilt != nil {
				bo = beforeBuilt.Object(ch.ObjectID)
			}
		} else if effBuilt != nil {
			bo = effBuilt.Object(ch.ObjectID)
		}
		var x, y float32
		if bo != nil {
			rx, ry, rw, _ := objectOutline(bo)
			p := bo.World.TransformPoint(math2d.Vec2{X: rx + rw, Y: ry})
			x, y = p.X, p.Y
		} else {
			obj := ver.afterObj
			if obj == nil {
				obj = ch.beforeObj
			}
			if obj != nil {
				x, y = obj.X+defaultMarkerHalfSize, obj.Y-defaultMarkerHalfSize
			}
		}
		pins = append(pins, ReviewViewportPin{
			ChangeID: ch.ID,
			File:     filePath,
			ObjectID: ch.ObjectID,
			Note:     noteText,
			Redone:   redone,
			X:        x,
			Y:        y,
		})
	}
	return pins
}
