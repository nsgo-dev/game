package editor

// Command is a reversible scene edit executed through History.
type Command interface {
	Do()
	Undo()
	Label() string
}

// History records executed Commands and supports linear undo and redo.
type History struct {
	commands []Command
	cursor   int
	onChange func()
}

// NewHistory returns an empty undo/redo History.
func NewHistory() *History {
	return &History{}
}

// Execute runs cmd.Do(), discards any redo tail beyond the current cursor,
// pushes cmd onto the stack, and notifies any change hook.
func (h *History) Execute(cmd Command) {
	if h == nil || cmd == nil {
		return
	}
	if h.cursor < len(h.commands) {
		clear(h.commands[h.cursor:])
		h.commands = h.commands[:h.cursor]
	}
	cmd.Do()
	h.commands = append(h.commands, cmd)
	h.cursor = len(h.commands)
	if h.onChange != nil {
		h.onChange()
	}
}

// Undo reverts the most recently executed command and returns true,
// or returns false when there is nothing to undo.
func (h *History) Undo() bool {
	if !h.CanUndo() {
		return false
	}
	h.cursor--
	h.commands[h.cursor].Undo()
	if h.onChange != nil {
		h.onChange()
	}
	return true
}

// Redo re-applies the most recently undone command and returns true,
// or returns false when there is nothing to redo.
func (h *History) Redo() bool {
	if !h.CanRedo() {
		return false
	}
	h.commands[h.cursor].Do()
	h.cursor++
	if h.onChange != nil {
		h.onChange()
	}
	return true
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
