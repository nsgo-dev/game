---
name: nsgo-game
description: Build, run, test, inspect and drive 2D and 3D games on NSGo with github.com/nsgo-dev/game. Use when scaffolding a game with `go run github.com/nsgo-dev/game/cmd/game@latest new` or running `go tool game`, writing fixed-step game loops, sprites, tilemaps, scenes, ECS worlds, physics, audio, 3D scenes, plain-text YAML game files, attaching `game/pkg/mcp` tools to `nsgo mcp`, or verifying a running game through MCP.
---

# NSGo Game

`github.com/nsgo-dev/game` is the game layer for [NSGo](https://github.com/nsgo-dev/nsgo) apps, written in Go. A game draws into one NSGo view (`core.Canvas`, pixel buffer, shader, or GPU view), while HUDs and menus remain native NSGo views layered on top. It uses only NSGo's public API and supports macOS, iOS, Windows, Linux, Android and the web.

Pair this skill with NSGo's own `nsgo` skill (`bunx skills add nsgo-dev/nsgo`) for UI primitives, layouts, signals, `nsgotest`, and `go tool nsgo`.

Read the version-matched source in your module cache rather than guessing signatures:

```sh
go get github.com/nsgo-dev/game@latest
go get -tool github.com/nsgo-dev/game/cmd/game@latest
go get -tool github.com/nsgo-dev/nsgo/cmd/nsgo@latest
go list -m -f '{{.Dir}}' github.com/nsgo-dev/game
```

Published English docs for agents live at <https://game.nsgo.dev/llms.txt> (index) and <https://game.nsgo.dev/llms-full.txt> (full Markdown).

## Core rules

- **Go only, no hand-written C.** Never add cgo or C toolchains; use NSGo services, purego or `syscall`.
- **Every authored file is plain-text YAML.** Scenes (`*.scene.yaml`), project settings (`game.yaml`), import sidecars (`*.import.yaml`), shared resources, data tables, animations, particles, input recordings, and gameplay configs are versioned YAML with strict unknown-key refusal and deterministic canonical saves. Program-written snapshots and save slots use YAML or JSON, never `encoding/gob` or custom binary layouts.
- **Keep third-party exports native.** Read Tiled `.tmj`, LDtk `.ldtk`, Aseprite/TexturePacker JSON, BMFont/MSDF fonts, glTF `.gltf`/`.glb`, and Yarn `.yarn` directly without binary conversion steps.
- **Vet and test with `go tool nsgo`.** Always run `go tool nsgo check` and `go tool nsgo test`, never `go vet` or `go test` directly. Pass all touched targets in one check: `go tool nsgo check -target macos,windows,linux,android,web`.
- **Respect stability tiers.** Core 2D packages (`game`, `math2d`, `random`, `screen`, `flow`, `tween`, `sprite`, `tilemap`, `scene`, `input`, `physics`, `sound`, `save`, `ai`, `loaderr`) are **Stable**; newer packages (including `mcp`, `entities`, `sceneformat`, 3D, and genre kits) are **Experimental** — pin versions and check the source.
- **Engine goroutine ownership.** Start `game.Loop`, `mcp.Attach`, and signal writes on the engine goroutine (`*core.Scope`). Use `cx.Post` from background goroutines and `app.Post` + `app.Settle` in `nsgotest`.

## Quick-start workflow

1. **Scaffold a project:** Create a project from outside a module with `go run github.com/nsgo-dev/game/cmd/game@latest new my-game` (nothing to install; templates require NSGo v0.3.4). Inside the created module, `go.mod` declares `tool github.com/nsgo-dev/game/cmd/game` and `tool github.com/nsgo-dev/nsgo/cmd/nsgo`, so run the pinned CLI with `go tool game <command>` and NSGo with `go tool nsgo <command>`:

   ```sh
   go run github.com/nsgo-dev/game/cmd/game@latest new my-game
   cd my-game
   ```

   Creates `go.mod` (with `game` and `nsgo` tools), `game.yaml`, `nsgo.yaml`, `main.go`, `game.go`, `project.go`, and tests.

2. **Generate gameplay building blocks:**

   ```sh
   go tool game generate -main scene arena
   go tool game generate prefab player
   go tool game generate component velocity
   go tool game generate system movement
   ```

3. **Build assets and validate references:**

   ```sh
   go tool game assets            # packs atlases, encodes KTX2, converts audio, writes typed manifest
   go tool game index             # assigns stable file UIDs and updates the project index
   go tool game check             # checks scenes, Tiled/LDtk maps, atlases, and all file references
   ```

4. **Run, test, and verify through MCP:**

   ```sh
   go tool game dev                                  # runs nsgo dev with live asset hot reload
   go tool nsgo test ./...                           # headless deterministic tests via nsgotest
   go tool nsgo dev -automation                      # exposes built-in + game/mcp tools for nsgo mcp
   claude mcp add nsgo -- go tool nsgo mcp           # connects an MCP client to the running app
   ```

5. **Package for release:**

   ```sh
   go tool game credits                              # verifies asset license sidecars
   go tool game release                              # builds export presets from game.yaml via nsgo build
   ```

## Minimal game with `game.Loop` and `game/pkg/mcp`

A game component owns a fixed-step `game.Loop` and triggers `core.Canvas` redraws via a frame signal so the native view tree does not re-commit every frame. Attach `mcp.Bridge` on the same `*core.Scope` so agents can inspect and drive the running game over `nsgo mcp` (in normal and release builds `automation.Register` is a no-op):

```go
func Root(cx *core.Scope) core.Node {
	frame := core.NewSignal(cx, uint64(0))
	rng := random.NewStreams(1)
	world := entities.NewWorld()
	sc := scene.NewScene(&tilemap.Camera{Width: 320, Height: 180})

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			stepWorld(world, rng, dt)
		},
		Draw: func(alpha float64) {
			frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)

	bridge, _ := mcp.Attach(cx, mcp.Config{
		Scene:         sc,
		WorldFunc:     func() *entities.World { return world },
		Loop:          loop,
		RandomStreams: rng,
	})
	_ = mcp.RegisterComponent[Velocity](bridge, "velocity")

	return core.Canvas{
		TestID: "game-canvas",
		Style:  core.Style{FlexGrow: 1, FlexShrink: 1},
		Draw: func(d *core.Drawing, size core.Size) {
			_ = frame.Get()
			sc.Draw(d)
		},
	}
}
```

## Verifying a running game through MCP

When testing or debugging a game over `nsgo mcp` (or headlessly via `nsgotest.Start` + `automation.Call`):

1. **Discover state:** call `game_scenes_list`, `game_layers_list`, and `game_entities_list` to find active scenes and stable generational entity references (`"entity:<index>:<generation>"`).
2. **Make runs deterministic:** call `game_loop_pause`, then `game_loop_seed` with a fixed unsigned 64-bit seed.
3. **Drive gameplay:** inject actions/axes/pointer events (`game_input_action`, `game_input_axis`, `game_input_pointer`) or spawn/edit entities (`game_entity_spawn`, `game_component_set`) and advance an exact step count synchronously with `game_loop_step` (60 steps at 60 Hz advances exactly `1.0` second).
4. **Check your change (capture → act → step → capture → diff):**
   - Save baselines before acting: `game_state_capture` (`name: "before"`) and `game_screenshot` (`baseline: "before"`).
   - Perform the edit or drive input and advance exact steps (`game_loop_step`).
   - Capture again (`game_state_capture` `name: "after"`) and diff with `game_state_diff` (`from: "before"`, `to: "after"`, optional `paths`) to verify only the intended entities/values changed.
   - Compare pixels with `game_screenshot` (`compare_to: "before"`, optional `tolerance`, `diff_path` / `include_diff_image`) to confirm changed-region bounding boxes and `gametest_match` / `within_tolerance`.
   - Read recent logs and any recovered panic with `game_log_read` (pass `next_cursor` back as `since` on follow-up checks).
5. **Inspect details & performance:** read specific component values with `game_entity_inspect` / `game_component_get`, check performance with `game_perf_readout`, and draw aligned debug overlays with `game_screenshot` (`bounds: true`, `collision_shapes: true`).
6. **Reproduce & regress:** record sequences with `game_replay_record_start` / `game_replay_record_stop` to save editable YAML recordings, then replay them deterministically with `game_replay_play` or `go tool game replay`.

## References

Load on demand for exact APIs, flags, schemas, and tool arguments:

- [references/packages.md](references/packages.md) — complete catalog of `pkg/*` packages grouped by stage and domain, with import paths and stability status.
- [references/cli-and-formats.md](references/cli-and-formats.md) — `go tool game` subcommands, companion tools, YAML file format contract, schemas, and external tool formats.
- [references/mcp-tools.md](references/mcp-tools.md) — full `game/pkg/mcp` tool reference, `mcp.Config` wiring, component/prefab/asset registration, and `nsgotest` verification patterns.
