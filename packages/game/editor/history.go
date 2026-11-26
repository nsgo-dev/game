package editor

import (
	"errors"
	"slices"
	"strings"
	"time"
)

// HistoryActor identifies whether a History step was initiated by the person using the editor or by an AI agent.
type HistoryActor string

const (
	// HistoryActorPerson attributes a command to the person using the editor.
	HistoryActorPerson HistoryActor = "person"
	// HistoryActorAgent attributes a command to an AI agent.
	HistoryActorAgent HistoryActor = "agent"
)

// authoredCommand is an optional Command extension declaring who initiated the command.
// Commands that do not implement authoredCommand default to HistoryActorPerson.
type authoredCommand interface {
	Command
	Actor() HistoryActor
}

// ErrHistoryPreview is returned when an edit is attempted while read-only History point-in-time preview is active.
var ErrHistoryPreview = errors.New("editor: cannot edit while previewing history")

const (
	// maxHistoryRecords bounds the number of timeline entries retained per History instance.
	maxHistoryRecords = 256
	// maxHistorySnapshotBytes bounds the total YAML and extra-file snapshot bytes retained per History instance.
	maxHistorySnapshotBytes = 32 << 20
)

// HistoryAuthor identifies who produced a timeline entry in History.
type HistoryAuthor string

const (
	// HistoryAuthorYou marks edits made directly by the person in the editor.
	HistoryAuthorYou HistoryAuthor = "you"
	// HistoryAuthorAgent marks turns made by an AI agent (through MCP or kept reviews).
	HistoryAuthorAgent HistoryAuthor = "agent"
	// HistoryAuthorGit marks external Git working-tree transitions (such as switching branches).
	HistoryAuthorGit HistoryAuthor = "git"
)

// Label returns the human-readable author prefix used in the History panel and MCP readouts.
func (a HistoryAuthor) Label() string {
	switch a {
	case HistoryAuthorAgent:
		return "Agent"
	case HistoryAuthorGit:
		return "git"
	case HistoryAuthorYou:
		return "You"
	default:
		return ""
	}
}

// HistoryEntryKind distinguishes undoable edit steps from saved-point markers on the History timeline.
type HistoryEntryKind string

const (
	// HistoryEntryEdit is a recorded manual edit, agent turn, external reload, or restore step.
	HistoryEntryEdit HistoryEntryKind = "edit"
	// HistoryEntrySaved is a saved-point marker recorded when files are persisted to disk or OnSave.
	HistoryEntrySaved HistoryEntryKind = "saved"
)

// HistoryFilter filters which entries the History panel or MCP history_list returns.
type HistoryFilter string

const (
	HistoryFilterAll   HistoryFilter = "all"
	HistoryFilterYou   HistoryFilter = "you"
	HistoryFilterAgent HistoryFilter = "agent"
)

// HistoryEntry is one visible row on the unified History timeline: a manual edit, an agent turn,
// a Git/external reload, a non-destructive restore, or a saved-point marker.
type HistoryEntry struct {
	// ID uniquely identifies this entry within the current History instance.
	ID uint64
	// Kind is HistoryEntryEdit or HistoryEntrySaved.
	Kind HistoryEntryKind
	// Author is who produced the step (HistoryAuthorYou, HistoryAuthorAgent, or HistoryAuthorGit; empty for saved markers).
	Author HistoryAuthor
	// Summary is the concise headline (for example "moved Knight", "slimes and coins", or "Saved").
	Summary string
	// Detail is the secondary description (for example "x 416 → 452", "6 changes · kept 5", or "level_01.scene.yaml").
	Detail string
	// Files lists the project-relative (or base) file paths touched by this entry.
	Files []string
	// Timestamp is when the step was recorded.
	Timestamp time.Time
	// ChangeCount is the number of individual changes proposed in an agent turn (0 when not applicable).
	ChangeCount int
	// KeptCount is the number of changes kept in an agent turn (0 when not applicable).
	KeptCount int
	// CommandIndex is the 1-based History command index right after this step ran, or 0 for saved markers and superseded steps.
	CommandIndex int
	// Undone reports whether this step is currently undone (ahead of the History undo cursor or superseded).
	Undone bool
	// RestoredFromID is non-zero when this step was created by restoring an earlier HistoryEntry.
	RestoredFromID uint64
}

