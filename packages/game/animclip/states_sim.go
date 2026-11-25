package animclip

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// InputDriver translates 2D character input actions (move_left/move_right or move_x axis, jump, attack, hurt) and simple vertical jump physics into standard state machine parameters (move.x, velocity.y, on_floor, jump, attack, hurt) on every fixed step.
// Both the editor's "Drive with keys" preview and scripted input tests use InputDriver so behavior is identical.
type InputDriver struct {
	// JumpVelocity is the initial upward velocity (negative Y) applied when jumping on the floor. Defaults to -260 when 0.
	JumpVelocity float64
	// Gravity is the downward acceleration (positive Y) applied while airborne. Defaults to 780 when 0.
	Gravity float64

	// PosY is the simulated vertical offset relative to the floor (0 = on floor, < 0 = airborne).
	PosY float64
	// VelY is the simulated vertical velocity (> 0 = falling).
	VelY float64
	// OnFloor reports whether the simulated character is standing on the floor.
	OnFloor bool

	// MoveX is the most recent horizontal movement input in [-1, 1].
	MoveX float64
}

// NewInputDriver returns an InputDriver standing on the floor with default platformer jump/gravity parameters.
func NewInputDriver() *InputDriver {
	return &InputDriver{
		JumpVelocity: -260,
		Gravity:      780,
		OnFloor:      true,
	}
}

// Reset puts the simulated character back on the floor with zero velocity.
func (d *InputDriver) Reset() {
	d.PosY = 0
	d.VelY = 0
	d.OnFloor = true
	d.MoveX = 0
}

// StepActions reads actions (stepped by the caller via actions.Step()), advances jump/fall vertical physics by dt, and feeds move.x, velocity.y, on_floor, jump, attack and hurt into sm.
func (d *InputDriver) StepActions(sm *StateMachine, actions *input.Actions, dt time.Duration) {
	if d == nil || sm == nil {
		return
	}
	var moveX float64
	var jumpPressed, attackPressed, hurtPressed bool
	if actions != nil {
		// Support either a named horizontal axis ("move_x" / "move") or left/right actions.
		if ax := actions.Axis("move_x"); ax != 0 {
			moveX = float64(ax)
		} else if ax := actions.Axis("move"); ax != 0 {
			moveX = float64(ax)
		} else {
			if actions.Pressed("move_left") || actions.Pressed("left") {
				moveX -= 1
			}
			if actions.Pressed("move_right") || actions.Pressed("right") {
				moveX += 1
			}
		}
		jumpPressed = actions.JustPressed("jump")
		attackPressed = actions.JustPressed("attack")
		hurtPressed = actions.JustPressed("hurt")
	}
	d.StepSignals(sm, moveX, jumpPressed, attackPressed, hurtPressed, dt)
}

// StepSignals advances jump/fall physics by dt from direct control signals and feeds parameters into sm.
func (d *InputDriver) StepSignals(sm *StateMachine, moveX float64, jumpPressed, attackPressed, hurtPressed bool, dt time.Duration) {
	if d == nil || sm == nil {
		return
	}
	jumpVel := d.JumpVelocity
	if jumpVel == 0 {
		jumpVel = -260
	}
	gravity := d.Gravity
	if gravity == 0 {
		gravity = 780
	}
	d.MoveX = moveX

	secs := dt.Seconds()
	if !d.OnFloor && secs > 0 {
		d.VelY += gravity * secs
		d.PosY += d.VelY * secs
		if d.PosY >= 0 {
			d.PosY = 0
			d.VelY = 0
			d.OnFloor = true
		}
	}

	sm.SetFloat("move.x", d.MoveX)
	sm.SetFloat("velocity.y", d.VelY)
	sm.SetBool("on_floor", d.OnFloor)

	if jumpPressed && d.OnFloor {
		// Feed on_floor=true on the takeoff step so "jump && on_floor" matches,
		// then launch into the air for subsequent steps.
		sm.Trigger("jump")
		d.OnFloor = false
		d.VelY = jumpVel
		d.PosY = -0.01
	}
	if attackPressed {
		sm.Trigger("attack")
	}
	if hurtPressed {
		sm.Trigger("hurt")
	}
}

// ScriptStep describes one timed input segment in a scripted simulation of a sprite state machine.
type ScriptStep struct {
	// At is when this step starts, relative to simulation start (0).
	At time.Duration `json:"at"`
	// Duration is how long held inputs (MoveX) stay active; 0 means a single-step pulse.
	Duration time.Duration `json:"duration,omitempty"`
	// MoveX holds horizontal movement input in [-1, 1] for Duration.
	MoveX float64 `json:"moveX,omitempty"`
	// Jump pulses the jump input on the first step of this segment.
	Jump bool `json:"jump,omitempty"`
	// Attack pulses the attack input on the first step of this segment.
	Attack bool `json:"attack,omitempty"`
	// Hurt pulses the hurt input on the first step of this segment.
	Hurt bool `json:"hurt,omitempty"`
	// Floats optionally sets arbitrary named float parameters for Duration.
	Floats map[string]float64 `json:"floats,omitempty"`
	// Bools optionally sets arbitrary named bool parameters for Duration.
	Bools map[string]bool `json:"bools,omitempty"`
	// Triggers optionally pulses arbitrary named trigger parameters at At.
	Triggers []string `json:"triggers,omitempty"`
}

