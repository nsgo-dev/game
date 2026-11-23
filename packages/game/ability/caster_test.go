package ability_test

import (
	"encoding/json"
	"fmt"
	"maps"
	mathrand "math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/ability"
	"github.com/nsgo-dev/game/packages/game/input"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"go.yaml.in/yaml/v3"
)

// *input.Actions is what games pass to Update.
var _ ability.Actions = (*input.Actions)(nil)

// keys is a scripted ability.Actions: set held before each Update, and it derives just-pressed from the previous step.
type keys struct {
	held, prev map[string]bool
}

func newKeys() *keys { return &keys{held: map[string]bool{}, prev: map[string]bool{}} }

func (k *keys) Pressed(name string) bool     { return k.held[name] }
func (k *keys) JustPressed(name string) bool { return k.held[name] && !k.prev[name] }

// step runs one Update with the actions in down held.
func (k *keys) step(c *ability.Caster, down ...string) {
	clear(k.held)
	for _, n := range down {
		k.held[n] = true
	}
	c.Update(k)
	clear(k.prev)
	for n, v := range k.held {
		k.prev[n] = v
	}
}

type recorder struct{ events []ability.Event }

func (r *recorder) on(e ability.Event) { r.events = append(r.events, e) }

func (r *recorder) take() []string {
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = fmtEvent(e)
	}
	r.events = r.events[:0]
	return out
}

func fmtEvent(e ability.Event) string {
	s := fmt.Sprintf("%d %s %s", e.Step, e.Ability, e.Kind)
	if e.Reason != ability.ReasonOK {
		s += " (" + e.Reason.String() + ")"
	}
	if e.Tick != 0 {
		s += fmt.Sprintf(" #%d", e.Tick)
	}
	return s
}

func newCaster(t testing.TB, doc string, rate int) (*ability.Caster, *recorder) {
	t.Helper()
	set, err := ability.Parse([]byte("version: 1\nabilities:\n" + doc))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ability.NewCaster(set, rate)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	c.OnEvent = rec.on
	return c, rec
}