// Clone returns a deep copy of e so callers cannot mutate History's internal slice.
func (e HistoryEntry) Clone() HistoryEntry {
	e.Files = slices.Clone(e.Files)
	return e
}

// HistoryMeta provides rich timeline metadata for a Command executed through History.
type HistoryMeta struct {
	Author         HistoryAuthor
	Summary        string
	Detail         string
	Files          []string
	ChangeCount    int
	KeptCount      int
	RestoredFromID uint64
	BeforeFiles    map[string]historyFileSnapshot
	AfterFiles     map[string]historyFileSnapshot
}

type historyFileSnapshot struct {
	exists bool
	data   []byte
}

func cloneHistoryFileSnapshots(src map[string]historyFileSnapshot) map[string]historyFileSnapshot {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]historyFileSnapshot, len(src))
	for k, v := range src {
		out[k] = historyFileSnapshot{
			exists: v.exists,
			data:   slices.Clone(v.data),
		}
	}
	return out
}

type historySnapshot struct {
	sceneYAML   []byte
	scenePath   string
	projectYAML []byte
	files       map[string]historyFileSnapshot
}

func (s historySnapshot) clone() historySnapshot {
	return historySnapshot{
		sceneYAML:   slices.Clone(s.sceneYAML),
		scenePath:   s.scenePath,
		projectYAML: slices.Clone(s.projectYAML),
		files:       cloneHistoryFileSnapshots(s.files),
	}
}

type historyRecord struct {
	entry       HistoryEntry
	snapshot    historySnapshot
	beforeFiles map[string]historyFileSnapshot
	superseded  bool
}

// historyMetaCommand is an optional interface implemented by Commands that supply rich timeline metadata.
type historyMetaCommand interface {
	historyMeta() HistoryMeta
}
// Command is a reversible edit executed through History: a change to the open scene, project or clip, or a move of the project's files (MoveFile).
type Command interface {
	Do()
	Undo()
	Label() string
}

// fallibleCommand is a Command whose steps can fail, such as a move of files on disk that something else on disk may block: History runs do and undo in place of Do and Undo, and keeps its place when one fails, so the step stays where it was and can be tried again.
type fallibleCommand interface {
	Command
	do() error
	undo() error
}

// History records executed Commands and supports linear undo and redo, a unified timeline of manual edits, agent turns, and saved points, plus non-destructive commit milestones.
type History struct {
	commands []Command
	cursor   int
	onChange func()
	// beforeExecute optionally rejects mutations (for example while read-only History preview is active).
	beforeExecute func() error
	// onRecord enriches a newly recorded timeline entry with scene/file snapshots and editor context.
	onRecord func(cmd Command, rec *historyRecord)
	// onClear runs when Clear empties the stack.
	onClear func()
	// clock returns the timestamp for newly recorded entries; defaults to time.Now when nil.
	clock func() time.Time
	// group collects the commands executed while Group runs, or is nil outside one.
	group            *groupCmd
	records          []historyRecord
	nextEntryID      uint64
	maxRecords       int
	maxSnapshotBytes int
	milestones       []CommitMilestone
}

// SetClock sets the time source used to timestamp newly recorded history entries. Passing nil restores time.Now.
func (h *History) SetClock(fn func() time.Time) {
	if h == nil {
		return
	}
	h.clock = fn
}

func (h *History) now() time.Time {
	if h != nil && h.clock != nil {
		return h.clock()
	}
	return time.Now()
}

// Entries returns a copy of all recorded timeline entries, newest first.
func (h *History) Entries() []HistoryEntry {
	return h.FilteredEntries(HistoryFilterAll)
}

// FilteredEntries returns a copy of recorded timeline entries matching filter, newest first.
func (h *History) FilteredEntries(filter HistoryFilter) []HistoryEntry {
	if h == nil || len(h.records) == 0 {
		return nil
	}
	out := make([]HistoryEntry, 0, len(h.records))
	for i := len(h.records) - 1; i >= 0; i-- {
		e := h.records[i].entry
		switch filter {
		case HistoryFilterYou:
			if e.Kind != HistoryEntryEdit || e.Author != HistoryAuthorYou {
				continue
			}
		case HistoryFilterAgent:
			if e.Kind != HistoryEntryEdit || e.Author != HistoryAuthorAgent {
				continue
			}
		}
		out = append(out, e.Clone())
	}
	return out
}

