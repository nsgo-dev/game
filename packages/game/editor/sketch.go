package editor

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/math2d"
	gamemcp "github.com/nsgo-dev/game/packages/game/mcp"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"
)

// SketchTool selects one of the four viewport sketch drawing tools.
type SketchTool string

const (
	// SketchToolPen draws a freehand polyline in scene coordinates.
	SketchToolPen SketchTool = "pen"
	// SketchToolArrow draws a directed arrow from start to end in scene coordinates.
	SketchToolArrow SketchTool = "arrow"
	// SketchToolBox draws a rectangular region in scene coordinates.
	SketchToolBox SketchTool = "box"
	// SketchToolText places a labelled anchor point in scene coordinates.
	SketchToolText SketchTool = "text"
)

// Valid reports whether t is one of the four built-in sketch tools.
func (t SketchTool) Valid() bool {
	switch t {
	case SketchToolPen, SketchToolArrow, SketchToolBox, SketchToolText:
		return true
	default:
		return false
	}
}

// String returns the human-readable tool label.
func (t SketchTool) String() string {
	switch t {
	case SketchToolPen:
		return "Pen"
	case SketchToolArrow:
		return "Arrow"
	case SketchToolBox:
		return "Box"
	case SketchToolText:
		return "Text"
	default:
		return "Arrow"
	}
}

// Hint returns the short status hint displayed below the floating sketch toolbar.
func (t SketchTool) Hint() string {
	switch t {
	case SketchToolPen:
		return "Pen · drag to draw a free line"
	case SketchToolArrow:
		return "Arrow · drag from where to where"
	case SketchToolBox:
		return "Box · drag to mark an area"
	case SketchToolText:
		return "Text · click to write a short label"
	default:
		return "Arrow · drag from where to where"
	}
}

// SketchHoldState describes the confirmation hold lifecycle for a sent viewport sketch.
type SketchHoldState string

const (
	// SketchHoldNone means no sent sketch is currently awaiting confirmation or editing.
	SketchHoldNone SketchHoldState = "none"
	// SketchHoldPending means a sketch was sent to the agent; mutating scene MCP tools hold until confirmed.
	SketchHoldPending SketchHoldState = "pending"
	// SketchHoldReading means the agent posted its restatement card; mutating scene MCP tools still hold until Go ahead or a reply.
	SketchHoldReading SketchHoldState = "reading"
	// SketchHoldApproved means the user answered Go ahead (or replied approvingly) and the agent may now edit the scene.
	SketchHoldApproved SketchHoldState = "approved"
)

// SketchPoint is a 2D point stored strictly in scene (world) coordinates.
type SketchPoint struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// SketchRect is an axis-aligned rectangle stored strictly in scene (world) coordinates.
type SketchRect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// SketchMark is one numbered annotation drawn on the viewport sketch layer in scene coordinates.
// Sketch marks live solely in editor session state and are never written into SceneDoc or *.scene.yaml.
type SketchMark struct {
	ID     string        `json:"id"`
	Number int           `json:"number"`
	Kind   SketchTool    `json:"kind"`
	Label  string        `json:"label,omitempty"`
	Points []SketchPoint `json:"points,omitempty"`
	Start  *SketchPoint  `json:"start,omitempty"`
	End    *SketchPoint  `json:"end,omitempty"`
	Rect   *SketchRect   `json:"rect,omitempty"`
	At     *SketchPoint  `json:"at,omitempty"`
}

// Clone returns a deep copy of m.
func (m SketchMark) Clone() SketchMark {
	out := m
	if len(m.Points) > 0 {
		out.Points = slices.Clone(m.Points)
	}
	if m.Start != nil {
		p := *m.Start
		out.Start = &p
	}
	if m.End != nil {
		p := *m.End
		out.End = &p
	}
	if m.Rect != nil {
		r := *m.Rect
		out.Rect = &r
	}
	if m.At != nil {
		p := *m.At
		out.At = &p
	}
	return out
}

// AnchorScenePoint returns the primary anchor point of m in scene coordinates,
// where its numbered badge attaches.
func (m SketchMark) AnchorScenePoint() SketchPoint {
	switch m.Kind {
	case SketchToolBox:
		if m.Rect != nil {
			return SketchPoint{X: m.Rect.X, Y: m.Rect.Y}
		}
	case SketchToolArrow:
		if m.Start != nil {
			return *m.Start
		}
	case SketchToolPen:
		if len(m.Points) > 0 {
			return m.Points[0]
		}
	case SketchToolText:
		if m.At != nil {
			return *m.At
		}
	}
	return SketchPoint{}
}

// GeometrySummary formats m's geometry concisely in scene coordinates for the preview popover and reading card.
func (m SketchMark) GeometrySummary() string {
	ri := func(v float32) int { return int(math.Round(float64(v))) }
	switch m.Kind {
	case SketchToolBox:
		if m.Rect != nil {
			return fmt.Sprintf("rect %d,%d %d×%d", ri(m.Rect.X), ri(m.Rect.Y), ri(m.Rect.Width), ri(m.Rect.Height))
		}
		return "rect 0,0 0×0"
	case SketchToolPen:
		if len(m.Points) >= 2 {
			first := m.Points[0]
			last := m.Points[len(m.Points)-1]
			return fmt.Sprintf("path %d,%d → %d,%d · %d points", ri(first.X), ri(first.Y), ri(last.X), ri(last.Y), len(m.Points))
		}
		if len(m.Points) == 1 {
			return fmt.Sprintf("path %d,%d · 1 point", ri(m.Points[0].X), ri(m.Points[0].Y))
		}
		return "path 0 points"
	case SketchToolArrow:
		if m.Start != nil && m.End != nil {
			return fmt.Sprintf("arrow %d,%d → %d,%d", ri(m.Start.X), ri(m.Start.Y), ri(m.End.X), ri(m.End.Y))
		}
		return "arrow"
	case SketchToolText:
		if m.At != nil {
			return fmt.Sprintf("text %d,%d", ri(m.At.X), ri(m.At.Y))
		}
		return "text"
	default:
		return string(m.Kind)
	}
}

// ReadingHeading formats the compact mark caption shown above an agent restatement row
// (for example `Box · “3 spikes”`).
func (m SketchMark) ReadingHeading() string {
	kind := m.Kind.String()
	if m.Kind == SketchToolPen {
		kind = "Line"
	}
	if strings.TrimSpace(m.Label) != "" {
		return fmt.Sprintf("%s · “%s”", kind, strings.TrimSpace(m.Label))
	}
	return kind
}

// SketchAgentRequest is the structured payload sent to the agent when the author sends a viewport sketch.
type SketchAgentRequest struct {
	Scene     string       `json:"scene"`
	ChipLabel string       `json:"chipLabel"`
	Prompt    string       `json:"prompt,omitempty"`
	View      View2D       `json:"view"`
	Marks     []SketchMark `json:"marks"`
	PNG       []byte       `json:"-"`
	Width     int          `json:"width,omitempty"`
	Height    int          `json:"height,omitempty"`
}

// SketchReadingItem is the agent's restatement of one numbered sketch mark before editing.
type SketchReadingItem struct {
	Number int    `json:"number"`
	Mark   string `json:"mark,omitempty"`
	Text   string `json:"text"`
}

// SketchReading is the reading card posted by the agent via MCP before scene_edit is allowed.
type SketchReading struct {
	Summary     string              `json:"summary,omitempty"`
	Items       []SketchReadingItem `json:"items"`
	Assumptions []string            `json:"assumptions,omitempty"`
	PostedAt    time.Time           `json:"postedAt,omitempty"`
}

// Clone returns a deep copy of r.
func (r *SketchReading) Clone() *SketchReading {
	if r == nil {
		return nil
	}
	out := *r
	out.Items = slices.Clone(r.Items)
	out.Assumptions = slices.Clone(r.Assumptions)
	return &out
}

// SketchReadingReply describes the user's answer to an agent sketch reading card.
type SketchReadingReply struct {
	Action     string `json:"action"` // "approve", "correct", or "reply"
	MarkNumber int    `json:"markNumber,omitempty"`
	Message    string `json:"message,omitempty"`
}

// SketchReviewChangeKind classifies one scene edit tracked by a post-sketch review.
type SketchReviewChangeKind string

