# NSGo Game Starter Template

A minimal 2D game template built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game`](https://github.com/nsgo-dev/game).

It demonstrates:

- A fixed-step `game.Loop` (60 Hz updates, interpolated `Draw` via `alpha`, automatic background pause).
- `sprite.Atlas`, `sprite.Animation`, `sprite.Sprite`, and `sprite.Batch` drawn into a full-screen `core.Canvas`.
- A native NSGo HUD (`core.Text` and `base.Button`) layered above the canvas with safe-area padding and pass-through pointer events.
- Headless tests using `nsgotest.Main` and `nsgotest.Start`.

The game's settings live in `game.yaml`, its project file. Edit the name and identifier there before shipping it, then write them into `nsgo.yaml` and check the project:

```sh
go run github.com/nsgo-dev/game/cmd/game project sync
go run github.com/nsgo-dev/game/cmd/game project check
```

## Running and Testing

Run with hot reload on your host desktop (macOS, Windows, or Linux):

```sh
go tool nsgo dev
```

Run on other targets:

```sh
# Web (opens in browser)
go tool nsgo dev -target web

# iOS simulator or connected device
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target ios

# Android emulator or connected device
go tool nsgo dev -target android
```

Run headless tests:

```sh
go tool nsgo test ./...
```

Vet and check across platforms:

```sh
go tool nsgo check -target macos,windows,linux,android,web
go tool nsgo check -target ios,ios_simulator   # on macOS with Xcode
```

Build release artifacts (output goes to `dist/`, with `-tags=release` to strip development-only debug code):

```sh
go tool game release
# or invoke nsgo build directly with GOFLAGS=-tags=release:
GOFLAGS=-tags=release go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
GOFLAGS=-tags=release go tool nsgo build -target ios,android_arm64
```

## Layout

- `game.yaml` — the project file: the game's name, id and version, its window and its export presets per platform, with a virtual resolution, an input map, audio buses and autoloaded scenes and resources to add as the game grows.
- `project.go` — embeds `game.yaml` as `Project`.
- `main.go` — entry point (`nsgo.Run(Root, ...)` with the window from `Project`).
- `game.go` — fixed-step `game.Loop`, procedural two-frame `sprite.Atlas`, `core.Canvas`, and native HUD overlay.
- `game_test.go`, `project_test.go` — headless `nsgotest` and unit tests, and a check that `game.yaml` is canonical, `nsgo.yaml` agrees with it and the files it autoloads load.
- `nsgo.yaml` — NSGo app metadata and dev settings; its name, id, version, appearance and build output and targets follow `game.yaml`.

## Git setup

Each clone enables the object-by-object scene merge driver once (`.gitattributes` names it, while Git keeps driver config local to `.git/config`):

```sh
go tool game merge -setup
```
