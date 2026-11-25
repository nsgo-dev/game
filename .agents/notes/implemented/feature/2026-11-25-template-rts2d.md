# Agent Note: The rts2d template, a 2D real-time strategy skirmish

Status: implemented

## Problem

Issue #797, under the per-genre templates of #786, asks for a `rts2d` template: a real-time strategy skirmish with resource gathering, a few unit types, building production, fog of war, box selection and control groups, and a simple AI opponent, built mainly on `game/rts`, `game/fog` and `game/ai`. Like every template it must be an original design small enough to read in an afternoon, play with keyboard and mouse, gamepad and touch, keep everything its author edits as text, and ship a test that plays it through nsgotest to a win and a loss.

## Decision

The template is the module `github.com/nsgo-dev/game/template/rts2d`, with its project in `template/rts2d/template/`, copied byte for byte to `packages/game/cmd/game/_templates/projects/rts2d` and registered in `templates.go`, as `docs/en/templates.md` describes. Its `go.mod.tmpl` pins `packages/game` at the `main` commit the template was built against, because `game/rts`'s crowd and selection and `game/project`'s key and gamepad bindings are newer than the starter's pin.

The game, Shardfront, is split by what changes for what reason:

- **Data**: `data/rules.yaml` (units, buildings, economy, starting units, limits) and `data/map.yaml` (the battlefield as a literal block of characters) are the template's own schemas: a `version`, unknown fields and a second document refused through `yaml.Decoder.KnownFields`, cross-checks between the two (every footprint on ground, no two footprints sharing a cell), durations bounded to a day so a conversion to steps cannot overflow, a gatherer refused damage because a worker never carries out attack orders, and the unit names the code trains (`worker`, `soldier`, `ranger`) required, and an encoder whose output is the file, which `TestDataFilesAreCanonical` checks. `data/formations.yaml` and `data/opponent.yaml` are in `game/rts`'s and `game/ai`'s formats, checked the same way with those packages' encoders.
- **Simulation** (`world.go`, `orders.go`): one `rts.Crowd` moves every unit of both sides over one `ai.BoolGrid`, so the two sides push against each other; crystal cells, rocks, headquarters and both sides' build pads are unwalkable from the start, so a barracks never appears on top of a unit; a mined-out crystal cell opens and the crowd rebuilds its fields. One `fog.Fog` holds a vision source per unit and building, updated every four steps. Each building keeps, per side that does not own it, a sighting (health and construction left) refreshed while a footprint cell is visible to that side; out of sight the side draws and targets that snapshot, and a building it has never seen stays hidden, because `fog.Explored` alone would show buildings put up after the ground was last seen. An entity killed earlier in a step does not act in it, and both headquarters falling in one step is a draw rather than a loss decided by iteration order. Everything iterates the world's entity slice in creation order, never a map, so a skirmish replays exactly (`TestSkirmishIsDeterministic`).
- **Orders**: a right click, a tap and the gamepad's East all call `world.command`: an enemy the player sees is attacked; otherwise combat units attack-move and workers mine the crystal cell or walk. The computer player gives its orders through the same `train`, `buildBarracks` and `attackMove`.
- **Opponent** (`opponent.go`): once a second an `ai.Reasoner` built from `data/opponent.yaml` scores six actions from a snapshot of its side and carries out the best one. It sees threats only through its own side's fog of war, but knows the map, so its attack goes at the player's headquarters without scouting it first.
- **Controls** (`control.go`): the mouse and fingers act on the battlefield view through `OnMouseDown`, `OnMouseMove`, `OnMouseUp` and `OnTouch` into one `rts.BoxSelector`; every key and gamepad button is a named action in `game.yaml`'s input map, read from `input.Actions` each step; the left stick, the D-pad and the arrow keys share one cursor whose pick and command buttons mirror the mouse's, so the gamepad and the keyboard alone both play. Control groups are ten actions (`group_0` to `group_9`) with `mod_assign` and `mod_add` modifiers, applied to `rts.ControlGroups`; they are keyboard-only, since a gamepad has no buttons left for ten groups and an assign gesture, and the gamepad's triggers and the command bar's Army, Workers and Clear buttons cover selection by type there and on touch.
- **Tests**: the two playthroughs drive `Root` through nsgotest with the keyboard and mouse only, at `game.Loop.TimeScale` 4 so a three-minute skirmish takes a quarter of the display frames, and assert a win (the player builds up, holds, then sends control group 1 at the enemy headquarters until it falls) and a loss (the player does nothing).

## Alternatives considered

- **Plain move as the default ground command**: classic, and it lets a player retreat, but then a group sent across the map walks through the enemy without fighting, and offering attack-move separately needs one more control in each of the three input schemes. The template makes ground commands attack-move and lists a retreat control as a change to make.
- **A worker that walks to the site and constructs**: closer to the genre, but it needs placement preview, a construction order and its cancellation, about a third more control code. A barracks goes on the side's next free build pad instead, so building is one key, button or gamepad press.
- **Selectable buildings with their own production panel**: `rts.Picker` finds crowd units only, so buildings would need a second picking path; the command bar trains at the least busy building of the right type instead.
- **A camera over a map larger than the screen**: the map fits a 480 by 264 virtual canvas, so no input scheme needs a pan control; the README says where a camera goes, and `GroupFocused` highlights the group where a camera would center on it.
- **Playthroughs at real speed**: they pass, but take several times as many display frames; `TimeScale` keeps every fixed step reading the input as it does at speed 1.

## Consequences

- The playthrough tests play real skirmishes, so their strategy is tuned to the shipped balance; the README and the guide say a large change to `rules.yaml` or `opponent.yaml` may need it adjusted.
- `go.mod.tmpl` pins a newer `packages/game` than the starter's; the CI `templates` job tests the template against that pin with `GOWORK=off`.
- On the web a browser may follow a tap with made-up mouse events, which `OnMouseDown` would hear as a click; the template ignores mouse events for half a second after a touch.

## Verification

- `go tool nsgo test ./template/rts2d/template` and `go tool nsgo check -target linux,web ./template/rts2d/template` from the repository root.
- A copy of the template outside the workspace with `go.mod` rendered from `go.mod.tmpl` and `GOWORK=off go tool nsgo test ./...`, as CI's `templates` job runs it.
- `go tool nsgo test ./cmd/game` from `packages/game`, which checks the registry, the embedded copy, the docs rows and `game new -template rts2d`.
