package ai_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/internal/formattest"
	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/loaderr"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/random"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestBlackboardTypesScopingAndNotifications(t *testing.T) {
	squad := ai.NewBlackboard()
	agent := ai.NewChildBlackboard(squad)

	if agent.Parent() != squad || squad.Parent() != nil {
		t.Fatal("unexpected Parent() pointers")
	}

	// All 7 typed keys on local blackboard.
	agent.SetBool("alert", true)
	agent.SetInt("ammo", 12)
	agent.SetFloat("dist", 42.5)
	agent.SetString("state", "chase")
	agent.SetVec2("pos2", math2d.V2(3, 4))
	agent.SetPoint("pt2", core.Point{X: 5, Y: 6})
	agent.SetVec3("pos3", ai.Vec3{X: 1, Y: 2, Z: 3})
	agent.SetEntity("target", 9001)

	if v, ok := agent.Bool("alert"); !ok || !v {
		t.Fatalf("Bool(alert) = (%v, %v)", v, ok)
	}
	if v, ok := agent.Int("ammo"); !ok || v != 12 {
		t.Fatalf("Int(ammo) = (%v, %v)", v, ok)
	}
	if v, ok := agent.Float("dist"); !ok || v != 42.5 {
		t.Fatalf("Float(dist) = (%v, %v)", v, ok)
	}
	if v, ok := agent.String("state"); !ok || v != "chase" {
		t.Fatalf("String(state) = (%v, %v)", v, ok)
	}
	if v, ok := agent.Vec2("pos2"); !ok || v != math2d.V2(3, 4) {
		t.Fatalf("Vec2(pos2) = (%v, %v)", v, ok)
	}
	if p, ok := agent.Point("pos2"); !ok || p != (core.Point{X: 3, Y: 4}) {
		t.Fatalf("Point(pos2) = (%v, %v)", p, ok)
	}
	if v, ok := agent.Vec2("pt2"); !ok || v != math2d.V2(5, 6) {
		t.Fatalf("Vec2(pt2) = (%v, %v)", v, ok)
	}
	if v, ok := agent.Vec3("pos3"); !ok || v != (ai.Vec3{X: 1, Y: 2, Z: 3}) {
		t.Fatalf("Vec3(pos3) = (%v, %v)", v, ok)
	}
	if id, ok := agent.Entity("target"); !ok || id != 9001 {
		t.Fatalf("Entity(target) = (%v, %v)", id, ok)
	}
	if _, ok := agent.Int("alert"); ok {
		t.Fatal("wrong-type getter should return false")
	}

	// No-op write with identical value must not bump revision.
	revBefore := agent.KeyRevision("ammo")
	agent.SetInt("ammo", 12)
	if agent.KeyRevision("ammo") != revBefore {
		t.Fatal("setting identical value should not bump KeyRevision")
	}

	// Shared parent fallback, shadowing, and notifications.
	var events []string
	cancelAll := agent.Watch("", func(ch ai.BlackboardChange) {
		events = append(events, fmt.Sprintf("%s:%v->%v", ch.Key, ch.Old.Kind, ch.New.Kind))
	})
	defer cancelAll()

	var orderEvents []string
	cancelOrder := agent.Watch("order", func(ch ai.BlackboardChange) {
		orderEvents = append(orderEvents, ch.Old.String+"->"+ch.New.String)
	})

	// SetShared writes to squad parent and notifies child watcher.
	agent.SetSharedString("order", "flank")
	if v, ok := squad.String("order"); !ok || v != "flank" {
		t.Fatalf("squad order = (%q, %v), want flank", v, ok)
	}
	if v, ok := agent.String("order"); !ok || v != "flank" {
		t.Fatalf("agent inherited order = (%q, %v), want flank", v, ok)
	}
	if agent.HasLocal("order") {
		t.Fatal("SetShared must write to parent, not local")
	}

	// Local shadow overrides parent without mutating parent.
	agent.SetString("order", "hold")
	if v, _ := agent.String("order"); v != "hold" {
		t.Fatalf("shadowed order = %q, want hold", v)
	}
	if v, _ := squad.String("order"); v != "flank" {
		t.Fatalf("parent order mutated to %q", v)
	}

	// Parent change while shadowed must NOT fire child watcher.
	squad.SetString("order", "retreat")
	// Deleting local shadow reveals parent's "retreat" and notifies child watcher!
	agent.Delete("order")
	if v, _ := agent.String("order"); v != "retreat" {
		t.Fatalf("order after Delete = %q, want retreat", v)
	}

	wantOrder := []string{"->flank", "flank->hold", "hold->retreat"}
	if !slices.Equal(orderEvents, wantOrder) {
		t.Fatalf("orderEvents = %v, want %v", orderEvents, wantOrder)
	}

	// Cancel stops further notifications.
	cancelOrder()
	squad.SetString("order", "regroup")
	if len(orderEvents) != len(wantOrder) {
		t.Fatalf("canceled watcher still fired: %v", orderEvents)
	}

	// Keys merges local + parent sorted and deduplicated.
	keys := agent.Keys(nil)
	if !slices.IsSorted(keys) || !slices.Contains(keys, "order") || !slices.Contains(keys, "ammo") {
		t.Fatalf("unexpected Keys = %v", keys)
	}

	// Clear removes all local keys while leaving parent intact.
	agent.Clear()
	if len(agent.LocalKeys(nil)) != 0 {
		t.Fatalf("LocalKeys after Clear = %v", agent.LocalKeys(nil))
	}
	if v, ok := agent.String("order"); !ok || v != "regroup" {
		t.Fatalf("parent key missing after local Clear: (%q, %v)", v, ok)
	}
}

