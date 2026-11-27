# Agent Note: The fighting2d template and the data it adds to frame data

Status: implemented

## Problem

Issue #791, under the templates umbrella #786, asks for a one-on-one 2D fighting game template built on `fighting` and `framedata`: two original characters, motion-input specials, combos, guarding, rounds with a timer, a CPU opponent, local two-player on one keyboard or two controllers, playable with keyboard, gamepad and touch, and nsgotest playthroughs that reach a win and a loss. `framedata` resolves hits but has no notion of movement during a move, hit levels (low and overhead), projectiles or chip damage, and `fighting` recognizes inputs per player but the input map in `game.yaml` is one namespace for the whole game. The template has to fill those gaps without changing the shared packages, since fourteen other templates land in parallel.

## Decision

The template lives in the module `github.com/nsgo-dev/game/template/fighting2d` (`template/fighting2d/template`), registered in `packages/game/cmd/game/templates.go` with its byte-for-byte copy under `_templates/projects/fighting2d`, and pins `packages/game` at `0ec1f801` (main as of 2026-11-25), the first commit with game.yaml key and gamepad bindings (#908), which its input map needs.

- **Data.** Each fighter has a `fighting.MoveSet` (`data/<id>.moves.yaml`) and a `framedata.Doc` (`data/<id>.frames.yaml`) in their packages' canonical formats. `data/match.yaml` is the template's own format (version 1, unknown fields refused, `encodeMatch` writes what `parseMatch` reads back unchanged): the rules, the CPU tuning and the roster, where each fighter's `moves` entries add what frame data does not carry: `level` (low or overhead), `special` (chip damage on block), `motion` (displacement over a frame range, a negative `dy` lifting the fighter into the air) and `projectile` (a looping move of the same document thrown as a `framedata.Fighter` on the thrower's team). `loadRoster` checks the references between the three files: every recognized move resolves to a frame data move standing, as `crouch_<name>` crouching or `air_<name>` airborne; the eight stance moves exist and loop; extras, motions, projectiles and CPU scripts name what exists.
- **Simulation.** `match` steps two `player`s with one `control` (absolute direction and held mask) each and knows nothing of devices or the screen. Per step: each player feeds its recognizer, starts the highest-priority recognized move it can, else picks a stance; holding back while threatened is a guard, and `Fighter.Blocking` is set before `World.Step` only when the guard pose matches the incoming move's level, so a wrong guard is simply hit. `World.Step` then finds the hits, which the match applies with damage, combo scaling, chip, knockback, wall pushback and hitstop; motions, gravity, projectiles and pushboxes follow. A round ends on a knockout or the timer; on time the larger share of health left wins, so a sturdier fighter's bigger bar is no advantage, and a double knockout or equal shares is a drawn round that counts as a win for both, which can end the match for the fighter already a round up or as a draw.
- **CPU.** `cpu` decides every `reaction` steps from a seeded PCG and emits controls through the same recognizer, so a special is a script of directions and presses from `match.yaml`, never a direct `Start`.
- **Input.** `game.yaml` declares every action twice, `p1_` and `p2_`. Originally each player held a bare `input.Actions` with controllers assigned by sorted ID each step; see [sticky controller assignments and disconnect pause in fighting2d](../bug-fix/2026-11-28-fighting2d-controller-disconnect.md) for the subsequent move onto `input.Players` with `DisconnectPolicy`. Touch is player 1's floating `VirtualStick` plus the `touchRegion` and `mouseRegion` bindings of its two attack buttons.
- **Drawing.** The fighters are drawn from their active boxes (first hurtbox as the head, hitboxes as strikes), so changing frame data changes the art; F1 toggles `debug.ShapeOverlay.DrawFighterBoxes`.
- **Tests.** Rule tests drive `match` directly (projectile, invulnerable reversal, charge, high/low guard, overhead, chip, cancel combo with scaling, time-up, drawn rounds and a drawn match, knockouts, bad stage walls in `match.yaml`, spacing, CPU determinism); nsgotest playthroughs lose to the CPU idle, beat it with a keyboard bot that uses specials, guards and combos, win a versus match with two gamepads and play with touch.

## Alternatives considered

- **Add hit levels, movement and projectiles to `framedata`** — the natural home, and other fighters would reuse them; but it changes a shared package's format and API while fourteen templates are built in parallel, and the issue says to work around gaps in the template. The extras in `match.yaml` are keyed by move name, so moving them into `framedata` later is a data migration, not a redesign.
- **One `input.Actions` for both players** — one keyboard is one namespace anyway; but an `Actions` reads either every controller or one, so two controllers need two `Actions`. Binding the whole map into both keeps game.yaml the single source of bindings.
- **`input.Players` for device assignment** — built for local multiplayer; but it assigns the keyboard to one player, while two players share one keyboard here, and its join flow is more than a title screen with two Start buttons needs.
- **Pin `packages/game` at the starter's commit (`a93b5ed9`)** — one pin for every template; but game.yaml at that commit has no key or gamepad bindings, so the controls would move into code and out of the project file.

## Consequences

- **Benefits**: a new fighter is three data files and no code; the CPU and both humans go through one input path, so a rule change applies to all of them; every playthrough is deterministic and runs headless.
- **Costs and known limits**: the guard level is decided before the step from the opponent's current move and projectile, so when a low projectile and an overhead jump-in are both on their way no guard matches both and the guard drops for those steps; there are no throws, knockdowns or meter; touch controls serve player 1 only. If `framedata` grows hit levels or movement, `match.yaml`'s `level` and `motion` should move into the frame data.

## Verification

- `go tool nsgo test ./template/fighting2d/template` and `go tool nsgo check -target linux,web ./template/fighting2d/template` in the workspace.
- `go tool nsgo test ./cmd/game` from `packages/game`: registry, byte-identical copy, docs rows, and a project created from the template built and tested against the local game layer.
- A copy outside the workspace with `go.mod` rendered from `go.mod.tmpl` and `GOWORK=off go tool nsgo test ./...`, as CI's `templates` job does against the pin.