// TransitionEvent records one state change observed during scripted simulation.
type TransitionEvent struct {
	// Time is the simulation time when the transition fired.
	Time time.Duration `json:"time"`
	// From is the state left.
	From string `json:"from"`
	// To is the state entered.
	To string `json:"to"`
}

// SimulationOptions configures SimulateStateMachine.
type SimulationOptions struct {
	// Duration is total simulated time; defaults to 8 seconds when <= 0.
	Duration time.Duration
	// Rate is fixed steps per second; defaults to 60 when <= 0.
	Rate int
	// Script are the timed inputs played during simulation; when empty, DefaultScript(Duration) is used.
	Script []ScriptStep
	// Driver optionally supplies a custom InputDriver; nil uses NewInputDriver().
	Driver *InputDriver
}

// SimulationReport summarizes a scripted simulation run of a StateMachineDoc.
type SimulationReport struct {
	// Duration is the total simulated duration.
	Duration time.Duration `json:"duration"`
	// Steps is the number of fixed steps executed.
	Steps int `json:"steps"`
	// Trace is the ordered sequence of states visited (entry state first, then each transition target).
	Trace []string `json:"trace"`
	// Transitions lists every state change in chronological order.
	Transitions []TransitionEvent `json:"transitions"`
	// VisitedStates are the sorted unique state names entered during the run.
	VisitedStates []string `json:"visitedStates"`
	// UnvisitedStates are the sorted state names declared in the document that were never entered.
	UnvisitedStates []string `json:"unvisitedStates,omitempty"`
	// StuckStates are the sorted state names that finished a non-looping clip and remained stuck without leaving, or have no outgoing transitions at all.
	StuckStates []string `json:"stuckStates,omitempty"`
}

// Summary formats a concise one-line status, e.g. "Played 8 s of scripted input · no stuck states".
func (r *SimulationReport) Summary() string {
	secs := r.Duration.Seconds()
	var durStr string
	if math.Abs(secs-math.Round(secs)) < 1e-6 {
		durStr = fmt.Sprintf("%.0f s", math.Round(secs))
	} else {
		durStr = fmt.Sprintf("%.1f s", secs)
	}
	if len(r.StuckStates) == 0 {
		return fmt.Sprintf("Played %s of scripted input · no stuck states", durStr)
	}
	noun := "stuck states"
	if len(r.StuckStates) == 1 {
		noun = "stuck state"
	}
	return fmt.Sprintf("Played %s of scripted input · %d %s: %s", durStr, len(r.StuckStates), noun, strings.Join(r.StuckStates, ", "))
}

// DefaultScript returns an 8-second scripted input sequence exercising idle, run, jump, fall, landing back into run, idle, attack and hurt.
func DefaultScript() []ScriptStep {
	return []ScriptStep{
		// 0.0s - 0.5s: idle
		// 0.5s - 2.6s: run right; jump at 1.2s (jump -> fall -> lands into run while still holding right)
		{At: 500 * time.Millisecond, Duration: 2100 * time.Millisecond, MoveX: 1},
		{At: 1200 * time.Millisecond, Jump: true},
		// 2.6s - 3.4s: released -> idle
		// 3.4s: attack -> clip ends -> idle
		{At: 3400 * time.Millisecond, Attack: true},
		// 4.5s - 5.5s: run left
		{At: 4500 * time.Millisecond, Duration: 1000 * time.Millisecond, MoveX: -1},
		// 5.8s: took damage (Any state -> hurt -> clip ends -> idle)
		{At: 5800 * time.Millisecond, Hurt: true},
		// 6.8s - 7.5s: jump from idle, land
		{At: 6800 * time.Millisecond, Jump: true},
	}
}

// NoopBinder is a Binder that accepts every track without mutating any scene, for headless state machine verification.
func NoopBinder(_ string, _ Property) (func(float64), error) {
	return func(float64) {}, nil
}