// Entry returns the timeline entry with ID id, or false when not found.
func (h *History) Entry(id uint64) (HistoryEntry, bool) {
	if h == nil || id == 0 {
		return HistoryEntry{}, false
	}
	for i := range h.records {
		if h.records[i].entry.ID == id {
			return h.records[i].entry.Clone(), true
		}
	}
	return HistoryEntry{}, false
}

func (h *History) recordIndexByID(id uint64) int {
	if h == nil || id == 0 {
		return -1
	}
	for i := range h.records {
		if h.records[i].entry.ID == id {
			return i
		}
	}
	return -1
}

// RecordSaved appends a saved-point marker to the timeline without altering the undo/redo stack.
func (h *History) RecordSaved(files []string, detail string, snap historySnapshot) HistoryEntry {
	if h == nil {
		return HistoryEntry{}
	}
	h.nextEntryID++
	entry := HistoryEntry{
		ID:        h.nextEntryID,
		Kind:      HistoryEntrySaved,
		Summary:   "Saved",
		Detail:    detail,
		Files:     slices.Clone(files),
		Timestamp: h.now(),
	}
	h.records = append(h.records, historyRecord{
		entry:    entry,
		snapshot: snap.clone(),
	})
	h.trimRecords()
	return entry.Clone()
}

func historyRecordBytes(r *historyRecord) int {
	if r == nil {
		return 0
	}
	n := len(r.snapshot.sceneYAML) + len(r.snapshot.projectYAML)
	for _, f := range r.snapshot.files {
		n += len(f.data)
	}
	for _, f := range r.beforeFiles {
		n += len(f.data)
	}
	return n
}

func (h *History) recordLimits() (int, int) {
	maxRecs := h.maxRecords
	if maxRecs <= 0 {
		maxRecs = maxHistoryRecords
	}
	maxBytes := h.maxSnapshotBytes
	if maxBytes <= 0 {
		maxBytes = maxHistorySnapshotBytes
	}
	return maxRecs, maxBytes
}

func (h *History) totalRecordBytes() int {
	total := 0
	for i := range h.records {
		total += historyRecordBytes(&h.records[i])
	}
	return total
}

// trimRecords enforces bounded entry count and snapshot byte budget on h.records so long sessions
// cannot grow without bound. When over the byte budget it prunes superseded (undone/branched)
// snapshots first before evicting the oldest active records from the front, always keeping the
// newest entry.
func (h *History) trimRecords() {
	if h == nil || len(h.records) <= 1 {
		return
	}
	maxRecs, maxBytes := h.recordLimits()

	// 1. Enforce max entry count by dropping oldest entries from the front.
	if len(h.records) > maxRecs {
		drop := len(h.records) - maxRecs
		copy(h.records, h.records[drop:])
		clear(h.records[len(h.records)-drop:])
		h.records = h.records[:len(h.records)-drop]
	}

	// 2. Enforce total snapshot byte budget.
	total := h.totalRecordBytes()
	if total <= maxBytes {
		return
	}

	// Pass A: compact/prune superseded records (oldest first, never touching the newest entry).
	kept := h.records[:0]
	lastIdx := len(h.records) - 1
	for i := range h.records {
		r := h.records[i]
		if total > maxBytes && i < lastIdx && r.superseded {
			total -= historyRecordBytes(&r)
			continue
		}
		kept = append(kept, r)
	}
	clear(h.records[len(kept):])
	h.records = kept

	// Pass B: drop oldest remaining entries from the front until within budget (keeping at least 1).
	drop := 0
	for total > maxBytes && len(h.records)-drop > 1 {
		total -= historyRecordBytes(&h.records[drop])
		drop++
	}
	if drop > 0 {
		copy(h.records, h.records[drop:])
		clear(h.records[len(h.records)-drop:])
		h.records = h.records[:len(h.records)-drop]
	}
}

func (h *History) markSupersededAfterCursor() {
	for i := range h.records {
		r := &h.records[i]
		if r.entry.Kind == HistoryEntryEdit && r.entry.CommandIndex > h.cursor {
			r.entry.CommandIndex = 0
			r.entry.Undone = true
			r.superseded = true
		}
	}
}

func (h *History) syncUndoFlags() {
	for i := range h.records {
		r := &h.records[i]
		if r.entry.Kind != HistoryEntryEdit {
			continue
		}
		if r.superseded || r.entry.CommandIndex <= 0 {
			r.entry.Undone = true
			continue
		}
		r.entry.Undone = r.entry.CommandIndex > h.cursor
	}
}

