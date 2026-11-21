# Getting Started

Create an NSGo game, run it on desktop, web, iOS and Android, draw an animated sprite on a fixed-step loop, handle touch and mouse input, and play sound.

## Requirements

- **Go 1.27** or later.
- **NSGo v0.2** or later to build the game. Scaffolding uses an NSGo command with [Go module template support](https://nsgo.dev/docs/getting-started/#templates-from-go-modules).
- For iOS builds: macOS with Xcode.
- For running on Android devices or emulators: `adb` (Android SDK Platform-Tools). Building an `android_arm64` APK needs only Go.

Run `go tool nsgo doctor` inside a project to check what toolchains this machine has.

## Create a project

Install a version of the NSGo command that supports Go module templates. The revision that first added the feature is available as a Go module version. Make sure Go's binary directory (`GOBIN`, or `GOPATH/bin` by default) is on your `PATH`.

```sh
go install github.com/nsgo-dev/nsgo/cmd/nsgo@v0.2.1-0.20261121150907-9e612921df1a
nsgo init -template github.com/nsgo-dev/game/template -module example.com/mygame mygame
cd mygame
go tool nsgo dev
```

The starter includes an animated sprite, tap-to-score input, and tests. Edit the app's display name and identifier in `nsgo.yaml` for your game.

To start from a checkout of this repository, copy `template/template/` to a new directory. Rename `go.mod.tmpl` to `go.mod` and replace `{{.Module}}` on the first line with your module path. From the checkout itself, run the original starter with `go tool nsgo dev ./template/template`.

Add `resources/` for files your game loads at runtime and `assets/` for files the tool uses to build the app, such as app icons:

```text
mygame/
├─ go.mod
├─ nsgo.yaml
├─ main.go
├─ game.go
├─ game_test.go
├─ resources/
│  ├─ hero.png
│  ├─ hero.json
│  ├─ jump.wav
│  └─ theme.m4a
└─ assets/
   └─ Icon.png
```

Anything under `resources/` is packaged with the app on every target and resolved by `"asset:<path>"` (for example `"asset:hero.png"` or `"asset:sounds/jump.wav"`). Atlas JSON files can be embedded directly into the Go binary with `//go:embed`.

## Run on every target

`nsgo dev` builds the app, launches it, and hot-reloads on save.

Desktop (this machine's native backend, the default in `nsgo.yaml`):

```sh
go tool nsgo dev
```

Web (starts a local dev server on port 8080 and opens the browser):

```sh
go tool nsgo dev -target web
```

iOS (simulator, or a connected iPhone signed with `-team` or Xcode's last team):

```sh
go tool nsgo dev -target ios_simulator
go tool nsgo dev -target ios
```

Android (a connected device or running emulator; pass `-device` when several are attached):

```sh
go tool nsgo dev -target android
```

Add `-fps` to any `nsgo dev` command to show NSGo's performance monitor over the running game.

## Draw a sprite on a fixed-step loop

A game is an NSGo component. It starts a `game.Loop` on its scope, updates state in `Update` at a fixed rate (60 updates a second by default), and triggers a `core.Canvas` redraw from `Draw` through a `core.Signal` without re-committing the view tree.

Pack a sprite sheet with TexturePacker or Aseprite (with rotation off), put the PNG in `resources/hero.png`, and embed its JSON with `//go:embed`. If you do not have a sheet yet, you can construct a `*sprite.Atlas` literal directly as shown in the [sprite guide](sprite.md).

```go
package main

import (
	_ "embed"
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

//go:embed resources/hero.json
var heroJSON []byte

func Game(cx *core.Scope) core.Node {
	atlas, err := sprite.LoadAtlas(core.Src("asset:hero.png"), heroJSON)
	if err != nil {
		panic(err)
	}

	walk := atlas.Animations["walk"]
	hero := sprite.NewSprite(walk.Frame())
	hero.OriginX = hero.Frame.Width / 2
	hero.OriginY = hero.Frame.Height / 2
	hero.X, hero.Y = 160, 120

	batch := &sprite.Batch{
		Atlas:   atlas,
		Sprites: []*sprite.Sprite{&hero},
	}

	x, y := hero.X, hero.Y
	lastX, lastY := x, y
	targetX, targetY := x, y
	alpha := core.NewSignal(cx, 0.0)

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			lastX, lastY = x, y
			dx, dy := targetX-x, targetY-y
			if dist := float32(math.Hypot(float64(dx), float64(dy))); dist > 1 {
				step := min(dist, 180*float32(dt.Seconds()))
				x += dx / dist * step
				y += dy / dist * step
				hero.FlipX = dx < 0
				walk.Update(dt)
				hero.Frame = walk.Frame()
			}
		},
		Draw: func(a float64) {
			alpha.Set(a)
		},
	}
	loop.Run(cx)

	return core.Canvas{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: core.RGB(0x181c24)},
		OnPress: func(e core.PressEvent) {
			targetX = float32(e.Position.X)
			targetY = float32(e.Position.Y)
		},
		Draw: func(d *core.Drawing, _ core.Size) {
			a := float32(alpha.Get())
			hero.X = lastX + (x-lastX)*a
			hero.Y = lastY + (y-lastY)*a
			batch.Draw(d)
		},
	}
}

func main() { nsgo.Run(Game) }
```

Because `Draw` reads `alpha.Get()`, NSGo redraws only the `Canvas` on each display frame. Interpolating between `(lastX, lastY)` and `(x, y)` keeps movement smooth on 60 Hz, 120 Hz and variable-refresh displays while the simulation stays locked to 60 Hz.

## Handle touch and mouse input

`core.Canvas.OnPress` handles taps on phones and clicks on desktops and the web, delivering `e.Position` in the canvas's own points.

For continuous pointer tracking, drags or multi-touch, wrap the `Canvas` in a `core.View` that listens to NSGo's pointer and touch handlers (`OnPressDown`, `OnPressUp`, `OnMouseMove`, `OnTouch`, `OnPan`) and layer native HUD views on top with `core.Absolute`:

```go
return core.View{
	Style: core.Style{FlexGrow: 1, FlexShrink: 1},
	OnMouseMove: func(e core.MouseEvent) {
		if e.PressedButton == core.MouseButtonLeft {
			targetX, targetY = float32(e.Position.X), float32(e.Position.Y)
		}
	},
	OnTouch: func(e core.TouchEvent) {
		if len(e.Touches) > 0 {
			targetX = float32(e.Touches[0].Position.X)
			targetY = float32(e.Touches[0].Position.Y)
		}
	},
	Children: []core.Node{
		canvas,
		core.View{
			Style: core.Style{
				Position:   core.Absolute,
				Top:        core.Pt(12),
				Left:       core.Pt(12),
				PaddingEnv: core.EnvAll(core.EnvSafeArea),
			},
			Children: []core.Node{
				core.Text{Content: "Score: 0", Style: core.Style{Color: core.RGB(0xffffff)}},
			},
		},
	},
}
```

Keyboard and gamepad action mapping will live in `game/input` ([#3](https://github.com/nsgo-dev/game/issues/3)).

## Play sound with sound.Mixer

Create one `sound.Mixer` on the component's scope. It persists master and group volumes in `core.Storage`, mutes automatically when the app moves to the background, and manages both short sound effects (`Effect`) and streaming music (`Music`).

Ship audio as WAV, AAC (`.m4a`) or MP3 under `resources/`:

```go
mixer := sound.NewMixer(cx, "sound")
music := mixer.Music("music")
music.Loop = true

var jump *sound.Effect
audio.LoadSound(cx, audio.Source{URI: "asset:jump.wav"}, func(s *audio.Sound, err error) {
	if err != nil {
		return
	}
	jump = mixer.Effect(s, "effects", 4)
})
```

Trigger the effect and start music on a user press (on the web, browsers block audio until the first user gesture):

```go
started := false
// Inside OnPress:
if !started {
	started = true
	music.Play(audio.Source{URI: "asset:theme.m4a"}, 2*time.Second)
}
if jump != nil {
	jump.Play(audio.Voice{Volume: 0.8})
}
```

## Next steps

- Read the package guides for [Loop](loop.md), [sprite](sprite.md) and [sound](sound.md), or the combined [Packages](packages.md) reference.
- When you are ready to verify all targets and produce release builds for macOS, Windows, Linux, the web, iOS and Android, read [Building and Shipping](shipping.md).