const (
	SketchChangeAdded   SketchReviewChangeKind = "added"
	SketchChangeChanged SketchReviewChangeKind = "changed"
	SketchChangeRemoved SketchReviewChangeKind = "removed"
)

// SketchReviewChange describes one object added, changed, or removed by the agent after approving a sketch.
type SketchReviewChange struct {
	Kind   SketchReviewChangeKind `json:"kind"`
	ID     string                 `json:"id"`
	Detail string                 `json:"detail,omitempty"`
}

type sketchDraftDrag struct {
	tool       SketchTool
	startWorld SketchPoint
	endWorld   SketchPoint
	points     []SketchPoint
	moved      bool
	hitMark    int
}

type sketchReviewState struct {
	open           bool
	overlayVisible bool
	baseYAML       []byte
	historyStart   int
	changes        []SketchReviewChange
	undoLabels     []string
}

type sketchState struct {
	active         bool
	panelOpen      bool
	panelDismissed bool
	tool           SketchTool
	marks          []SketchMark
	undoStack      [][]SketchMark
	nextSeq        uint64
	selectedMark   int
	marksHidden    bool
	attached       bool
	previewOpen    bool
	promptDraft    string
	hold           SketchHoldState
	reading        *SketchReading
	lastRequest    *SketchAgentRequest
	draft          *sketchDraftDrag
	review         *sketchReviewState
	forcePNGMarks  bool
}

const (
	maxSketchMarks              = 200
	maxSketchUndoDepth          = 64
	maxSketchPenPoints          = 2048
	maxSketchLabelRunes         = 256
	maxSketchReadingItems       = maxSketchMarks
	maxSketchReadingAssumptions = 32
	maxSketchReadingTextRunes   = 2048
	maxSketchPNGDim             = 2048
	maxSketchPNGPixels          = 4_000_000
	sketchBadgeRadiusPt         = 9.0
	sketchDragMinDistPt         = 3.0
	sketchPenStepMinPt          = 3.0
	sketchDefaultBoxW           = 96.0
	sketchDefaultBoxH           = 40.0
	sketchDefaultArrowDX        = 84.0
	sketchDefaultArrowDY        = 36.0
)

func clampSketchRunes(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if maxRunes <= 0 || len(s) <= maxRunes {
		return s
	}
	n := 0
	for i := range s {
		if n == maxRunes {
			return s[:i]
		}
		n++
	}
	return s
}

func (e *Editor) resetSketchForProject() {
	if e == nil {
		return
	}
	e.sketch = sketchState{}
}

func (e *Editor) sketchBlockedByScreen() bool {
	if e == nil {
		return true
	}
	return e.playing ||
		e.StartScreenOpen() ||
		e.SettingsOpen() ||
		e.MergeConflictOpen() ||
		e.ChangesOpen() ||
		e.BuildReleaseScreenOpen() ||
		e.UsagesScreenOpen() ||
		e.PlaytestReportOpen() ||
		e.AssetGeneratorOpen() ||
		e.HUDLayoutOpen()
}

var (
	// ErrSketchEmpty is returned when sending or reading a sketch with no marks.
	ErrSketchEmpty = errors.New("editor: no sketch marks on the viewport")
	// ErrSketchHoldActive is returned when a mutating agent edit runs while a sent sketch awaits confirmation.
	ErrSketchHoldActive = errors.New("editor: sent sketch is waiting for confirmation (Go ahead or Correct a mark)")
	// ErrNoSketchReview is returned when Keep or Revert is invoked without an open sketch review.
	ErrNoSketchReview = errors.New("editor: no sketch review is open")
)

func cloneSketchMarks(marks []SketchMark) []SketchMark {
	if len(marks) == 0 {
		return nil
	}
	out := make([]SketchMark, len(marks))
	for i, m := range marks {
		out[i] = m.Clone()
	}
	return out
}

func cloneSketchState(src sketchState) sketchState {
	dst := src
	dst.marks = cloneSketchMarks(src.marks)
	if len(src.undoStack) > 0 {
		dst.undoStack = make([][]SketchMark, len(src.undoStack))
		for i, snap := range src.undoStack {
			dst.undoStack[i] = cloneSketchMarks(snap)
		}
	}
	dst.reading = src.reading.Clone()
	if src.lastRequest != nil {
		req := *src.lastRequest
		req.Marks = cloneSketchMarks(src.lastRequest.Marks)
		req.PNG = slices.Clone(src.lastRequest.PNG)
		dst.lastRequest = &req
	}
	dst.draft = nil
	if src.review != nil {
		rv := *src.review
		rv.baseYAML = slices.Clone(src.review.baseYAML)
		rv.changes = slices.Clone(src.review.changes)
		rv.undoLabels = slices.Clone(src.review.undoLabels)
		dst.review = &rv
	}
	return dst
}

func (e *Editor) ensureSketchTool() SketchTool {
	if !e.sketch.tool.Valid() {
		e.sketch.tool = SketchToolArrow
	}
	return e.sketch.tool
}

func (e *Editor) pushSketchUndo() {
	snap := cloneSketchMarks(e.sketch.marks)
	e.sketch.undoStack = append(e.sketch.undoStack, snap)
	if len(e.sketch.undoStack) > maxSketchUndoDepth {
		e.sketch.undoStack = e.sketch.undoStack[len(e.sketch.undoStack)-maxSketchUndoDepth:]
	}
}

func (e *Editor) renumberSketchMarks() {
	for i := range e.sketch.marks {
		e.sketch.marks[i].Number = i + 1
	}
	if e.sketch.selectedMark > len(e.sketch.marks) {
		e.sketch.selectedMark = len(e.sketch.marks)
	}
	if len(e.sketch.marks) == 0 {
		e.sketch.selectedMark = 0
		e.sketch.attached = false
		e.sketch.previewOpen = false
	}
}

// SketchActive reports whether viewport sketch drawing mode is currently active.
func (e *Editor) SketchActive() bool {
	return e != nil && e.sketch.active && e.ViewMode == ViewMode2D && !e.playing
}

// SetSketchActive turns viewport sketch drawing mode on or off.
// Turning sketch mode off keeps existing marks on the layer until sent, cleared, or kept.
func (e *Editor) SetSketchActive(on bool) {
	if e == nil || e.playing {
		return
	}
	if on && e.sketchBlockedByScreen() {
		return
	}
	if on && e.ViewMode != ViewMode2D {
		e.SetViewMode(ViewMode2D)
	}
	if on {
		e.ensureSketchTool()
		e.cancelActiveObjectDrags()
		e.sketch.active = true
		e.sketch.panelOpen = true
		e.sketch.panelDismissed = false
		e.sketch.marksHidden = false
		if len(e.sketch.marks) > 0 {
			e.sketch.attached = true
		}
		e.Status = "Sketch layer · " + e.sketch.tool.String()
	} else {
		e.sketch.active = false
		e.sketch.draft = nil
		if len(e.sketch.marks) > 0 {
			e.Status = fmt.Sprintf("Sketch finished · %s", e.SketchBarCountLabel())
		}
	}
	e.notify()
}

// StartSketch activates viewport sketch mode and opens the Sketch/Agent panel.
func (e *Editor) StartSketch() {
	e.SetSketchActive(true)
}

// FinishSketch exits sketch drawing mode (Done / Esc) while keeping all drawn marks on the viewport
// until they are sent, cleared, or kept.
func (e *Editor) FinishSketch() {
	e.SetSketchActive(false)
}

// ToggleSketch toggles viewport sketch drawing mode on or off.
func (e *Editor) ToggleSketch() {
	if e == nil {
		return
	}
	e.SetSketchActive(!e.SketchActive())
}

// SketchPanelOpen reports whether the right-side Sketch / Agent panel should be shown.
func (e *Editor) SketchPanelOpen() bool {
	if e == nil || e.playing || e.ViewMode != ViewMode2D {
		return false
	}
	if e.sketch.panelDismissed && !e.sketch.active {
		return false
	}
	return e.sketch.panelOpen || e.sketch.active || e.sketch.reading != nil || e.SketchReviewOpen()
}