// truncateRecordsAfterCommand removes non-superseded timeline records whose CommandIndex > cursor
// (used when collapsing intermediate review edits into one kept step).
func (h *History) truncateRecordsAfterCommand(cursor int) {
	if h == nil || len(h.records) == 0 {
		return
	}
	kept := h.records[:0]
	for _, r := range h.records {
		if r.entry.Kind == HistoryEntryEdit && !r.superseded && r.entry.CommandIndex > cursor {
			continue
		}
		kept = append(kept, r)
	}
	clear(h.records[len(kept):])
	h.records = kept
}

func (h *History) recordCommand(cmd Command, commandIndex int) {
	h.nextEntryID++
	entry := HistoryEntry{
		ID:           h.nextEntryID,
		Kind:         HistoryEntryEdit,
		Author:       HistoryAuthorYou,
		Summary:      strings.TrimSpace(cmd.Label()),
		Timestamp:    h.now(),
		CommandIndex: commandIndex,
	}
	rec := historyRecord{entry: entry}
	if mc, ok := cmd.(historyMetaCommand); ok {
		m := mc.historyMeta()
		if m.Author != "" {
			rec.entry.Author = m.Author
		}
		if m.Summary != "" {
			rec.entry.Summary = m.Summary
		}
		if m.Detail != "" {
			rec.entry.Detail = m.Detail
		}
		if len(m.Files) > 0 {
			rec.entry.Files = slices.Clone(m.Files)
		}
		if m.ChangeCount > 0 {
			rec.entry.ChangeCount = m.ChangeCount
		}
		if m.KeptCount > 0 {
			rec.entry.KeptCount = m.KeptCount
		}
		if m.RestoredFromID > 0 {
			rec.entry.RestoredFromID = m.RestoredFromID
		}
		rec.beforeFiles = cloneHistoryFileSnapshots(m.BeforeFiles)
		rec.snapshot.files = cloneHistoryFileSnapshots(m.AfterFiles)
	}
	if h.onRecord != nil {
		h.onRecord(cmd, &rec)
	}
	if rec.entry.Summary == "" {
		rec.entry.Summary = "Edit"
	}
	h.records = append(h.records, rec)
	h.trimRecords()
	h.syncUndoFlags()
}

// RecordMilestone records a Git commit milestone in history without altering the undo/redo command stack or rebuilding the scene.
func (h *History) RecordMilestone(m CommitMilestone) {
	if h == nil {
		return
	}
	h.milestones = append([]CommitMilestone{m}, h.milestones...)
}

// MarkBranchPushed marks all recorded milestones on branch as pushed.
func (h *History) MarkBranchPushed(branch string) {
	if h == nil {
		return
	}
	for i := range h.milestones {
		if h.milestones[i].Branch == branch {
			h.milestones[i].Pushed = true
		}
	}
}

// Milestones returns a copy of all recorded commit milestones, most recent first.
func (h *History) Milestones() []CommitMilestone {
	if h == nil || len(h.milestones) == 0 {
		return nil
	}
	out := make([]CommitMilestone, len(h.milestones))
	copy(out, h.milestones)
	return out
}

// NewHistory returns an empty undo/redo History.
func NewHistory() *History {
	return &History{}
}

// Execute runs cmd.Do(), discards any redo tail beyond the current cursor, pushes cmd onto the stack, and notifies any change hook.
func (h *History) Execute(cmd Command) {
	_ = h.execute(cmd)
}

// execute is Execute for a command that may fail: when it does, the history is left as it was, redo tail included, and execute returns its error.
func (h *History) execute(cmd Command) error {
	if h == nil || cmd == nil {
		return nil
	}
	if h.beforeExecute != nil {
		if err := h.beforeExecute(); err != nil {
			return err
		}
	}
	if f, ok := cmd.(fallibleCommand); ok {
		if err := f.do(); err != nil {
			return err
		}
	} else {
		cmd.Do()
	}
	if h.group != nil {
		h.group.cmds = append(h.group.cmds, cmd)
		if h.onChange != nil {
			h.onChange()
		}
		return nil
	}
	if h.cursor < len(h.commands) {
		h.markSupersededAfterCursor()
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, cmd)
	h.cursor = len(h.commands)
	h.recordCommand(cmd, h.cursor)
	if h.onChange != nil {
		h.onChange()
	}
	return nil
}

