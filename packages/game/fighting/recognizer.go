package fighting

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

// Source supplies the actions and axes a Recognizer reads each step. *input.Actions, *replay.Recorder and *replay.Player all satisfy it.
type Source interface {
	Pressed(name string) bool
	Axis(name string) float64
}

// Match is a move or combo completed on a step.
type Match struct {
	// Name is the move's or combo's name.
	Name string
	// Index is the position of the move in MoveSet.Moves, or of the combo in MoveSet.Combos.
	Index int
	// Start is the step of the earliest input that formed the match: the first motion entry or chord button for a move, the first move's Step for a combo.
	Start int
	// Step is the step on which the match completed.
	Step int
}

// Result lists what completed on one step.
//
// Its slices are reused by the next Update, Feed or Reset; copy them to keep them longer.
type Result struct {
	// Step is the step the result describes.
	Step int
	// Moves are the moves completed on Step, highest Priority first and in definition order among equal priorities. Moves[0] is the move a game would perform.
	Moves []Match
	// Combos are the combos whose last move completed on Step, in definition order.
	Combos []Match
}

// Recognizer buffers per-step input and recognizes the moves and combos of a MoveSet.
//
// Call Update (or Feed) exactly once per fixed step, after the Source has stepped. Recognition is deterministic: the same frames always give the same Results. For rollback, save the recognizer's state with AppendState alongside the rest of the simulation and restore it with LoadState before re-simulating. Update allocates nothing after construction. A Recognizer is not safe for concurrent use.
type Recognizer struct {
	input    Input
	deadzone float64
	buttons  []string
	moves    []compiledMove
	order    []int
	combos   []compiledCombo
	// done holds, per move, the step it last completed on, or -1. A motion that completed a move cannot complete it again: the next completion needs motion entries after that step.
	done []int
	buf  *Buffer
	res  Result
}

type compiledMove struct {
	name        string
	sets        []dirSet
	window      int
	press       uint32
	pressAll    bool
	pressWindow int
	buffer      int
	charge      dirSet
	chargeSteps int
	chargeGrace int
}

type compiledCombo struct {
	name    string
	seq     []int
	gap     int
	partial []comboProgress
}

type comboProgress struct {
	next  int
	start int
	last  int
}

// NewRecognizer validates set and returns a Recognizer for it whose buffer holds set.Lookback() steps. Later changes to set do not affect the Recognizer.
func NewRecognizer(set *MoveSet) (*Recognizer, error) {
	if err := set.Validate(); err != nil {
		return nil, err
	}
	r := &Recognizer{
		input:    set.Input,
		deadzone: set.Input.Deadzone,
		buttons:  set.Buttons(),
		moves:    make([]compiledMove, len(set.Moves)),
		order:    make([]int, len(set.Moves)),
		combos:   make([]compiledCombo, len(set.Combos)),
		done:     make([]int, len(set.Moves)),
		buf:      NewBuffer(set.Lookback()),
	}
	for i := range r.done {
		r.done[i] = -1
	}
	if r.deadzone == 0 {
		r.deadzone = DefaultDeadzone
	}
	bit := make(map[string]uint32, len(r.buttons))
	for i, b := range r.buttons {
		bit[b] = 1 << i
	}
	index := make(map[string]int, len(set.Moves))
	for i := range set.Moves {
		m := &set.Moves[i]
		index[m.Name] = i
		cm := compiledMove{
			name:        m.Name,
			sets:        make([]dirSet, len(m.Motion)),
			window:      m.Window,
			pressAll:    m.PressAll,
			pressWindow: m.PressWindow,
			buffer:      m.Buffer,
		}
		for j, entry := range m.Motion {
			cm.sets[j], _ = parseDirSet(entry)
		}
		for _, b := range m.Press {
			cm.press |= bit[b]
		}
		if m.Charge != nil {
			cm.charge, _ = parseDirSet(m.Charge.Dirs)
			cm.chargeSteps = m.Charge.Steps
			cm.chargeGrace = m.Charge.Grace
		}
		r.moves[i] = cm
		r.order[i] = i
	}
	slices.SortStableFunc(r.order, func(a, b int) int { return cmp.Compare(set.Moves[b].Priority, set.Moves[a].Priority) })
	maxMoves := len(set.Moves)
	for i := range set.Combos {
		c := &set.Combos[i]
		cc := compiledCombo{name: c.Name, seq: make([]int, len(c.Sequence)), gap: c.Gap, partial: make([]comboProgress, 0, len(c.Sequence))}
		for j, name := range c.Sequence {
			cc.seq[j] = index[name]
		}
		r.combos[i] = cc
	}
	r.res.Moves = make([]Match, 0, maxMoves)
	r.res.Combos = make([]Match, 0, len(set.Combos))
	return r, nil
}