// SetSketchPanelOpen shows or hides the right-side Sketch / Agent panel.
func (e *Editor) SetSketchPanelOpen(open bool) {
	if e == nil {
		return
	}
	if !open {
		e.sketch.active = false
		e.sketch.draft = nil
		e.sketch.panelDismissed = true
	} else {
		e.sketch.panelDismissed = false
	}
	e.sketch.panelOpen = open
	e.notify()
}

// SketchTool returns the active sketch drawing tool (Pen, Arrow, Box, or Text).
func (e *Editor) SketchTool() SketchTool {
	if e == nil {
		return SketchToolArrow
	}
	return e.ensureSketchTool()
}

// SetSketchTool switches the active sketch tool and ensures sketch mode is active.
func (e *Editor) SetSketchTool(tool SketchTool) {
	if e == nil || !tool.Valid() || e.playing {
		return
	}
	e.sketch.tool = tool
	if !e.sketch.active {
		e.SetSketchActive(true)
		return
	}
	e.sketch.draft = nil
	e.Status = "Sketch layer · " + tool.String()
	e.notify()
}

// SketchMarks returns a deep copy of all marks currently on the viewport sketch layer, in sequential badge order.
func (e *Editor) SketchMarks() []SketchMark {
	if e == nil {
		return nil
	}
	return cloneSketchMarks(e.sketch.marks)
}

// SketchMarkCount returns the number of marks currently on the viewport sketch layer.
func (e *Editor) SketchMarkCount() int {
	if e == nil {
		return 0
	}
	return len(e.sketch.marks)
}

// SketchMarkByNumber returns the 1-based numbered sketch mark, if present.
func (e *Editor) SketchMarkByNumber(number int) (SketchMark, bool) {
	if e == nil || number < 1 || number > len(e.sketch.marks) {
		return SketchMark{}, false
	}
	return e.sketch.marks[number-1].Clone(), true
}

// SelectedSketchMark returns the 1-based number of the currently selected sketch mark, or 0 when none is selected.
func (e *Editor) SelectedSketchMark() int {
	if e == nil || e.sketch.selectedMark < 1 || e.sketch.selectedMark > len(e.sketch.marks) {
		return 0
	}
	return e.sketch.selectedMark
}

// SelectSketchMark selects the 1-based mark number (or clears mark selection when number <= 0).
func (e *Editor) SelectSketchMark(number int) {
	if e == nil {
		return
	}
	if number < 1 || number > len(e.sketch.marks) {
		if e.sketch.selectedMark != 0 {
			e.sketch.selectedMark = 0
			e.notify()
		}
		return
	}
	if e.sketch.selectedMark != number {
		e.sketch.selectedMark = number
		e.notify()
	}
}

// SketchMarksHidden reports whether the sketch layer marks are temporarily hidden via the eye button.
func (e *Editor) SketchMarksHidden() bool {
	return e != nil && e.sketch.marksHidden
}

// SetSketchMarksHidden hides or shows sketch marks on the viewport without clearing them.
func (e *Editor) SetSketchMarksHidden(hidden bool) {
	if e == nil || e.sketch.marksHidden == hidden {
		return
	}
	e.sketch.marksHidden = hidden
	e.notify()
}

// ToggleSketchMarksHidden flips whether sketch marks are temporarily hidden on the viewport.
func (e *Editor) ToggleSketchMarksHidden() {
	if e == nil {
		return
	}
	e.SetSketchMarksHidden(!e.sketch.marksHidden)
}

// SketchAttached reports whether the active sketch marks are attached to the next agent request chip.
func (e *Editor) SketchAttached() bool {
	return e != nil && e.sketch.attached && len(e.sketch.marks) > 0
}

// SetSketchAttached attaches or detaches the sketch chip from the agent composer without erasing marks.
func (e *Editor) SetSketchAttached(attached bool) {
	if e == nil {
		return
	}
	want := attached && len(e.sketch.marks) > 0
	if e.sketch.attached == want {
		return
	}
	e.sketch.attached = want
	if !want {
		e.sketch.previewOpen = false
	}
	e.notify()
}

// SketchPreviewOpen reports whether the "Sent with this request" sketch preview card is open.
func (e *Editor) SketchPreviewOpen() bool {
	return e != nil && e.sketch.previewOpen && e.SketchAttached()
}

// SetSketchPreviewOpen opens or closes the "Sent with this request" preview card.
func (e *Editor) SetSketchPreviewOpen(open bool) {
	if e == nil {
		return
	}
	want := open && e.SketchAttached()
	if e.sketch.previewOpen == want {
		return
	}
	e.sketch.previewOpen = want
	e.notify()
}

// ToggleSketchPreview toggles the "Sent with this request" preview card.
func (e *Editor) ToggleSketchPreview() {
	if e == nil {
		return
	}
	e.SetSketchPreviewOpen(!e.SketchPreviewOpen())
}

// SketchPromptDraft returns the current text in the Sketch/Agent composer field.
func (e *Editor) SketchPromptDraft() string {
	if e == nil {
		return ""
	}
	return e.sketch.promptDraft
}

// SetSketchPromptDraft updates the Sketch/Agent composer draft text.
func (e *Editor) SetSketchPromptDraft(text string) {
	if e == nil || e.sketch.promptDraft == text {
		return
	}
	e.sketch.promptDraft = text
	e.notify()
}

// SketchChipLabel returns the composer context chip label, e.g. "Sketch · 3 marks".
func (e *Editor) SketchChipLabel() string {
	n := e.SketchMarkCount()
	if n == 1 {
		return "Sketch · 1 mark"
	}
	return fmt.Sprintf("Sketch · %d marks", n)
}

// SketchBarCountLabel returns the sketch toolbar count text, e.g. "3 marks not saved".
func (e *Editor) SketchBarCountLabel() string {
	n := e.SketchMarkCount()
	if n == 1 {
		return "1 mark not saved"
	}
	return fmt.Sprintf("%d marks not saved", n)
}

// SketchStatusSceneCoords returns the live scene coordinates shown in the status bar while sketching
// (the pointer position in scene space when hovering the viewport, otherwise the selected/last mark or view center).
func (e *Editor) SketchStatusSceneCoords() (float32, float32) {
	if e == nil {
		return 0, 0
	}
	if e.hasPointer2D {
		return e.CanvasToWorld(e.pointer2D.X, e.pointer2D.Y)
	}
	if sel := e.SelectedSketchMark(); sel > 0 {
		p := e.sketch.marks[sel-1].AnchorScenePoint()
		return p.X, p.Y
	}
	if n := len(e.sketch.marks); n > 0 {
		p := e.sketch.marks[n-1].AnchorScenePoint()
		return p.X, p.Y
	}
	v := e.View2D()
	return v.X, v.Y
}

// SketchDragging reports whether a sketch mark drag is currently in progress on the viewport.
func (e *Editor) SketchDragging() bool {
	return e != nil && e.sketch.draft != nil
}

func (e *Editor) appendSketchMark(m SketchMark) SketchMark {
	e.pushSketchUndo()
	e.sketch.nextSeq++
	if m.ID == "" {
		m.ID = fmt.Sprintf("mark_%d", e.sketch.nextSeq)
	}
	m.Label = clampSketchRunes(m.Label, maxSketchLabelRunes)
	m.Number = len(e.sketch.marks) + 1
	if len(e.sketch.marks) >= maxSketchMarks {
		e.sketch.marks = e.sketch.marks[1:]
	}
	e.sketch.marks = append(e.sketch.marks, m.Clone())
	e.renumberSketchMarks()
	added := e.sketch.marks[len(e.sketch.marks)-1]
	e.sketch.selectedMark = added.Number
	e.sketch.attached = true
	e.sketch.marksHidden = false
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.notify()
	return added.Clone()
}

