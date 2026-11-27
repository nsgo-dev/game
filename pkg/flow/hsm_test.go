package flow_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/flow"
)

func TestHSMCombatNestedStatesAndParentStunTransition(t *testing.T) {
	type enemyState string
	const (
		patrol   enemyState = "patrol"
		combat   enemyState = "combat"
		approach enemyState = "approach"
		attack   enemyState = "attack"
		recover  enemyState = "recover"
		stunned  enemyState = "stunned"
		dead     enemyState = "dead"
	)

	var log []string
	step := 0
	spotted := false
	inRange := false
	attackDone := false
	gotStunned := false
	hp := 10

	combatHSM := flow.NewHSM(approach)
	combatHSM.AddState(approach, flow.State{
		Enter:  func() { log = append(log, "enter:approach") },
		Update: func(dt time.Duration) { log = append(log, "update:approach") },
		Exit:   func() { log = append(log, "exit:approach") },
	})
	combatHSM.AddState(attack, flow.State{
		Enter:  func() { log = append(log, "enter:attack") },
		Update: func(dt time.Duration) { log = append(log, "update:attack") },
		Exit:   func() { log = append(log, "exit:attack") },
	})
	combatHSM.AddState(recover, flow.State{
		Enter:  func() { log = append(log, "enter:recover") },
		Update: func(dt time.Duration) { log = append(log, "update:recover") },
		Exit:   func() { log = append(log, "exit:recover") },
	})
	combatHSM.AddTransition(approach, attack, func() bool { return inRange })
	combatHSM.AddTransition(attack, recover, func() bool { return attackDone })
	combatHSM.AddTransition(recover, approach, func() bool { return combatHSM.TimeInState() >= 40*time.Millisecond })

	hsm := flow.NewHSM(patrol)
	hsm.AddState(patrol, flow.State{
		Enter:  func() { log = append(log, "enter:patrol") },
		Update: func(dt time.Duration) { log = append(log, "update:patrol") },
		Exit:   func() { log = append(log, "exit:patrol") },
	})
	hsm.AddState(combat, flow.State{
		Enter:  func() { log = append(log, "enter:combat") },
		Update: func(dt time.Duration) { log = append(log, "update:combat") },
		Exit:   func() { log = append(log, "exit:combat") },
	})
	hsm.AddChild(combat, combatHSM)
	hsm.AddState(stunned, flow.State{
		Enter:  func() { log = append(log, "enter:stunned") },
		Update: func(dt time.Duration) { log = append(log, "update:stunned") },
		Exit:   func() { log = append(log, "exit:stunned") },
	})
	hsm.AddState(dead, flow.State{
		Enter: func() { log = append(log, "enter:dead") },
	})

	hsm.AddAnyTransition(dead, func() bool { return hp <= 0 })
	hsm.AddTransition(patrol, combat, func() bool { return spotted })
	hsm.AddTransition(combat, stunned, func() bool { return gotStunned })
	hsm.AddTransition(stunned, combat, func() bool { return !gotStunned && hsm.TimeInState() >= 40*time.Millisecond })

	// Before Start, Current reports the initial path.
	if got := hsm.Current(); !slices.Equal(got, []enemyState{patrol}) {
		t.Fatalf("before Start Current() = %v, want [patrol]", got)
	}

	hsm.Start()
	hsm.Start() // idempotent
	if !slices.Equal(log, []string{"enter:patrol"}) {
		t.Fatalf("after Start log = %v, want [enter:patrol]", log)
	}

	// 50 Hz = 20ms per step.
	app := startLoop(t, 50, func(dt time.Duration) {
		step++
		switch step {
		case 2:
			spotted = true
		case 3:
			inRange = true
		case 4:
			// Parent-level stunned transition interrupts attack inside combat.
			gotStunned = true
		case 6:
			// After 40ms in stunned (steps 4 and 5), recover back into combat (without history -> approach).
			gotStunned = false
			inRange = false
		case 7:
			hp = 0
		}
		hsm.Update(dt)
	})

	// Step 1: patrol.
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{patrol}) || hsm.Leaf() != patrol || !hsm.In(patrol) || hsm.In(combat) {
		t.Fatalf("step 1: current=%v leaf=%v", hsm.Current(), hsm.Leaf())
	}

	// Step 2: patrol -> combat (enters combat then approach, updates combat then approach).
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{combat, approach}) || !hsm.In(combat) || !hsm.In(approach) || hsm.Leaf() != approach {
		t.Fatalf("step 2: current=%v leaf=%v", hsm.Current(), hsm.Leaf())
	}
	if !slices.Equal(combatHSM.Current(), []enemyState{approach}) {
		t.Fatalf("step 2: combatHSM.Current() = %v, want [approach]", combatHSM.Current())
	}

	// Step 3: approach -> attack (combat stays active, time in combat accumulates to 40ms, attack is 20ms).
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{combat, attack}) {
		t.Fatalf("step 3: current=%v, want [combat attack]", hsm.Current())
	}
	if hsm.TimeIn(combat) != 40*time.Millisecond || hsm.TimeIn(attack) != 20*time.Millisecond || combatHSM.TimeInState() != 20*time.Millisecond {
		t.Fatalf("step 3 timings: combat=%v attack=%v child=%v", hsm.TimeIn(combat), hsm.TimeIn(attack), combatHSM.TimeInState())
	}

	// Step 4: parent-level combat -> stunned exits attack then combat (inside-out), enters stunned.
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{stunned}) || hsm.Previous() != combat {
		t.Fatalf("step 4: current=%v prev=%v", hsm.Current(), hsm.Previous())
	}
	if combatHSM.Current() != nil {
		t.Fatalf("step 4: inactive child Current() = %v, want nil", combatHSM.Current())
	}

	// Step 5: stays stunned (40ms total).
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{stunned}) || hsm.TimeInState() != 40*time.Millisecond {
		t.Fatalf("step 5: current=%v time=%v", hsm.Current(), hsm.TimeInState())
	}

	// Step 6: stunned -> combat (no history: enters combat then initial approach).
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{combat, approach}) {
		t.Fatalf("step 6: current=%v, want [combat approach]", hsm.Current())
	}

	// Step 7: wildcard dead interrupts [combat, approach] -> exits approach, exits combat, enters dead.
	app.Advance(20 * time.Millisecond)
	if !slices.Equal(hsm.Current(), []enemyState{dead}) {
		t.Fatalf("step 7: current=%v, want [dead]", hsm.Current())
	}

	wantLog := []string{
		"enter:patrol",
		// Step 1
		"update:patrol",
		// Step 2: patrol -> combat -> approach
		"exit:patrol",
		"enter:combat",
		"enter:approach",
		"update:combat",
		"update:approach",
		// Step 3: approach -> attack
		"exit:approach",
		"enter:attack",
		"update:combat",
		"update:attack",
		// Step 4: combat -> stunned (inside-out exit)
		"exit:attack",
		"exit:combat",
		"enter:stunned",
		"update:stunned",
		// Step 5: stunned
		"update:stunned",
		// Step 6: stunned -> combat -> approach (outside-in enter)
		"exit:stunned",
		"enter:combat",
		"enter:approach",
		"update:combat",
		"update:approach",
		// Step 7: any -> dead (inside-out exit)
		"exit:approach",
		"exit:combat",
		"enter:dead",
	}
	if !slices.Equal(log, wantLog) {
		t.Fatalf("callback log mismatch:\n got: %v\nwant: %v", log, wantLog)
	}
}

