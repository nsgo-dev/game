# Agent Note: The puzzle2d template, Spark Slide

Status: implemented

## Problem

Issue #795, under #786, asks for a `puzzle2d` template: a grid puzzle with match or slide mechanics, levels defined in YAML, undo, move counting, stars per level and a level select, built mainly on `tween`, `undo`, `input` and `save`, original, playable with the keyboard, a game controller and touch, and tested through `nsgotest` to a win and a loss. It has to stay small enough to read in an afternoon, follow the template infrastructure of #787 (`docs/en/templates.md`), and keep every file its author writes in text.

## Decision

The template is the module `github.com/nsgo-dev/game/template/puzzle2d`, with the project in `template/puzzle2d/template/`, its copy in `packages/game/cmd/game/_templates/projects/puzzle2d`, one registry entry in `templates.go`, a `go.work` entry and a row and section in `docs/{en,zh}/templates.md`. CI's `templates` matrix finds it from its directory.

The game, Spark Slide, is a sliding puzzle: a spark slides until a wall stops it, lighting every lamp it passes; lighting every lamp within the level's moves wins, and a slide into a void, or running out of moves with a lamp dark, loses. A blocked move bumps the spark and costs nothing.

- **Rules and undo** (`board.go`): `Board` holds the fixed grid and what moves change (spark, lamps lit, moves, fallen). The only move type is `*slideMove`, which records in `Apply` the cell it left, the lamps it lit and whether it fell, so `Revert` restores the board exactly and `undo.History[*Board, *slideMove]` redoes it on the same board. A won level refuses undo, so the saved stars always match a won board; a lost one allows it, so a player can step back from a fall.
- **Level format** (`levels.yaml`, `levels.go`): a template-local YAML file with `version: 1`, decoded with `KnownFields(true)`, one document only, validated (ID pattern, unique IDs, `1 <= stars.three <= stars.two <= moves`, rectangular rows, one start, at least one lamp, known tile characters), and written canonically by `EncodeLevels` (yaml.v3, two-space indent, declaration order). Rows are strings of tile characters so a level reads as its picture in review. Level IDs, not positions, key the saved stars, so reordering or renaming levels keeps progress.
- **Par is checked, not guessed**: `TestLevelsAreSolvableAtPar` solves every level by breadth-first search over (spark cell, lamps lit) and requires `stars.three` to equal the fewest winning moves and `moves` to leave two more; `TestEveryLevelIsWonInPlay` plays each solution through `nsgotest`. The four later levels were found by a seeded random search for grids with a given shortest solution, then checked by the same solver; the first two are hand-made tutorials.
- **Input**: `game.yaml`'s input map binds the directions, undo, redo, restart, confirm and back to keys, key codes and controller buttons, plus two left-stick axes with a half-travel dead zone, through `project.File.BindInput`. The stick makes one move per push (`stickDir` latch). Touch swipes come from `input.GestureRecognizer` bound to the direction actions; a tap or click on the board, via the board view's `OnPress`, moves toward the point along its dominant axis, so the mouse and a single tap play too. Window-level keys use `Actions.WatchKeys`, controllers `Actions.WatchGamepads`.
- **Logic first, animation after**: a move changes the board at once and `tween` animates the drawn spark, lamp glow and fall afterwards; the next input settles any running animation before it acts. The result card shows only once the animation ends, and replaces the control bar so its buttons are the only ones reachable.
- **Progress** (`progress.go`): `save.Store` over `core.Storage` with schema version 1 keeps `Progress{Stars map[string]int}` as JSON, a file the program writes for itself; a level is unlocked when the one before it has stars.
- **Pin**: `go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125054131-af10f8ca9ebe`, a `main` commit that has `project.File.BindInput` with key and game-controller bindings (#908); `starter` keeps its own older pin. `go.yaml.in/yaml/v3` becomes a direct requirement at the version `packages/game` already uses.

## Alternatives considered

### A match-3 board instead of a sliding one

Strongest argument: the issue names match mechanics first, and `examples/slidegem` is a working match-3 to learn from. Not chosen because a match-3 board with gravity and refills is random by nature, which makes undo, par and a hand-checked level file less meaningful, and `slidegem` already covers it; a deterministic slide puzzle exercises undo, move limits and per-level par directly and keeps the template smaller than the example.

### Levels as Tiled maps or a list of tile objects

Strongest argument: Tiled is the editor many 2D games already use, and `tilemap` reads it. Not chosen because a level here is a handful of tile kinds on a small grid; rows of characters read as the board itself in a diff and need no external editor, while a Tiled export would hide the grid in CSV or base64 layers.

### Saving the undo log with the level in progress

Strongest argument: `undo.Log` makes a resumable level nearly free. Not chosen to keep the template within scope: the issue asks for stars per level, which `Progress` keeps, and a resumable level would add a second save format to read in an afternoon.

## Consequences

- A new template a game author can start from with `game new -template puzzle2d`, and a level file whose tests tell the author the par of a new level.
- The template's pin is newer than `starter`'s; both move forward independently as packages they use change.
- The level format is local to the template, documented in its README rather than in `docs/en/formats.md`, which covers the formats `packages/game` defines.