// AddSketchBox adds a numbered Box mark strictly in scene coordinates without mutating SceneDoc or History.
func (e *Editor) AddSketchBox(x, y, width, height float32, label string) SketchMark {
	if e == nil || e.playing {
		return SketchMark{}
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(width) || !isFiniteFloat32(height) {
		return SketchMark{}
	}
	if width < 0 {
		x += width
		width = -width
	}
	if height < 0 {
		y += height
		height = -height
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) || !isFiniteFloat32(width) || !isFiniteFloat32(height) {
		return SketchMark{}
	}
	r := SketchRect{X: x, Y: y, Width: width, Height: height}
	return e.appendSketchMark(SketchMark{
		Kind:  SketchToolBox,
		Label: clampSketchRunes(label, maxSketchLabelRunes),
		Rect:  &r,
	})
}

// AddSketchArrow adds a numbered Arrow mark strictly in scene coordinates without mutating SceneDoc or History.
func (e *Editor) AddSketchArrow(x0, y0, x1, y1 float32, label string) SketchMark {
	if e == nil || e.playing {
		return SketchMark{}
	}
	if !isFiniteFloat32(x0) || !isFiniteFloat32(y0) || !isFiniteFloat32(x1) || !isFiniteFloat32(y1) {
		return SketchMark{}
	}
	s := SketchPoint{X: x0, Y: y0}
	t := SketchPoint{X: x1, Y: y1}
	return e.appendSketchMark(SketchMark{
		Kind:  SketchToolArrow,
		Label: clampSketchRunes(label, maxSketchLabelRunes),
		Start: &s,
		End:   &t,
	})
}

// AddSketchPen adds a numbered Pen polyline mark strictly in scene coordinates without mutating SceneDoc or History.
func (e *Editor) AddSketchPen(points []SketchPoint, label string) SketchMark {
	if e == nil || e.playing || len(points) == 0 {
		return SketchMark{}
	}
	clean := make([]SketchPoint, 0, min(len(points), maxSketchPenPoints))
	for _, pt := range points {
		if isFiniteFloat32(pt.X) && isFiniteFloat32(pt.Y) {
			clean = append(clean, pt)
			if len(clean) >= maxSketchPenPoints {
				break
			}
		}
	}
	if len(clean) == 0 {
		return SketchMark{}
	}
	return e.appendSketchMark(SketchMark{
		Kind:   SketchToolPen,
		Label:  clampSketchRunes(label, maxSketchLabelRunes),
		Points: clean,
	})
}

// AddSketchText adds a numbered Text mark strictly in scene coordinates without mutating SceneDoc or History.
func (e *Editor) AddSketchText(x, y float32, label string) SketchMark {
	if e == nil || e.playing {
		return SketchMark{}
	}
	if !isFiniteFloat32(x) || !isFiniteFloat32(y) {
		return SketchMark{}
	}
	at := SketchPoint{X: x, Y: y}
	return e.appendSketchMark(SketchMark{
		Kind:  SketchToolText,
		Label: clampSketchRunes(label, maxSketchLabelRunes),
		At:    &at,
	})
}

// AddSketchMarkAtCursor places a new mark using the active sketch tool at the current pointer or view center
// in scene coordinates, providing a full keyboard path for creating marks without pointer dragging.
func (e *Editor) AddSketchMarkAtCursor(label string) SketchMark {
	if e == nil || e.playing {
		return SketchMark{}
	}
	tool := e.ensureSketchTool()
	wx, wy := e.SketchStatusSceneCoords()
	switch tool {
	case SketchToolBox:
		return e.AddSketchBox(wx-sketchDefaultBoxW*0.5, wy-sketchDefaultBoxH*0.5, sketchDefaultBoxW, sketchDefaultBoxH, label)
	case SketchToolArrow:
		return e.AddSketchArrow(wx-sketchDefaultArrowDX*0.5, wy-sketchDefaultArrowDY*0.5, wx+sketchDefaultArrowDX*0.5, wy+sketchDefaultArrowDY*0.5, label)
	case SketchToolPen:
		return e.AddSketchPen([]SketchPoint{
			{X: wx - 40, Y: wy},
			{X: wx, Y: wy - 12},
			{X: wx + 40, Y: wy},
		}, label)
	case SketchToolText:
		return e.AddSketchText(wx, wy, label)
	default:
		return e.AddSketchBox(wx-sketchDefaultBoxW*0.5, wy-sketchDefaultBoxH*0.5, sketchDefaultBoxW, sketchDefaultBoxH, label)
	}
}

// SetSketchMarkLabel updates the optional text label on the 1-based sketch mark.
func (e *Editor) SetSketchMarkLabel(number int, label string) bool {
	if e == nil || e.playing || number < 1 || number > len(e.sketch.marks) {
		return false
	}
	clean := clampSketchRunes(label, maxSketchLabelRunes)
	if e.sketch.marks[number-1].Label == clean {
		return true
	}
	e.sketch.marks[number-1].Label = clean
	e.notify()
	return true
}

// NudgeSelectedSketchMark translates the selected sketch mark by (dx, dy) in scene coordinates.
func (e *Editor) NudgeSelectedSketchMark(dx, dy float32) bool {
	if e == nil || e.playing || !isFiniteFloat32(dx) || !isFiniteFloat32(dy) || (dx == 0 && dy == 0) {
		return false
	}
	sel := e.SelectedSketchMark()
	if sel == 0 {
		return false
	}
	m := &e.sketch.marks[sel-1]
	switch m.Kind {
	case SketchToolBox:
		if m.Rect != nil {
			m.Rect.X += dx
			m.Rect.Y += dy
		}
	case SketchToolArrow:
		if m.Start != nil {
			m.Start.X += dx
			m.Start.Y += dy
		}
		if m.End != nil {
			m.End.X += dx
			m.End.Y += dy
		}
	case SketchToolPen:
		for i := range m.Points {
			m.Points[i].X += dx
			m.Points[i].Y += dy
		}
	case SketchToolText:
		if m.At != nil {
			m.At.X += dx
			m.At.Y += dy
		}
	}
	e.notify()
	return true
}

// DeleteSketchMark removes the 1-based mark and renumbers remaining marks sequentially.
func (e *Editor) DeleteSketchMark(number int) bool {
	if e == nil || e.playing || number < 1 || number > len(e.sketch.marks) {
		return false
	}
	e.pushSketchUndo()
	e.sketch.marks = slices.Delete(e.sketch.marks, number-1, number)
	e.renumberSketchMarks()
	e.notify()
	return true
}

// CanUndoSketchMark reports whether the sketch layer has a previous mark state to undo.
func (e *Editor) CanUndoSketchMark() bool {
	return e != nil && len(e.sketch.undoStack) > 0
}

// UndoLastSketchMark restores the previous sketch mark list without touching SceneDoc or Editor.History.
func (e *Editor) UndoLastSketchMark() bool {
	if e == nil || e.playing || len(e.sketch.undoStack) == 0 {
		return false
	}
	prev := e.sketch.undoStack[len(e.sketch.undoStack)-1]
	e.sketch.undoStack = e.sketch.undoStack[:len(e.sketch.undoStack)-1]
	e.sketch.marks = prev
	e.renumberSketchMarks()
	e.notify()
	return true
}

// ClearSketch clears all sketch marks, any pending reading card, and any active confirmation hold.
// It never modifies SceneDoc or Editor.History.
func (e *Editor) ClearSketch() {
	if e == nil || e.playing {
		return
	}
	if len(e.sketch.marks) > 0 {
		e.pushSketchUndo()
	}
	e.sketch.marks = nil
	e.sketch.selectedMark = 0
	e.sketch.attached = false
	e.sketch.previewOpen = false
	e.sketch.draft = nil
	e.sketch.hold = SketchHoldNone
	e.sketch.reading = nil
	e.sketch.lastRequest = nil
	e.Status = "Sketch layer cleared"
	e.notify()
}

// HitTestSketchMark returns the 1-based mark number whose badge or geometry lies under (canvasX, canvasY),
// or 0 if none is hit.
func (e *Editor) HitTestSketchMark(canvasX, canvasY float32) int {
	if e == nil || !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) || len(e.sketch.marks) == 0 {
		return 0
	}
	const badgeHitR = sketchBadgeRadiusPt + 5
	for i := len(e.sketch.marks) - 1; i >= 0; i-- {
		m := e.sketch.marks[i]
		bx, by := e.sketchBadgeCanvasPos(m)
		if math.Hypot(float64(canvasX-bx), float64(canvasY-by)) <= badgeHitR {
			return m.Number
		}
	}
	return 0
}

