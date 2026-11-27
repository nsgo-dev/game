package fighting

// Frame is the input sampled on one step.
type Frame struct {
	// Step is the zero-based index of the step since the buffer was created or last reset.
	Step int
	// Raw is the absolute direction, with 4 meaning left and 6 meaning right.
	Raw Direction
	// Dir is Raw relative to Facing, with 4 meaning back and 6 meaning forward. Moves match against Dir.
	Dir Direction
	// Facing is the side the character faced on this step.
	Facing Facing
	// Held has bit i set while button i is held.
	Held uint32
	// Pressed has bit i set when button i is held on this step but was not held on the previous one.
	Pressed uint32
}

// Buffer is a fixed-capacity ring of the most recent Frames, one per step.
//
// Push allocates nothing, so a Buffer can sit in a fixed-step update loop. A Buffer is not safe for concurrent use.
type Buffer struct {
	frames []Frame
	count  int
}

// NewBuffer returns an empty Buffer that keeps the last capacity frames; a capacity below 1 keeps one frame.
func NewBuffer(capacity int) *Buffer {
	return &Buffer{frames: make([]Frame, max(capacity, 1))}
}

// Push records the next step's absolute direction, held buttons and facing, derives Dir and Pressed, and returns the stored frame. An invalid direction is recorded as Neutral.
func (b *Buffer) Push(raw Direction, held uint32, facing Facing) Frame {
	if !raw.Valid() {
		raw = Neutral
	}
	if facing != FaceLeft {
		facing = FaceRight
	}
	var prevHeld uint32
	if prev, ok := b.Latest(); ok {
		prevHeld = prev.Held
	}
	f := Frame{
		Step:    b.count,
		Raw:     raw,
		Dir:     facing.Relative(raw),
		Facing:  facing,
		Held:    held,
		Pressed: held &^ prevHeld,
	}
	b.frames[b.count%len(b.frames)] = f
	b.count++
	return f
}

// Cap returns how many frames the buffer keeps.
func (b *Buffer) Cap() int { return len(b.frames) }

// Len returns how many frames the buffer currently holds.
func (b *Buffer) Len() int { return min(b.count, len(b.frames)) }

// Steps returns how many frames have been pushed since the buffer was created or last reset; the latest frame's Step is Steps()-1.
func (b *Buffer) Steps() int { return b.count }

// Oldest returns the Step of the oldest frame still held, or Steps() when the buffer is empty.
func (b *Buffer) Oldest() int { return b.count - b.Len() }

// Latest returns the most recent frame and whether there is one.
func (b *Buffer) Latest() (Frame, bool) { return b.At(b.count - 1) }

// At returns the frame for step and whether it is still held.
func (b *Buffer) At(step int) (Frame, bool) {
	if step < b.Oldest() || step >= b.count {
		return Frame{}, false
	}
	return b.frames[step%len(b.frames)], true
}

// Reset empties the buffer and restarts step numbering at zero.
func (b *Buffer) Reset() {
	clear(b.frames)
	b.count = 0
}

// dir returns the facing-relative direction for a step known to be held.
func (b *Buffer) dir(step int) Direction { return b.frames[step%len(b.frames)].Dir }

// pressed returns the pressed mask for a step known to be held.
func (b *Buffer) pressed(step int) uint32 { return b.frames[step%len(b.frames)].Pressed }
