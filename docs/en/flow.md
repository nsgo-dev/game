# flow

`game/flow` holds gameplay control-flow primitives driven exclusively by `game.Loop`'s fixed `dt`: game-clock `Timers`, typed finite state machines (`Machine[S]`), hierarchical state machines (`HSM[S]`), composable step sequences (`Seq`, `Parallel`, `Race`), and a zero-global-state typed event `Bus`.

```go
import "github.com/nsgo-dev/game/packages/game/flow"
```

Nothing in `flow` reads the wall clock, spawns goroutines, or holds package-level state — every primitive pauses with your loop, replays deterministically, and isolates cleanly per scene.

## Game-clock Timers

`Timers` schedules one-shot (`After`) and drift-free repeating (`Every`) callbacks on the game clock. Its zero value is ready to use:

```go
var timers flow.Timers

// Fire once after 2 seconds of game time
invuln := timers.After(2*time.Second, func() {
	player.Invulnerable = false
})

// Repeat every 500ms, carrying overshoot so periods never drift
spawner := timers.Every(500*time.Millisecond, spawnWave)

// Inside Loop.Update:
timers.Update(dt)
```

Pause timers during menus or hit-stop with `timers.SetPaused(true)` (or `timers.Paused = true`), inspect remaining time with `invuln.Remaining()` / `invuln.Active()`, cancel with `spawner.Cancel()`, or cancel all with `timers.Clear()`.

## Finite state machines (`Machine[S]`)

`Machine[S]` models character, boss, or match states keyed by any comparable type `S`:

```go
type HeroState int

const (
	StateIdle HeroState = iota
	StateRun
	StateHurt
)

fsm := flow.NewMachine(StateIdle)
fsm.AddState(StateIdle, flow.State{
	Enter:  func() { playAnim("idle") },
	Update: func(dt time.Duration) { recoverStamina(dt) },
})
fsm.AddState(StateRun, flow.State{
	Enter:  func() { playAnim("run") },
	Update: func(dt time.Duration) { moveHero(dt) },
	Exit:   func() { stopFootsteps() },
})
fsm.AddState(StateHurt, flow.State{
	Enter: func() { knockback() },
})

fsm.AddTransition(StateIdle, StateRun, func() bool { return moveSpeed > 0 })
fsm.AddTransition(StateRun, StateIdle, func() bool { return moveSpeed == 0 })
fsm.AddAnyTransition(StateHurt, func() bool { return hitThisStep })
fsm.AddTransition(StateHurt, StateIdle, func() bool { return fsm.TimeInState() >= 400*time.Millisecond })

// Inside Loop.Update:
fsm.Update(dt)
```

- `Update(dt)` starts the machine on its first call (or call `fsm.Start()` explicitly), evaluates transitions in declaration order (taking at most one per step), advances `TimeInState()`, and runs the active state's `Update`.
- Force an immediate switch with `fsm.SetState(to)`. Inspect `fsm.Current()`, `fsm.Previous()`, `fsm.TimeInState()`, and `fsm.Started()`.
- **Save and restore**: `snap := fsm.Snapshot()` returns a plain `MachineSnapshot[S]` struct (`Version`, `Current`, `Previous`, `TimeInState`, `Started`) that marshals cleanly to YAML and JSON. `fsm.Restore(snap)` validates that `Current` (and any non-zero `Previous`) belongs to the machine before replacing its state, refuses unknown states or invalid timings with `ErrInvalidSnapshot` without mutating the machine, and never runs `Exit` or `Enter` hooks.

## Hierarchical state machines (`HSM[S]`)

When character or enemy logic outgrows a flat machine, `HSM[S]` nests child machines inside parent states so shared interrupts (such as `stunned` or `dead`) apply across every descendant without duplicating transitions:

```go
type EnemyState string

const (
	StatePatrol   EnemyState = "patrol"
	StateCombat   EnemyState = "combat"
	StateApproach EnemyState = "approach"
	StateAttack   EnemyState = "attack"
	StateRecover  EnemyState = "recover"
	StateStunned  EnemyState = "stunned"
)

type StunHit struct{ Poise float64 }

combat := flow.NewHSM(StateApproach)
combat.SetHistory(true) // re-entering combat resumes its last active sub-state
combat.AddState(StateApproach, flow.State{Enter: func() { playAnim("approach") }})
combat.AddState(StateAttack, flow.State{Enter: func() { playAnim("attack") }})
combat.AddState(StateRecover, flow.State{Enter: func() { playAnim("recover") }})
combat.AddTransition(StateApproach, StateAttack, func() bool { return inRange })
combat.AddEventTransition(StateAttack, StateRecover, "attack_done")
combat.AddTransition(StateRecover, StateApproach, func() bool { return combat.TimeInState() >= 400*time.Millisecond })

enemy := flow.NewHSM(StatePatrol)
enemy.AddState(StatePatrol, flow.State{Enter: func() { playAnim("patrol") }})
enemy.AddState(StateCombat, flow.State{
	Enter: func() { drawWeapon() },
	Exit:  func() { sheatheWeapon() },
})
enemy.AddChild(StateCombat, combat)
enemy.AddState(StateStunned, flow.State{Enter: func() { playAnim("stunned") }})

enemy.AddTransition(StatePatrol, StateCombat, func() bool { return spotted })
flow.OnEvent(enemy, StateCombat, StateStunned, func(e StunHit) bool { return e.Poise >= 15 })
enemy.AddTransition(StateStunned, StateCombat, func() bool { return enemy.TimeInState() >= 800*time.Millisecond })

// Optional: forward typed events from a flow.Bus into the HSM queue.
unsub := flow.BindBus[StunHit](enemy, &bus)
defer unsub()

// Inside Loop.Update:
enemy.Update(dt)
```