func TestBlackboardConditionOperators(t *testing.T) {
	bb := ai.NewBlackboard()
	bb.SetBool("flag", true)
	bb.SetInt("hp", 50)
	bb.SetFloat("dist", 12.5)
	bb.SetString("role", "sniper")
	bb.SetVec2("pos2", math2d.V2(1, 2))
	bb.SetVec3("pos3", ai.Vec3{X: 1, Y: 2, Z: 3})
	bb.SetEntity("target", 77)

	cases := []struct {
		name string
		key  string
		op   ai.BlackboardOp
		val  ai.BlackboardValue
		want ai.Status
	}{
		{"set present", "flag", ai.BBOpSet, ai.BlackboardValue{}, ai.StatusSuccess},
		{"set absent", "missing", ai.BBOpSet, ai.BlackboardValue{}, ai.StatusFailure},
		{"unset absent", "missing", ai.BBOpUnset, ai.BlackboardValue{}, ai.StatusSuccess},
		{"unset present", "flag", ai.BBOpUnset, ai.BlackboardValue{}, ai.StatusFailure},
		{"bool eq", "flag", ai.BBOpEq, ai.BBBool(true), ai.StatusSuccess},
		{"bool ne", "flag", ai.BBOpNe, ai.BBBool(false), ai.StatusSuccess},
		{"unset ne", "missing", ai.BBOpNe, ai.BBInt(1), ai.StatusSuccess},
		{"unset eq", "missing", ai.BBOpEq, ai.BBInt(1), ai.StatusFailure},
		{"int eq float", "hp", ai.BBOpEq, ai.BBFloat(50), ai.StatusSuccess},
		{"float lt int", "dist", ai.BBOpLt, ai.BBInt(13), ai.StatusSuccess},
		{"float lte", "dist", ai.BBOpLte, ai.BBFloat(12.5), ai.StatusSuccess},
		{"float gt", "dist", ai.BBOpGt, ai.BBFloat(12.5), ai.StatusFailure},
		{"int gte", "hp", ai.BBOpGte, ai.BBInt(50), ai.StatusSuccess},
		{"string eq", "role", ai.BBOpEq, ai.BBString("sniper"), ai.StatusSuccess},
		{"string lt", "role", ai.BBOpLt, ai.BBString("tank"), ai.StatusSuccess},
		{"vec2 eq", "pos2", ai.BBOpEq, ai.BBPoint(core.Point{X: 1, Y: 2}), ai.StatusSuccess},
		{"vec2 ne", "pos2", ai.BBOpNe, ai.BBVec2(math2d.V2(9, 9)), ai.StatusSuccess},
		{"vec3 eq", "pos3", ai.BBOpEq, ai.BBVec3(ai.Vec3{X: 1, Y: 2, Z: 3}), ai.StatusSuccess},
		{"entity eq int", "target", ai.BBOpEq, ai.BBInt(77), ai.StatusSuccess},
		{"entity gt", "target", ai.BBOpGt, ai.BBEntity(10), ai.StatusSuccess},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node := ai.BlackboardCondition(bb, tc.key, tc.op, tc.val, ai.AbortNone, nil)
			if got := node.Tick(16 * time.Millisecond); got != tc.want {
				t.Fatalf("Tick() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBlackboardConditionObserverAborts(t *testing.T) {
	dt := 50 * time.Millisecond

	t.Run("AbortLower aborts running lower-priority branch on parent key change", func(t *testing.T) {
		squad := ai.NewBlackboard()
		agent := ai.NewChildBlackboard(squad)
		squad.SetBool("alarm", false)

		var log []string
		combat := &trackedAction{name: "combat", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
		patrol := &trackedAction{name: "patrol", ticksNeeded: 4, finalStatus: ai.StatusSuccess, log: &log}

		tree := ai.Selector(
			ai.BlackboardCondition(agent, "alarm", ai.BBOpEq, ai.BBBool(true), ai.AbortLower, combat),
			patrol,
		)

		// Tick 1 & 2: alarm is false -> patrol runs.
		if st := tree.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v", st)
		}
		if st := tree.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 2 = %v", st)
		}
		// Shared squad blackboard changes alarm -> true.
		squad.SetBool("alarm", true)
		// Tick 3: next tick immediately aborts patrol (Reset) and enters combat!
		if st := tree.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 3 = %v", st)
		}
		// Even if alarm turns false while combat runs, AbortLower does NOT abort self.
		squad.SetBool("alarm", false)
		if st := tree.Tick(dt); st != ai.StatusSuccess {
			t.Fatalf("tick 4 = %v, want Success (AbortLower must not abort self)", st)
		}

		want := []string{"patrol", "patrol", "reset:patrol", "combat", "combat"}
		if !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	})

	t.Run("AbortNone neither aborts lower nor self", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("seen", false)

		var log []string
		chase := &trackedAction{name: "chase", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}
		patrol := &trackedAction{name: "patrol", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}

		tree := ai.Selector(
			ai.BlackboardCondition(bb, "seen", ai.BBOpEq, ai.BBBool(true), ai.AbortNone, chase),
			patrol,
		)

		// Tick 1: patrol starts running.
		tree.Tick(dt)
		// Key flips true while patrol is running: AbortNone lets patrol finish!
		bb.SetBool("seen", true)
		if st := tree.Tick(dt); st != ai.StatusSuccess {
			t.Fatalf("tick 2 = %v, want patrol to finish with Success", st)
		}
		// Tick 3: fresh selector pass now enters chase.
		if st := tree.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 3 = %v, want chase Running", st)
		}
		// Key flips false while chase is running: AbortNone lets chase finish!
		bb.SetBool("seen", false)
		if st := tree.Tick(dt); st != ai.StatusSuccess {
			t.Fatalf("tick 4 = %v, want chase to finish with Success", st)
		}

		want := []string{"patrol", "patrol", "chase", "chase"}
		if !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	})

	t.Run("AbortSelf aborts self but not lower", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("seen", false)

		var log []string
		chase := &trackedAction{name: "chase", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
		patrol := &trackedAction{name: "patrol", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}

		tree := ai.Selector(
			ai.BlackboardCondition(bb, "seen", ai.BBOpEq, ai.BBBool(true), ai.AbortSelf, chase),
			patrol,
		)

		tree.Tick(dt) // patrol 1/2
		bb.SetBool("seen", true)
		tree.Tick(dt) // AbortSelf does not abort patrol -> patrol finishes 2/2
		tree.Tick(dt) // fresh pass -> chase 1/3
		bb.SetBool("seen", false)
		tree.Tick(dt) // AbortSelf resets chase and falls through to patrol 1/2!

		want := []string{"patrol", "patrol", "chase", "reset:chase", "patrol"}
		if !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	})

	t.Run("AbortBoth aborts both lower and self", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("seen", false)

		var log []string
		chase := &trackedAction{name: "chase", ticksNeeded: 4, finalStatus: ai.StatusSuccess, log: &log}
		patrol := &trackedAction{name: "patrol", ticksNeeded: 4, finalStatus: ai.StatusSuccess, log: &log}

		tree := ai.Selector(
			ai.BlackboardCondition(bb, "seen", ai.BBOpEq, ai.BBBool(true), ai.AbortBoth, chase),
			patrol,
		)

		tree.Tick(dt) // patrol 1/4
		bb.SetBool("seen", true)
		tree.Tick(dt) // aborts patrol -> chase 1/4
		bb.SetBool("seen", false)
		tree.Tick(dt) // aborts chase -> patrol 1/4

		want := []string{"patrol", "reset:patrol", "chase", "reset:chase", "patrol"}
		if !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	})

	t.Run("Sequence leaf BlackboardCondition with AbortSelf", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("armed", true)

		var log []string
		channel := &trackedAction{name: "channel", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
		seq := ai.Sequence(
			ai.BlackboardCondition(bb, "armed", ai.BBOpEq, ai.BBBool(true), ai.AbortSelf, nil),
			channel,
		)
		if st := seq.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v", st)
		}
		bb.SetBool("armed", false)
		if st := seq.Tick(dt); st != ai.StatusFailure {
			t.Fatalf("tick 2 = %v, want Failure", st)
		}
		want := []string{"channel", "reset:channel"}
		if !slices.Equal(log, want) {
			t.Fatalf("log = %v, want %v", log, want)
		}
	})

	t.Run("Zero allocations per Tick", func(t *testing.T) {
		squad := ai.NewBlackboard()
		agent := ai.NewChildBlackboard(squad)
		squad.SetBool("seen", false)
		tree := ai.Selector(
			ai.BlackboardCondition(agent, "seen", ai.BBOpEq, ai.BBBool(true), ai.AbortBoth,
				ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning }),
			),
			ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning }),
		)
		tree.Tick(dt)
		allocs := testing.AllocsPerRun(100, func() {
			tree.Tick(dt)
		})
		if allocs != 0 {
			t.Fatalf("Tick allocated %v times, want 0", allocs)
		}
	})
}