func TestHSMCrossBranchTransitionExitsToLowestCommonAncestor(t *testing.T) {
	var log []string
	rec := func(name string) flow.State {
		return flow.State{
			Enter: func() { log = append(log, "enter:"+name) },
			Exit:  func() { log = append(log, "exit:"+name) },
		}
	}

	// Tree:
	// root
	// ├── alive (initial: explore)
	// │   ├── explore (initial: idle)
	// │   │   ├── idle
	// │   │   └── patrol
	// │   └── combat (initial: approach)
	// │       ├── approach
	// │       └── attack
	// └── dead
	explore := flow.NewHSM("idle")
	explore.AddState("idle", rec("idle"))
	explore.AddState("patrol", rec("patrol"))

	combat := flow.NewHSM("approach")
	combat.AddState("approach", rec("approach"))
	combat.AddState("attack", rec("attack"))

	alive := flow.NewHSM("explore")
	alive.AddState("explore", rec("explore"))
	alive.AddChild("explore", explore)
	alive.AddState("combat", rec("combat"))
	alive.AddChild("combat", combat)

	root := flow.NewHSM("alive")
	root.AddState("alive", rec("alive"))
	root.AddChild("alive", alive)
	root.AddState("dead", rec("dead"))

	// Cross-branch transition directly from idle -> attack (LCA is alive!).
	goAttack := false
	explore.AddTransition("idle", "attack", func() bool { return goAttack })

	// Cross-branch transition from attack -> patrol (LCA is alive!).
	goPatrol := false
	combat.AddTransition("attack", "patrol", func() bool { return goPatrol })

	root.Start()
	if !slices.Equal(root.Current(), []string{"alive", "explore", "idle"}) {
		t.Fatalf("initial current = %v", root.Current())
	}

	log = nil
	goAttack = true
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"alive", "combat", "attack"}) {
		t.Fatalf("after idle->attack: current = %v", root.Current())
	}
	wantAttack := []string{"exit:idle", "exit:explore", "enter:combat", "enter:attack"}
	if !slices.Equal(log, wantAttack) {
		t.Fatalf("idle->attack log = %v, want %v (alive must stay active)", log, wantAttack)
	}

	log = nil
	goAttack = false
	goPatrol = true
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"alive", "explore", "patrol"}) {
		t.Fatalf("after attack->patrol: current = %v", root.Current())
	}
	wantPatrol := []string{"exit:attack", "exit:combat", "enter:explore", "enter:patrol"}
	if !slices.Equal(log, wantPatrol) {
		t.Fatalf("attack->patrol log = %v, want %v (alive must stay active)", log, wantPatrol)
	}
}

