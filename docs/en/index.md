# Overview

game is a game layer for [NSGo](https://nsgo.dev) apps, written in Go. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top. It uses only NSGo's public API (`core`, `audio`, and later `gamepad`), so an app that does not import it links none of it.

> The project is early: the packages below work and are tested, but their APIs will change. Start with [Getting Started](getting-started.md) to build and run a game.

## Stages

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn at 60 fps or the display's rate.
2. **3D**: meshes, cameras, lights and glTF models drawn with depth on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)).

The loop, input actions, sound and scene switching are shared by both stages, so 3D adds drawing without replacing them. The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047); its tasks are this repository's issues and the engine primitives NSGo adds for it.

## Packages

| Package | Import path | Guide | What it holds |
| --- | --- | --- | --- |
| `game` | `github.com/nsgo-dev/game/packages/game` | [Loop](loop.md) | `Loop`, the fixed-step loop every package shares |
| `sprite` | `github.com/nsgo-dev/game/packages/game/sprite` | [sprite](sprite.md) | Atlases, frames, animations, sprites and batches drawn into a `Canvas` |
| `sound` | `github.com/nsgo-dev/game/packages/game/sound` | [sound](sound.md) | The `Mixer`, its effects and music |
| `input` | `github.com/nsgo-dev/game/packages/game/input` | [input](input.md) | Named actions and axes from touch regions and mouse buttons, persisted in `core.Storage` |
| `tilemap` | `github.com/nsgo-dev/game/packages/game/tilemap` | [tilemap](tilemap.md) | 2D `Camera`, culled tilemaps, Tiled `.tmj` loader, animated tiles and raycasts |
| `scene` | `github.com/nsgo-dev/game/packages/game/scene` | [scene](scene.md) | World/screen layers, hit testing, scene stacks and animated transitions |
| `physics` | `github.com/nsgo-dev/game/packages/game/physics` | [physics](physics.md) | `Box`/`Circle` overlaps, `MoveAndSlide` tile resolution and `World` queries |
| `screen` | `github.com/nsgo-dev/game/packages/game/screen` | [screen](screen.md) | Virtual resolution `Viewport` scaling, pixel snapping and safe-area rectangles |
| `assets` | `github.com/nsgo-dev/game/packages/game/assets` | [assets](assets.md) | Async per-scene asset groups, reactive progress and `LoadingScreen` |
| `text` | `github.com/nsgo-dev/game/packages/game/text` | [text](text.md) | AngelCode BMFont bitmap fonts and platform text via `DrawPlatform` |
| `particles` | `github.com/nsgo-dev/game/packages/game/particles` | [particles](particles.md) | Seeded deterministic 2D `Emitter`, spawn shapes, bursts and lifetime curves |
| `save` | `github.com/nsgo-dev/game/packages/game/save` | [save](save.md) | Versioned JSON save slots, schema migrations and atomic backup recovery |
| `debug` | `github.com/nsgo-dev/game/packages/game/debug` | [debug](debug.md) | Frame-time graph, wireframe shapes, scene inspector and `-tags release` stripping |
| `flow` | `github.com/nsgo-dev/game/packages/game/flow` | [flow](flow.md) | Game-clock `Timers`, `Machine[S]`, `Seq`/`Parallel`/`Race` and typed event `Bus` |
| `tween` | `github.com/nsgo-dev/game/packages/game/tween` | [tween](tween.md) | Fixed-step `Tween`, `Timeline` and camera choreography helpers |
| `ai` | `github.com/nsgo-dev/game/packages/game/ai` | [ai](ai.md) | Grid `AStar`, 2D `NavMesh`, steering behaviors and behavior trees |
| `replay` | `github.com/nsgo-dev/game/packages/game/replay` | [replay](replay.md) | Delta-encoded input `Recorder`/`Player` and deterministic test assertions |

For a single-page summary of the core APIs, see [Packages](packages.md).

## Install

The module needs Go 1.27 and NSGo v0.2 or later:

```sh
go get github.com/nsgo-dev/game/packages/game
```

## How a frame runs

A game is an NSGo component. It starts a `game.Loop` on its scope, which runs on NSGo's `RequestAnimationFrame`:

1. Each display frame adds the time since the last one and runs `Update` once for each whole fixed step it holds, zero or more times.
2. `Update` moves the game's state by the fixed `dt`, advancing animations and timers.
3. `Draw` runs once, with how far the frame falls into the next step, and sets a signal the game's `Canvas` reads, so the canvas draws again without a commit of the view tree.

While the window is in the background, the loop runs no updates and the mixer plays nothing; both resume from where they were.

## Targets

macOS, iOS, Windows, Linux, Android and the web. HarmonyOS, mini programs, watchOS and tvOS are outside the first stage.

See [Building and Shipping](shipping.md) for icons, signing, notarization, store packaging and `nsgo check` across all six targets.
