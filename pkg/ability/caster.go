package ability

import (
	"fmt"
	"math"
	"math/bits"
	"time"
)

// MaxRate is the highest step rate NewCaster accepts, in steps a second.
const MaxRate = 1_000_000

// Actions is the input a Caster reads each step: the named actions an input.Actions samples once per fixed game.Loop step. *input.Actions satisfies it, and so can a replay or network input source.
type Actions interface {
	// Pressed reports whether the action is held on this step.
	Pressed(name string) bool
	// JustPressed reports whether the action became held on this step.
	JustPressed(name string) bool
}

// Resources holds what abilities spend. A Caster checks Amount before it starts an ability and again before it spends, and calls Spend only after every cost of the use is affordable.
type Resources interface {
	// Amount returns how much of resource is available.
	Amount(resource string) float64
	// Spend takes amount of resource away.
	Spend(resource string, amount float64)
}

// Pool is a Resources kept in a map from resource name to amount. A missing resource has 0.
type Pool map[string]float64

// Amount returns p[resource].
func (p Pool) Amount(resource string) float64 { return p[resource] }

// Spend subtracts amount from p[resource].
func (p Pool) Spend(resource string, amount float64) { p[resource] -= amount }

// Phase is what an ability is doing.
type Phase string

const (
	// PhaseIdle is an ability that is neither casting nor channeling.
	PhaseIdle Phase = "idle"
	// PhaseCasting is an ability counting down its cast time; nothing is spent yet.
	PhaseCasting Phase = "casting"
	// PhaseChanneling is an ability that took effect and is channeling.
	PhaseChanneling Phase = "channeling"
)

// Reason says why an ability could not be activated or why its cast stopped. ReasonOK means it could.
type Reason uint8

const (
	// ReasonOK means the ability was (or can be) activated.
	ReasonOK Reason = iota
	// ReasonUnknown means no ability in the set has the ID.
	ReasonUnknown
	// ReasonBusy means an ability is already casting or channeling.
	ReasonBusy
	// ReasonCooldown means the ability's cooldown is running.
	ReasonCooldown
	// ReasonNoCharges means every charge of the ability is spent.
	ReasonNoCharges
	// ReasonCondition means one of the ability's conditions does not hold.
	ReasonCondition
	// ReasonCost means a resource the ability costs is short.
	ReasonCost
)

var reasonNames = [...]string{"ok", "unknown", "busy", "cooldown", "no charges", "condition", "cost"}

// String returns a short lower-case name for r.
func (r Reason) String() string {
	if int(r) < len(reasonNames) {
		return reasonNames[r]
	}
	return fmt.Sprintf("Reason(%d)", uint8(r))
}

// EventKind says what happened to an ability.
type EventKind uint8

const (
	// EventStarted is sent when an ability is activated and starts casting. An ability with no cast time sends it on the same step as EventActivated.
	EventStarted EventKind = iota + 1
	// EventActivated is sent when an ability's cast completes and it takes effect: its costs are spent, a charge is used and its cooldown starts. This is where a game applies the ability's effect.
	EventActivated
	// EventChannelTick is sent each TickInterval of a channel; Event.Tick counts the ticks from 1.
	EventChannelTick
	// EventChannelEnded is sent when a channel runs its full ChannelTime.
	EventChannelEnded
	// EventCanceled is sent when Caster.Cancel stops a cast or channel, or a TriggerHold ability's action is released.
	EventCanceled
	// EventInterrupted is sent when a cast or channel stops because a condition no longer holds (Event.Reason is ReasonCondition) or a cost is no longer affordable when the cast completes (ReasonCost).
	EventInterrupted
	// EventCooldownEnded is sent on the step an ability's cooldown reaches zero.
	EventCooldownEnded
	// EventChargeRestored is sent on the step a spent charge comes back.
	EventChargeRestored
)

var eventNames = [...]string{"", "started", "activated", "channel tick", "channel ended", "canceled", "interrupted", "cooldown ended", "charge restored"}

// String returns a short lower-case name for k.
func (k EventKind) String() string {
	if k > 0 && int(k) < len(eventNames) {
		return eventNames[k]
	}
	return fmt.Sprintf("EventKind(%d)", uint8(k))
}

// Event reports one change to an ability's state.
type Event struct {
	// Kind is what happened.
	Kind EventKind
	// Ability is the ability's ID.
	Ability string
	// Step is the Caster's step count when it happened: the number of Update calls so far, including the one running.
	Step uint64
	// Reason is why an EventInterrupted happened, and ReasonOK for every other kind.
	Reason Reason
	// Tick counts an EventChannelTick's ticks within its channel, from 1; it is 0 for every other kind.
	Tick int
}