func TestHSMInnermostTransitionHasPriorityOverParent(t *testing.T) {
	t.Run("Polled", func(t *testing.T) {
		bothTrue := false

		combat := flow.NewHSM("attack")
		combat.AddState("attack", flow.State{})
		combat.AddState("recover", flow.State{})
		combat.AddTransition("attack", "recover", func() bool { return bothTrue })

		root := flow.NewHSM("combat")
		root.AddState("combat", flow.State{})
		root.AddChild("combat", combat)
		root.AddState("stunned", flow.State{})
		root.AddTransition("combat", "stunned", func() bool { return bothTrue })

		root.Start()
		bothTrue = true
		root.Update(16 * time.Millisecond)

		// Innermost (attack -> recover) must win over parent (combat -> stunned).
		if !slices.Equal(root.Current(), []string{"combat", "recover"}) {
			t.Fatalf("step 1 current = %v, want [combat recover]", root.Current())
		}

		// On the next step, recover has no outgoing transition, so parent (combat -> stunned) applies.
		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"stunned"}) {
			t.Fatalf("step 2 current = %v, want [stunned]", root.Current())
		}
	})

	t.Run("EventOverrideAndBubble", func(t *testing.T) {
		combat := flow.NewHSM("attack")
		combat.AddState("attack", flow.State{})
		combat.AddState("recover", flow.State{})
		// attack intercepts "hit" to recoil into recover; recover does not, so "hit" bubbles to combat.
		combat.AddEventTransition("attack", "recover", "hit")

		root := flow.NewHSM("combat")
		root.AddState("combat", flow.State{})
		root.AddChild("combat", combat)
		root.AddState("stunned", flow.State{})
		root.AddEventTransition("combat", "stunned", "hit")

		root.Start()
		root.Send("hit")
		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"combat", "recover"}) {
			t.Fatalf("after first hit: current = %v, want [combat recover]", root.Current())
		}

		root.Send("hit")
		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"stunned"}) {
			t.Fatalf("after second hit: current = %v, want [stunned]", root.Current())
		}
	})
}