// Undo reverts the most recently executed command and returns true. It returns false when there is nothing to undo, and also when the command is one whose step can fail, such as a file move (MoveFile) that something on disk now blocks, and it failed; the command then stays applied. Editor.Undo reports why in the status bar.
func (h *History) Undo() bool {
	ok, _ := h.undo()
	return ok
}

// undo is Undo reporting why a command that may fail did not revert; the cursor then stays where it was.
func (h *History) undo() (bool, error) {
	if !h.CanUndo() {
		return false, nil
	}
	cmd := h.commands[h.cursor-1]
	if f, ok := cmd.(fallibleCommand); ok {
		if err := f.undo(); err != nil {
			return false, err
		}
	} else {
		cmd.Undo()
	}
	h.cursor--
	h.syncUndoFlags()
	if h.onChange != nil {
		h.onChange()
	}
	return true, nil
}

// Redo re-applies the most recently undone command and returns true. It returns false when there is nothing to redo, and also when the command is one whose step can fail, such as a file move (MoveFile) that something on disk now blocks, and it failed; the command then stays undone. Editor.Redo reports why in the status bar.
func (h *History) Redo() bool {
	ok, _ := h.redo()
	return ok
}

// redo is Redo reporting why a command that may fail did not re-apply; the cursor then stays where it was.
func (h *History) redo() (bool, error) {
	if !h.CanRedo() {
		return false, nil
	}
	cmd := h.commands[h.cursor]
	if f, ok := cmd.(fallibleCommand); ok {
		if err := f.do(); err != nil {
			return false, err
		}
	} else {
		cmd.Do()
	}
	h.cursor++
	h.syncUndoFlags()
	if h.onChange != nil {
		h.onChange()
	}
	return true, nil
}

// CanUndo reports whether Undo has a command to revert.
func (h *History) CanUndo() bool {
	return h != nil && h.cursor > 0
}

// CanRedo reports whether Redo has a command to re-apply.
func (h *History) CanRedo() bool {
	return h != nil && h.cursor < len(h.commands)
}

// Clear empties the command stack and timeline entries.
func (h *History) Clear() {
	if h == nil {
		return
	}
	h.commands = nil
	h.cursor = 0
	h.records = nil
	if h.group != nil {
		// What the group did belongs to the document the clear left behind.
		h.group.cmds = nil
	}
	if h.onClear != nil {
		h.onClear()
	}
	if h.onChange != nil {
		h.onChange()
	}
}

// Len returns the total number of commands retained in history (applied plus redoable).
func (h *History) Len() int {
	if h == nil {
		return 0
	}
	return len(h.commands)
}

// Index returns the number of currently applied commands.
func (h *History) Index() int {
	if h == nil {
		return 0
	}
	return h.cursor
}

// topUndoCommand returns the most recently applied command that Undo would revert, or nil.
func (h *History) topUndoCommand() Command {
	if !h.CanUndo() {
		return nil
	}
	return h.commands[h.cursor-1]
}

// UndoLabel returns the label of the next command Undo would revert, or "".
func (h *History) UndoLabel() string {
	if !h.CanUndo() {
		return ""
	}
	return h.commands[h.cursor-1].Label()
}

// UndoActor returns who initiated the next command Undo would revert, or "" when nothing can be undone.
func (h *History) UndoActor() HistoryActor {
	if !h.CanUndo() {
		return ""
	}
	return commandActor(h.commands[h.cursor-1])
}

func commandActor(cmd Command) HistoryActor {
	if cmd == nil {
		return HistoryActorPerson
	}
	if a, ok := cmd.(authoredCommand); ok && a.Actor() != "" {
		return a.Actor()
	}
	return HistoryActorPerson
}

// RedoLabel returns the label of the next command Redo would re-apply, or "".
func (h *History) RedoLabel() string {
	if !h.CanRedo() {
		return ""
	}
	return h.commands[h.cursor].Label()
}

