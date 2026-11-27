package flow_test

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/game/pkg/flow"
)

type CoinPicked struct{ Value int }

// A Bus delivers typed events to the handlers subscribed for that type: Publish at once, Enqueue on the next Flush.
func ExampleBus() {
	var bus flow.Bus
	score := 0
	unsubscribe := flow.Subscribe(&bus, func(e CoinPicked) { score += e.Value })
	flow.SubscribeOnce(&bus, func(e CoinPicked) { fmt.Println("first coin:", e.Value) })

	flow.Publish(&bus, CoinPicked{Value: 5})
	flow.Enqueue(&bus, CoinPicked{Value: 10})
	fmt.Println("before flush:", score)
	bus.Flush()
	fmt.Println("after flush:", score)

	unsubscribe()
	flow.Publish(&bus, CoinPicked{Value: 100})
	fmt.Println("unsubscribed:", score)
	// Output:
	// first coin: 5
	// before flush: 5
	// after flush: 15
	// unsubscribed: 15
}

// A Machine switches states when a transition's condition holds, running each state's Enter and Exit.
func ExampleMachine() {
	type mode string
	stamina := 2.0
	m := flow.NewMachine[mode]("run")
	m.AddState("run", flow.State{
		Enter:  func() { fmt.Println("enter run") },
		Update: func(dt time.Duration) { stamina -= dt.Seconds() },
		Exit:   func() { fmt.Println("exit run") },
	})
	m.AddState("rest", flow.State{Enter: func() { fmt.Println("enter rest") }})
	m.AddTransition("run", "rest", func() bool { return stamina <= 0 })
	m.Start()
	for range 3 {
		m.Update(time.Second)
	}
	fmt.Println(m.Current(), m.Previous(), m.TimeInState())
	// Output:
	// enter run
	// exit run
	// enter rest
	// rest run 1s
}

// An HSM nests child machines inside parent states so parent transitions apply to every descendant, running Enter outside-in and Exit inside-out.
func ExampleHSM() {
	combat := flow.NewHSM("approach")
	combat.SetHistory(true)
	combat.AddState("approach", flow.State{
		Enter: func() { fmt.Println("enter approach") },
		Exit:  func() { fmt.Println("exit approach") },
	})
	combat.AddState("attack", flow.State{
		Enter: func() { fmt.Println("enter attack") },
		Exit:  func() { fmt.Println("exit attack") },
	})
	combat.AddEventTransition("approach", "attack", "in_range")

	enemy := flow.NewHSM("combat")
	enemy.AddState("combat", flow.State{
		Enter: func() { fmt.Println("enter combat") },
		Exit:  func() { fmt.Println("exit combat") },
	})
	enemy.AddChild("combat", combat)
	enemy.AddState("stunned", flow.State{
		Enter: func() { fmt.Println("enter stunned") },
		Exit:  func() { fmt.Println("exit stunned") },
	})
	enemy.AddEventTransition("combat", "stunned", "stun")
	enemy.AddEventTransition("stunned", "combat", "recover")

	enemy.Start()
	enemy.Send("in_range")
	enemy.Update(16 * time.Millisecond)
	fmt.Println("active:", enemy.Current())

	enemy.Send("stun")
	enemy.Update(16 * time.Millisecond)
	fmt.Println("active:", enemy.Current())

	enemy.Send("recover")
	enemy.Update(16 * time.Millisecond)
	fmt.Println("resumed:", enemy.Current())
	// Output:
	// enter combat
	// enter approach
	// exit approach
	// enter attack
	// active: [combat attack]
	// exit attack
	// exit combat
	// enter stunned
	// active: [stunned]
	// exit stunned
	// enter combat
	// enter attack
	// resumed: [combat attack]
}

// A Runner plays Steps over game time, like a coroutine: here a cutscene that waits, then does two things at once. Each line shows the game time at the end of the update that ran it.
func ExampleRunner() {
	var r flow.Runner
	elapsed := time.Duration(0)
	log := func(s string) flow.Step { return flow.Do(func() { fmt.Println(elapsed, s) }) }
	h := r.Start(flow.Seq(
		log("fade in"),
		flow.Wait(time.Second),
		flow.Parallel(
			flow.Seq(flow.Wait(500*time.Millisecond), log("door opens")),
			flow.Repeat(2, func() flow.Step { return flow.Seq(flow.Wait(time.Second), log("footstep")) }),
		),
		log("done"),
	))
	for !h.Done() {
		elapsed += 250 * time.Millisecond
		r.Update(250 * time.Millisecond)
	}
	// Output:
	// 250ms fade in
	// 1.5s door opens
	// 2s footstep
	// 3s footstep
	// 3s done
}

// Timers runs callbacks on game time, so pausing the game pauses them too.
func ExampleTimers() {
	var ts flow.Timers
	ticks := 0
	ts.Every(time.Second, func() { ticks++ })
	bomb := ts.After(3*time.Second, func() { fmt.Println("boom") })

	ts.Update(2 * time.Second)
	ts.SetPaused(true)
	ts.Update(10 * time.Second)
	fmt.Println(ticks, bomb.Remaining())
	ts.SetPaused(false)
	ts.Update(time.Second)
	fmt.Println(ticks, bomb.Active())
	// Output:
	// 2 1s
	// boom
	// 3 false
}

const shop = `title: Shop
---
<<declare $gold = 15>>
Merchant: You have {$gold} gold.
-> Buy a potion (10 gold) <<if $gold >= 10>>
    <<set $gold -= 10>>
    <<give_item potion 1>>
    Merchant: Here you go.
-> Buy a sword (50 gold) <<if $gold >= 50>>
    Merchant: A fine choice.
-> Leave
===
`

// A DialogueRunner steps through a script parsed by ParseYarn, returning lines and choices and running game commands.
func ExampleDialogueRunner() {
	script, err := flow.ParseYarn(shop)
	if err != nil {
		fmt.Println(err)
		return
	}
	r := flow.NewDialogueRunner(script, flow.DialogueOptions{
		OnCommand: func(e flow.DialogueEvent) { fmt.Println("command:", e.Command, e.Args) },
	})
	step, _ := r.Start("Shop")
	fmt.Printf("%s: %s\n", step.Line.Speaker, step.Line.Text)
	step, _ = r.Continue()
	for _, c := range step.Choices {
		fmt.Println(c.Index, c.Text, c.Available)
	}
	step, _ = r.Choose(0)
	fmt.Println(step.Kind, step.Line.Text)
	gold, _ := r.Var("gold")
	fmt.Println("gold:", gold)
	// Output:
	// Merchant: You have 15 gold.
	// 0 Buy a potion (10 gold) true
	// 1 Buy a sword (50 gold) false
	// 2 Leave true
	// command: give_item [potion 1]
	// line Here you go.
	// gold: 5
}