- **Outside-in `Enter` / inside-out `Exit`**: entering `StateCombat` runs `combat`'s `Enter` before its active child's `Enter`; leaving `StateCombat` runs the child's `Exit` before `combat`'s `Exit`. Cross-branch transitions exit up to the lowest common ancestor and enter down to the target leaf.
- **Innermost-first transitions**: transitions declared on a parent apply while any descendant is active, with the innermost active state checked first so child states can override parent transitions.
- **Deterministic events**: send events with `enemy.Send(ev)` or subscribe a `flow.Bus` event type with `flow.BindBus[E](enemy, &bus)`. Register value-matched transitions with `AddEventTransition` / `AddAnyEventTransition` or typed payload transitions with `flow.OnEvent` / `flow.OnAnyEvent`. Queued events are processed once per `Update` step in FIFO send order; events sent during callbacks wait for the next step.
- **Shallow history**: call `child.SetHistory(true)` so re-entering its parent resumes the last active direct child instead of `initial` (reset with `ClearHistory()`).
- **Path inspection**: `enemy.Current()` returns the active path from root to leaf (`[]S`), `enemy.Leaf()` returns the innermost active state, `enemy.In(state)` checks whether a state is anywhere on the active path, and `enemy.TimeIn(state)` reports elapsed game time in an active state.

## Step sequences (`Runner`, `Seq`, `Parallel`, `Race`)

Compose cutscenes, attack patterns, and scripted encounters without goroutines using `Runner` and `Step` combinators:

```go
var runner flow.Runner

bossIntro := flow.Seq(
	flow.Do(func() { lockPlayerInput(true) }),
	flow.Parallel(
		flow.Wait(600*time.Millisecond),
		flow.While(func() bool { return doorOpen < 1 }, func(dt time.Duration) {
			doorOpen += float32(dt.Seconds()) * 2
		}),
	),
	flow.Race(
		flow.WaitUntil(func() bool { return actions.JustPressed("skip") }),
		flow.Seq(
			flow.Do(func() { showBossTitle() }),
			flow.Wait(2*time.Second),
		),
	),
	flow.Do(func() { lockPlayerInput(false) }),
)

handle := runner.Start(bossIntro)

// Inside Loop.Update:
runner.Update(dt)
```

Built-in `Step` combinators:

| Combinator | Behavior |
| --- | --- |
| `Do(fn)` | Calls `fn` once and finishes immediately, passing all remaining `dt` onward. |
| `Wait(d)` | Waits for `d` of game time to elapse. |
| `WaitUntil(cond)` | Finishes on the first step where `cond()` returns `true`. |
| `While(cond, update)` | Runs `update(dt)` each step while `cond()` is `true`. |
| `Seq(steps...)` | Runs steps in order, carrying leftover `dt` from a finished step into the next on the same tick. |
| `Parallel(steps...)` | Steps all branches each tick until every branch finishes. |
| `Race(steps...)` | Steps branches in order until the first finishes, then cancels the rest. |
| `Repeat(n, makeStep)` | Repeats the step from `makeStep()` `n` times sequentially. |
| `Loop(makeStep)` | Repeats indefinitely until its `Handle` or enclosing `Race` cancels it. |

Cancel an in-flight sequence cleanly with `handle.Cancel()` (or `runner.Clear()`), and check `handle.Done()`.

## Typed event Bus

`Bus` decouples systems inside a scene without globals or string keys:

```go
type EnemyDefeated struct {
	Kind  string
	Score int
}

var bus flow.Bus

unsub := flow.Subscribe(&bus, func(ev EnemyDefeated) {
	score += ev.Score
})
defer unsub()

// Immediate synchronous delivery in subscription order:
flow.Publish(&bus, EnemyDefeated{Kind: "slime", Score: 50})

// Or buffer events during physics/collision and flush once per step:
flow.Enqueue(&bus, EnemyDefeated{Kind: "bat", Score: 100})
bus.Flush()
```

Use `flow.SubscribeOnce(&bus, fn)` for one-shot listeners, and `bus.Clear()` when tearing down a scene. Subscribing or unsubscribing inside a handler never skips remaining listeners for the event in flight.
