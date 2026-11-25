# Agent Note: The action3d template, a third-person 3D action adventure

Status: implemented

## Problem

`game new` offered only the 2D `starter` template (#786). A developer starting a 3D action game had to assemble the orbit camera, character movement, animation state machine, melee combat, level data and input for three kinds of device from `examples/lantern3d` and the package docs, and lantern3d shows rendering and camera control, not a game with rules, enemies, a win and a loss. #794 asks for a small, complete, original third-person action adventure that builds and runs from `game new -template action3d` without edits and whose headless test reaches both a win and a loss.

## Decision

`template/action3d` is the module `github.com/nsgo-dev/game/template/action3d`, with the project in `template/action3d/template/` and a byte-for-byte copy under `packages/game/cmd/game/_templates/projects/action3d`, registered in `projectTemplates`, listed in `docs/{en,zh}/templates.md` and described in `docs/{en,zh}/template-action3d.md`. Its `go.mod.tmpl` pins `packages/game` at `v0.0.0-20261125054131-af10f8ca9ebe`, the first `main` commit with game.yaml's key and gamepad bindings (#908), and requires `go.yaml.in/yaml/v3` directly for its own data format.

The game is a walled courtyard: the hero fights three guards, jumps onto a ledge too high to step up for the key, and the key opens the gate to the shrine. Reaching the shrine wins; losing all health loses.

- **Data lives in text the project file loads.** `game.yaml` autoloads two files, embedded with `go:embed` and read through `project.File.LoadAutoloads` so no code parses `game.yaml` or opens files at run time: the level `scenes/keep.scene.yaml` (a sceneformat scene) and `data/actors.yaml`.
- **The level is a scene file with components.** Objects carry the component types `levelComponents` registers (`block`, `spawn`, `enemy`, `key`, `door`, `goal`, `lamp`); position is `x`, `y` (up), `posZ` in meters and scale sizes a box. A `connections` entry wires the key's `collected` event to the door's `unlock` handler, whose `HandlerFunc` receives the `*game` as `sceneformat.Connect`'s context, so the scene, not Go code, decides what the key opens.
- **`data/actors.yaml` is the template's own format** (`actors.go`): version 1, `KnownFields`, one document, validation of every number, color, joint reference and clip time, and an encoder (`encodeActors`) whose output parses back to the same bytes, checked by `TestActorsFileIsCanonical` and `FuzzParseActors`. It holds the hero's and enemies' tuning (`Fighter`), a rig of boxes (`Joint`: parent, pivot, size, offset, paint slot) and clips of per-joint Euler keys in degrees with optional shifts and named events.
- **Animation is anim3d's.** `newRig` turns the clips into `anim3d.Clip`s; each actor has an `anim3d.StateMachine` with a `move` state whose `BlendSpace1D` blends idle into run by the `speed` parameter, clip states for jump, attack (scaled by `attackSpeed`), hurt and fall, and `When` transitions for leaving and reaching the ground, a swing or flinch ending, and falling. A blow lands on the attack clip's `hit` event (`DrainEvents`), so the animation, not a timer in Go, decides when a hit happens, and a flinch that replaces the attack state cancels the pending hit.
- **Movement is physics3d's.** Each actor is a `physics3d.CharacterController` in a `World` of static boxes; the door is a static body removed on unlock.
- **The camera is scene3d's.** `game.camera` builds a `scene3d.OrbitController` from the interpolated hero position and camera yaw each frame and shortens it to the first `World.RaycastFirst` hit between the hero and the camera.
- **Rendering is render3d's lit PBR pipeline.** `renderer` uploads one unit cube, a camera buffer, a light buffer (moon, ambient and the scene's lamps) and one `PBRDrawBuffer` per box the level can show, and draws every box with `NewLitPBRPipeline`.
- **Input goes through one `input.Actions`.** `Project.BindInput` binds game.yaml's keys, game controller buttons, axes and stick axes; `WatchKeys` and `WatchGamepads` hear the devices; an `input.VirtualPad` with floating move and camera sticks and attack and jump buttons feeds the same axis and action names, drawn once the screen has been touched; a mouse drag orbits the camera.
- **The simulation is separate from the view.** `game` (game.go) steps from a `controls` value and knows nothing of devices or the GPU; `play` (view.go) reads the devices into `controls` on each fixed `game.Loop` step and draws the HUD. Tests drive the simulation directly with a route-following bot, and the same bot drives a full playthrough through the app with a game controller.

