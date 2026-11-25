# Agent Note: The shmup2d template and its pilot-driven playthrough

Status: implemented

## Problem

Issue #790 adds a vertical scrolling shooter template under the infrastructure of #787: enemy waves, bullet patterns, power-ups, a boss with phases, lives and a score, playable with the keyboard, a controller and touch, with an `nsgotest` playthrough that reaches both a win and a loss in CI. A shooter's win depends on dodging bullets, so a test that reaches it has to play well, and the playthrough runs in CI against the pinned game layer on whatever architecture the runner has.

## Decision

- The template lives in `template/shmup2d/template` as the module `github.com/nsgo-dev/game/template/shmup2d`, registered in `packages/game/cmd/game/templates.go`, embedded under `_templates/projects/shmup2d` and listed in `go.work` and `docs/{en,zh}/templates.md`. `go.mod.tmpl` pins `packages/game` at the main commit the template was built on, which has the input map's key and gamepad bindings (#908) it needs, and requires `go.yaml.in/yaml/v3` directly for the stage format.
- The stage is data: `data/stage.yaml` holds the ship, the enemy kinds, the waves and the boss, in a format `stage.go` defines in the template itself (a `version`, unknown fields refused, a canonical encoding, a round-trip test and `FuzzParseStage`). The bullet patterns and the explosion use the `bullets` and `particles` formats as they are.
- `world.go` is a device-free simulation: `world.step(dt, controls)` takes one step's controls and reports `events`, and the systems run in a fixed order (timeline, movement, firing, bomb, collisions, pickups, removal). `controls.go` builds `controls` from `game.yaml`'s input map through `input.Actions` and from a drag: a finger or the left mouse button moves the ship by the pointer's own motion and fires while down. The mouse drag uses `View.OnMouseDown` and `OnMouseUp`, which carry the button, rather than press events.
- The win is reached by `pilot_test.go`'s pilot, a closed-loop player that reads the world each step (bullet positions a few tenths of a second ahead, enemies, items, the boss) and chooses stick values, fire, focus and bomb. `TestGamepadPlaythroughClearsTheStage` feeds its choices through a fake controller's stick and buttons each frame, so the playthrough goes through the same input path as a player; the loss is the ship left idle after Return starts the run. On the shipped stage the pilot clears it without losing a life, which leaves a wide margin for floating-point differences between architectures.

## Alternatives considered

- A shared `packages/game/shmup` package for waves, paths and bosses: it would serve other shooters, but a template is the code a developer copies and changes, and a new library package brings a stability row, an example and an API to keep; the genre logic stays in the template, where it is meant to be edited.
- An invincibility or assist switch for the win playthrough: it reaches the boss's defeat with certainty, but proves nothing about the stage being winnable by playing it, and adds a feature only the test needs.
- Replaying a recorded input sequence: a recording that wins on one architecture can lose on another, since `bullets` documents that floating-point results differ across CPUs, while a closed-loop pilot reacts to where the bullets really are.
- A `touchRegion` binding in `game.yaml` for the touch bomb button: it would keep the button's place in the project file, but an input map's touch region is held by any finger inside it, so the finger steering the ship would set off a bomb as it slid over the button; the drag decides on the finger that goes down instead.
- Driving the mouse drag from `OnPressDown`: `nsgotest`'s `MouseDownAt` sends only mouse-button events, and a press does not say which button went down, so the left button is read from `OnMouseDown`.

## Consequences

- A designer changes the stage, patterns and explosion in YAML; the tests check that the files load, stay canonical and that the pilot still clears the stage, so a change that makes it unwinnable for a careful player fails `TestPilotClearsTheStage`.
- Raising the pin is needed whenever the template starts using a game layer API newer than the pinned commit; CI's templates job builds it with `GOWORK=off` against the pin.
- `nsgotest` puts down one finger at a time (`App.Touch` always uses finger 1), so dragging while pressing the touch bomb button with a second finger is not covered by a test; the drag follows its own finger ID and sets off a bomb for each other finger that goes down on the button.

## Verification

- `go tool nsgo test ./template/shmup2d/template` and `go tool nsgo check -target linux,web ./template/shmup2d/template` from the repository root.
- `go test -run '^$' -fuzz FuzzParseStage -fuzztime 30s` and `go test -run '^$' -bench Step` in `template/shmup2d/template`.
- `go tool nsgo test ./cmd/game` in `packages/game`, which checks the registry, the embedded copy, `go.work` and the docs rows, and creates a project from the template.