func (e *Editor) sketchBadgeCanvasPos(m SketchMark) (float32, float32) {
	if m.Kind == SketchToolPen && len(m.Points) > 0 {
		minX, minY := e.WorldToCanvas(m.Points[0].X, m.Points[0].Y)
		for _, pt := range m.Points[1:] {
			cx, cy := e.WorldToCanvas(pt.X, pt.Y)
			if cx < minX {
				minX = cx
			}
			if cy < minY {
				minY = cy
			}
		}
		return minX - 6, minY - 14
	}
	a := m.AnchorScenePoint()
	cx, cy := e.WorldToCanvas(a.X, a.Y)
	return cx - 10, cy - 12
}

// BeginSketchDrag starts drawing a sketch mark from canvas point (canvasX, canvasY) using the active sketch tool.
// Coordinates are converted immediately via CanvasToWorld so all stored geometry is in scene coordinates.
func (e *Editor) BeginSketchDrag(canvasX, canvasY float32) bool {
	if e == nil || !e.SketchActive() || !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	tool := e.ensureSketchTool()
	hit := e.HitTestSketchMark(canvasX, canvasY)
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	if !isFiniteFloat32(wx) || !isFiniteFloat32(wy) {
		return false
	}
	start := SketchPoint{X: wx, Y: wy}
	e.sketch.draft = &sketchDraftDrag{
		tool:       tool,
		startWorld: start,
		endWorld:   start,
		points:     []SketchPoint{start},
		hitMark:    hit,
	}
	e.notify()
	return true
}

// UpdateSketchDrag extends the in-progress sketch mark to canvas point (canvasX, canvasY).
func (e *Editor) UpdateSketchDrag(canvasX, canvasY float32) bool {
	if e == nil || e.sketch.draft == nil || !isFiniteFloat32(canvasX) || !isFiniteFloat32(canvasY) {
		return false
	}
	dr := e.sketch.draft
	wx, wy := e.CanvasToWorld(canvasX, canvasY)
	if !isFiniteFloat32(wx) || !isFiniteFloat32(wy) {
		return false
	}
	pt := SketchPoint{X: wx, Y: wy}
	sx, sy := e.WorldToCanvas(dr.startWorld.X, dr.startWorld.Y)
	if math.Hypot(float64(canvasX-sx), float64(canvasY-sy)) >= sketchDragMinDistPt {
		dr.moved = true
	}
	dr.endWorld = pt
	if dr.tool == SketchToolPen && len(dr.points) < maxSketchPenPoints {
		last := dr.points[len(dr.points)-1]
		lx, ly := e.WorldToCanvas(last.X, last.Y)
		if math.Hypot(float64(canvasX-lx), float64(canvasY-ly)) >= sketchPenStepMinPt {
			dr.points = append(dr.points, pt)
		}
	}
	e.notify()
	return true
}

// EndSketchDrag commits the in-progress sketch drag as a numbered mark in scene coordinates,
// or selects the clicked badge when no drag occurred.
func (e *Editor) EndSketchDrag() (SketchMark, bool) {
	if e == nil || e.sketch.draft == nil {
		return SketchMark{}, false
	}
	dr := e.sketch.draft
	e.sketch.draft = nil

	if !dr.moved && dr.hitMark > 0 {
		e.sketch.selectedMark = dr.hitMark
		e.notify()
		return e.sketch.marks[dr.hitMark-1].Clone(), false
	}

	switch dr.tool {
	case SketchToolText:
		return e.AddSketchText(dr.startWorld.X, dr.startWorld.Y, ""), true
	case SketchToolBox:
		if !dr.moved {
			e.notify()
			return SketchMark{}, false
		}
		x0 := min(dr.startWorld.X, dr.endWorld.X)
		y0 := min(dr.startWorld.Y, dr.endWorld.Y)
		w := float32(math.Abs(float64(dr.endWorld.X - dr.startWorld.X)))
		h := float32(math.Abs(float64(dr.endWorld.Y - dr.startWorld.Y)))
		return e.AddSketchBox(x0, y0, w, h, ""), true
	case SketchToolArrow:
		if !dr.moved {
			e.notify()
			return SketchMark{}, false
		}
		return e.AddSketchArrow(dr.startWorld.X, dr.startWorld.Y, dr.endWorld.X, dr.endWorld.Y, ""), true
	case SketchToolPen:
		if !dr.moved {
			e.notify()
			return SketchMark{}, false
		}
		pts := dr.points
		if len(pts) == 0 || pts[len(pts)-1] != dr.endWorld {
			pts = append(pts, dr.endWorld)
		}
		return e.AddSketchPen(pts, ""), true
	default:
		e.notify()
		return SketchMark{}, false
	}
}

// CancelSketchDrag cancels an in-progress sketch mark drag without creating a mark.
func (e *Editor) CancelSketchDrag() {
	if e == nil || e.sketch.draft == nil {
		return
	}
	e.sketch.draft = nil
	e.notify()
}

// RenderSketchViewportPNG renders the 2D viewport with the numbered sketch marks overlaid
// as a PNG image at scale pixels per point (defaulting to 1 when scale <= 0).
func (e *Editor) RenderSketchViewportPNG(scale float32) ([]byte, int, int, error) {
	if e == nil {
		return nil, 0, 0, errors.New("editor: nil editor")
	}
	if e.ViewMode != ViewMode2D {
		return nil, 0, 0, errors.New("editor: viewport sketch PNG requires 2D view mode")
	}
	if scale <= 0 {
		scale = 1
	}
	if !(scale >= 0.25 && scale <= 4) || !isFiniteFloat32(scale) {
		return nil, 0, 0, fmt.Errorf("editor: sketch render scale %v must be between 0.25 and 4", scale)
	}
	w, h := e.CanvasSize()
	if !isFiniteFloat32(w) || !isFiniteFloat32(h) || w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("editor: invalid viewport canvas size %vx%v", w, h)
	}
	// Cap rendered dimensions to prevent OOM on ultra-high-DPI / oversized viewports.
	pw := max(int(math.Round(float64(w*scale))), 1)
	ph := max(int(math.Round(float64(h*scale))), 1)
	if pw > maxSketchPNGDim || ph > maxSketchPNGDim || int64(pw)*int64(ph) > maxSketchPNGPixels {
		fit := math.Min(float64(maxSketchPNGDim)/float64(w), float64(maxSketchPNGDim)/float64(h))
		areaFit := math.Sqrt(float64(maxSketchPNGPixels) / (float64(w) * float64(h)))
		scale = float32(math.Min(float64(scale), math.Min(fit, areaFit)))
		if scale < 0.25 {
			scale = 0.25
		}
		pw = min(max(int(math.Round(float64(w*scale))), 1), maxSketchPNGDim)
		ph = min(max(int(math.Round(float64(h*scale))), 1), maxSketchPNGDim)
	}
	var d core.Drawing
	d.Scale(scale, scale)
	size := core.Size{Width: core.Points(w), Height: core.Points(h)}

	prevForce := e.sketch.forcePNGMarks
	e.sketch.forcePNGMarks = true
	e.DrawViewport(&d, size)
	e.sketch.forcePNGMarks = prevForce

	res, err := gamemcp.RenderPNG(d.Ops, pw, ph, e.SpecPalette(nil).Viewport)
	if err != nil {
		return nil, 0, 0, err
	}
	pngBytes := res.PNG
	if rendered, ok := renderSketchOverlayTextPNG(pngBytes, d.Ops); ok {
		pngBytes = rendered
	}
	return pngBytes, res.Width, res.Height, nil
}