func expectEvents(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s: events\n%s\nwant\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func status(t *testing.T, c *ability.Caster, id string) ability.Status {
	t.Helper()
	st, ok := c.Status(id)
	if !ok {
		t.Fatalf("no status for %q", id)
	}
	return st
}

func TestStepsRoundUpAndMatchLoopRate(t *testing.T) {
	c, _ := newCaster(t, "  - id: a\n", 0)
	if c.Rate() != 60 {
		t.Fatalf("Rate() = %d, want 60", c.Rate())
	}
	for _, tc := range []struct {
		d    time.Duration
		want int
	}{
		{0, 0}, {-time.Second, 0}, {1, 1}, {2 * time.Second, 120}, {100 * time.Millisecond, 6}, {time.Second / 60, 1}, {time.Second/60 + 1, 2}, {24 * time.Hour, 5_184_000},
	} {
		if got := c.Steps(tc.d); got != tc.want {
			t.Errorf("Steps(%v) = %d, want %d", tc.d, got, tc.want)
		}
	}
	if got := c.Duration(120); got != 2*time.Second {
		t.Errorf("Duration(120) = %v, want 2s", got)
	}
	if _, err := ability.NewCaster(&ability.Set{Version: 1}, ability.MaxRate+1); err == nil {
		t.Error("NewCaster accepted a rate above MaxRate")
	}
	if _, err := ability.NewCaster(&ability.Set{Version: 3}, 60); err == nil {
		t.Error("NewCaster accepted an invalid set")
	}
}

func TestCooldownLastsExactlyItsStepsAtSixtyHertz(t *testing.T) {
	// game.Loop's 60 Hz dt is 16_666_666ns, so summing dt would need 121 steps to reach 2s; counting whole steps takes exactly 120.
	c, rec := newCaster(t, "  - id: bolt\n    action: fire\n    cooldown: 2s\n", 60)
	k := newKeys()
	k.step(c, "fire")
	expectEvents(t, "first press", rec.take(), "1 bolt started", "1 bolt activated")
	if st := status(t, c, "bolt"); st.Cooldown != 120 || st.CooldownTotal != 120 || st.Ready() {
		t.Fatalf("status after use = %+v", st)
	}
	// Holding the key, or pressing it during the cooldown, does nothing.
	for step := 2; step < 121; step++ {
		if step%2 == 1 {
			k.step(c, "fire")
		} else {
			k.step(c)
		}
	}
	expectEvents(t, "during cooldown", rec.take())
	if got := c.CanActivate("bolt"); got != ability.ReasonCooldown {
		t.Fatalf("CanActivate during cooldown = %v", got)
	}
	// Step 121 is 120 steps after the use: the cooldown ends and the same step's press activates.
	k.step(c, "fire")
	expectEvents(t, "step 121", rec.take(), "121 bolt cooldown ended", "121 bolt started", "121 bolt activated")
}

func TestChargesRechargeOneAtATime(t *testing.T) {
	// 50 Hz: 4s recharge = 200 steps, 100ms cooldown = 5 steps.
	c, rec := newCaster(t, "  - id: dash\n    action: dash\n    cooldown: 100ms\n    charges: 3\n    recharge: 4s\n", 50)
	k := newKeys()
	run := func(n int, down ...string) {
		for range n {
			k.step(c, down...)
			k.step(c)
		}
	}
	run(1, "dash") // step 1
	expectEvents(t, "first dash", rec.take(), "1 dash started", "1 dash activated")
	// Each run is a press step and a release step, so one step of the timers has already run.
	if st := status(t, c, "dash"); st.Charges != 2 || st.Recharge != 199 || st.Cooldown != 4 {
		t.Fatalf("after first dash: %+v", st)
	}
	// Presses on steps 3 and 5 hit the 5-step cooldown, which ends on step 6.
	run(2, "dash")
	expectEvents(t, "cooldown between charges", rec.take(), "6 dash cooldown ended")
	run(1, "dash") // step 7
	run(3)         // steps 9..14
	run(1, "dash") // step 15
	expectEvents(t, "second and third dash", rec.take(), "7 dash started", "7 dash activated", "12 dash cooldown ended", "15 dash started", "15 dash activated")
	if st := status(t, c, "dash"); st.Charges != 0 || st.Recharge != 185 {
		t.Fatalf("after third dash: %+v", st)
	}
	if got := c.CanActivate("dash"); got != ability.ReasonCooldown {
		t.Fatalf("CanActivate during cooldown = %v", got)
	}
	for c.Step() < 20 {
		k.step(c)
	}
	if got := c.CanActivate("dash"); got != ability.ReasonNoCharges {
		t.Fatalf("CanActivate with no charges = %v", got)
	}
	// The first charge returns 200 steps after the first dash (step 201), the next 200 steps later: charges come back one at a time.
	for c.Step() < 600 {
		k.step(c)
	}
	expectEvents(t, "recharge", rec.take(), "20 dash cooldown ended", "201 dash charge restored", "401 dash charge restored")
	if st := status(t, c, "dash"); st.Charges != 2 || st.Recharge != 1 {
		t.Fatalf("at step 600: %+v", st)
	}
	k.step(c)
	expectEvents(t, "full", rec.take(), "601 dash charge restored")
	if st := status(t, c, "dash"); st.Charges != 3 || st.Recharge != 0 || !st.Ready() {
		t.Fatalf("full again: %+v", st)
	}
}

func TestSpendingWhileRechargingKeepsTheRunningRecharge(t *testing.T) {
	c, rec := newCaster(t, "  - id: dash\n    charges: 2\n    recharge: 100ms\n", 100)
	if r := c.TryActivate("dash"); r != ability.ReasonOK {
		t.Fatal(r)
	}
	for range 4 {
		c.Update(nil)
	}
	c.TryActivate("dash")
	if st := status(t, c, "dash"); st.Charges != 0 || st.Recharge != 6 {
		t.Fatalf("after second use: %+v", st)
	}
	for range 16 {
		c.Update(nil)
	}
	expectEvents(t, "two charges", rec.take(), "0 dash started", "0 dash activated", "4 dash started", "4 dash activated", "10 dash charge restored", "20 dash charge restored")
}

func TestCastSpendsOnlyWhenItCompletes(t *testing.T) {
	c, rec := newCaster(t, "  - id: fireball\n    action: cast\n    costs: [{resource: mana, amount: 20}]\n    cooldown: 1s\n    castTime: 100ms\n", 50)
	mana := ability.Pool{"mana": 30}
	c.Resources = mana
	k := newKeys()
	k.step(c, "cast")
	if id, phase, elapsed, ok := c.Active(); !ok || id != "fireball" || phase != ability.PhaseCasting || elapsed != 0 {
		t.Fatalf("Active() = %q %q %d %v", id, phase, elapsed, ok)
	}
	for range 4 {
		k.step(c)
	}
	if mana["mana"] != 30 {
		t.Fatalf("mana spent before the cast completed: %v", mana["mana"])
	}
	if st := status(t, c, "fireball"); st.Phase != ability.PhaseCasting || st.Elapsed != 4 || st.PhaseTotal != 5 {
		t.Fatalf("casting status = %+v", st)
	}
	if got := c.CanActivate("fireball"); got != ability.ReasonBusy {
		t.Fatalf("CanActivate while casting = %v", got)
	}
	k.step(c)
	expectEvents(t, "cast", rec.take(), "1 fireball started", "6 fireball activated")
	if mana["mana"] != 10 {
		t.Fatalf("mana after cast = %v, want 10", mana["mana"])
	}
	if _, _, _, ok := c.Active(); ok {
		t.Fatal("still active after the cast completed")
	}

	// A canceled cast spends nothing and starts no cooldown.
	c.Reset()
	mana["mana"] = 30
	c.TryActivate("fireball")
	k.step(c)
	if !c.Cancel() || c.Cancel() {
		t.Fatal("Cancel should succeed once")
	}
	if st := status(t, c, "fireball"); mana["mana"] != 30 || st.Cooldown != 0 || !st.Ready() {
		t.Fatalf("after cancel: mana %v status %+v", mana["mana"], st)
	}
	expectEvents(t, "cancel", rec.take(), "6 fireball started", "7 fireball canceled")

	// Short of mana: refused at the start, and interrupted at completion if it ran out meanwhile.
	mana["mana"] = 5
	if r := c.TryActivate("fireball"); r != ability.ReasonCost {
		t.Fatalf("TryActivate short of mana = %v", r)
	}
	mana["mana"] = 25
	c.TryActivate("fireball")
	mana["mana"] = 15
	for range 5 {
		k.step(c)
	}
	expectEvents(t, "out of mana", rec.take(), "7 fireball started", "12 fireball interrupted (cost)")
	if mana["mana"] != 15 {
		t.Fatalf("interrupted cast spent mana: %v", mana["mana"])
	}
	c.Resources = nil
	if r := c.TryActivate("fireball"); r != ability.ReasonCost {
		t.Fatalf("TryActivate with nil Resources = %v", r)
	}
}

func TestCostsAreAllOrNothing(t *testing.T) {
	c, _ := newCaster(t, "  - id: a\n    costs: [{resource: mana, amount: 5}, {resource: rage, amount: 10}]\n", 60)
	pool := ability.Pool{"mana": 10, "rage": 9}
	c.Resources = pool
	if r := c.TryActivate("a"); r != ability.ReasonCost || pool["mana"] != 10 {
		t.Fatalf("TryActivate = %v, mana %v", r, pool["mana"])
	}
	pool["rage"] = 10
	if r := c.TryActivate("a"); r != ability.ReasonOK || pool["mana"] != 5 || pool["rage"] != 0 {
		t.Fatalf("TryActivate = %v, pool %v", r, pool)
	}
}

func TestConditionsGateStartAndInterruptCasts(t *testing.T) {
	c, rec := newCaster(t, "  - id: smash\n    castTime: 100ms\n    conditions: [grounded, '!stunned']\n", 50)
	if r := c.TryActivate("smash"); r != ability.ReasonCondition {
		t.Fatalf("nil Condition: TryActivate = %v, want condition", r)
	}
	state := map[string]bool{"grounded": true}
	c.Condition = func(name string) bool { return state[name] }
	if r := c.TryActivate("smash"); r != ability.ReasonOK {
		t.Fatalf("TryActivate = %v", r)
	}
	c.Update(nil)
	c.Update(nil)
	state["stunned"] = true
	c.Update(nil)
	expectEvents(t, "stunned", rec.take(), "0 smash started", "3 smash interrupted (condition)")
	if r := c.TryActivate("smash"); r != ability.ReasonCondition {
		t.Fatalf("stunned: TryActivate = %v", r)
	}
	if r := c.CanActivate("nope"); r != ability.ReasonUnknown {
		t.Fatalf("CanActivate unknown = %v", r)
	}
	if r := c.TryActivate("nope"); r != ability.ReasonUnknown {
		t.Fatalf("TryActivate unknown = %v", r)
	}
	if _, ok := c.Status("nope"); ok {
		t.Fatal("Status of unknown ability ok")
	}
}

func TestChannelTicksAndHoldRelease(t *testing.T) {
	c, rec := newCaster(t, "  - id: drain\n    action: drain\n    trigger: hold\n    cooldown: 1s\n    channelTime: 100ms\n    tickInterval: 40ms\n", 50)
	k := newKeys()
	// channelTime 5 steps, tickInterval 2 steps: ticks on channel steps 2 and 4, then it ends on step 5.
	for range 7 {
		k.step(c, "drain")
	}
	expectEvents(t, "full channel", rec.take(), "1 drain started", "1 drain activated", "3 drain channel tick #1", "5 drain channel tick #2", "6 drain channel ended")
	if st := status(t, c, "drain"); st.Cooldown != 44 {
		t.Fatalf("cooldown after channel = %+v", st)
	}

	// Releasing the action cancels the channel on the step it is no longer held.
	c.Reset()
	k.step(c, "drain")
	k.step(c, "drain")
	k.step(c)
	expectEvents(t, "release", rec.take(), "8 drain started", "8 drain activated", "10 drain canceled")
}

func TestOneAbilityPerActionInSetOrder(t *testing.T) {
	doc := "  - id: air-kick\n    action: attack\n    conditions: ['!grounded']\n" +
		"  - id: punch\n    action: attack\n    cooldown: 100ms\n" +
		"  - id: kick\n    action: attack\n"
	c, rec := newCaster(t, doc, 50)
	grounded := true
	c.Condition = func(string) bool { return grounded }
	k := newKeys()
	k.step(c, "attack")
	k.step(c)
	k.step(c, "attack")
	grounded = false
	k.step(c)
	k.step(c, "attack")
	expectEvents(t, "attack", rec.take(), "1 punch started", "1 punch activated", "3 kick started", "3 kick activated", "5 air-kick started", "5 air-kick activated")
}

func TestOnEventMayChainAbilities(t *testing.T) {
	c, rec := newCaster(t, "  - id: a\n    castTime: 20ms\n  - id: b\n", 50)
	c.OnEvent = func(e ability.Event) {
		rec.on(e)
		if e.Kind == ability.EventActivated && e.Ability == "a" {
			if r := c.TryActivate("b"); r != ability.ReasonOK {
				t.Errorf("chained TryActivate = %v", r)
			}
		}
	}
	c.TryActivate("a")
	c.Update(nil)
	expectEvents(t, "chain", rec.take(), "0 a started", "1 a activated", "1 b started", "1 b activated")
}

func TestEventAndReasonNames(t *testing.T) {
	if ability.EventChargeRestored.String() != "charge restored" || ability.EventKind(0).String() != "EventKind(0)" {
		t.Fatal("EventKind.String")
	}
	if ability.ReasonNoCharges.String() != "no charges" || ability.Reason(99).String() != "Reason(99)" {
		t.Fatal("Reason.String")
	}
}

// script drives a Caster from a seeded PRNG so two runs see identical inputs on identical steps.
type script struct {
	rng   *mathrand.Rand
	keys  *keys
	drain bool
}

func (s *script) step(c *ability.Caster, pool ability.Pool, cond map[string]bool) {
	var down []string
	for _, a := range []string{"cast", "dash"} {
		if s.rng.IntN(4) == 0 {
			down = append(down, a)
		}
	}
	// The drain channel needs its hold trigger held for long runs.
	if s.rng.IntN(150) == 0 {
		s.drain = !s.drain
	}
	if s.drain {
		down = append(down, "drain")
	}
	cond["grounded"] = s.rng.IntN(100) != 0
	cond["silenced"] = s.rng.IntN(400) == 0
	pool["mana"] = min(pool["mana"]+0.5, 100)
	pool["stamina"] = min(pool["stamina"]+0.25, 50)
	s.keys.step(c, down...)
}

func newScripted(t testing.TB, seed uint64) (*ability.Caster, *recorder, ability.Pool, map[string]bool, *script) {
	t.Helper()
	set, err := ability.Parse(readSample(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ability.NewCaster(set, 60)
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	pool := ability.Pool{"mana": 100, "stamina": 50}
	cond := map[string]bool{}
	c.OnEvent = rec.on
	c.Resources = pool
	c.Condition = func(n string) bool { return cond[n] }
	return c, rec, pool, cond, &script{rng: mathrand.New(mathrand.NewPCG(seed, 517)), keys: newKeys()}
}

func TestSameInputsGiveSameEventsAndSnapshotsResume(t *testing.T) {
	a, recA, poolA, condA, sa := newScripted(t, 1)
	b, recB, poolB, condB, sb := newScripted(t, 1)
	for range 3000 {
		sa.step(a, poolA, condA)
		sb.step(b, poolB, condB)
	}
	if !reflect.DeepEqual(recA.events, recB.events) || len(recA.events) < 50 {
		t.Fatalf("runs diverged or did too little: %d vs %d events", len(recA.events), len(recB.events))
	}
	kinds := map[ability.EventKind]bool{}
	for _, e := range recA.events {
		kinds[e.Kind] = true
	}
	for k := ability.EventStarted; k <= ability.EventChargeRestored; k++ {
		if !kinds[k] {
			t.Errorf("script never produced %v", k)
		}
	}

	// Snapshot a mid-run Caster, carry it through JSON and YAML, and resume a fresh Caster from it: both continue identically.
	snap := a.Snapshot()
	js, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var fromJSON ability.State
	if err := json.Unmarshal(js, &fromJSON); err != nil {
		t.Fatal(err)
	}
	ys, err := yaml.Marshal(&fromJSON)
	if err != nil {
		t.Fatal(err)
	}
	var fromYAML ability.State
	if err := yaml.Unmarshal(ys, &fromYAML); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromYAML, snap) {
		t.Fatalf("state round trip changed it:\n%+v\n%+v", snap, fromYAML)
	}
	c, recC, poolC, condC, sc := newScripted(t, 99)
	if err := c.Restore(&fromYAML); err != nil {
		t.Fatal(err)
	}
	maps.Copy(poolC, poolA)
	maps.Copy(condC, condA)
	sc.rng = mathrand.New(mathrand.NewPCG(7, 7))
	sa.rng = mathrand.New(mathrand.NewPCG(7, 7))
	maps.Copy(sc.keys.prev, sa.keys.prev)
	sc.drain = sa.drain
	recA.events = recA.events[:0]
	for range 1500 {
		sa.step(a, poolA, condA)
		sc.step(c, poolC, condC)
	}
	if !reflect.DeepEqual(recA.events, recC.events) {
		t.Fatalf("restored Caster diverged: %d vs %d events", len(recA.events), len(recC.events))
	}
	if !reflect.DeepEqual(a.Snapshot(), c.Snapshot()) {
		t.Fatal("final snapshots differ")
	}
}

func TestRestoreRefusesForeignState(t *testing.T) {
	c, _ := newCaster(t, "  - id: a\n    cooldown: 1s\n    castTime: 100ms\n  - id: b\n    charges: 2\n    recharge: 1s\n", 10)
	good := c.Snapshot()
	mutate := func(f func(*ability.State)) *ability.State {
		st := c.Snapshot()
		st.Abilities = slices.Clone(st.Abilities)
		f(&st)
		return &st
	}
	cases := map[string]*ability.State{
		"nil":            nil,
		"rate":           mutate(func(s *ability.State) { s.Rate = 60 }),
		"count":          mutate(func(s *ability.State) { s.Abilities = s.Abilities[:1] }),
		"id":             mutate(func(s *ability.State) { s.Abilities[1].ID = "c" }),
		"charges":        mutate(func(s *ability.State) { s.Abilities[1].Charges = 3 }),
		"cooldown":       mutate(func(s *ability.State) { s.Abilities[0].Cooldown = 11 }),
		"recharge":       mutate(func(s *ability.State) { s.Abilities[1].Recharge = 11 }),
		"no recharge":    mutate(func(s *ability.State) { s.Abilities[1].Charges = 1 }),
		"stray recharge": mutate(func(s *ability.State) { s.Abilities[1].Recharge = 3 }),
		"active id":      mutate(func(s *ability.State) { s.Active = &ability.ActiveState{Ability: "z", Phase: ability.PhaseCasting} }),
		"active phase":   mutate(func(s *ability.State) { s.Active = &ability.ActiveState{Ability: "a", Phase: ability.PhaseIdle} }),
		"active elapsed": mutate(func(s *ability.State) {
			s.Active = &ability.ActiveState{Ability: "a", Phase: ability.PhaseCasting, Elapsed: 1}
		}),
		"instant cast": mutate(func(s *ability.State) { s.Active = &ability.ActiveState{Ability: "b", Phase: ability.PhaseCasting} }),
	}
	for name, st := range cases {
		if err := c.Restore(st); err == nil {
			t.Errorf("%s: Restore succeeded", name)
		}
	}
	if !reflect.DeepEqual(c.Snapshot(), good) {
		t.Fatal("a refused Restore changed the Caster")
	}
	ok := mutate(func(s *ability.State) {
		s.Step = 40
		s.Abilities[0].Cooldown = 10
		s.Abilities[1] = ability.AbilityState{ID: "b", Charges: 0, Recharge: 4}
		s.Active = &ability.ActiveState{Ability: "a", Phase: ability.PhaseCasting, Elapsed: 0}
	})
	if err := c.Restore(ok); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Snapshot(), *ok) {
		t.Fatalf("Snapshot after Restore = %+v, want %+v", c.Snapshot(), *ok)
	}
}

func TestDrivenByInputActionsInGameLoop(t *testing.T) {
	c, rec := newCaster(t, "  - id: bolt\n    action: fire\n    cooldown: 60ms\n    castTime: 40ms\n", 50)
	actions := input.NewActions()
	step := 0
	l := &game.Loop{Rate: 50, Update: func(time.Duration) {
		step++
		actions.Step()
		c.Update(actions)
	}}
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})
	// 20 ms steps: a trigger before step 1 casts on steps 1..3, takes effect on step 3, and the 3-step cooldown ends on step 6.
	actions.Trigger("fire")
	app.Advance(20 * time.Millisecond)
	actions.Trigger("fire") // busy casting: ignored
	app.Advance(100 * time.Millisecond)
	actions.Trigger("fire")
	app.Advance(20 * time.Millisecond)
	if step != 7 {
		t.Fatalf("step = %d, want 7", step)
	}
	expectEvents(t, "loop", rec.take(), "1 bolt started", "3 bolt activated", "6 bolt cooldown ended", "7 bolt started")
}

