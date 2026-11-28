# flow

`game/flow` holds gameplay control-flow primitives driven exclusively by `game.Loop`'s fixed `dt`: game-clock `Timers`, typed finite state machines (`Machine[S]`), hierarchical state machines (`HSM[S]`), composable step sequences (`Seq`, `Parallel`, `Race`), and a zero-global-state typed event `Bus`.

```go
import "github.com/nsgo-dev/game/pkg/flow"
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
- Force an immediate switch with `fsm.SetState(to)`. Inspect `fsm.Current()`, `fsm.Previous()`, `fsm.TimeInState()`, `fsm.Started()`, and `fsm.Step()`.
- **Save and restore**: `snap := fsm.Snapshot()` returns a plain `MachineSnapshot[S]` struct (`Version`, `Current`, `Previous`, `TimeInState`, `Started`) that marshals cleanly to YAML and JSON. `fsm.Restore(snap)` validates that `Current` (and any non-zero `Previous`) belongs to the machine before replacing its state, refuses unknown states or invalid timings with `ErrInvalidSnapshot` without mutating the machine, and never runs `Exit` or `Enter` hooks.
- **Bounded transition history**: `Machine[S]` keeps the most recent `DefaultHistoryLimit` (32) transitions in a fixed ring buffer (`TransitionRecord[S]{From, To, Step, Transition}`), tunable with `SetHistoryLimit(n)` (`0` disables recording) and cleared with `ClearHistory()`. Label transitions with `AddNamedTransition` / `AddNamedAnyTransition` (unnamed transitions default to `"from->to"` / `"*->to"`, and `SetState` records `"SetState"`). Read history with `History()` or zero-allocation `HistoryInto(dst)`.
- **Inspection and `statedump` formatting**: every `*Machine[S]` implements the type-erased `MachineInspector` interface (`Inspect() MachineInspection`, `FormatState() string`) so you can pass any machine to [`game/pkg/tooling/gamedebug`](gamedebug.md)'s AI panel or record `fsm.FormatState()` in [`statedump`](statedump.md) `Values` to diff transitions between runs.

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

## Shared variables and switches (`Vars` and `vars.yaml`)

Treat `Vars` as the authoritative project state. `SetFromExpr` accepts expression values only when the declaration's kind and bounds permit them, and returns `ErrUndeclaredVar` for names absent from `vars.yaml`. Register `OnChange` listeners when UI, quests, or scene rules need synchronous updates, and use `Snapshot`/`Restore` for save checkpoints.

Declare project-wide switches (`bool`) and typed variables (`int`, `float`, `string`) once in a versioned `vars.yaml` (or `*.vars.yaml`) file so dialogue scripts, quests, scene rules, saves, and the editor's remote inspector share a single source of truth:

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/vars.v1.json
version: 1
vars:
  - name: bridge_repaired
    kind: bool
    doc: True once the east bridge has been repaired.
  - name: gold
    kind: int
    default: 50
    min: 0
    max: 9999
    doc: Player gold coins.
  - name: mira_affinity
    kind: float
    default: 0.5
    min: 0
    max: 1
    doc: Relationship score with Mira.
  - name: hero_name
    kind: string
    default: Aria
    doc: Display name of the protagonist.
```

Load declarations with `flow.LoadVarsFS(fsys, "vars.yaml")` (or `flow.ParseVars(data)`) and construct a runtime store with `flow.NewVars(doc)`:

```go
doc, err := flow.LoadVarsFS(assetsFS, flow.VarsFileName)
if err != nil {
	return err
}
vars, err := flow.NewVars(doc)
if err != nil {
	return err
}

// Observe mutations synchronously in registration order (empty name observes all variables):
unsub := vars.OnChange("bridge_repaired", func(ch flow.VarChange) {
	unlockEastBridge(ch.New.(bool))
})
defer unsub()

// Typed reads and writes enforce declared kinds and [min, max] bounds:
_ = vars.SetInt("gold", 120)
gold, _ := vars.Int("gold")

// Scoped per-object self variables for instance state ("this chest was opened"):
vars.Self("chest_01").SetBool("opened", true)
opened := vars.Self("chest_01").Bool("opened")
```