// SimulateStateMachine runs doc deterministically under opts.Script for opts.Duration at opts.Rate Hz and returns a SimulationReport detailing the visited state trace, transitions and any stuck states.
// If bind is nil, NoopBinder is used.
func SimulateStateMachine(doc *StateMachineDoc, clips ClipResolver, reg *sceneformat.ComponentRegistry, bind Binder, opts SimulationOptions) (*SimulationReport, error) {
	if bind == nil {
		bind = NoopBinder
	}
	sm, err := NewStateMachine(doc, clips, reg, bind)
	if err != nil {
		return nil, err
	}
	dur := opts.Duration
	if dur <= 0 {
		dur = 8 * time.Second
	}
	rate := opts.Rate
	if rate <= 0 {
		rate = 60
	}
	script := opts.Script
	if len(script) == 0 {
		script = DefaultScript()
	}
	driver := opts.Driver
	if driver == nil {
		driver = NewInputDriver()
	} else {
		driver.Reset()
	}

	dt := time.Second / time.Duration(rate)
	steps := int(math.Round(dur.Seconds() * float64(rate)))
	if steps < 1 {
		steps = 1
	}

	report := &SimulationReport{
		Duration: dur,
		Steps:    steps,
		Trace:    []string{sm.State()},
	}
	visited := map[string]bool{sm.State(): true}
	stuckSet := map[string]bool{}
	var simTime time.Duration

	sm.OnTransition = func(from, to string) {
		report.Trace = append(report.Trace, to)
		report.Transitions = append(report.Transitions, TransitionEvent{
			Time: simTime,
			From: from,
			To:   to,
		})
		visited[to] = true
	}

	// Track which one-shot pulses from script have fired.
	fired := make([]bool, len(script))
	// Track whether the current state has sat at Done() past a generous grace window without leaving.
	doneStreak := 0
	lastState := sm.State()
	leftAfterDone := map[string]bool{}
	stayedDone := map[string]bool{}
	stuckThreshold := max(rate, 60) // 1 second stuck at clip end without leaving

	for step := range steps {
		simTime = time.Duration(step) * dt
		var moveX float64
		var jump, attack, hurt bool
		var floatOverrides map[string]float64
		var boolOverrides map[string]bool
		var trigOverrides []string
		for i, s := range script {
			end := s.At + s.Duration
			if simTime >= s.At && (s.Duration == 0 && !fired[i] || (s.Duration > 0 && simTime < end)) {
				if s.MoveX != 0 {
					moveX = s.MoveX
				}
				for k, v := range s.Floats {
					if floatOverrides == nil {
						floatOverrides = make(map[string]float64)
					}
					floatOverrides[k] = v
				}
				for k, v := range s.Bools {
					if boolOverrides == nil {
						boolOverrides = make(map[string]bool)
					}
					boolOverrides[k] = v
				}
			}
			if !fired[i] && simTime >= s.At {
				fired[i] = true
				if s.Jump {
					jump = true
				}
				if s.Attack {
					attack = true
				}
				if s.Hurt {
					hurt = true
				}
				trigOverrides = append(trigOverrides, s.Triggers...)
			}
		}

		// Run InputDriver first when standard locomotion parameters exist, then apply any explicit parameter overrides on top.
		if hasDriverParams(doc) {
			driver.StepSignals(sm, moveX, jump, attack, hurt, dt)
		}
		for k, v := range floatOverrides {
			sm.SetFloat(k, v)
		}
		for k, v := range boolOverrides {
			sm.SetBool(k, v)
		}
		for _, trig := range trigOverrides {
			sm.Trigger(trig)
		}

		sm.Update(dt)
		cur := sm.State()
		if cur != lastState {
			if doneStreak > 0 {
				leftAfterDone[lastState] = true
			}
			doneStreak = 0
			lastState = cur
		} else if sm.StateDone() {
			doneStreak++
			if doneStreak >= stuckThreshold {
				stayedDone[cur] = true
			}
		} else {
			doneStreak = 0
		}
	}
	// If the simulation ended while stuck on a finished non-looping clip (even for < 1s), flag it.
	if doneStreak >= 2 {
		stayedDone[lastState] = true
	}
	for st := range stayedDone {
		if !leftAfterDone[st] {
			stuckSet[st] = true
		}
	}

	// Also flag any state that has no outgoing transitions at all (including Any-state transitions to other states).
	hasAnyOther := func(stName string) bool {
		for _, tr := range doc.Transitions {
			if tr.From == stName || (tr.From == AnyState && tr.To != stName) {
				return true
			}
		}
		return false
	}
	for _, st := range doc.States {
		if visited[st.Name] {
			report.VisitedStates = append(report.VisitedStates, st.Name)
		} else {
			report.UnvisitedStates = append(report.UnvisitedStates, st.Name)
		}
		if len(doc.States) > 1 && !hasAnyOther(st.Name) {
			stuckSet[st.Name] = true
		}
	}
	slices.Sort(report.VisitedStates)
	slices.Sort(report.UnvisitedStates)
	for st := range stuckSet {
		report.StuckStates = append(report.StuckStates, st)
	}
	slices.Sort(report.StuckStates)
	return report, nil
}

func hasDriverParams(doc *StateMachineDoc) bool {
	for _, p := range doc.Parameters {
		switch p.Name {
		case "move.x", "velocity.y", "on_floor", "jump", "attack", "hurt":
			return true
		}
	}
	return false
}
