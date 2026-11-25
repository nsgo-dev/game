# Agent Note: The platformer2d template

Status: implemented

## Problem

Issue #788 (part of #786) asks for a `platformer2d` template: a small, original side-scrolling platformer that `game new -template platformer2d` and `nsgo init -template` write, built mainly on `platformer`, `tilemap`, `rooms`, `physics` and `sprite`, with a controller that has coyote time and jump buffering, a few rooms with camera bounds, hazards, collectibles, a checkpoint and a goal. It must play with the keyboard, a game controller and touch, keep every file its author edits as text, and have an nsgotest playthrough that reaches a win and a loss in CI, where the template is tested against the game layer version its `go.mod.tmpl` pins rather than the checkout.

## Decision

- The template is the module `github.com/nsgo-dev/game/template/platformer2d`, with its project in `template/platformer2d/template`, a byte-for-byte copy under `packages/game/cmd/game/_templates/projects/platformer2d`, one registry entry in `templates.go`, a `go.work` entry, a row in `docs/{en,zh}/templates.md` and a guide page `docs/{en,zh}/template-platformer2d.md`. CI's template matrix finds it from its directory, so `.github/workflows/test.yml` is unchanged.
- `go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125051754-af952ae5732f`, the first published version with the key, key code and gamepad button bindings of `game.yaml`'s input map (#908), and requires `go.yaml.in/yaml/v3` directly, since the level format uses it.
- The game keeps its data in three YAML files embedded with `go:embed`, so every target, the web and phones included, runs it without reading files: `data/level.yaml` in a format the template defines (`Level` in `level.go`: `version`, `name`, `tileSize`, `lives` and the tiles as a literal block, one glyph per tile), `data/rooms.yaml` in `rooms.Map`'s format and `data/player.yaml` in `platformer.Config`'s format. Each parser refuses unknown fields and trailing documents, and `TestDataFilesAreCanonical` keeps each file in its encoder's canonical form.
- `world.go` holds one run, independent of devices and drawing: a `physics.Grid` the `platformer.Controller` moves the body against, a `physics.World` with no gravity whose triggers are the spikes, sparks, lamps and goal, a `tilemap.Camera` moved by a `rooms.Director`, and the lives, checkpoint and outcome. Trigger callbacks only set flags, which the step applies after `StepTriggers`, so a respawn never moves the body while triggers are tested. The player is frozen while the director slides between rooms and once the run has ended.
- Every binding is in `game.yaml`'s input map and bound with `Project.BindInput`: keys and key codes, gamepad buttons and the left stick as a `gamepadAxes` entry sharing the `move` axis, and touch and mouse regions in the 320 by 180 virtual screen, which `Actions.MapPoint` maps view points into. The on-screen controls are drawn on those regions, on phones and tablets and anywhere once the screen has been touched.
- The playthrough tests drive the real app through nsgotest with a reactive player: hold right, and jump while grounded when spikes, a wall or a bottomless pit is just ahead. The same player wins with the keyboard, a controller and two-finger touch, and without jumping it runs into the first spikes until the run is lost.

## Alternatives considered

- **The level as a Tiled map (`.tmj`)**, as Cave Run does: Tiled is the editor most platformer authors know and `tilemap.LoadTiled` reads it. But a `.tmj` with its tileset is large, hard to read in review, and needs an image file for the tiles, where the template draws its art by code; a grid of glyphs in YAML is the smallest readable level and keeps the whole level visible in a diff. The README points to `tilemap.LoadTiled` for a game that moves to Tiled.
- **A scripted input timeline for the playthrough** (press and release at fixed steps): exact, but any edit to the level or to `player.yaml` breaks it in ways that are hard to read. Reacting to what is ahead survives tuning and small level edits, and a failure names where the player stood.
- **Native HUD buttons for touch**, as Cave Run does: they need their own pressed state merged into the actions each step. Touch regions in `game.yaml` go through the same `input.Actions` as every other device and keep the bindings in one file.
- **Sharing a world package across templates**: each template must be self-contained and small enough to read, so the systems live in the template's own `main` package.

## Consequences

- A developer changes the level, the rooms and the jump's feel in text files, and the tests say which file is wrong and print its canonical form.
- The template follows `packages/game` only as far as its pin; using a newer package means bumping the pin and `go.sum` together, as `docs/en/templates.md` describes.
- `nsgotest.App.Touch` gives every finger ID 1 (`nsgotest/pointer.go:173` in NSGo v0.3.0 and on main), so a test cannot hold two fingers with it; the touch playthrough reports multi-touch `EventTouch` events with `App.Report` instead.
- Gaps left for later: no sound (no audio assets), no pause menu, no enemies or moving platforms, and the rooms of this level span its full height, so a vertical climb needs taller rooms and a playthrough that climbs.

## Verification

- `go tool nsgo test ./template/platformer2d/template` from the root, and the same in a copy outside the workspace with `GOWORK=off` and the rendered `go.mod`, as CI's templates job does.
- `go test -run XXX -fuzz FuzzParseLevel -fuzztime 30s` in the template directory, and `go tool nsgo check -target linux,web` there.
- `go tool nsgo test ./cmd/game` and `go tool nsgo test ./refs` from `packages/game`, and `go run ./cmd/game index ../../template`, which reports the index up to date.