func TestSelectorMixedBlackboardAndPlainPreemption(t *testing.T) {
	const dt = 50 * time.Millisecond

	modes := []ai.AbortMode{ai.AbortNone, ai.AbortSelf, ai.AbortLower, ai.AbortBoth}

	t.Run("Sequence Condition before BlackboardCondition preempts across all abort modes", func(t *testing.T) {
		for _, mode := range modes {
			t.Run(string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetInt("ammo", 3)
				seen := false
				attacks := 0
				patrolResets := 0

				var log []string
				patrol := &trackedAction{name: "patrol", ticksNeeded: 100, finalStatus: ai.StatusSuccess, log: &log}
				tree := ai.Selector(
					ai.Sequence(
						ai.Condition(func() bool { return seen }),
						ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), mode,
							ai.Action(func(time.Duration) ai.Status {
								attacks++
								return ai.StatusSuccess
							}),
						),
					),
					patrol,
				)

				// Tick 1: seen=false -> patrol starts running.
				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 1 = %v, want Running", st)
				}
				if attacks != 0 || patrol.resets != 0 {
					t.Fatalf("tick 1: attacks=%d patrol.resets=%d, want 0, 0", attacks, patrol.resets)
				}

				// Flip seen=true (ammo unchanged) and tick 5 times: patrol must be preempted on first tick and attack runs 5 times.
				seen = true
				for i := range 5 {
					if st := tree.Tick(dt); st != ai.StatusSuccess {
						t.Fatalf("tick %d = %v, want Success", i+2, st)
					}
				}
				patrolResets = patrol.resets
				if attacks != 5 || patrolResets != 1 {
					t.Fatalf("after 5 ticks: attacks=%d patrolResets=%d, want 5, 1", attacks, patrolResets)
				}
			})
		}
	})

	t.Run("Sequence Condition before running BlackboardCondition preserves AbortSelf and AbortBoth", func(t *testing.T) {
		for _, mode := range modes {
			t.Run(string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetInt("ammo", 3)
				seen := false
				var log []string
				attack := &trackedAction{name: "attack", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
				patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

				tree := ai.Selector(
					ai.Sequence(
						ai.Condition(func() bool { return seen }),
						ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), mode, attack),
					),
					patrol,
				)

				tree.Tick(dt) // patrol 1/10
				seen = true
				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("preempt tick = %v, want Running (attack 1/3)", st)
				}
				if patrol.resets != 1 {
					t.Fatalf("patrol.resets = %d, want 1", patrol.resets)
				}

				// Drop ammo to 0 while attack is running.
				bb.SetInt("ammo", 0)
				tree.Tick(dt)
				wantSelfAbort := mode == ai.AbortSelf || mode == ai.AbortBoth
				if wantSelfAbort {
					if attack.resets != 1 {
						t.Fatalf("mode %s: attack.resets = %d, want 1 (self-abort)", mode, attack.resets)
					}
				} else {
					if attack.resets != 0 {
						t.Fatalf("mode %s: attack.resets = %d, want 0 (must not self-abort)", mode, attack.resets)
					}
				}
			})
		}
	})

	t.Run("Sequence BlackboardCondition leaf before Condition preempts and preserves self-abort", func(t *testing.T) {
		for _, mode := range modes {
			t.Run(string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetInt("ammo", 3)
				seen := false
				var log []string
				attack := &trackedAction{name: "attack", ticksNeeded: 3, finalStatus: ai.StatusSuccess, log: &log}
				patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

				tree := ai.Selector(
					ai.Sequence(
						ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), mode, nil),
						ai.Condition(func() bool { return seen }),
						attack,
					),
					patrol,
				)

				// Tick 1: ammo>0 passes, seen=false fails -> patrol runs.
				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 1 = %v, want Running", st)
				}
				// Tick 2: seen flips true with ammo unchanged -> must preempt patrol and enter attack!
				seen = true
				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 2 = %v, want Running (attack)", st)
				}
				if patrol.resets != 1 || attack.ticks != 1 {
					t.Fatalf("tick 2: patrol.resets=%d attack.ticks=%d, want 1, 1", patrol.resets, attack.ticks)
				}
				// Tick 3: ammo drops to 0 while attack runs -> AbortSelf/AbortBoth must self-abort attack via Sequence.
				bb.SetInt("ammo", 0)
				tree.Tick(dt)
				wantSelfAbort := mode == ai.AbortSelf || mode == ai.AbortBoth
				if wantSelfAbort && attack.resets != 1 {
					t.Fatalf("mode %s: attack.resets = %d, want 1", mode, attack.resets)
				}
				if !wantSelfAbort && attack.resets != 0 {
					t.Fatalf("mode %s: attack.resets = %d, want 0", mode, attack.resets)
				}
			})
		}
	})

	t.Run("Guard wrapping BlackboardCondition and BlackboardCondition wrapping Guard preempt", func(t *testing.T) {
		for _, mode := range modes {
			t.Run("GuardOuter/"+string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetInt("ammo", 3)
				seen := true
				attacks := 0
				var log []string
				patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

				tree := ai.Selector(
					ai.Guard(func() bool { return seen },
						ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), mode,
							ai.Action(func(time.Duration) ai.Status {
								attacks++
								return ai.StatusSuccess
							}),
						),
					),
					patrol,
				)

				// First tick primes BlackboardCondition's seenRev while seen=true.
				if st := tree.Tick(dt); st != ai.StatusSuccess || attacks != 1 {
					t.Fatalf("tick 1: st=%v attacks=%d, want Success, 1", st, attacks)
				}
				// Drop seen -> patrol starts running.
				seen = false
				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 2 = %v, want Running (patrol)", st)
				}
				// Restore seen with ammo unchanged -> Guard must preempt patrol!
				seen = true
				if st := tree.Tick(dt); st != ai.StatusSuccess || attacks != 2 || patrol.resets != 1 {
					t.Fatalf("tick 3: st=%v attacks=%d patrol.resets=%d, want Success, 2, 1", st, attacks, patrol.resets)
				}
			})

			t.Run("GuardInner/"+string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetInt("ammo", 3)
				seen := false
				attacks := 0
				var log []string
				patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

				tree := ai.Selector(
					ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), mode,
						ai.Guard(func() bool { return seen },
							ai.Action(func(time.Duration) ai.Status {
								attacks++
								return ai.StatusSuccess
							}),
						),
					),
					patrol,
				)

				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 1 = %v, want Running (patrol)", st)
				}
				seen = true
				if st := tree.Tick(dt); st != ai.StatusSuccess || attacks != 1 || patrol.resets != 1 {
					t.Fatalf("tick 2: st=%v attacks=%d patrol.resets=%d, want Success, 1, 1", st, attacks, patrol.resets)
				}
			})
		}
	})

	t.Run("Nested Selector and WithAttackToken mixed with BlackboardCondition preempt", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetBool("alert", false)
		bb.SetInt("ammo", 3)
		seen := false
		attacks := 0
		var log []string
		patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

		nestedTree := ai.Selector(
			ai.Selector(
				ai.Guard(func() bool { return seen }, ai.Action(func(time.Duration) ai.Status {
					attacks++
					return ai.StatusSuccess
				})),
				ai.BlackboardCondition(bb, "alert", ai.BBOpEq, ai.BBBool(true), ai.AbortLower,
					ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }),
				),
			),
			patrol,
		)

		if st := nestedTree.Tick(dt); st != ai.StatusRunning {
			t.Fatalf("nested tick 1 = %v, want Running", st)
		}
		seen = true
		if st := nestedTree.Tick(dt); st != ai.StatusSuccess || attacks != 1 || patrol.resets != 1 {
			t.Fatalf("nested tick 2: st=%v attacks=%d patrol.resets=%d, want Success, 1, 1", st, attacks, patrol.resets)
		}

		// WithAttackToken after BlackboardCondition in a Sequence.
		pool := ai.NewTokenPool(ai.TokenPoolConfig{Capacity: 1})
		pool.Request(2, 10)
		pool.Step(0)
		tokenHits := 0
		patrol2 := &trackedAction{name: "patrol2", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}
		tokenTree := ai.Selector(
			ai.Sequence(
				ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), ai.AbortLower, nil),
				ai.WithAttackToken(pool, 1, nil, ai.Action(func(time.Duration) ai.Status {
					tokenHits++
					return ai.StatusSuccess
				})),
			),
			patrol2,
		)
		if st := tokenTree.Tick(dt); st != ai.StatusRunning || tokenHits != 0 {
			t.Fatalf("token tick 1: st=%v hits=%d, want Running, 0", st, tokenHits)
		}
		pool.Release(2)
		pool.Step(0)
		if st := tokenTree.Tick(dt); st != ai.StatusSuccess || tokenHits != 1 || patrol2.resets != 1 {
			t.Fatalf("token tick 2: st=%v hits=%d patrol2.resets=%d, want Success, 1, 1", st, tokenHits, patrol2.resets)
		}
	})

	t.Run("Pure blackboard-only branches keep key-driven behavior", func(t *testing.T) {
		for _, mode := range []ai.AbortMode{ai.AbortNone, ai.AbortSelf} {
			t.Run("SequenceOnlyBB/"+string(mode), func(t *testing.T) {
				bb := ai.NewBlackboard()
				bb.SetBool("ready", false)
				var log []string
				shoot := &trackedAction{name: "shoot", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: &log}
				patrol := &trackedAction{name: "patrol", ticksNeeded: 2, finalStatus: ai.StatusSuccess, log: &log}

				tree := ai.Selector(
					ai.Sequence(
						ai.BlackboardCondition(bb, "ready", ai.BBOpEq, ai.BBBool(true), mode, nil),
						shoot,
					),
					patrol,
				)

				tree.Tick(dt) // patrol 1/2
				bb.SetBool("ready", true)
				if st := tree.Tick(dt); st != ai.StatusSuccess {
					t.Fatalf("tick 2 = %v, want patrol to finish with Success", st)
				}
				if patrol.resets != 0 {
					t.Fatalf("patrol.resets = %d, want 0 for pure-BB %s", patrol.resets, mode)
				}
				tree.Tick(dt) // fresh pass enters shoot
				want := []string{"patrol", "patrol", "shoot"}
				if !slices.Equal(log, want) {
					t.Fatalf("log = %v, want %v", log, want)
				}
			})
		}

		// Sibling isolation: a pure-BB AbortNone sibling stays key-driven while a mixed sibling still preempts.
		bb := ai.NewBlackboard()
		bb.SetBool("alarm", false)
		bb.SetInt("ammo", 3)
		seen := false
		alarmHits := 0
		attackHits := 0
		var log []string
		patrol := &trackedAction{name: "patrol", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: &log}

		tree := ai.Selector(
			ai.BlackboardCondition(bb, "alarm", ai.BBOpEq, ai.BBBool(true), ai.AbortNone,
				ai.Action(func(time.Duration) ai.Status {
					alarmHits++
					return ai.StatusSuccess
				}),
			),
			ai.Sequence(
				ai.Condition(func() bool { return seen }),
				ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), ai.AbortNone,
					ai.Action(func(time.Duration) ai.Status {
						attackHits++
						return ai.StatusSuccess
					}),
				),
			),
			patrol,
		)

		tree.Tick(dt) // patrol 1/10
		bb.SetBool("alarm", true)
		if st := tree.Tick(dt); st != ai.StatusRunning || alarmHits != 0 || patrol.resets != 0 {
			t.Fatalf("tick 2 (alarm AbortNone): st=%v alarmHits=%d patrol.resets=%d, want Running, 0, 0", st, alarmHits, patrol.resets)
		}
		seen = true
		if st := tree.Tick(dt); st != ai.StatusSuccess || attackHits != 1 || patrol.resets != 1 {
			t.Fatalf("tick 3 (mixed sibling): st=%v attackHits=%d patrol.resets=%d, want Success, 1, 1", st, attackHits, patrol.resets)
		}
	})

	t.Run("YAML-built trees match Go preemption behavior", func(t *testing.T) {
		for _, mode := range modes {
			t.Run(string(mode), func(t *testing.T) {
				src := fmt.Sprintf(`version: 1
blackboard:
  - key: ammo
    type: int
    default: 3
root:
  type: selector
  children:
    - type: sequence
      name: attack_branch
      children:
        - type: condition
          condition: seen
        - type: blackboard_condition
          key: ammo
          op: gt
          value: 0
          abort: %s
          child:
            type: action
            action: attack
    - type: action
      action: patrol
`, mode)
				doc, err := ai.ParseBehaviorTree([]byte(src))
				if err != nil {
					t.Fatalf("ParseBehaviorTree: %v", err)
				}
				bb := ai.NewBlackboard()
				seen := false
				attacks := 0
				var log []string
				patrol := &trackedAction{name: "patrol", ticksNeeded: 100, finalStatus: ai.StatusSuccess, log: &log}

				tree, err := ai.BuildBehaviorTree(doc, ai.BTBindings{
					Blackboard: bb,
					Conditions: map[string]func(ai.BTNodeContext) (func() bool, error){
						"seen": func(ai.BTNodeContext) (func() bool, error) {
							return func() bool { return seen }, nil
						},
					},
					Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
						"attack": func(ai.BTNodeContext) (ai.Node, error) {
							return ai.Action(func(time.Duration) ai.Status {
								attacks++
								return ai.StatusSuccess
							}), nil
						},
						"patrol": func(ai.BTNodeContext) (ai.Node, error) {
							return patrol, nil
						},
					},
				})
				if err != nil {
					t.Fatalf("BuildBehaviorTree: %v", err)
				}

				if st := tree.Tick(dt); st != ai.StatusRunning {
					t.Fatalf("tick 1 = %v, want Running", st)
				}
				seen = true
				for i := range 5 {
					if st := tree.Tick(dt); st != ai.StatusSuccess {
						t.Fatalf("tick %d = %v, want Success", i+2, st)
					}
				}
				if attacks != 5 || patrol.resets != 1 {
					t.Fatalf("YAML mode %s: attacks=%d patrol.resets=%d, want 5, 1", mode, attacks, patrol.resets)
				}
			})
		}
	})

	t.Run("Zero allocations per Tick on mixed selector", func(t *testing.T) {
		bb := ai.NewBlackboard()
		bb.SetInt("ammo", 3)
		bb.SetBool("alarm", false)
		seen := false

		tree := ai.Selector(
			ai.BlackboardCondition(bb, "alarm", ai.BBOpEq, ai.BBBool(true), ai.AbortLower,
				ai.Action(func(time.Duration) ai.Status { return ai.StatusSuccess }),
			),
			ai.Sequence(
				ai.Condition(func() bool { return seen }),
				ai.BlackboardCondition(bb, "ammo", ai.BBOpGt, ai.BBInt(0), ai.AbortBoth,
					ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning }),
				),
			),
			ai.Action(func(time.Duration) ai.Status { return ai.StatusRunning }),
		)

		// Warm up while patrol runs.
		tree.Tick(dt)
		allocsPatrol := testing.AllocsPerRun(100, func() {
			tree.Tick(dt)
		})
		if allocsPatrol != 0 {
			t.Fatalf("Tick (patrol with mixed sibling) allocated %v times, want 0", allocsPatrol)
		}

		// Warm up after preempting into the mixed branch.
		seen = true
		tree.Tick(dt)
		allocsAttack := testing.AllocsPerRun(100, func() {
			tree.Tick(dt)
		})
		if allocsAttack != 0 {
			t.Fatalf("Tick (running mixed branch) allocated %v times, want 0", allocsAttack)
		}
	})
}