// Buttons returns the button action names in bit order, as MoveSet.Buttons does.
func (r *Recognizer) Buttons() []string { return slices.Clone(r.buttons) }

// Buffer returns the recognizer's input history. Push frames through Feed or Update rather than directly, so combos stay in step.
func (r *Recognizer) Buffer() *Buffer { return r.buf }

// Direction reads the absolute direction from src: digital actions add ±1 to their axis, analog axes add their value, and an axis whose total magnitude reaches the deadzone points that way.
func (r *Recognizer) Direction(src Source) Direction {
	in := &r.input
	var x, y float64
	if in.Right != "" && src.Pressed(in.Right) {
		x++
	}
	if in.Left != "" && src.Pressed(in.Left) {
		x--
	}
	if in.Up != "" && src.Pressed(in.Up) {
		y++
	}
	if in.Down != "" && src.Pressed(in.Down) {
		y--
	}
	if in.AxisX != "" {
		x += finite(src.Axis(in.AxisX))
	}
	if in.AxisY != "" {
		v := finite(src.Axis(in.AxisY))
		if !in.YUp {
			v = -v
		}
		y += v
	}
	return DirectionOf(r.sign(x), r.sign(y))
}

func finite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

func (r *Recognizer) sign(v float64) int {
	switch {
	case v >= r.deadzone:
		return 1
	case v <= -r.deadzone:
		return -1
	}
	return 0
}

// Held reads the held button mask from src, bit i for Buttons()[i].
func (r *Recognizer) Held(src Source) uint32 {
	var held uint32
	for i, b := range r.buttons {
		if src.Pressed(b) {
			held |= 1 << i
		}
	}
	return held
}

// Update samples src for the current step with the character facing facing and returns what completed on it.
func (r *Recognizer) Update(src Source, facing Facing) Result {
	return r.Feed(r.Direction(src), r.Held(src), facing)
}

// Feed records one step from an absolute direction and a held button mask (bit i for Buttons()[i]), for inputs that arrive already decoded, such as rollback netcode, and returns what completed on that step.
func (r *Recognizer) Feed(raw Direction, held uint32, facing Facing) Result {
	f := r.buf.Push(raw, held, facing)
	now := f.Step
	r.res.Step = now
	r.res.Moves = r.res.Moves[:0]
	r.res.Combos = r.res.Combos[:0]
	for _, i := range r.order {
		if start, ok := r.match(i, now); ok {
			r.res.Moves = append(r.res.Moves, Match{Name: r.moves[i].name, Index: i, Start: start, Step: now})
			r.done[i] = now
		}
	}
	if len(r.res.Moves) > 0 {
		r.advanceCombos(r.res.Moves[0].Index, now)
	}
	return r.res
}

// Reset clears the input history and combo progress and restarts step numbering at zero.
func (r *Recognizer) Reset() {
	r.buf.Reset()
	for i := range r.done {
		r.done[i] = -1
	}
	for i := range r.combos {
		r.combos[i].partial = r.combos[i].partial[:0]
	}
	r.res.Step = 0
	r.res.Moves = r.res.Moves[:0]
	r.res.Combos = r.res.Combos[:0]
}

// stateVersion is the first byte of AppendState's output.
const stateVersion = 1

// AppendState appends the recognizer's complete state (the buffered frames, step count and the progress of every move and combo) to dst and returns the extended slice. The bytes are an in-memory snapshot for rollback, meant for LoadState on a Recognizer built from the same move set; they are not a file format.
func (r *Recognizer) AppendState(dst []byte) []byte {
	b := r.buf
	dst = append(dst, stateVersion)
	dst = binary.AppendUvarint(dst, uint64(b.count))
	for s := b.Oldest(); s < b.count; s++ {
		f := b.frames[s%len(b.frames)]
		dst = append(dst, byte(f.Raw), byte(f.Facing))
		dst = binary.AppendUvarint(dst, uint64(f.Held))
		dst = binary.AppendUvarint(dst, uint64(f.Pressed))
	}
	dst = binary.AppendUvarint(dst, uint64(len(r.done)))
	for _, d := range r.done {
		dst = binary.AppendVarint(dst, int64(d))
	}
	dst = binary.AppendUvarint(dst, uint64(len(r.combos)))
	for i := range r.combos {
		c := &r.combos[i]
		dst = binary.AppendUvarint(dst, uint64(len(c.partial)))
		for _, p := range c.partial {
			dst = binary.AppendUvarint(dst, uint64(p.next))
			dst = binary.AppendVarint(dst, int64(p.start))
			dst = binary.AppendVarint(dst, int64(p.last))
		}
	}
	return dst
}

