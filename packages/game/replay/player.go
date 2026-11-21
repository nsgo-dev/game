package replay

var _ ActionSource = (*Player)(nil)

// Player plays back a Recording as an ActionSource.
//
// Each call to Step advances one recorded step and updates the action and axis snapshots so Pressed, JustPressed, JustReleased and Axis reproduce the recorded run on every step, including transitions on step 0, multi-step holds, rapid single-step taps and axis changes.
type Player struct {
	rec        *Recording
	actionIdx  map[string]int
	axisIdx    map[string]int
	prevAction []bool
	currAction []bool
	currAxis   []float64
	step       int
	nextEvent  int
}

// NewPlayer returns a Player ready to play rec from step 0. A nil rec plays as an empty recording whose Done is immediately true.
func NewPlayer(rec *Recording) *Player {
	p := &Player{rec: rec}
	if rec == nil {
		return p
	}
	if len(rec.Actions) > 0 {
		p.actionIdx = make(map[string]int, len(rec.Actions))
		for i, name := range rec.Actions {
			p.actionIdx[name] = i
		}
		p.prevAction = make([]bool, len(rec.Actions))
		p.currAction = make([]bool, len(rec.Actions))
	}
	if len(rec.Axes) > 0 {
		p.axisIdx = make(map[string]int, len(rec.Axes))
		for i, name := range rec.Axes {
			p.axisIdx[name] = i
		}
		p.currAxis = make([]float64, len(rec.Axes))
	}
	return p
}

// Step advances playback by one recorded step and updates the action and axis snapshots.
//
// Once Done is true, further calls to Step release any actions that were still pressed on the final recorded step (firing JustReleased once) and leave all actions and axes idle.
func (p *Player) Step() {
	copy(p.prevAction, p.currAction)

	if p.rec == nil || p.step >= p.rec.TotalSteps {
		clear(p.currAction)
		clear(p.currAxis)
		return
	}

	stepIdx := p.step
	p.step++

	for p.nextEvent < len(p.rec.Events) && p.rec.Events[p.nextEvent].Step < stepIdx {
		p.nextEvent++
	}
	if p.nextEvent < len(p.rec.Events) && p.rec.Events[p.nextEvent].Step == stepIdx {
		ev := p.rec.Events[p.nextEvent]
		p.nextEvent++
		for _, idx := range ev.Press {
			if idx >= 0 && idx < len(p.currAction) {
				p.currAction[idx] = true
			}
		}
		for _, idx := range ev.Release {
			if idx >= 0 && idx < len(p.currAction) {
				p.currAction[idx] = false
			}
		}
		for _, ch := range ev.Axes {
			if ch.Index >= 0 && ch.Index < len(p.currAxis) {
				p.currAxis[ch.Index] = ch.Value
			}
		}
	}
}

// Pressed reports whether name is pressed during the current step.
func (p *Player) Pressed(name string) bool {
	idx, ok := p.actionIdx[name]
	if !ok {
		return false
	}
	return p.currAction[idx]
}

// JustPressed reports whether name became pressed on the current step and was not pressed on the previous step.
func (p *Player) JustPressed(name string) bool {
	idx, ok := p.actionIdx[name]
	if !ok {
		return false
	}
	return p.currAction[idx] && !p.prevAction[idx]
}

// JustReleased reports whether name was pressed on the previous step and is no longer pressed on the current step.
func (p *Player) JustReleased(name string) bool {
	idx, ok := p.actionIdx[name]
	if !ok {
		return false
	}
	return !p.currAction[idx] && p.prevAction[idx]
}

// Axis returns the recorded value of axis name for the current step. If name is not a recorded axis, it falls back to 1 when action name is pressed and 0 otherwise, matching input.Actions.Axis.
func (p *Player) Axis(name string) float64 {
	if idx, ok := p.axisIdx[name]; ok {
		return p.currAxis[idx]
	}
	if p.Pressed(name) {
		return 1
	}
	return 0
}

// Seed returns the random seed recorded in the underlying Recording.
func (p *Player) Seed() uint64 {
	if p.rec == nil {
		return 0
	}
	return p.rec.Seed
}

// Rate returns the fixed step rate recorded in the underlying Recording (60 when unset).
func (p *Player) Rate() int {
	if p.rec == nil || p.rec.Rate <= 0 {
		return defaultRate
	}
	return p.rec.Rate
}

// StepIndex returns the number of steps played so far, from 0 before the first Step up to TotalSteps when Done is true.
func (p *Player) StepIndex() int {
	return p.step
}

// TotalSteps returns the total number of steps in the Recording.
func (p *Player) TotalSteps() int {
	if p.rec == nil {
		return 0
	}
	return p.rec.TotalSteps
}

// Done reports whether all recorded steps have been played.
func (p *Player) Done() bool {
	return p.rec == nil || p.step >= p.rec.TotalSteps
}

// Reset rewinds playback to step 0 and clears the current and previous step snapshots.
func (p *Player) Reset() {
	clear(p.prevAction)
	clear(p.currAction)
	clear(p.currAxis)
	p.step = 0
	p.nextEvent = 0
}