// Status is an ability's state at the current step. Times are in the Caster's steps; Caster.Duration turns them into game time for display.
type Status struct {
	// Charges is how many uses are stored now, and MaxCharges the most it stores.
	Charges, MaxCharges int
	// Cooldown is how many steps of cooldown are left, out of CooldownTotal for a full cooldown.
	Cooldown, CooldownTotal int
	// Recharge is how many steps are left until the next charge comes back, out of RechargeTotal for one charge; 0 when no charge is spent.
	Recharge, RechargeTotal int
	// Phase is PhaseCasting or PhaseChanneling while the ability is the active one, and PhaseIdle otherwise.
	Phase Phase
	// Elapsed is how many steps of Phase have run, out of PhaseTotal; both are 0 while idle.
	Elapsed, PhaseTotal int
}

// Ready reports whether the ability is idle with a charge and no cooldown. It does not check conditions, costs or whether another ability is busy; Caster.CanActivate does.
func (s Status) Ready() bool {
	return s.Phase == PhaseIdle && s.Charges > 0 && s.Cooldown == 0
}

type condition struct {
	name    string
	negated bool
}

type slot struct {
	id         string
	action     string
	hold       bool
	costs      []Cost
	conditions []condition
	group      int

	maxCharges    int
	cooldownSteps int
	rechargeSteps int
	castSteps     int
	channelSteps  int
	tickSteps     int

	charges  int
	cooldown int
	recharge int
}

// Caster runs one character's abilities, one fixed step at a time.
//
// Every duration in its Set becomes a whole number of steps at its rate, rounded up, and every timer counts whole steps, so the same inputs on the same steps produce the same events on the same steps on any machine. Call Update once per game.Loop step, after stepping the input.Actions it reads.
//
// At most one ability casts or channels at a time. Its methods, and OnEvent, run on the goroutine that calls Update, as Loop.Update does.
type Caster struct {
	// Resources pays ability costs. When nil, an ability with costs cannot be activated.
	Resources Resources
	// Condition reports whether a named condition holds, such as "grounded" or "stunned". When nil, every condition is false, so an ability that needs one cannot be activated and an ability that needs "!name" can.
	Condition func(name string) bool
	// OnEvent, when set, receives every Event in the order it happened. It may call the Caster's methods, including TryActivate and Cancel; events those cause follow the current one. Events that happened before a Reset or Restore called from OnEvent are still delivered, and when that call is made during Update, Update goes on to read input against the reset or restored state.
	OnEvent func(Event)

	rate    int
	step    uint64
	slots   []slot
	index   map[string]int
	active  int
	phase   Phase
	elapsed int

	groupDone []bool
	queue     []Event
	flushing  bool
}

// NewCaster returns a Caster for set at rate steps a second, with every ability idle, charged and off cooldown. A rate of 0 or less means 60, as game.Loop's Rate does. The Caster keeps its own copy of set.
func NewCaster(set *Set, rate int) (*Caster, error) {
	if err := Validate(set); err != nil {
		return nil, err
	}
	if rate <= 0 {
		rate = 60
	}
	if rate > MaxRate {
		return nil, fmt.Errorf("ability: rate %d is more than %d", rate, MaxRate)
	}
	c := &Caster{
		rate:   rate,
		slots:  make([]slot, len(set.Abilities)),
		index:  make(map[string]int, len(set.Abilities)),
		active: -1,
		phase:  PhaseIdle,
	}
	groups := make(map[string]int)
	for i := range set.Abilities {
		d := &set.Abilities[i]
		s := &c.slots[i]
		s.id = d.ID
		s.action = d.Action
		s.hold = d.EffectiveTrigger() == TriggerHold
		s.costs = append([]Cost(nil), d.Costs...)
		for _, cond := range d.Conditions {
			name, neg := splitCondition(cond)
			s.conditions = append(s.conditions, condition{name: name, negated: neg})
		}
		s.group = -1
		if d.Action != "" {
			g, ok := groups[d.Action]
			if !ok {
				g = len(groups)
				groups[d.Action] = g
			}
			s.group = g
		}
		s.maxCharges = d.MaxChargesOrOne()
		s.cooldownSteps = stepsFor(d.Cooldown, rate)
		s.rechargeSteps = stepsFor(d.Recharge, rate)
		s.castSteps = stepsFor(d.CastTime, rate)
		s.channelSteps = stepsFor(d.ChannelTime, rate)
		s.tickSteps = stepsFor(d.TickInterval, rate)
		s.charges = s.maxCharges
		c.index[d.ID] = i
	}
	c.groupDone = make([]bool, len(groups))
	return c, nil
}

// stepsFor returns d in whole steps at rate, rounded up. Validate bounds d by MaxDuration and NewCaster bounds rate by MaxRate, so the product fits in 128 bits with a quotient that fits in an int.
func stepsFor(d time.Duration, rate int) int {
	if d <= 0 {
		return 0
	}
	hi, lo := bits.Mul64(uint64(d), uint64(rate))
	q, r := bits.Div64(hi, lo, uint64(time.Second))
	if r != 0 {
		q++
	}
	return int(q)
}