func TestHSMEventsDeterministicSendOrderAndOncePerStep(t *testing.T) {
	t.Run("SendOrderDeterminesOutcome", func(t *testing.T) {
		build := func() (*flow.HSM[string], *[]string) {
			var log []string
			rec := func(name string) flow.State {
				return flow.State{
					Enter: func() { log = append(log, "enter:"+name) },
					Exit:  func() { log = append(log, "exit:"+name) },
				}
			}
			combat := flow.NewHSM("approach")
			combat.AddState("approach", rec("approach"))
			combat.AddState("attack", rec("attack"))
			combat.AddEventTransition("approach", "attack", "in_range")

			root := flow.NewHSM("patrol")
			root.AddState("patrol", rec("patrol"))
			root.AddState("combat", rec("combat"))
			root.AddChild("combat", combat)
			root.AddState("stunned", rec("stunned"))
			root.AddEventTransition("patrol", "combat", "spot")
			root.AddEventTransition("combat", "stunned", "stun")
			root.Start()
			log = nil
			return root, &log
		}

		// Order 1: spot -> in_range -> stun all processed in send order on one step.
		m1, log1 := build()
		m1.Send("spot")
		m1.Send("in_range")
		m1.Send("stun")
		// Events must NOT fire before Update.
		if !slices.Equal(m1.Current(), []string{"patrol"}) {
			t.Fatalf("before Update current = %v, want [patrol]", m1.Current())
		}
		m1.Update(16 * time.Millisecond)
		if !slices.Equal(m1.Current(), []string{"stunned"}) {
			t.Fatalf("order 1 current = %v, want [stunned]", m1.Current())
		}
		want1 := []string{
			"exit:patrol", "enter:combat", "enter:approach",
			"exit:approach", "enter:attack",
			"exit:attack", "exit:combat", "enter:stunned",
		}
		if !slices.Equal(*log1, want1) {
			t.Fatalf("order 1 log = %v, want %v", *log1, want1)
		}

		// Order 2: stun sent BEFORE spot -> stun ignored in patrol, then spot enters combat/approach, then in_range enters attack.
		m2, log2 := build()
		m2.Send("stun")
		m2.Send("spot")
		m2.Send("in_range")
		m2.Update(16 * time.Millisecond)
		if !slices.Equal(m2.Current(), []string{"combat", "attack"}) {
			t.Fatalf("order 2 current = %v, want [combat attack]", m2.Current())
		}
		want2 := []string{
			"exit:patrol", "enter:combat", "enter:approach",
			"exit:approach", "enter:attack",
		}
		if !slices.Equal(*log2, want2) {
			t.Fatalf("order 2 log = %v, want %v", *log2, want2)
		}

		// Events are consumed once: next Update with no new events stays in [combat, attack].
		*log2 = nil
		m2.Update(16 * time.Millisecond)
		if !slices.Equal(m2.Current(), []string{"combat", "attack"}) || len(*log2) != 0 {
			t.Fatalf("next step current=%v log=%v, want unchanged", m2.Current(), *log2)
		}
	})

	t.Run("EventsSentDuringCallbacksWaitForNextStep", func(t *testing.T) {
		var hsm *flow.HSM[string]
		hsm = flow.NewHSM("a")
		hsm.AddState("a", flow.State{})
		hsm.AddState("b", flow.State{
			Enter: func() {
				// Sending inside Enter must NOT cascade within the same Update step.
				hsm.Send("to_c")
			},
		})
		hsm.AddState("c", flow.State{})
		hsm.AddEventTransition("a", "b", "to_b")
		hsm.AddEventTransition("b", "c", "to_c")

		hsm.Start()
		hsm.Send("to_b")

		hsm.Update(16 * time.Millisecond)
		if !slices.Equal(hsm.Current(), []string{"b"}) {
			t.Fatalf("step 1 current = %v, want [b] (deferred to_c must wait for step 2)", hsm.Current())
		}

		hsm.Update(16 * time.Millisecond)
		if !slices.Equal(hsm.Current(), []string{"c"}) {
			t.Fatalf("step 2 current = %v, want [c]", hsm.Current())
		}
	})

	t.Run("GuardedAndNonComparableEvents", func(t *testing.T) {
		type nonComparable struct{ Tags []string }
		shielded := true

		hsm := flow.NewHSM("idle")
		hsm.AddState("idle", flow.State{})
		hsm.AddState("hurt", flow.State{})
		hsm.AddEventTransition("idle", "hurt", "hit", func() bool { return !shielded })

		hsm.Start()
		// Non-comparable event must not panic when compared against comparable transition events.
		hsm.Send(nonComparable{Tags: []string{"test"}})
		hsm.Send("hit")
		hsm.Update(16 * time.Millisecond)
		if !slices.Equal(hsm.Current(), []string{"idle"}) {
			t.Fatalf("while shielded current = %v, want [idle]", hsm.Current())
		}

		shielded = false
		hsm.Send("hit")
		hsm.Update(16 * time.Millisecond)
		if !slices.Equal(hsm.Current(), []string{"hurt"}) {
			t.Fatalf("when unshielded current = %v, want [hurt]", hsm.Current())
		}
	})
}