- **Shared by dialogue**: pass `dialogue.Options{Vars: vars}` to `dialogue.NewRunner` ([dialogue](dialogue.md)) so `<<if>>`, choice guards, `{expr}` interpolations, and `<<set>>` read and write the shared store directly instead of a private map. When `Vars` is set, `<<declare>>` and `<<set>>` of an undeclared name return `flow.ErrUndeclaredVar`, and writes enforce declared kinds and bounds.
- **Save and restore**: `snap := vars.Snapshot()` returns a plain `flow.VarsSnapshot` (`Version`, `Vars`, `Self`) that marshals cleanly to JSON or YAML for `save.Store`. `vars.Restore(snap)` validates versions, names, kinds, and bounds atomically without invoking `OnChange` listeners, leaving the store unchanged on error (`flow.ErrInvalidSnapshot`). Variables added to `vars.yaml` after an older save keep their declared defaults on restore, and a zero `VarsSnapshot` resets to defaults and clears self variables.
- **Static validation**: `go tool game check` validates every `vars.yaml` / `*.vars.yaml` file and checks every `$name` and `{name}` variable reference in `.yarn` dialogue scripts against the project's declarations, reporting undeclared names with their exact file and line.
- **Live remote inspection**: pass `Vars: vars` (or `VarsFunc`) to `mcp.Config` to expose `game_vars_list`, `game_var_get`, and `game_var_set`; the editor's Remote Inspector lists declared variables with live editable controls alongside active per-object self variables.

## Map events (`ParseMapEvent`, `EventRunner`)

A map event is a chest, sign, door or one-time trigger written as YAML instead of Go: an ordered list of pages, each with conditions, a trigger and a list of commands.

```yaml
version: 1
pages:
  - when:
      self:
        opened: false
    trigger: action
    commands:
      - say:
          text: chest.found_potion
      - wait:
          seconds: 0.5
      - self:
          name: opened
          value: true
  - when:
      self:
        opened: true
    trigger: action
```

- **Format**: `flow.ParseMapEvent` reads version 1 (`flow.MapEventVersion`) and `flow.EncodeMapEvent` writes its canonical form. Unknown fields and commands, a list entry with no command or more than one, an unknown trigger, a negative, infinite or NaN wait, and a `set` or `self` without a name are refused at load with a `loaderr.Error` that names the field and line. A page without `trigger` reads as `action`.
- **Pages**: `doc.ActivePage(self, switches, variables)` returns the last page whose `when` conditions all hold, as in RPG Maker, or -1 when none does: every `self` and `switches` entry equals its value, and every `variables` entry is at least its value. A missing name reads as false or 0.
- **Triggers**: a page's trigger is `action`, `touch`, `autorun` or `parallel` (`flow.TriggerAction` and so on). The runner does not watch input or movement; the game starts the page when its trigger fires, for example `action` when the player faces the event and presses the button.
- **Commands**: `say` passes its text, usually a localization key, to `EventRunner.Say`; `wait` pauses for game seconds; `set` writes an integer to `EventRunner.Variables`; `self` writes a self switch to `EventRunner.Self`.
- **Running**: `flow.NewEventRunner(doc)` is idle until `Start(page)`. `Update(dt)` on each fixed step runs commands until one waits or the page ends, never blocking the loop, and returns true once the runner is idle; time left over when a wait ends carries into the commands after it.
- **Saving mid-event**: `Snapshot` returns a `flow.EventSnapshot` (the page, -1 when idle, the next command, the time left on a wait, and the self switches and variables) that marshals to YAML or JSON. `Restore` refuses a snapshot that does not fit the runner's document with `flow.ErrInvalidEventSnapshot` and leaves the runner unchanged.

The commands are a first set. Choices, conditional branches, item and transfer commands, game-registered commands and integration with `Vars` are not part of the format yet.
