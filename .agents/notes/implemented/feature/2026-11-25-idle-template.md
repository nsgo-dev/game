# Agent Note: The idle template, Hearthkeeper

Status: implemented

## Problem

Issue #800 asks for an `idle` template under #786's rules: an original, small idle game with generators, upgrades, large-number formatting, offline progress on return and prestige, built mainly on `idle`, `save` and `gameui`, playable with keyboard, controller and touch, and an nsgotest playthrough that reaches a win and a loss. Idle games rarely have a loss, so the genre equivalent had to be designed rather than borrowed, and offline progress had to stay consistent with whatever can lose.

## Decision

`template/idle` is the module `github.com/nsgo-dev/game/template/idle`; its project is Hearthkeeper, a fire to keep alive.

- **Loss is a fuel gauge.** The fire burns fuel every second and goes out at zero, which loses the run; stoking adds fuel, and fuel upgrades (Bellows, Coal Chute) feed it until it sustains itself. Rekindling starts a new run and keeps the ash. The win is buying the Everflame (1e12 embers), after which play goes on.
- **Offline progress replays the rules.** `hearth.creditAway` uses `idle.Offline.Absence` for the minimum, the cap and clock rewinds, then replays the credited time with `idle.Away.Steps` in one-second steps through the same `hearth.step` the loop runs, at the offline efficiency. A fire that is not self-sustaining therefore goes out while the player is away, exactly as it would online.
- **Rules apart from NSGo.** `sim.go` holds the whole game state with no view and no clock, so the loop, the offline replay, the balance test and the playthroughs run one implementation. `game.go` owns the loop, actions, lifecycle and saves; `shop.go` owns the `gameui` tree.
- **Balance in one versioned YAML file.** `hearth.yaml` (fuel, generators, upgrades, prestige, goal, offline rules) is read with `KnownFields`, refuses trailing documents and out-of-range values, and `encodeData` writes it back byte for byte. Amounts are `idle.Number` text, so a designer can write `"1e400"`.
- **One shop for every input.** The shop is a `gameui` tree drawn on the canvas inside the `screen.Viewport` from `game.yaml`'s display; canvas presses map into the virtual resolution and reach `Root.OnPress`, and keyboard and controller actions from `game.yaml`'s input map drive a `SpatialNavigator`. Buy buttons stay focusable when unaffordable and show it in their frame, so the focus never jumps away under a controller player.
- **Saves are by id and never clobber.** `savedGame` stores generators and upgrades by id, numbers as text, the notation and the last-seen wall time, and `LoadChecked` refuses values the game cannot use. A save that cannot be read turns saving off and shows why, instead of overwriting it with a new game.
- **The pin is the current main.** `go.mod.tmpl` requires `packages/game` at `v0.0.0-20261125062210-04dfb19bc6b7`: the starter's pin predates the `key` and `gamepadButton` binding kinds in `game.yaml`'s input map, which this template uses.

## Alternatives considered

- **Crediting offline time with `idle.Offline.Earn` (rate × time).** One multiplication instead of a replay and the package's own helper, but it ignores the fuel gauge, so leaving the game would be the one way to dodge the loss. Replaying 8 hours in 1-second steps is 28,800 cheap steps and keeps one set of rules.
- **A timed challenge as the loss.** Common in the genre, but it adds a second mode and makes the loss optional; the fuel gauge makes the loss part of the core loop and teaches automation.
- **`tooling/datatable` for generators and upgrades.** It gives versioned YAML tables for free, but its columns are primitive types, so costs would be float64 rather than `idle.Number`, and the file needs scalar sections (fuel, prestige, goal, offline) that a table of rows does not hold.
- **Native NSGo views for the shop.** Better for screen readers and simpler to query in tests, but controllers would need a hand-written focus model; `gameui` brings spatial navigation and is the package the issue names. The HUD and the cards stay native.
- **Keeping the starter's pin.** Fewer moving parts, but keys and controller buttons would have to be bound in code, and the template would build against a `packages/game` older than the one its workspace tests use.

## Consequences

- The playthrough test takes about 60 eight-hour absences to reach the win with offline progress alone (a few seconds headless); `TestBalanceReachesTheGoal` pins the pace at 4 to 20 sessions of ten minutes' play plus an absence, so a change to `hearth.yaml` that breaks the pace fails a test.
- Each new run starts unautomated, so the first minute after a prestige is active play by design.
- The shop's buttons are drawn on the canvas and are not individually exposed to assistive technology; the canvas carries one label and the HUD is native text.
- When the template's next `go.mod.tmpl` pin moves, its `go.sum` is regenerated with `GOWORK=off go mod tidy` in a copy outside the repository, as `docs/en/templates.md` describes.

## Verification

- `go tool nsgo test ./template/idle/template` from the repository root, and the same copy with `GOWORK=off` against the pinned version, as CI's `templates` job runs it.
- `go tool nsgo test ./cmd/game` from `packages/game` for the registry, the embedded copy and the docs rows.