// SendSketch packages the active sketch marks (in exact scene coordinates), the rendered viewport PNG
// with numbered marks overlaid, and prompt into a SketchAgentRequest, arms the confirmation hold gate
// so scene_edit refuses mutations until the person confirms, and invokes Editor.OnSendSketch if set.
func (e *Editor) SendSketch(prompt string) (SketchAgentRequest, error) {
	if e == nil {
		return SketchAgentRequest{}, errors.New("editor: nil editor")
	}
	if e.playing {
		return SketchAgentRequest{}, ErrPlaying
	}
	if len(e.sketch.marks) == 0 || !e.sketch.attached {
		return SketchAgentRequest{}, ErrSketchEmpty
	}
	if strings.TrimSpace(prompt) == "" {
		prompt = strings.TrimSpace(e.sketch.promptDraft)
	} else {
		prompt = strings.TrimSpace(prompt)
	}
	pngBytes, pw, ph, err := e.RenderSketchViewportPNG(1)
	if err != nil {
		return SketchAgentRequest{}, err
	}
	sceneName := e.ScenePath
	if sceneName == "" && e.Doc != nil {
		sceneName = e.Doc.Name
	}
	if sceneName == "" {
		sceneName = "the open scene"
	}
	req := SketchAgentRequest{
		Scene:     sceneName,
		ChipLabel: e.SketchChipLabel(),
		Prompt:    prompt,
		View:      e.View2D(),
		Marks:     e.SketchMarks(),
		PNG:       pngBytes,
		Width:     pw,
		Height:    ph,
	}
	stored := req
	stored.Marks = cloneSketchMarks(req.Marks)
	stored.PNG = slices.Clone(req.PNG)
	e.sketch.lastRequest = &stored
	e.sketch.active = false
	e.sketch.draft = nil
	e.sketch.previewOpen = false
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.sketch.hold = SketchHoldPending
	e.sketch.reading = nil
	e.sketch.promptDraft = ""
	e.Status = fmt.Sprintf("Sent %s · waiting for agent reading", req.ChipLabel)
	e.notify()

	if e.OnSendSketch != nil {
		e.OnSendSketch(req)
	}
	return req, nil
}

// LastSketchRequest returns the most recent SketchAgentRequest sent via SendSketch.
func (e *Editor) LastSketchRequest() (SketchAgentRequest, bool) {
	if e == nil || e.sketch.lastRequest == nil {
		return SketchAgentRequest{}, false
	}
	req := *e.sketch.lastRequest
	req.Marks = cloneSketchMarks(e.sketch.lastRequest.Marks)
	req.PNG = slices.Clone(e.sketch.lastRequest.PNG)
	return req, true
}

// SketchHoldState returns the current confirmation hold state of the sketch layer.
func (e *Editor) SketchHoldState() SketchHoldState {
	if e == nil || e.sketch.hold == "" {
		return SketchHoldNone
	}
	return e.sketch.hold
}

// SketchHoldActive reports whether mutating scene MCP edits must hold until the user confirms the sent sketch.
func (e *Editor) SketchHoldActive() bool {
	if e == nil {
		return false
	}
	return e.sketch.hold == SketchHoldPending || e.sketch.hold == SketchHoldReading
}

// SketchApproved reports whether the user clicked Go ahead on the active sketch and allowed agent edits.
func (e *Editor) SketchApproved() bool {
	return e != nil && e.sketch.hold == SketchHoldApproved
}

// SketchReading returns a copy of the agent's posted sketch reading card, or nil when none is open.
func (e *Editor) SketchReading() *SketchReading {
	if e == nil {
		return nil
	}
	return e.sketch.reading.Clone()
}

// PostSketchReading records the agent's restatement of the active sketch marks and assumptions,
// opening the reading card in the Sketch/Agent panel while keeping the scene_edit hold gate active
// until the user answers Go ahead, Correct a mark, or replies in words.
func (e *Editor) PostSketchReading(reading SketchReading) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(e.sketch.marks) == 0 {
		return ErrSketchEmpty
	}
	if len(reading.Items) == 0 {
		return errors.New("editor: sketch reading must include at least one item")
	}
	if len(reading.Items) > maxSketchReadingItems {
		return fmt.Errorf("editor: sketch reading has %d items, max %d", len(reading.Items), maxSketchReadingItems)
	}
	if len(reading.Assumptions) > maxSketchReadingAssumptions {
		return fmt.Errorf("editor: sketch reading has %d assumptions, max %d", len(reading.Assumptions), maxSketchReadingAssumptions)
	}
	summary := clampSketchRunes(reading.Summary, maxSketchReadingTextRunes)
	if summary == "" {
		summary = "I read your sketch as:"
	}
	items := make([]SketchReadingItem, len(reading.Items))
	for i, it := range reading.Items {
		if it.Number < 1 || it.Number > len(e.sketch.marks) {
			return fmt.Errorf("editor: sketch reading item number %d out of range (1..%d)", it.Number, len(e.sketch.marks))
		}
		it.Text = clampSketchRunes(it.Text, maxSketchReadingTextRunes)
		if it.Text == "" {
			return fmt.Errorf("editor: sketch reading item %d has empty text", it.Number)
		}
		it.Mark = clampSketchRunes(it.Mark, maxSketchLabelRunes)
		if it.Mark == "" {
			it.Mark = e.sketch.marks[it.Number-1].ReadingHeading()
		}
		items[i] = it
	}
	assumptions := make([]string, 0, len(reading.Assumptions))
	for _, a := range reading.Assumptions {
		if s := clampSketchRunes(a, maxSketchReadingTextRunes); s != "" {
			assumptions = append(assumptions, s)
		}
	}
	now := time.Now()
	if e.Now != nil {
		now = e.Now()
	}
	e.sketch.reading = &SketchReading{
		Summary:     summary,
		Items:       items,
		Assumptions: assumptions,
		PostedAt:    now,
	}
	e.sketch.hold = SketchHoldReading
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.Status = "Agent posted sketch reading · choose Go ahead or Correct a mark"
	e.notify()
	return nil
}

// ApproveSketchReading answers "Go ahead" on the active sketch reading, releasing the hold gate
// so scene_edit can proceed and preparing a pre-edit review snapshot for Keep / Revert.
func (e *Editor) ApproveSketchReading() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(e.sketch.marks) == 0 {
		return ErrSketchEmpty
	}
	baseYAML, err := e.SaveYAML()
	if err != nil {
		return err
	}
	histStart := 0
	if e.History != nil {
		histStart = e.History.cursor
	}
	e.sketch.hold = SketchHoldApproved
	e.sketch.active = false
	e.sketch.draft = nil
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.sketch.review = &sketchReviewState{
		open:           false,
		overlayVisible: true,
		baseYAML:       baseYAML,
		historyStart:   histStart,
	}
	e.Status = "Sketch approved · agent may edit the scene"
	e.notify()

	if e.OnReplySketchReading != nil {
		e.OnReplySketchReading(SketchReadingReply{Action: "approve"})
	}
	return nil
}

// CorrectSketchMark answers "Correct a mark" on the active sketch reading, reopening sketch mode
// with that exact mark selected (defaulting to mark 1 if number <= 0) while keeping the hold gate active.
func (e *Editor) CorrectSketchMark(number int) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if len(e.sketch.marks) == 0 {
		return ErrSketchEmpty
	}
	if number <= 0 {
		number = 1
	}
	if number > len(e.sketch.marks) {
		return fmt.Errorf("editor: sketch mark %d out of range (1..%d)", number, len(e.sketch.marks))
	}
	e.sketch.selectedMark = number
	e.sketch.tool = e.sketch.marks[number-1].Kind
	e.sketch.active = true
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.sketch.marksHidden = false
	e.sketch.attached = true
	e.sketch.hold = SketchHoldPending
	e.Status = fmt.Sprintf("Correcting mark %d · edit or redraw, then send again", number)
	e.notify()

	if e.OnReplySketchReading != nil {
		e.OnReplySketchReading(SketchReadingReply{Action: "correct", MarkNumber: number})
	}
	return nil
}

// ReplyToSketchReading answers an agent sketch reading in words. It forwards the message through
// Editor.OnReplySketchReading and releases the hold gate so the agent can apply the clarified edits.
func (e *Editor) ReplyToSketchReading(message string) error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		return errors.New("editor: reply message cannot be empty")
	}
	if len(e.sketch.marks) == 0 {
		return ErrSketchEmpty
	}
	baseYAML, err := e.SaveYAML()
	if err != nil {
		return err
	}
	histStart := 0
	if e.History != nil {
		histStart = e.History.cursor
	}
	e.sketch.hold = SketchHoldApproved
	e.sketch.promptDraft = ""
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	if e.sketch.review == nil {
		e.sketch.review = &sketchReviewState{
			open:           false,
			overlayVisible: true,
			baseYAML:       baseYAML,
			historyStart:   histStart,
		}
	}
	e.Status = "Replied to agent reading"
	e.notify()

	if e.OnReplySketchReading != nil {
		e.OnReplySketchReading(SketchReadingReply{Action: "reply", Message: msg})
	}
	return nil
}

