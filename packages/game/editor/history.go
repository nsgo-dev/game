package editor

// Command is a reversible scene edit executed through History.
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

// Undo reverts the most recently executed command and returns true,
// or returns false when there is nothing to undo.
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

// Redo re-applies the most recently undone command and returns true,
// or returns false when there is nothing to redo.
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