func TestBehaviorTreeYAMLMatchesGoPatrolChaseAttack(t *testing.T) {
	// Run the exact same patrol/chase/attack scenario across 12 fixed 50ms game.Loop steps:
	//   1) Built from testdata/bt/guard.bt.yaml + patrol.bt.yaml
	//   2) Built directly in Go with ai.Selector / ai.BlackboardCondition / ai.Cooldown / ai.Sequence / ai.Wait
	// Both must produce the exact same leaf & reset sequence!
	runScenario := func(t *testing.T, build func(bb *ai.Blackboard, log *[]string) ai.Node) []string {
		t.Helper()
		bb := ai.NewBlackboard()
		var log []string
		tree := build(bb, &log)

		step := 0
		loop := &game.Loop{
			Rate: 20, // 50ms per tick
			Update: func(dt time.Duration) {
				step++
				switch step {
				case 1:
					// Patrol starts (enemy_seen=false).
				case 2:
					// Enemy spotted far away -> aborts patrol, starts chase!
					bb.SetBool("enemy_seen", true)
					bb.SetFloat("enemy_distance", 80)
				case 4:
					// Enemy closes into attack range (<= 32) -> aborts chase, attacks!
					bb.SetFloat("enemy_distance", 20)
				case 6:
					// Enemy backs off out of attack range -> aborts attack branch, chases again!
					bb.SetFloat("enemy_distance", 60)
				case 8:
					// Enemy escapes sight -> aborts chase, returns to patrol!
					bb.SetBool("enemy_seen", false)
				}
				tree.Tick(dt)
			},
		}
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			loop.Run(cx)
			return core.View{}
		})
		app.Advance(600 * time.Millisecond) // 12 steps
		if step != 12 {
			t.Fatalf("ran %d steps, want 12", step)
		}
		return log
	}

	// 1. Go-built tree.
	goLog := runScenario(t, func(bb *ai.Blackboard, log *[]string) ai.Node {
		bb.SetBool("enemy_seen", false)
		bb.SetFloat("enemy_distance", 999)
		bb.SetEntity("target_id", 0)
		bb.SetVec2("home_pos", math2d.V2(10, 20))

		attack := ai.Action(func(time.Duration) ai.Status {
			*log = append(*log, "attack:15")
			return ai.StatusSuccess
		})
		chase := &trackedAction{name: "chase:120", ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: log}
		movePatrol := &trackedAction{name: "move_patrol:10,20", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: log}
		lookAround := &trackedAction{name: "look_around", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: log}

		return ai.Selector(
			ai.BlackboardCondition(bb, "enemy_seen", ai.BBOpEq, ai.BBBool(true), ai.AbortBoth,
				ai.Selector(
					ai.BlackboardCondition(bb, "enemy_distance", ai.BBOpLte, ai.BBFloat(32), ai.AbortBoth,
						ai.Cooldown(500*time.Millisecond, attack),
					),
					chase,
				),
			),
			ai.Sequence(movePatrol, ai.Wait(200*time.Millisecond), lookAround),
		)
	})

	// 2. YAML-built tree from testdata/bt/guard.bt.yaml + patrol.bt.yaml.
	yamlLog := runScenario(t, func(bb *ai.Blackboard, log *[]string) ai.Node {
		fsys := fstest.MapFS{}
		for _, name := range []string{"guard.bt.yaml", "patrol.bt.yaml"} {
			data, err := readTestFile(t, "testdata/bt/"+name)
			if err != nil {
				t.Fatal(err)
			}
			fsys[name] = &fstest.MapFile{Data: data}
		}
		tree, err := ai.BuildBehaviorTreeFS(fsys, "guard.bt.yaml", ai.BTBindings{
			Blackboard: bb,
			Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
				"attack": func(ctx ai.BTNodeContext) (ai.Node, error) {
					dmg, ok := ctx.Params.Int("damage")
					if !ok {
						return nil, errors.New("missing damage")
					}
					return ai.Action(func(time.Duration) ai.Status {
						*log = append(*log, fmt.Sprintf("attack:%d", dmg))
						return ai.StatusSuccess
					}), nil
				},
				"chase": func(ctx ai.BTNodeContext) (ai.Node, error) {
					spd, _ := ctx.Params.Int("speed")
					return &trackedAction{name: fmt.Sprintf("chase:%d", spd), ticksNeeded: 10, finalStatus: ai.StatusSuccess, log: log}, nil
				},
				"move_patrol": func(ctx ai.BTNodeContext) (ai.Node, error) {
					homeKey := ctx.Key("home")
					home, _ := ctx.Blackboard.Vec2(homeKey)
					return &trackedAction{
						name:        fmt.Sprintf("move_patrol:%.0f,%.0f", home.X, home.Y),
						ticksNeeded: 1,
						finalStatus: ai.StatusSuccess,
						log:         log,
					}, nil
				},
				"look_around": func(ctx ai.BTNodeContext) (ai.Node, error) {
					return &trackedAction{name: "look_around", ticksNeeded: 1, finalStatus: ai.StatusSuccess, log: log}, nil
				},
			},
		})
		if err != nil {
			t.Fatalf("BuildBehaviorTreeFS: %v", err)
		}
		return tree
	})

	if !slices.Equal(yamlLog, goLog) {
		t.Fatalf("YAML trace differs from Go trace:\nYAML: %v\nGo:   %v", yamlLog, goLog)
	}
	if !slices.Contains(yamlLog, "attack:15") || !slices.Contains(yamlLog, "chase:120") || !slices.Contains(yamlLog, "look_around") {
		t.Fatalf("expected patrol, chase, and attack in trace: %v", yamlLog)
	}
}