// Rate returns the Caster's steps a second.
func (c *Caster) Rate() int { return c.rate }

// Step returns how many times Update has run.
func (c *Caster) Step() uint64 { return c.step }

// Steps returns d in the Caster's whole steps, rounded up, as it counts every duration in its Set.
func (c *Caster) Steps(d time.Duration) int {
	if d > MaxDuration {
		d = MaxDuration
	}
	return stepsFor(d, c.rate)
}

// Duration returns how much game time steps of the Caster's fixed step last, rounded down to a nanosecond, for showing a cooldown or cast bar. A negative step count gives 0, and a count longer than the largest time.Duration gives that largest Duration.
func (c *Caster) Duration(steps int) time.Duration {
	if steps <= 0 {
		return 0
	}
	// Whole seconds and the remainder separately, so steps * time.Second never overflows: the remainder is below rate, which is at most MaxRate.
	secs, rem := int64(steps)/int64(c.rate), int64(steps)%int64(c.rate)
	if secs > math.MaxInt64/int64(time.Second)-1 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(secs*int64(time.Second) + rem*int64(time.Second)/int64(c.rate))
}

// Update advances the Caster by one fixed step.
//
// In order, it: counts the step; advances every ability's cooldown and recharge, in set order; advances the active cast or channel, interrupting it if a condition no longer holds, canceling a TriggerHold ability whose action is no longer held, and completing a cast or ending a channel whose time is up; then, when in is not nil, tries each ability whose action was just pressed (TriggerPress) or is held (TriggerHold), in set order, activating at most one ability per action. So an ability whose cooldown ends on a step can be activated again on that same step.
func (c *Caster) Update(in Actions) {
	c.step++
	for i := range c.slots {
		s := &c.slots[i]
		if s.cooldown > 0 {
			s.cooldown--
			if s.cooldown == 0 {
				c.emit(Event{Kind: EventCooldownEnded, Ability: s.id})
			}
		}
		if s.recharge > 0 {
			s.recharge--
			if s.recharge == 0 {
				s.charges++
				if s.charges < s.maxCharges {
					s.recharge = s.rechargeSteps
				}
				c.emit(Event{Kind: EventChargeRestored, Ability: s.id})
			}
		}
	}
	if c.active >= 0 {
		c.advanceActive(in)
	}
	c.flush()
	if in == nil {
		return
	}
	clear(c.groupDone)
	for i := range c.slots {
		s := &c.slots[i]
		if s.group < 0 || c.groupDone[s.group] {
			continue
		}
		var want bool
		if s.hold {
			want = in.Pressed(s.action)
		} else {
			want = in.JustPressed(s.action)
		}
		if want && c.activate(i) == ReasonOK {
			c.groupDone[s.group] = true
		}
	}
	c.flush()
}

func (c *Caster) advanceActive(in Actions) {
	i := c.active
	s := &c.slots[i]
	if s.hold && in != nil && !in.Pressed(s.action) {
		c.stop()
		c.emit(Event{Kind: EventCanceled, Ability: s.id})
		return
	}
	if !c.conditionsHold(s) {
		c.stop()
		c.emit(Event{Kind: EventInterrupted, Ability: s.id, Reason: ReasonCondition})
		return
	}
	c.elapsed++
	switch c.phase {
	case PhaseCasting:
		if c.elapsed >= s.castSteps {
			c.complete(i)
		}
	case PhaseChanneling:
		if s.tickSteps > 0 && c.elapsed%s.tickSteps == 0 {
			c.emit(Event{Kind: EventChannelTick, Ability: s.id, Tick: c.elapsed / s.tickSteps})
		}
		if c.elapsed >= s.channelSteps {
			c.stop()
			c.emit(Event{Kind: EventChannelEnded, Ability: s.id})
		}
	}
}

// complete spends the active ability's costs and a charge, starts its cooldown and moves it to channeling or idle.
func (c *Caster) complete(i int) {
	s := &c.slots[i]
	if !c.affordable(s) {
		c.stop()
		c.emit(Event{Kind: EventInterrupted, Ability: s.id, Reason: ReasonCost})
		return
	}
	for _, cost := range s.costs {
		c.Resources.Spend(cost.Resource, cost.Amount)
	}
	if s.rechargeSteps > 0 {
		if s.charges == s.maxCharges {
			s.recharge = s.rechargeSteps
		}
		s.charges--
	}
	s.cooldown = s.cooldownSteps
	if s.channelSteps > 0 {
		c.phase = PhaseChanneling
		c.elapsed = 0
	} else {
		c.stop()
	}
	c.emit(Event{Kind: EventActivated, Ability: s.id})
}

