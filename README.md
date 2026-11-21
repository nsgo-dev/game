# game

A game layer for [NSGo](https://github.com/nsgo-dev/nsgo) apps, written in Go, in two stages:

1. **2D**: a fixed-step loop, input actions, sprites, tilemaps, scenes and sound. A game draws into one NSGo view, a `Canvas`, a pixel buffer or a shader, and its HUD and menus stay ordinary native NSGo views on top.
2. **3D**: meshes, cameras, lights and glTF models on a GPU view NSGo adds for it ([nsgo#1059](https://github.com/nsgo-dev/nsgo/issues/1059)).

The loop, input actions, sound and scene switching are shared by both stages. It uses only NSGo's public API, so an app that does not import it links none of it.

## Status

Not started. The plan is [nsgo#1047](https://github.com/nsgo-dev/nsgo/issues/1047), and its tasks are this repository's issues. The engine-side primitives it needs (key-up and held keys, display frame times, gamepads, audio everywhere, a batched `DrawAtlas`, the GPU view) are tracked in NSGo.

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
```

Import the packages from `github.com/nsgo-dev/game/packages/game`:

- `game`: the fixed-step `Loop` every package shares.
- `game/sprite`: atlases, frames, animations, sprites and batches drawn into a `Canvas`.
- `game/sound`: the `Mixer`, its effects and music.
- `game/input`: input actions ([#3](https://github.com/nsgo-dev/game/issues/3)).
- `game/tilemap`: tilemaps and the camera ([#5](https://github.com/nsgo-dev/game/issues/5)).
- `game/scene`: drawing layers, hit testing and scene switching ([#6](https://github.com/nsgo-dev/game/issues/6)).

`examples` will hold a playable sample game ([#8](https://github.com/nsgo-dev/game/issues/8)).

## Targets

macOS, iOS, Windows, Linux, Android and the web.
