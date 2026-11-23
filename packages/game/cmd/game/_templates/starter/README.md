# NSGo Game Starter Template

A minimal 2D game template built with [NSGo](https://github.com/nsgo-dev/nsgo) and [`github.com/nsgo-dev/game/packages/game`](https://github.com/nsgo-dev/game).

It demonstrates:

- A fixed-step `game.Loop` (60 Hz updates, interpolated `Draw` via `alpha`, automatic background pause).
- `sprite.Atlas`, `sprite.Animation`, `sprite.Sprite`, and `sprite.Batch` drawn into a full-screen `core.Canvas`.
- A native NSGo HUD (`core.Text` and `base.Button`) layered above the canvas with safe-area padding and pass-through pointer events.
- Headless tests using `nsgotest.Main` and `nsgotest.Start`.

Edit the app's display name and identifier in `nsgo.yaml` before shipping it.

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

Build release artifacts (output goes to `dist/`):

```sh
go tool nsgo build -target macos_arm64,windows_x64,linux_x64,web
go tool nsgo build -target ios,android_arm64
```

## Layout

- `main.go` — entry point (`nsgo.Run(Root)`).
- `game.go` — fixed-step `game.Loop`, procedural two-frame `sprite.Atlas`, `core.Canvas`, and native HUD overlay.
- `game_test.go` — headless `nsgotest` and unit tests.
- `nsgo.yaml` — NSGo app metadata, dev settings, and build targets.