// LoadState restores state written by AppendState, so the recognizer continues exactly as the one that wrote it would. It refuses state written for a different move set, or malformed state, and then leaves the recognizer unchanged.
func (r *Recognizer) LoadState(state []byte) error {
	if err := r.decodeState(state, false); err != nil {
		return err
	}
	_ = r.decodeState(state, true)
	return nil
}

// decodeState parses state, and when apply is set also writes it into r. LoadState runs it once to check and once to apply, so a bad snapshot changes nothing and a good one allocates nothing.
func (r *Recognizer) decodeState(state []byte, apply bool) error {
	d := stateDecoder{buf: state}
	if v := d.u8(); v != stateVersion {
		return fmt.Errorf("fighting: recognizer state version %d (want %d)", v, stateVersion)
	}
	b := r.buf
	count := d.uvarint(math.MaxInt32)
	held := min(count, len(b.frames))
	if apply {
		clear(b.frames)
		b.count = count
	}
	for s := count - held; s < count; s++ {
		raw, facing := Direction(d.u8()), Facing(d.u8())
		heldMask := uint32(d.uvarint(math.MaxUint32))
		pressed := uint32(d.uvarint(math.MaxUint32))
		if d.err == nil && (!raw.Valid() || facing > FaceLeft) {
			d.err = errors.New("bad frame")
		}
		if apply {
			b.frames[s%len(b.frames)] = Frame{Step: s, Raw: raw, Dir: facing.Relative(raw), Facing: facing, Held: heldMask, Pressed: pressed}
		}
	}
	if n := d.uvarint(math.MaxInt32); d.err == nil && n != len(r.done) {
		return fmt.Errorf("fighting: recognizer state has %d moves (want %d)", n, len(r.done))
	}
	for i := range r.done {
		v := d.varint(-1, count-1)
		if apply {
			r.done[i] = v
		}
	}
	if n := d.uvarint(math.MaxInt32); d.err == nil && n != len(r.combos) {
		return fmt.Errorf("fighting: recognizer state has %d combos (want %d)", n, len(r.combos))
	}
	for i := range r.combos {
		c := &r.combos[i]
		n := d.uvarint(cap(c.partial))
		if apply {
			c.partial = c.partial[:n]
		}
		for j := range n {
			p := comboProgress{next: d.uvarint(len(c.seq) - 1), start: d.varint(0, count-1), last: d.varint(0, count-1)}
			if d.err == nil && (p.next < 1 || p.start > p.last) {
				d.err = errors.New("bad combo progress")
			}
			if apply {
				c.partial[j] = p
			}
		}
	}
	if d.err == nil && len(d.buf) > 0 {
		d.err = errors.New("trailing bytes")
	}
	if d.err != nil {
		return fmt.Errorf("fighting: recognizer state: %w", d.err)
	}
	return nil
}

// stateDecoder reads AppendState's encoding, remembering the first error so callers check once.
type stateDecoder struct {
	buf []byte
	err error
}

func (d *stateDecoder) u8() byte {
	if d.err != nil {
		return 0
	}
	if len(d.buf) == 0 {
		d.err = errors.New("truncated")
		return 0
	}
	v := d.buf[0]
	d.buf = d.buf[1:]
	return v
}

func (d *stateDecoder) uvarint(hi int) int {
	if d.err != nil {
		return 0
	}
	v, n := binary.Uvarint(d.buf)
	if n <= 0 {
		d.err = errors.New("truncated")
		return 0
	}
	d.buf = d.buf[n:]
	if v > uint64(hi) {
		d.err = fmt.Errorf("value %d above %d", v, hi)
		return 0
	}
	return int(v)
}

func (d *stateDecoder) varint(lo, hi int) int {
	if d.err != nil {
		return 0
	}
	v, n := binary.Varint(d.buf)
	if n <= 0 {
		d.err = errors.New("truncated")
		return 0
	}
	d.buf = d.buf[n:]
	if v < int64(lo) || v > int64(hi) {
		d.err = fmt.Errorf("value %d outside [%d, %d]", v, lo, hi)
		return 0
	}
	return int(v)
}