func (c *Caster) stop() {
	c.active = -1
	c.phase = PhaseIdle
	c.elapsed = 0
}

// CanActivate reports whether TryActivate(id) would activate the ability now, and if not, the first reason it would not, checked in this order: ReasonUnknown, ReasonBusy, ReasonCooldown, ReasonNoCharges, ReasonCondition, ReasonCost. It changes nothing.
func (c *Caster) CanActivate(id string) Reason {
	i, ok := c.index[id]
	if !ok {
		return ReasonUnknown
	}
	return c.check(i)
}

func (c *Caster) check(i int) Reason {
	s := &c.slots[i]
	switch {
	case c.active >= 0:
		return ReasonBusy
	case s.cooldown > 0:
		return ReasonCooldown
	case s.charges <= 0:
		return ReasonNoCharges
	case !c.conditionsHold(s):
		return ReasonCondition
	case !c.affordable(s):
		return ReasonCost
	}
	return ReasonOK
}

// TryActivate activates the ability id if CanActivate allows it, and returns the reason it did not otherwise. An activated ability starts casting; one with no cast time takes effect at once, before TryActivate returns, spending its costs and a charge and starting its cooldown.
func (c *Caster) TryActivate(id string) Reason {
	i, ok := c.index[id]
	if !ok {
		return ReasonUnknown
	}
	r := c.activate(i)
	c.flush()
	return r
}

func (c *Caster) activate(i int) Reason {
	if r := c.check(i); r != ReasonOK {
		return r
	}
	s := &c.slots[i]
	c.active = i
	c.phase = PhaseCasting
	c.elapsed = 0
	c.emit(Event{Kind: EventStarted, Ability: s.id})
	if s.castSteps == 0 {
		c.complete(i)
	}
	return ReasonOK
}

// Cancel stops the active cast or channel and sends EventCanceled. A canceled cast spends nothing; a canceled channel keeps what its cast spent. It reports whether an ability was active.
func (c *Caster) Cancel() bool {
	if c.active < 0 {
		return false
	}
	id := c.slots[c.active].id
	c.stop()
	c.emit(Event{Kind: EventCanceled, Ability: id})
	c.flush()
	return true
}

// Active returns the ID, phase and elapsed steps of the ability casting or channeling now, or ok false when none is.
func (c *Caster) Active() (id string, phase Phase, elapsed int, ok bool) {
	if c.active < 0 {
		return "", PhaseIdle, 0, false
	}
	return c.slots[c.active].id, c.phase, c.elapsed, true
}

// Status returns the state of the ability id, or ok false when the set has no such ability.
func (c *Caster) Status(id string) (st Status, ok bool) {
	i, ok := c.index[id]
	if !ok {
		return Status{}, false
	}
	s := &c.slots[i]
	st = Status{
		Charges:       s.charges,
		MaxCharges:    s.maxCharges,
		Cooldown:      s.cooldown,
		CooldownTotal: s.cooldownSteps,
		Recharge:      s.recharge,
		RechargeTotal: s.rechargeSteps,
		Phase:         PhaseIdle,
	}
	if c.active == i {
		st.Phase = c.phase
		st.Elapsed = c.elapsed
		if c.phase == PhaseCasting {
			st.PhaseTotal = s.castSteps
		} else {
			st.PhaseTotal = s.channelSteps
		}
	}
	return st, true
}

// Reset returns every ability to idle, fully charged and off cooldown, without sending events. The step count is kept. Called from OnEvent, it still delivers the events that happened before it, after the current one.
func (c *Caster) Reset() {
	c.stop()
	for i := range c.slots {
		s := &c.slots[i]
		s.charges = s.maxCharges
		s.cooldown = 0
		s.recharge = 0
	}
}

func (c *Caster) conditionsHold(s *slot) bool {
	for _, cond := range s.conditions {
		holds := c.Condition != nil && c.Condition(cond.name)
		if holds == cond.negated {
			return false
		}
	}
	return true
}

func (c *Caster) affordable(s *slot) bool {
	if len(s.costs) == 0 {
		return true
	}
	if c.Resources == nil {
		return false
	}
	for _, cost := range s.costs {
		if c.Resources.Amount(cost.Resource) < cost.Amount {
			return false
		}
	}
	return true
}

func (c *Caster) emit(e Event) {
	e.Step = c.step
	c.queue = append(c.queue, e)
}

// flush delivers queued events in order. Events that OnEvent causes are appended to the queue and delivered by the same loop, after the one that caused them.
func (c *Caster) flush() {
	if c.flushing {
		return
	}
	c.flushing = true
	for i := 0; i < len(c.queue); i++ {
		if c.OnEvent != nil {
			c.OnEvent(c.queue[i])
		}
	}
	c.queue = c.queue[:0]
	c.flushing = false
}