func TestUpdateDoesNotAllocate(t *testing.T) {
	c, _, pool, cond, s := newScripted(t, 3)
	c.OnEvent = func(ability.Event) {}
	for range 100 {
		s.step(c, pool, cond)
	}
	k := newKeys()
	k.held["cast"] = true
	cond["grounded"] = true
	allocs := testing.AllocsPerRun(200, func() {
		pool["mana"] = 100
		c.Update(k)
	})
	if allocs != 0 {
		t.Fatalf("Update allocates %v times per step", allocs)
	}
}

func BenchmarkCasterUpdate64Abilities(b *testing.B) {
	var doc strings.Builder
	doc.WriteString("version: 1\nabilities:\n")
	for i := range 64 {
		fmt.Fprintf(&doc, "  - id: a%d\n    action: act%d\n    cooldown: 500ms\n    charges: 2\n    recharge: 2s\n    castTime: 50ms\n    costs: [{resource: mana, amount: 1}]\n    conditions: ['!stunned']\n", i, i%8)
	}
	set, err := ability.Parse([]byte(doc.String()))
	if err != nil {
		b.Fatal(err)
	}
	c, err := ability.NewCaster(set, 60)
	if err != nil {
		b.Fatal(err)
	}
	c.Resources = ability.Pool{"mana": 1e18}
	c.Condition = func(string) bool { return false }
	c.OnEvent = func(ability.Event) {}
	k := newKeys()
	for i := range 8 {
		k.held[fmt.Sprintf("act%d", i)] = true
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		c.Update(k)
		clear(k.prev)
	}
}