// match reports whether move i completes on step now and the step its inputs started.
func (r *Recognizer) match(i, now int) (int, bool) {
	m := &r.moves[i]
	b := r.buf
	oldest := b.Oldest()
	start := now
	n := len(m.sets)
	if m.press != 0 {
		if b.pressed(now)&m.press == 0 {
			return 0, false
		}
		if m.pressAll {
			var seen uint32
			for s := now; s >= max(now-m.pressWindow, oldest); s-- {
				if p := b.pressed(s) & m.press; p&^seen != 0 {
					seen |= p
					start = s
				}
			}
			if seen != m.press {
				return 0, false
			}
		}
		if n == 0 {
			return start, true
		}
	} else if now > oldest {
		// A motion without a button completes only on the first step it fits, so holding its last direction does not repeat it.
		if _, ok := r.fit(m, now-1, -1); ok {
			return 0, false
		}
	}
	loLast := now
	if m.press != 0 {
		loLast = max(now-m.buffer, oldest)
	}
	for tl := now; tl >= loLast; tl-- {
		if t0, ok := r.fit(m, tl, r.done[i]); ok {
			return min(start, t0), true
		}
	}
	return 0, false
}

// fit returns the latest step t0 > after on which m's motion can start with its last entry on tl, within the window and after the charge.
func (r *Recognizer) fit(m *compiledMove, tl, after int) (int, bool) {
	b := r.buf
	n := len(m.sets)
	if !m.sets[n-1].has(b.dir(tl)) {
		return 0, false
	}
	lo0 := max(tl-m.window, b.Oldest(), after+1)
	if n == 1 {
		lo0 = max(tl, after+1)
	}
	for t0 := tl - (n - 1); t0 >= lo0; t0-- {
		if m.sets[0].has(b.dir(t0)) && r.middleFits(m, t0, tl) && r.chargeHeld(m, t0) {
			return t0, true
		}
	}
	return 0, false
}

// middleFits reports whether the motion entries between the first and the last match on increasing steps strictly between t0 and tl, placing each as early as possible.
func (r *Recognizer) middleFits(m *compiledMove, t0, tl int) bool {
	if len(m.sets) <= 2 {
		return true
	}
	t := t0
	for _, set := range m.sets[1 : len(m.sets)-1] {
		t++
		for t < tl && !set.has(r.buf.dir(t)) {
			t++
		}
		if t >= tl {
			return false
		}
	}
	return true
}

// chargeHeld reports whether m's charge was held for its steps, ending no more than its grace before the step that precedes t0. Moves without a charge always pass.
func (r *Recognizer) chargeHeld(m *compiledMove, t0 int) bool {
	if m.charge == 0 {
		return true
	}
	oldest := r.buf.Oldest()
	for end := t0 - 1; end >= max(t0-1-m.chargeGrace, oldest); end-- {
		run := 0
		for s := end; s >= oldest && run < m.chargeSteps && m.charge.has(r.buf.dir(s)); s-- {
			run++
		}
		if run >= m.chargeSteps {
			return true
		}
	}
	return false
}

// advanceCombos feeds the move performed on step now to every combo. A move that is not the next in a combo's sequence, or that comes more than the combo's gap after the previous one, drops that partial combo.
func (r *Recognizer) advanceCombos(move, now int) {
	for ci := range r.combos {
		c := &r.combos[ci]
		done, doneStart := false, 0
		kept := c.partial[:0]
		for _, p := range c.partial {
			if now-p.last > c.gap || c.seq[p.next] != move {
				continue
			}
			p.next++
			p.last = now
			if p.next == len(c.seq) {
				if !done || p.start > doneStart {
					doneStart = p.start
				}
				done = true
				continue
			}
			kept = appendProgress(kept, p)
		}
		if c.seq[0] == move {
			kept = appendProgress(kept, comboProgress{next: 1, start: now, last: now})
		}
		c.partial = kept
		if done {
			r.res.Combos = append(r.res.Combos, Match{Name: c.name, Index: ci, Start: doneStart, Step: now})
		}
	}
}

// appendProgress adds p unless an entry already waits for the same move, in which case the later start wins, so a combo keeps at most one partial per position.
func appendProgress(list []comboProgress, p comboProgress) []comboProgress {
	for i := range list {
		if list[i].next == p.next {
			if p.start > list[i].start {
				list[i] = p
			}
			return list
		}
	}
	return append(list, p)
}
