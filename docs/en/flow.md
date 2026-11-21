# flow

`game/flow` holds gameplay control-flow primitives driven exclusively by `game.Loop`'s fixed `dt`: game-clock `Timers`, typed finite state machines (`Machine[S]`), composable step sequences (`Seq`, `Parallel`, `Race`), and a zero-global-state typed event `Bus`.

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
- Force an immediate switch with `fsm.SetState(to)`. Inspect `fsm.Current()`, `fsm.Previous()`, and `fsm.TimeInState()`.

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