func TestHSMBusIntegrationAndTypedEvents(t *testing.T) {
	type spotEvent struct{ Distance float64 }
	type hitEvent struct{ Poise float64 }
	type dieEvent struct{}

	var bus flow.Bus

	combat := flow.NewHSM("approach")
	combat.AddState("approach", flow.State{})
	combat.AddState("attack", flow.State{})
	flow.OnEvent(combat, "approach", "attack", func(e spotEvent) bool { return e.Distance <= 2.0 })

	root := flow.NewHSM("patrol")
	root.AddState("patrol", flow.State{})
	root.AddState("combat", flow.State{})
	root.AddChild("combat", combat)
	root.AddState("stunned", flow.State{})
	root.AddState("dead", flow.State{})

	flow.OnEvent(root, "patrol", "combat", func(e spotEvent) bool { return e.Distance <= 10.0 })
	flow.OnEvent(root, "combat", "stunned", func(e hitEvent) bool { return e.Poise >= 15.0 })
	flow.OnAnyEvent[dieEvent](root, "dead", nil)

	unsubSpot := flow.BindBus[spotEvent](root, &bus)
	unsubHit := flow.BindBus[hitEvent](root, &bus)
	unsubDie := flow.BindBus[dieEvent](root, &bus)
	defer unsubSpot()
	defer unsubHit()
	defer unsubDie()

	root.Start()

	// Enqueue multiple bus events and flush before step: deterministic FIFO order.
	flow.Enqueue(&bus, spotEvent{Distance: 8.0}) // patrol -> combat/approach
	flow.Enqueue(&bus, spotEvent{Distance: 1.5}) // approach -> attack
	flow.Enqueue(&bus, hitEvent{Poise: 5.0})     // guard false (5 < 15)
	bus.Flush()

	// Nothing changes until Update runs.
	if !slices.Equal(root.Current(), []string{"patrol"}) {
		t.Fatalf("after Flush before Update: current = %v, want [patrol]", root.Current())
	}

	root.Update(20 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"combat", "attack"}) {
		t.Fatalf("step 1 current = %v, want [combat attack]", root.Current())
	}

	// Publish high-poise hit on bus -> combat -> stunned.
	flow.Publish(&bus, hitEvent{Poise: 20.0})
	root.Update(20 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"stunned"}) {
		t.Fatalf("step 2 current = %v, want [stunned]", root.Current())
	}

	// Unsubscribe dieEvent, verify it no longer transitions.
	unsubDie()
	flow.Publish(&bus, dieEvent{})
	root.Update(20 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"stunned"}) {
		t.Fatalf("after unsubDie current = %v, want [stunned]", root.Current())
	}

	// Re-bind and verify wildcard typed event transitions to dead.
	unsubDie2 := flow.BindBus[dieEvent](root, &bus)
	defer unsubDie2()
	flow.Publish(&bus, dieEvent{})
	root.Update(20 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"dead"}) {
		t.Fatalf("after dieEvent current = %v, want [dead]", root.Current())
	}
}

