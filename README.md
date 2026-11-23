# game

A game layer for [NSGo](https://github.com/nsgo-dev/nsgo) apps, written in Go, in two stages:

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top.
2. **3D**: meshes, cameras, lights and glTF models on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)).

The loop, input actions, sound and scene switching are shared by both stages. It uses only NSGo's public API, so an app that does not import it links none of it.

## Status

Early and unreleased. Every package is either stable or experimental: stable packages break their API only in a minor release and only after a deprecation period, experimental ones may change in any minor release. The [API stability policy](docs/en/stability.md) ([中文](docs/zh/stability.md)) defines both, describes how deprecations work across releases, and lists each package's status.

The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047), and its tasks are this repository's issues. The engine-side primitives it needs (key-up and held keys, display frame times, gamepads, audio everywhere, a batched `DrawAtlas`, the GPU view) are tracked in NSGo.

## Layout

A monorepo. The Go module lives in `packages/game`, and the root `go.work` uses it, so Go commands also run from the repository root.

```
game/
├─ go.work
├─ packages/
│  └─ game/          Go module github.com/nsgo-dev/game/packages/game
│     ├─ sprite/
│     └─ sound/
├─ apps/website/     the site at https://game.nsgo.dev
├─ examples/         sample games
└─ docs/
   ├─ en/            the site's docs in English
   └─ zh/            the same pages in Simplified Chinese
```

Import the packages from `github.com/nsgo-dev/game/packages/game`:

- `game`: the fixed-step `Loop` every package shares ([guide](docs/en/loop.md)).
- `game/sprite`: atlases, frames, animations, sprites and batches drawn into a `Canvas` ([guide](docs/en/sprite.md)).
- `game/sound`: the `Mixer`, its effects and music ([guide](docs/en/sound.md)).
- `game/input`: input actions ([#3](https://github.com/nsgo-dev/game/issues/3)).
- `game/tilemap`: tilemaps and the camera ([#5](https://github.com/nsgo-dev/game/issues/5)).
- `game/scene`: drawing layers, hit testing and scene switching ([#6](https://github.com/nsgo-dev/game/issues/6)).

`examples/caverun` is the playable sample game, [Cave Run](examples/caverun/README.md) ([#8](https://github.com/nsgo-dev/game/issues/8)).

## Docs

- [Overview](docs/en/index.md) ([中文](docs/zh/index.md))
- [Getting Started](docs/en/getting-started.md) ([中文](docs/zh/getting-started.md))
- [Tutorial: Star Catcher](docs/en/tutorial.md) ([中文](docs/zh/tutorial.md)), nine chapters from an empty project to a shipped game, with each chapter's code in [`examples/tutorial`](examples/tutorial/)
- [Packages](docs/en/packages.md) ([中文](docs/zh/packages.md)): [Loop](docs/en/loop.md), [sprite](docs/en/sprite.md), [sound](docs/en/sound.md)
- [API Stability](docs/en/stability.md) ([中文](docs/zh/stability.md))
- [3D Models from Blender](docs/en/3d-models-blender.md) ([中文](docs/zh/3d-models-blender.md))
- [Building and Shipping](docs/en/shipping.md) ([中文](docs/zh/shipping.md))

## Targets

macOS, iOS, Windows, Linux, Android and the web. See [Building and Shipping](docs/en/shipping.md).

Consoles (Nintendo Switch, PlayStation and Xbox) are out of scope: their SDKs are under NDA and need C toolchains, and NSGo and this repository are Go only, with no hand-written C.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for the repository layout, local test and cross-target check commands, coding conventions and pull request guidelines.


## Compatibility

| `game` | Go | NSGo |
| --- | --- | --- |
| Unreleased / `v0.1.0` | 1.27+ | `v0.3.0` or later |

## Networking

`game/net` provides cross-target WebSocket, UDP and loopback transports with snapshot replication and deterministic lockstep/rollback helpers over `game.Loop` ([#41](https://github.com/nsgo-dev/game/issues/41)). Matchmaking, hosted backends and account services stay out of the `game` layer: games use Go's standard library or any networking package directly. Local multiplayer on one device belongs in `game/input` ([#3](https://github.com/nsgo-dev/game/issues/3), [#128](https://github.com/nsgo-dev/game/issues/128)).