## Gaps found in shared packages and NSGo

- **NSGo's software GPU drops triangles that cross the near plane.** `rasterTriangle` in `internal/driver/gpu_software.go` (NSGo v0.3.0, lines 1766-1770, unchanged on NSGo `main`) returns when any vertex has `w <= 1e-6` instead of clipping the triangle, so under nsgotest a floor that extends behind the camera disappears; real GPUs clip it. The template's render tests check the opening view, where all the level lies in front of the camera, and the game itself needs no workaround.
- **Character controllers do not collide with each other.** physics3d documents this; `game.separate` pushes standing actors' capsules apart on the ground and refuses a push that would put a capsule inside a wall, tested with `World.QueryOverlap`.
- **render3d has no primitive mesh builders.** `cubeMesh` writes the unit cube's 24 vertices.
- **anim3d has no text format for clips.** Clips come from glTF (`ClipFromGLTF`) or Go; the template defines its clips in `data/actors.yaml`.
- **The website sidebar** (`apps/website/src/content/groups.ts`) names its pages; `template-action3d` is not added there, to keep the shared change to the registry, the templates table, the CLI reference and `go.work`, so the page lists under "other" until the template pages get a group.

## Alternatives considered

- **Loading a glTF character and level, as lantern3d does** — the most realistic art path, and anim3d plays glTF skins directly. Rejected: a binary `.glb` the template would ship cannot be read or edited as text by the person extending it, and generating it needs a tool the template would also have to carry; boxes driven by a text rig keep every authored file text and keep the template readable in an afternoon. The docs say how to swap in glTF models.
- **Hard-coding the level in Go** — shorter, and the sibling 2D templates use small custom level files. Rejected because a scene file opens in the editor, carries components and connections, and is what a 3D game grows into; the template shows the connection mechanism on the key and the door.
- **Defining the actors file in `packages/game`** — would give other games the rig-of-boxes format. Rejected for this issue: the scope is one template, and a shared format would need its own design, docs and stability row.
- **Using `ThirdPersonController` for the camera** — it follows the target's forward and smooths, which suits a chase camera. Rejected because the issue asks for an orbit camera that the player turns, and the orbit controller with a raycast gives that with less state.

## Consequences

- **Benefits**: `game new -template action3d` gives a 3D project whose headless tests play a full run to a win (`TestGamepadPlaythroughWins`, `TestBotWinsTheRun`) and to a loss (`TestKeyboardRunIntoTheGuardLoses`, `TestStandingStillAmongEnemiesLoses`), cover touch, mouse and keyboard, and draw the level on a full and a WebGL 2 software GPU; every number of the fight is in `data/actors.yaml`.
- **Costs and known limits**: the software GPU renders the PBR shader at about a second a frame at 96 by 60 points, so the render tests draw two or three frames and run in a few seconds; a level change needs the bot's route in `game_test.go` changed too; actors are boxes; the camera ray can pass over a wall's top when the hero stands close to a low wall, which the outer walls' 4 m height avoids.

## Verification

- `go tool nsgo test ./template/action3d/template` and `go tool nsgo check -target linux,web ./template/action3d/template` from the repository root.
- CI's `templates` job tests a copy of the template outside the workspace with `GOWORK=off` against the pinned `packages/game`.
- `go tool nsgo test ./cmd/game` from `packages/game` checks the copy, the registry, the docs rows and the CLI reference.
- `go run ./cmd/game index -dry-run ../../template` from `packages/game` reports the index up to date.