func TestHSMShallowHistory(t *testing.T) {
	var log []string
	rec := func(name string) flow.State {
		return flow.State{
			Enter: func() { log = append(log, "enter:"+name) },
			Exit:  func() { log = append(log, "exit:"+name) },
		}
	}

	// 3-level hierarchy to confirm history is SHALLOW:
	// root
	// ├── patrol
	// └── combat (History = true, initial = approach)
	//     ├── approach
	//     └── melee (History = false, initial = windup)
	//         ├── windup
	//         └── strike
	melee := flow.NewHSM("windup")
	melee.AddState("windup", rec("windup"))
	melee.AddState("strike", rec("strike"))
	melee.AddEventTransition("windup", "strike", "swing")

	combat := flow.NewHSM("approach")
	combat.SetHistory(true)
	if !combat.History() {
		t.Fatal("combat.History() = false, want true")
	}
	combat.AddState("approach", rec("approach"))
	combat.AddState("melee", rec("melee"))
	combat.AddChild("melee", melee)
	combat.AddEventTransition("approach", "melee", "engage")

	root := flow.NewHSM("patrol")
	root.AddState("patrol", rec("patrol"))
	root.AddState("combat", rec("combat"))
	root.AddChild("combat", combat)
	root.AddEventTransition("patrol", "combat", "spot")
	root.AddEventTransition("combat", "patrol", "flee")

	root.Start()

	// Enter combat -> approach -> melee -> windup -> strike.
	root.Send("spot")
	root.Send("engage")
	root.Send("swing")
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"combat", "melee", "strike"}) {
		t.Fatalf("before flee: current = %v, want [combat melee strike]", root.Current())
	}

	// Leave combat -> patrol.
	log = nil
	root.Send("flee")
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"patrol"}) {
		t.Fatalf("after flee: current = %v, want [patrol]", root.Current())
	}
	wantExit := []string{"exit:strike", "exit:melee", "exit:combat", "enter:patrol"}
	if !slices.Equal(log, wantExit) {
		t.Fatalf("flee log = %v, want %v", log, wantExit)
	}

	// Re-enter combat: shallow history resumes melee (last direct child of combat),
	// while melee itself has History=false so it starts at its initial windup (not strike).
	log = nil
	root.Send("spot")
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"combat", "melee", "windup"}) {
		t.Fatalf("after re-enter with shallow history: current = %v, want [combat melee windup]", root.Current())
	}
	wantReenter := []string{"exit:patrol", "enter:combat", "enter:melee", "enter:windup"}
	if !slices.Equal(log, wantReenter) {
		t.Fatalf("re-enter log = %v, want %v", log, wantReenter)
	}

	// ClearHistory resets combat back to entering its initial state (approach).
	root.Send("flee")
	root.Update(16 * time.Millisecond)
	root.ClearHistory()
	root.Send("spot")
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"combat", "approach"}) {
		t.Fatalf("after ClearHistory re-enter: current = %v, want [combat approach]", root.Current())
	}
}

func TestHSMSetStateAndUpdateReentrancy(t *testing.T) {
	t.Run("SetStateBeforeAndAfterStart", func(t *testing.T) {
		var log []string
		combat := flow.NewHSM("approach")
		combat.AddState("approach", flow.State{
			Enter: func() { log = append(log, "enter:approach") },
			Exit:  func() { log = append(log, "exit:approach") },
		})
		combat.AddState("attack", flow.State{
			Enter: func() { log = append(log, "enter:attack") },
			Exit:  func() { log = append(log, "exit:attack") },
		})

		root := flow.NewHSM("patrol")
		root.AddState("patrol", flow.State{Enter: func() { log = append(log, "enter:patrol") }})
		root.AddState("combat", flow.State{
			Enter: func() { log = append(log, "enter:combat") },
			Exit:  func() { log = append(log, "exit:combat") },
		})
		root.AddChild("combat", combat)

		// SetState before Start enters target directly without exiting initial state.
		root.SetState("attack")
		if !slices.Equal(root.Current(), []string{"combat", "attack"}) || root.Previous() != "patrol" {
			t.Fatalf("SetState before Start: current=%v prev=%v", root.Current(), root.Previous())
		}
		if !slices.Equal(log, []string{"enter:combat", "enter:attack"}) {
			t.Fatalf("log = %v, want [enter:combat enter:attack]", log)
		}

		// SetState to current leaf is a no-op.
		log = nil
		root.SetState("attack")
		if len(log) != 0 {
			t.Fatalf("SetState(current leaf) ran callbacks: %v", log)
		}

		// SetState on child machine switches within parent.
		combat.SetState("approach")
		if !slices.Equal(root.Current(), []string{"combat", "approach"}) {
			t.Fatalf("combat.SetState(approach): current=%v", root.Current())
		}
		if !slices.Equal(log, []string{"exit:attack", "enter:approach"}) {
			t.Fatalf("child SetState log = %v", log)
		}
	})

	t.Run("ParentUpdateSetStateSkipsExitedChildUpdate", func(t *testing.T) {
		childUpdated := false
		var root *flow.HSM[string]

		sub := flow.NewHSM("leaf")
		sub.AddState("leaf", flow.State{
			Update: func(dt time.Duration) { childUpdated = true },
		})

		root = flow.NewHSM("parent")
		root.AddState("parent", flow.State{
			Update: func(dt time.Duration) {
				root.SetState("other")
			},
		})
		root.AddChild("parent", sub)
		root.AddState("other", flow.State{})

		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"other"}) {
			t.Fatalf("current = %v, want [other]", root.Current())
		}
		if childUpdated {
			t.Fatal("exited child Update ran after parent Update switched state")
		}
	})

	t.Run("AddChildCyclePanics", func(t *testing.T) {
		a := flow.NewHSM("s1")
		b := flow.NewHSM("s2")
		a.AddChild("s1", b)

		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on cycle in AddChild")
			}
		}()
		b.AddChild("s2", a)
	})
}