func readTestFile(t *testing.T, p string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(p)
}

func TestBehaviorTreeYAMLAllNodeTypes(t *testing.T) {
	raw := []byte(`version: 1
name: kitchen_sink
blackboard:
  - key: ready
    type: bool
    default: true
root:
  type: service
  service: pulse
  interval: 50ms
  child:
    type: sequence
    children:
      - type: guard
        condition: can_run
        child:
          type: parallel
          policy: require_all
          children:
            - type: inverter
              child:
                type: failer
                child:
                  type: condition
                  condition: always_true
            - type: succeeder
              child:
                type: retry
                count: 1
                child:
                  type: action
                  action: fail_once
      - type: time_limit
        duration: 200ms
        child:
          type: repeater
          count: 2
          child:
            type: action
            action: ping
      - type: until_fail
        child:
          type: action
          action: count_down
      - type: random_sequence
        children:
          - type: action
            action: tag
            params:
              label: a
          - type: action
            action: tag
            params:
              label: b
      - type: random_selector
        children:
          - type: action
            action: tag
            params:
              label: r1
          - type: action
            action: tag
            params:
              label: r2
      - type: weighted_selector
        children:
          - type: action
            weight: 1
            action: tag
            params:
              label: w1
          - type: action
            weight: 3
            action: tag
            params:
              label: w2
`)

	doc, err := ai.ParseBehaviorTree(raw)
	if err != nil {
		t.Fatalf("ParseBehaviorTree: %v", err)
	}
	encoded, err := ai.EncodeBehaviorTree(doc)
	if err != nil {
		t.Fatalf("EncodeBehaviorTree: %v", err)
	}
	doc2, err := ai.ParseBehaviorTree(encoded)
	if err != nil {
		t.Fatalf("re-parse encoded: %v", err)
	}
	encoded2, err := ai.EncodeBehaviorTree(doc2)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(encoded, encoded2) {
		t.Fatalf("EncodeBehaviorTree not idempotent:\n1:\n%s\n2:\n%s", encoded, encoded2)
	}

	var serviceTicks, pings, failOnceCalls, downLeft int
	downLeft = 1
	var tags []string

	buildTree := func(seed uint64) ai.Node {
		serviceTicks, pings, failOnceCalls, downLeft = 0, 0, 0, 1
		tags = nil
		tree, err := ai.BuildBehaviorTree(doc, ai.BTBindings{
			Stream: random.New(seed),
			Services: map[string]func(ai.BTNodeContext) (func(time.Duration), error){
				"pulse": func(ai.BTNodeContext) (func(time.Duration), error) {
					return func(time.Duration) { serviceTicks++ }, nil
				},
			},
			Conditions: map[string]func(ai.BTNodeContext) (func() bool, error){
				"can_run":     func(ai.BTNodeContext) (func() bool, error) { return func() bool { return true }, nil },
				"always_true": func(ai.BTNodeContext) (func() bool, error) { return func() bool { return true }, nil },
			},
			Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
				"fail_once": func(ai.BTNodeContext) (ai.Node, error) {
					return ai.Action(func(time.Duration) ai.Status {
						failOnceCalls++
						if failOnceCalls == 1 {
							return ai.StatusFailure
						}
						return ai.StatusSuccess
					}), nil
				},
				"ping": func(ai.BTNodeContext) (ai.Node, error) {
					return ai.Action(func(time.Duration) ai.Status {
						pings++
						return ai.StatusSuccess
					}), nil
				},
				"count_down": func(ai.BTNodeContext) (ai.Node, error) {
					return ai.Action(func(time.Duration) ai.Status {
						if downLeft > 0 {
							downLeft--
							return ai.StatusSuccess
						}
						return ai.StatusFailure
					}), nil
				},
				"tag": func(ctx ai.BTNodeContext) (ai.Node, error) {
					lbl, _ := ctx.Params.String("label")
					return ai.Action(func(time.Duration) ai.Status {
						tags = append(tags, lbl)
						return ai.StatusSuccess
					}), nil
				},
			},
		})
		if err != nil {
			t.Fatalf("BuildBehaviorTree: %v", err)
		}
		return tree
	}

	run := func(seed uint64) []string {
		tree := buildTree(seed)
		for range 10 {
			if tree.Tick(50*time.Millisecond) == ai.StatusSuccess {
				break
			}
		}
		return slices.Clone(tags)
	}

	run1 := run(42)
	if serviceTicks == 0 || pings != 2 || failOnceCalls != 2 || len(run1) != 4 {
		t.Fatalf("unexpected execution counts: service=%d pings=%d failOnce=%d tags=%v", serviceTicks, pings, failOnceCalls, run1)
	}
	// Determinism with same seed.
	run2 := run(42)
	if !slices.Equal(run1, run2) {
		t.Fatalf("same seed produced different tags: %v vs %v", run1, run2)
	}
}

