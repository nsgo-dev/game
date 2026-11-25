package editor

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

// History records executed Commands and supports linear undo and redo, plus non-destructive commit milestones.
type History struct {
	commands []Command
	cursor   int
	onChange func()
	// group collects the commands executed while Group runs, or is nil outside one.
	group      *groupCmd
	milestones []CommitMilestone
}

// RecordMilestone records a Git commit milestone in history without altering the undo/redo command stack.
func (h *History) RecordMilestone(m CommitMilestone) {
	if h == nil {
		return
	}
	h.milestones = append([]CommitMilestone{m}, h.milestones...)
	if h.onChange != nil {
		h.onChange()
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
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, cmd)
	h.cursor = len(h.commands)
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

// Clear empties the command stack.
func (h *History) Clear() {
	if h == nil {
		return
	}
	h.commands = nil
	h.cursor = 0
	if h.group != nil {
		// What the group did belongs to the document the clear left behind.
		h.group.cmds = nil
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

// UndoLabel returns the label of the next command Undo would revert, or "".
func (h *History) UndoLabel() string {
	if !h.CanUndo() {
		return ""
	}
	return h.commands[h.cursor-1].Label()
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
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, g)
	h.cursor = len(h.commands)
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
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	h.commands = append(h.commands, cmd)
	h.cursor = len(h.commands)
	if h.onChange != nil {
		h.onChange()
	}
	return true
}