func TestHSMAutoStartsAndEventPreventsDoublePoll(t *testing.T) {
	polledCond := true
	root := flow.NewHSM("a")
	root.AddState("a", flow.State{})
	root.AddState("b", flow.State{})
	root.AddState("c", flow.State{})
	root.AddEventTransition("a", "b", "go_b")
	root.AddTransition("b", "c", func() bool { return polledCond })

	// Send before first Update: auto-starts in "a", applies event to "b", does NOT immediately poll "b -> c" on the same step.
	root.Send("go_b")
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"b"}) {
		t.Fatalf("step 1 current = %v, want [b]", root.Current())
	}

	// Next step with no events: polled transition b -> c fires.
	root.Update(16 * time.Millisecond)
	if !slices.Equal(root.Current(), []string{"c"}) {
		t.Fatalf("step 2 current = %v, want [c]", root.Current())
	}
}

func TestHSMEdgeCases(t *testing.T) {
	t.Run("AnyEventTransitionDoesNotReenterActiveTarget", func(t *testing.T) {
		enters := 0
		combat := flow.NewHSM("approach")
		combat.AddState("approach", flow.State{})

		root := flow.NewHSM("combat")
		root.AddState("combat", flow.State{Enter: func() { enters++ }})
		root.AddChild("combat", combat)
		root.AddState("dead", flow.State{})
		root.AddAnyEventTransition("combat", "fight")
		root.AddAnyEventTransition("dead", "die")

		root.Start()
		if enters != 1 {
			t.Fatalf("enters = %d, want 1", enters)
		}
		// Already in [combat, approach]: any-event targeting combat must not re-enter combat.
		root.Send("fight")
		root.Update(16 * time.Millisecond)
		if enters != 1 || !slices.Equal(root.Current(), []string{"combat", "approach"}) {
			t.Fatalf("after fight while in combat: enters=%d current=%v", enters, root.Current())
		}

		// Transition to dead, then fight re-enters combat.
		root.Send("die")
		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"dead"}) {
			t.Fatalf("after die: current=%v", root.Current())
		}
		root.Send("fight")
		root.Update(16 * time.Millisecond)
		if enters != 2 || !slices.Equal(root.Current(), []string{"combat", "approach"}) {
			t.Fatalf("after fight from dead: enters=%d current=%v", enters, root.Current())
		}
	})

	t.Run("SelfTransitionReentersSubtree", func(t *testing.T) {
		var log []string
		sub := flow.NewHSM("sub1")
		sub.AddState("sub1", flow.State{
			Enter: func() { log = append(log, "enter:sub1") },
			Exit:  func() { log = append(log, "exit:sub1") },
		})
		sub.AddState("sub2", flow.State{
			Enter: func() { log = append(log, "enter:sub2") },
			Exit:  func() { log = append(log, "exit:sub2") },
		})
		sub.AddEventTransition("sub1", "sub2", "next")

		root := flow.NewHSM("p")
		root.AddState("p", flow.State{
			Enter: func() { log = append(log, "enter:p") },
			Exit:  func() { log = append(log, "exit:p") },
		})
		root.AddChild("p", sub)
		root.AddEventTransition("p", "p", "restart")

		root.Start()
		root.Send("next")
		root.Update(16 * time.Millisecond)
		log = nil

		root.Send("restart")
		root.Update(16 * time.Millisecond)
		want := []string{"exit:sub2", "exit:p", "enter:p", "enter:sub1"}
		if !slices.Equal(log, want) || !slices.Equal(root.Current(), []string{"p", "sub1"}) {
			t.Fatalf("self-transition log=%v current=%v, want %v [p sub1]", log, root.Current(), want)
		}
	})

	t.Run("ChildSendAndAddStatePreservesChildAndDetach", func(t *testing.T) {
		sub := flow.NewHSM("s1")
		sub.AddState("s1", flow.State{})
		sub.AddState("s2", flow.State{})
		sub.AddEventTransition("s1", "s2", "go")

		root := flow.NewHSM("p")
		// Attach child BEFORE registering callbacks for p.
		root.AddChild("p", sub)
		pEntered := false
		root.AddState("p", flow.State{Enter: func() { pEntered = true }})

		// Calling Update on an attached child is a no-op.
		sub.Update(16 * time.Millisecond)
		if pEntered {
			t.Fatal("child.Update started root")
		}

		// Sending via child delegates to root.
		root.Start()
		if !pEntered || !slices.Equal(root.Current(), []string{"p", "s1"}) {
			t.Fatalf("after Start: pEntered=%v current=%v", pEntered, root.Current())
		}
		sub.Send("go")
		root.Update(16 * time.Millisecond)
		if !slices.Equal(root.Current(), []string{"p", "s2"}) {
			t.Fatalf("after child.Send: current=%v, want [p s2]", root.Current())
		}

		// Current returns a defensive copy.
		cur := root.Current()
		cur[0] = "mutated"
		if !slices.Equal(root.Current(), []string{"p", "s2"}) {
			t.Fatalf("mutating Current() corrupted HSM: %v", root.Current())
		}

		// Detaching child with nil allows re-attaching elsewhere.
		root.AddChild("p", nil)
		other := flow.NewHSM("q")
		other.AddChild("q", sub)
		other.Start()
		if !slices.Equal(other.Current(), []string{"q", "s1"}) {
			t.Fatalf("re-attached current = %v, want [q s1]", other.Current())
		}
	})

	t.Run("ReattachWithoutDetachPanics", func(t *testing.T) {
		sub := flow.NewHSM("s1")
		a := flow.NewHSM("a")
		b := flow.NewHSM("b")
		a.AddChild("a", sub)

		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic when attaching already-attached child to another parent")
			}
		}()
		b.AddChild("b", sub)
	})

	t.Run("ReentrantSetStateInEnterDefersCleanly", func(t *testing.T) {
		var log []string
		var root *flow.HSM[string]
		root = flow.NewHSM("a")
		root.AddState("a", flow.State{
			Enter: func() { log = append(log, "enter:a") },
			Exit:  func() { log = append(log, "exit:a") },
		})
		root.AddState("b", flow.State{
			Enter: func() {
				log = append(log, "enter:b")
				root.SetState("c")
			},
			Exit: func() { log = append(log, "exit:b") },
		})
		root.AddState("c", flow.State{
			Enter: func() { log = append(log, "enter:c") },
		})

		root.Start()
		root.SetState("b")
		want := []string{"enter:a", "exit:a", "enter:b", "exit:b", "enter:c"}
		if !slices.Equal(log, want) || !slices.Equal(root.Current(), []string{"c"}) {
			t.Fatalf("re-entrant SetState log=%v current=%v, want %v [c]", log, root.Current(), want)
		}
	})

	t.Run("NilAndZeroSafetyAndNegativeDt", func(t *testing.T) {
		var nilHSM *flow.HSM[string]
		nilHSM.Send("x")
		flow.OnEvent[string](nilHSM, "a", "b", nil)
		flow.OnAnyEvent[string](nilHSM, "b", nil)
		unsub := flow.BindBus[string](nilHSM, nil)
		unsub()

		hsm := flow.NewHSM("a")
		hsm.Send(nil)
		hsm.AddEventTransition("a", "b", nil)
		hsm.AddAnyEventTransition("b", nil)
		hsm.Update(-10 * time.Millisecond)
		if hsm.TimeInState() != 0 || hsm.TimeIn("missing") != 0 {
			t.Fatalf("negative dt advanced TimeInState: %v", hsm.TimeInState())
		}
	})
}