func TestBehaviorTreeYAMLSubtreesBindingsAndCycles(t *testing.T) {
	t.Run("Transitive key/param bindings and diamond reuse", func(t *testing.T) {
		fsys := fstest.MapFS{
			"root.bt.yaml": &fstest.MapFile{Data: []byte(`version: 1
root:
  type: sequence
  children:
    - type: subtree
      path: sub/mid.bt.yaml
      params:
        wait: 50ms
        threshold: 10
      keys:
        mid_hp: hero_hp
    - type: subtree
      path: sub/mid.bt.yaml
      params:
        wait: 100ms
        threshold: 20
      keys:
        mid_hp: boss_hp
`)},
			"sub/mid.bt.yaml": &fstest.MapFile{Data: []byte(`version: 1
params:
  wait: 200ms
  threshold: 5
root:
  type: subtree
  path: leaf.bt.yaml
  params:
    pause: ${wait}
    min_hp: ${threshold}
  keys:
    hp: mid_hp
`)},
			"sub/leaf.bt.yaml": &fstest.MapFile{Data: []byte(`version: 1
root:
  type: blackboard_condition
  key: hp
  op: gte
  value: ${min_hp}
  child:
    type: sequence
    children:
      - type: wait
        duration: ${pause}
      - type: action
        action: record
        params:
          min_hp: ${min_hp}
`)},
		}

		bb := ai.NewBlackboard()
		bb.SetInt("hero_hp", 15)
		bb.SetInt("boss_hp", 25)

		var recorded []string
		tree, err := ai.BuildBehaviorTreeFS(fsys, "root.bt.yaml", ai.BTBindings{
			Blackboard: bb,
			Actions: map[string]func(ai.BTNodeContext) (ai.Node, error){
				"record": func(ctx ai.BTNodeContext) (ai.Node, error) {
					key := ctx.Key("hp")
					minHP, _ := ctx.Params.Int("min_hp")
					return ai.Action(func(time.Duration) ai.Status {
						v, _ := ctx.Blackboard.Int(key)
						recorded = append(recorded, fmt.Sprintf("%s=%d>=%d", key, v, minHP))
						return ai.StatusSuccess
					}), nil
				},
			},
		})
		if err != nil {
			t.Fatalf("BuildBehaviorTreeFS: %v", err)
		}

		// Tick 1 (50ms): branch 1 wait finishes (50ms), records hero_hp, then branch 2 wait starts (50/100ms -> Running).
		if st := tree.Tick(50 * time.Millisecond); st != ai.StatusRunning {
			t.Fatalf("tick 1 = %v, want Running", st)
		}
		// Tick 2 (50ms): branch 2 wait finishes (100/100ms), records boss_hp -> Success.
		if st := tree.Tick(50 * time.Millisecond); st != ai.StatusSuccess {
			t.Fatalf("tick 2 = %v, want Success", st)
		}
		want := []string{"hero_hp=15>=10", "boss_hp=25>=20"}
		if !slices.Equal(recorded, want) {
			t.Fatalf("recorded = %v, want %v", recorded, want)
		}
	})

	t.Run("Cycle detection reports exact file and line", func(t *testing.T) {
		// Direct self-cycle via ParseBehaviorTreeWith.
		selfYAML := []byte("version: 1\nroot:\n  type: subtree\n  path: self.bt.yaml\n")
		_, err := ai.ParseBehaviorTreeWith(selfYAML, "self.bt.yaml")
		le, ok := loaderr.As(err)
		if !ok || le.File != "self.bt.yaml" || le.Line != 4 || !strings.Contains(le.Problem, "cycle") {
			t.Fatalf("self cycle err = %v (loaderr=%+v), want file=self.bt.yaml line=4", err, le)
		}

		// Indirect 2-file cycle a.bt.yaml -> b.bt.yaml -> a.bt.yaml.
		fsys := fstest.MapFS{
			"a.bt.yaml": &fstest.MapFile{Data: []byte("version: 1\nroot:\n  type: subtree\n  path: b.bt.yaml\n")},
			"b.bt.yaml": &fstest.MapFile{Data: []byte("version: 1\nname: loop_b\nroot:\n  type: sequence\n  children:\n    - type: subtree\n      path: a.bt.yaml\n")},
		}
		_, err = ai.LoadBehaviorTreeFS(fsys, "a.bt.yaml")
		le, ok = loaderr.As(err)
		if !ok || le.File != "b.bt.yaml" || le.Line != 7 || !strings.Contains(le.Problem, "cycle") {
			t.Fatalf("2-file cycle err = %v (loaderr=%+v), want file=b.bt.yaml line=7", err, le)
		}

		// Also caught directly by BuildBehaviorTree when parsed without LoadBehaviorTreeFS.
		docA, err := ai.ParseBehaviorTreeWith(fsys["a.bt.yaml"].Data, "a.bt.yaml")
		if err != nil {
			t.Fatalf("parse a.bt.yaml: %v", err)
		}
		_, err = ai.BuildBehaviorTree(docA, ai.BTBindings{FS: fsys})
		le, ok = loaderr.As(err)
		if !ok || le.File != "b.bt.yaml" || le.Line != 7 || !strings.Contains(le.Problem, "cycle") {
			t.Fatalf("BuildBehaviorTree cycle err = %v (loaderr=%+v), want file=b.bt.yaml line=7", err, le)
		}
	})
}