// SketchReviewOpen reports whether a post-sketch agent edit review is open.
func (e *Editor) SketchReviewOpen() bool {
	return e != nil && e.sketch.review != nil && e.sketch.review.open
}

// SketchReviewOverlayVisible reports whether the Sketch overlay toggle is enabled during review.
func (e *Editor) SketchReviewOverlayVisible() bool {
	if e == nil {
		return false
	}
	if e.sketch.review != nil {
		return e.sketch.review.overlayVisible
	}
	return !e.sketch.marksHidden
}

// SetSketchReviewOverlayVisible toggles the Sketch overlay in review (laying the faint dotted sketch over the result).
func (e *Editor) SetSketchReviewOverlayVisible(on bool) {
	if e == nil {
		return
	}
	if e.sketch.review == nil {
		e.sketch.marksHidden = !on
		e.notify()
		return
	}
	if e.sketch.review.overlayVisible == on {
		return
	}
	e.sketch.review.overlayVisible = on
	e.notify()
}

// ToggleSketchReviewOverlay flips the Sketch overlay visibility during review.
func (e *Editor) ToggleSketchReviewOverlay() {
	if e == nil {
		return
	}
	e.SetSketchReviewOverlayVisible(!e.SketchReviewOverlayVisible())
}

// SketchReviewChanges returns the changes recorded in the active post-sketch review.
func (e *Editor) SketchReviewChanges() []SketchReviewChange {
	if e == nil || e.sketch.review == nil {
		return nil
	}
	return slices.Clone(e.sketch.review.changes)
}

// RecordSketchReviewEdits records scene edits performed by the agent after the user approved a sketch,
// opening the review card with the Sketch overlay toggle enabled so the person can Keep or Revert.
func (e *Editor) RecordSketchReviewEdits(label string, addedIDs, changedIDs, removedIDs []string) {
	if e == nil || !e.SketchApproved() {
		return
	}
	if e.sketch.review == nil {
		e.sketch.review = &sketchReviewState{overlayVisible: true}
	}
	rv := e.sketch.review
	rv.open = true
	if label != "" {
		rv.undoLabels = append(rv.undoLabels, label)
	}
	addChange := func(kind SketchReviewChangeKind, id, detail string) {
		if id == "" {
			return
		}
		for i := range rv.changes {
			if rv.changes[i].ID == id && rv.changes[i].Kind == kind {
				if detail != "" {
					rv.changes[i].Detail = detail
				}
				return
			}
		}
		rv.changes = append(rv.changes, SketchReviewChange{Kind: kind, ID: id, Detail: detail})
	}
	for _, id := range addedIDs {
		addChange(SketchChangeAdded, id, label)
	}
	for _, id := range changedIDs {
		addChange(SketchChangeChanged, id, label)
	}
	for _, id := range removedIDs {
		addChange(SketchChangeRemoved, id, label)
	}
	if len(rv.changes) == 0 && label != "" {
		rv.changes = append(rv.changes, SketchReviewChange{Kind: SketchChangeChanged, ID: label})
	}
	e.sketch.panelOpen = true
	e.sketch.panelDismissed = false
	e.notify()
}

// KeepSketchReview keeps the agent's scene edits from the active sketch review, closes the review,
// and clears the sketch layer (marks, reading card, and hold).
func (e *Editor) KeepSketchReview() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.SketchReviewOpen() {
		return ErrNoSketchReview
	}
	count := len(e.sketch.review.changes)
	e.sketch.review = nil
	e.clearSketchOnReviewKeep()
	e.Status = fmt.Sprintf("Kept %d agent changes · sketch cleared", count)
	e.notify()
	return nil
}

// RevertSketchReview reverts the agent's scene edits back to the pre-approval scene snapshot,
// closes the review, and KEEPS the sketch marks intact so the user can send them again.
func (e *Editor) RevertSketchReview() error {
	if e == nil {
		return errors.New("editor: nil editor")
	}
	if e.playing {
		return ErrPlaying
	}
	if !e.SketchReviewOpen() {
		return ErrNoSketchReview
	}
	rv := e.sketch.review
	e.sketch.review = nil

	if len(rv.baseYAML) > 0 {
		doc, err := sceneformat.ParseWith(rv.baseYAML, sceneformat.ParseOptions{Components: e.ComponentRegistry()})
		if err != nil {
			return fmt.Errorf("editor: revert sketch review: %w", err)
		}
		e.Doc = doc
		if e.History != nil && rv.historyStart >= 0 {
			cut := min(rv.historyStart, len(e.History.commands))
			clear(e.History.commands[cut:])
			e.History.commands = e.History.commands[:cut]
			if e.History.cursor > cut {
				e.History.cursor = cut
			}
		}
		e.Rebuild()
		// Drop any selection IDs that pointed at agent-added objects removed by Revert.
		if e.SelectedID != "" {
			if o, _, _ := e.FindObject(e.SelectedID); o == nil {
				e.SelectedID = ""
			}
		}
		if len(e.selectedIDs) > 0 {
			kept := make([]string, 0, len(e.selectedIDs))
			for _, id := range e.selectedIDs {
				if o, _, _ := e.FindObject(id); o != nil {
					kept = append(kept, id)
				}
			}
			e.selectedIDs = kept
			if e.SelectedID == "" && len(kept) > 0 {
				e.SelectedID = kept[len(kept)-1]
			}
		}
	}
	e.keepSketchOnReviewRevert()
	e.Status = "Reverted agent changes · sketch kept to send again"
	e.notify()
	return nil
}

// clearSketchOnReviewKeep clears the sketch layer when any agent review (sketch review or tile review) is Kept.
func (e *Editor) clearSketchOnReviewKeep() {
	if e == nil {
		return
	}
	e.sketch.active = false
	e.sketch.marks = nil
	e.sketch.undoStack = nil
	e.sketch.selectedMark = 0
	e.sketch.attached = false
	e.sketch.previewOpen = false
	e.sketch.draft = nil
	e.sketch.hold = SketchHoldNone
	e.sketch.reading = nil
	e.sketch.lastRequest = nil
	e.sketch.panelOpen = false
	e.sketch.panelDismissed = false
}

// keepSketchOnReviewRevert preserves the sketch marks ready to send again when an agent review is Reverted.
func (e *Editor) keepSketchOnReviewRevert() {
	if e == nil {
		return
	}
	e.sketch.active = false
	e.sketch.draft = nil
	e.sketch.hold = SketchHoldNone
	e.sketch.reading = nil
	if len(e.sketch.marks) > 0 {
		e.sketch.attached = true
		e.sketch.marksHidden = false
		e.sketch.panelOpen = true
		e.sketch.panelDismissed = false
	}
}

// drawSketchOverlay renders the viewport sketch layer (strokes, numbered badges, labels, in-progress drag preview,
// and review comparison outlines) in canvas coordinates projected from scene coordinates via WorldToCanvas.
func (e *Editor) drawSketchOverlay(d *core.Drawing) {
	if e == nil || d == nil || e.playing || e.ViewMode != ViewMode2D {
		return
	}
	spec := e.SpecPalette(nil)

	inReview := e.SketchReviewOpen() || e.TileReviewOpen()
	if e.SketchReviewOpen() {
		e.drawSketchReviewChangeOutlines(d, spec)
	}

	hasMarks := len(e.sketch.marks) > 0 || e.sketch.draft != nil
	if !hasMarks {
		return
	}
	if !e.sketch.forcePNGMarks {
		if inReview {
			if !e.SketchReviewOverlayVisible() {
				return
			}
		} else if e.sketch.marksHidden {
			return
		}
	}

	reviewDotted := inReview && !e.sketch.forcePNGMarks && !e.sketch.active
	alpha := float32(1.0)
	if !e.sketch.active && !e.sketch.forcePNGMarks {
		if reviewDotted {
			alpha = 0.45
		} else {
			alpha = 0.65
		}
	}

	d.Save()
	for _, m := range e.sketch.marks {
		selected := e.sketch.active && m.Number == e.sketch.selectedMark && !e.sketch.forcePNGMarks
		e.drawSketchMark(d, spec, m, selected, reviewDotted, alpha)
	}
	if e.sketch.draft != nil && !e.sketch.forcePNGMarks {
		e.drawSketchDraft(d, spec, e.sketch.draft)
	}
	d.Restore()
}