// Group runs fn and records every command fn executes through h as one step labeled label, so that one Undo reverts them all, last first, and one Redo applies them again. When fn returns an error, Group undoes the commands fn executed, last first, records nothing and returns the error, leaving the history as it was. A group in which fn executes nothing records nothing. While fn runs, a nested Group joins the outer one, and a Clear drops what the group collected, since it belonged to the document the clear left behind; fn must not call Undo or Redo.
func (h *History) Group(label string, fn func() error) error {
	if h == nil {
		return fn()
	}
	if h.beforeExecute != nil {
		if err := h.beforeExecute(); err != nil {
			return err
		}
	}
	if h.group != nil {
		return fn()
	}
	g := &groupCmd{label: label}
	h.group = g
	err := func() (err error) {
		defer func() {
			if failure := recover(); failure != nil {
				h.group = nil
				g.rollback(len(g.cmds))
				panic(failure)
			}
		}()
		return fn()
	}()
	h.group = nil
	if err != nil {
		if len(g.cmds) > 0 {
			g.rollback(len(g.cmds))
			if h.onChange != nil {
				h.onChange()
			}
		}
		return err
	}
	if len(g.cmds) == 0 {
		return nil
	}
	if h.cursor < len(h.commands) {
		h.markSupersededAfterCursor()
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, g)
	h.cursor = len(h.commands)
	h.recordCommand(g, h.cursor)
	if h.onChange != nil {
		h.onChange()
	}
	return nil
}

// groupCmd is the step Group records: its commands in the order they ran.
type groupCmd struct {
	label string
	cmds  []Command
}

func (g *groupCmd) Label() string { return g.label }

func (g *groupCmd) Do() { _ = g.do() }

func (g *groupCmd) Undo() { _ = g.undo() }

// do applies the commands in order; when one that can fail fails, it reverts those it applied and returns the error.
func (g *groupCmd) do() error {
	for i, c := range g.cmds {
		if f, ok := c.(fallibleCommand); ok {
			if err := f.do(); err != nil {
				g.rollback(i)
				return err
			}
			continue
		}
		c.Do()
	}
	return nil
}

// undo reverts the commands last first; when one that can fail fails, it applies again those it reverted and returns the error.
func (g *groupCmd) undo() error {
	for i := len(g.cmds) - 1; i >= 0; i-- {
		c := g.cmds[i]
		if f, ok := c.(fallibleCommand); ok {
			if err := f.undo(); err != nil {
				for _, r := range g.cmds[i+1:] {
					if f, ok := r.(fallibleCommand); ok {
						_ = f.do()
					} else {
						r.Do()
					}
				}
				return err
			}
			continue
		}
		c.Undo()
	}
	return nil
}

// rollback reverts the first n commands, last first, as a failed group or a failed do leaves them.
func (g *groupCmd) rollback(n int) {
	for i := n - 1; i >= 0; i-- {
		if f, ok := g.cmds[i].(fallibleCommand); ok {
			_ = f.undo()
		} else {
			g.cmds[i].Undo()
		}
	}
}

// previewGroup records a temporary preview capture group and any outer Group it suspended.
type previewGroup struct {
	g    *groupCmd
	prev *groupCmd
}

// beginPreview starts capturing commands executed through h into a temporary group without
// pushing anything onto the undo stack yet. Used by interactive handle drags so every move
// previews live, cancels cleanly by rolling back, and commits as one undo step via commitPreview.
func (h *History) beginPreview(label string) previewGroup {
	if h == nil {
		return previewGroup{}
	}
	g := &groupCmd{label: label}
	prev := h.group
	h.group = g
	return previewGroup{g: g, prev: prev}
}

// endPreview stops capturing commands into p, restores any suspended outer group, and returns the captured commands.
func (h *History) endPreview(p previewGroup) []Command {
	if h == nil || p.g == nil {
		return nil
	}
	if h.group == p.g {
		h.group = p.prev
	}
	return p.g.cmds
}

// commitPreview pushes already-applied preview commands onto h as one undoable step without
// re-running Do(). Returns true when a step was recorded.
func (h *History) commitPreview(label string, cmds []Command) bool {
	if h == nil || len(cmds) == 0 {
		return false
	}
	if h.beforeExecute != nil {
		if err := h.beforeExecute(); err != nil {
			return false
		}
	}
	var cmd Command
	if len(cmds) == 1 && label == "" {
		cmd = cmds[0]
	} else {
		cmd = &groupCmd{label: label, cmds: cmds}
	}
	if h.group != nil {
		h.group.cmds = append(h.group.cmds, cmd)
		if h.onChange != nil {
			h.onChange()
		}
		return true
	}
	if h.cursor < len(h.commands) {
		h.markSupersededAfterCursor()
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, cmd)
	h.cursor = len(h.commands)
	h.recordCommand(cmd, h.cursor)
	if h.onChange != nil {
		h.onChange()
	}
	return true
}