func TestBehaviorTreeYAMLErrorsReportLineNumbers(t *testing.T) {
	cases := []struct {
		name     string
		yaml     string
		wantLine int
		wantSub  string
	}{
		{
			name:     "unknown top-level field",
			yaml:     "version: 1\nbogusField: true\nroot:\n  type: wait\n  duration: 1s\n",
			wantLine: 2,
			wantSub:  "bogusField",
		},
		{
			name:     "unknown node field",
			yaml:     "version: 1\nroot:\n  type: wait\n  duration: 1s\n  typoField: 5\n",
			wantLine: 5,
			wantSub:  "typoField",
		},
		{
			name:     "unknown node type",
			yaml:     "version: 1\nroot:\n  type: teleport\n",
			wantLine: 3,
			wantSub:  "teleport",
		},
		{
			name:     "invalid blackboard op",
			yaml:     "version: 1\nroot:\n  type: blackboard_condition\n  key: hp\n  op: approx\n  value: 10\n",
			wantLine: 5,
			wantSub:  "approx",
		},
		{
			name:     "invalid abort mode",
			yaml:     "version: 1\nroot:\n  type: blackboard_condition\n  key: hp\n  op: eq\n  value: 10\n  abort: always\n",
			wantLine: 7,
			wantSub:  "always",
		},
		{
			name:     "value forbidden on set op",
			yaml:     "version: 1\nroot:\n  type: blackboard_condition\n  key: hp\n  op: set\n  value: 10\n",
			wantLine: 6,
			wantSub:  "does not take a value",
		},
		{
			name:     "ordering op on bool value",
			yaml:     "version: 1\nroot:\n  type: blackboard_condition\n  key: flag\n  op: lt\n  value: true\n",
			wantLine: 5,
			wantSub:  "not supported",
		},
		{
			name:     "unsupported version",
			yaml:     "version: 2\nroot:\n  type: wait\n  duration: 1s\n",
			wantLine: 1,
			wantSub:  "version",
		},
		{
			name:     "trailing YAML document",
			yaml:     "version: 1\nroot:\n  type: wait\n  duration: 1s\n---\nversion: 1\n",
			wantLine: 5,
			wantSub:  "trailing",
		},
		{
			name:     "irrelevant field on wait node",
			yaml:     "version: 1\nroot:\n  type: wait\n  duration: 1s\n  action: punch\n",
			wantLine: 5,
			wantSub:  "not valid on node type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ai.ParseBehaviorTreeWith([]byte(tc.yaml), "enemy.bt.yaml")
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			le, ok := loaderr.As(err)
			if !ok {
				t.Fatalf("error %v is not *loaderr.Error", err)
			}
			if le.File != "enemy.bt.yaml" {
				t.Errorf("File = %q, want enemy.bt.yaml", le.File)
			}
			if le.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d (err: %v)", le.Line, tc.wantLine, err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestBehaviorTreeZeroValueRoundTrip(t *testing.T) {
	// Ensure explicit zero values (false, 0, "") survive Encode(Parse(b)) == b.
	raw := []byte(`# yaml-language-server: $schema=https://game.nsgo.dev/schemas/bt.v1.json
version: 1
blackboard:
  - key: active
    type: bool
    default: false
  - key: count
    type: int
    default: 0
  - key: tag
    type: string
    default: ""
root:
  type: sequence
  children:
    - type: blackboard_condition
      key: active
      op: eq
      value: false
    - type: blackboard_condition
      key: count
      op: eq
      value: 0
    - type: blackboard_condition
      key: tag
      op: eq
      value: ""
`)
	doc, err := ai.ParseBehaviorTree(raw)
	if err != nil {
		t.Fatalf("ParseBehaviorTree: %v", err)
	}
	encoded, err := ai.EncodeBehaviorTree(doc)
	if err != nil {
		t.Fatalf("EncodeBehaviorTree: %v", err)
	}
	if !bytes.Equal(encoded, raw) {
		t.Fatalf("round-trip changed bytes:\ngot:\n%s\nwant:\n%s", encoded, raw)
	}
}

func TestBehaviorTreeFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*ai.BehaviorTreeDoc]{
		Decode: ai.ParseBehaviorTree,
		Encode: ai.EncodeBehaviorTree,
		Files:  []string{"testdata/bt/*.bt.yaml"},
		Doc:    "ai#bt",
	})
}