func (e *Editor) drawSketchReviewChangeOutlines(d *core.Drawing, spec SpecPalette) {
	scene := e.ActiveScene()
	if scene == nil || e.sketch.review == nil {
		return
	}
	d.Save()
	d.SetLineWidth(2)
	d.SetLineDash(6, 4)
	for _, ch := range e.sketch.review.changes {
		bo := scene.Object(ch.ID)
		if bo == nil || bo.Doc == nil {
			continue
		}
		space := sceneformat.SpaceWorld
		var px, py float32 = 1, 1
		if layer := scene.Layer(bo.Layer); layer != nil {
			space, px, py = layer.Space, layer.ParallaxX, layer.ParallaxY
		}
		m := e.layerAffineFor(space, px, py).Mul(bo.World)
		rx, ry, rw, rh := objectOutline(bo)
		pts := [4]struct{ x, y float32 }{
			{rx - 4, ry - 4},
			{rx + rw + 4, ry - 4},
			{rx + rw + 4, ry + rh + 4},
			{rx - 4, ry + rh + 4},
		}
		var cpts [4]struct{ x, y float32 }
		for i, pt := range pts {
			v := m.TransformPoint(math2d.Vec2{X: pt.x, Y: pt.y})
			cpts[i] = struct{ x, y float32 }{v.X, v.Y}
		}
		col := spec.Changed.Edge
		fill := ui.Alpha(spec.Changed.Fg, 0.14)
		if ch.Kind == SketchChangeAdded {
			col = spec.Added.Edge
			fill = ui.Alpha(spec.Added.Fg, 0.16)
		} else if ch.Kind == SketchChangeRemoved {
			col = spec.Removed.Edge
			fill = ui.Alpha(spec.Removed.Fg, 0.16)
		}
		d.SetFillColor(fill)
		d.BeginPath().MoveTo(cpts[0].x, cpts[0].y).LineTo(cpts[1].x, cpts[1].y).LineTo(cpts[2].x, cpts[2].y).LineTo(cpts[3].x, cpts[3].y).ClosePath().Fill()
		d.SetStrokeColor(col)
		d.BeginPath().MoveTo(cpts[0].x, cpts[0].y).LineTo(cpts[1].x, cpts[1].y).LineTo(cpts[2].x, cpts[2].y).LineTo(cpts[3].x, cpts[3].y).ClosePath().Stroke()
	}
	d.SetLineDash()
	d.Restore()
}

func (e *Editor) drawSketchDraft(d *core.Drawing, spec SpecPalette, dr *sketchDraftDrag) {
	if !dr.moved {
		return
	}
	preview := SketchMark{Number: len(e.sketch.marks) + 1, Kind: dr.tool}
	switch dr.tool {
	case SketchToolBox:
		r := SketchRect{
			X:      min(dr.startWorld.X, dr.endWorld.X),
			Y:      min(dr.startWorld.Y, dr.endWorld.Y),
			Width:  float32(math.Abs(float64(dr.endWorld.X - dr.startWorld.X))),
			Height: float32(math.Abs(float64(dr.endWorld.Y - dr.startWorld.Y))),
		}
		preview.Rect = &r
	case SketchToolArrow:
		s, t := dr.startWorld, dr.endWorld
		preview.Start, preview.End = &s, &t
	case SketchToolPen:
		pts := slices.Clone(dr.points)
		pts = append(pts, dr.endWorld)
		preview.Points = pts
	default:
		return
	}
	e.drawSketchMark(d, spec, preview, true, false, 0.9)
}

func (e *Editor) drawSketchMark(d *core.Drawing, spec SpecPalette, m SketchMark, selected, reviewDotted bool, alpha float32) {
	haloColor := core.RGBA(0x000000, 0.55*alpha)
	if !spec.Dark {
		haloColor = ui.Alpha(spec.Bg, 0.72*alpha)
	}
	haloWidth := float32(5)
	if selected {
		haloColor = ui.Alpha(spec.Accent.Edge, 0.75)
		haloWidth = 6
	}
	strokeColor := ui.Alpha(spec.TextPrimary, alpha)
	strokeWidth := float32(2)
	if reviewDotted {
		strokeWidth = 2.5
	}

	strokeGeometry := func(col core.Color, width float32, dotted bool) {
		d.SetStrokeColor(col).SetLineWidth(width)
		if dotted {
			d.SetLineDash(2, 6)
		} else {
			d.SetLineDash()
		}
		switch m.Kind {
		case SketchToolBox:
			if m.Rect == nil {
				return
			}
			pts := worldRectCanvasCorners(e, m.Rect.X, m.Rect.Y, m.Rect.Width, m.Rect.Height)
			strokePolygon(d, pts)
		case SketchToolPen:
			if len(m.Points) < 2 {
				return
			}
			x0, y0 := e.WorldToCanvas(m.Points[0].X, m.Points[0].Y)
			d.BeginPath().MoveTo(x0, y0)
			for _, pt := range m.Points[1:] {
				cx, cy := e.WorldToCanvas(pt.X, pt.Y)
				d.LineTo(cx, cy)
			}
			d.Stroke()
		case SketchToolArrow:
			if m.Start == nil || m.End == nil {
				return
			}
			x0, y0 := e.WorldToCanvas(m.Start.X, m.Start.Y)
			x1, y1 := e.WorldToCanvas(m.End.X, m.End.Y)
			d.BeginPath().MoveTo(x0, y0).LineTo(x1, y1).Stroke()
			dx, dy := x1-x0, y1-y0
			dist := float32(math.Hypot(float64(dx), float64(dy)))
			if dist > 4 {
				ux, uy := dx/dist, dy/dist
				const headLen float32 = 11
				const headHalfW float32 = 6
				bx, by := x1-ux*headLen, y1-uy*headLen
				px, py := -uy*headHalfW, ux*headHalfW
				d.BeginPath().MoveTo(bx+px, by+py).LineTo(x1, y1).LineTo(bx-px, by-py).Stroke()
			}
		case SketchToolText:
			if m.At == nil {
				return
			}
			cx, cy := e.WorldToCanvas(m.At.X, m.At.Y)
			d.SetFillColor(col)
			d.BeginPath().Arc(cx, cy, 4, 0, float32(2*math.Pi), false).Fill()
		}
	}

	if !reviewDotted {
		strokeGeometry(haloColor, haloWidth, false)
	}
	strokeGeometry(strokeColor, strokeWidth, reviewDotted)
	d.SetLineDash()

	if reviewDotted {
		return
	}

	// Draw sequential numbered badge ("1", "2", "3", ...).
	bx, by := e.sketchBadgeCanvasPos(m)
	badgeFill := ui.Alpha(spec.TextPrimary, alpha)
	badgeInk := spec.OnTextPrimary
	if selected {
		badgeFill = spec.Accent.Fg
		badgeInk = spec.Accent.OnFill
		d.SetStrokeColor(spec.TextPrimary).SetLineWidth(1.5)
		d.BeginPath().Arc(bx, by, sketchBadgeRadiusPt+2.5, 0, float32(2*math.Pi), false).Stroke()
	}
	d.SetFillColor(badgeFill)
	d.BeginPath().Arc(bx, by, sketchBadgeRadiusPt, 0, float32(2*math.Pi), false).Fill()
	numStr := strconv.Itoa(m.Number)
	badgeFont := core.Font{Size: 11, Weight: 600}
	tx := bx - float32(len(numStr))*3.2
	ty := by + 3.5
	d.SetFillColor(badgeInk).FillText(numStr, tx, ty, badgeFont)

	// Draw optional label pill next to the badge.
	if label := strings.TrimSpace(m.Label); label != "" {
		lx := bx + sketchBadgeRadiusPt + 6
		ly := by - 9
		pw := float32(len([]rune(label)))*5.8 + 14
		const ph float32 = 18
		d.SetFillColor(ui.Alpha(spec.PillBg, 0.88*alpha))
		d.BeginPath().Rect(lx, ly, pw, ph).Fill()
		d.SetFillColor(ui.Alpha(spec.TextPrimary, alpha)).FillText(label, lx+6, ly+12.5, core.Font{Size: 11.5, Weight: 500})
	}
}
