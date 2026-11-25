# Tutorial 1: An Empty Project

Create the Go module for Star Catcher, describe the app in `nsgo.yaml`, open a window that paints the playfield in a `Canvas`, and test it without a window.

The finished code is in [`examples/tutorial/ch01-empty-project`](../../examples/tutorial/ch01-empty-project/).

## Create the module

A game is an ordinary Go module that requires the game library and NSGo, with the `nsgo` command as one of its tools:

```sh
mkdir starcatcher && cd starcatcher
go mod init example.com/starcatcher
go get github.com/nsgo-dev/game/packages/game
go get -tool github.com/nsgo-dev/nsgo/cmd/nsgo@v0.3.4
```

`go tool nsgo` now runs the `nsgo` command at the version the module pins, so everyone who builds the game builds it with the same one.

## Describe the app

`nsgo.yaml`, next to `go.mod`, names the app and says how `nsgo dev` runs it:

```yaml
name: Star Catcher 1
id: dev.nsgo.starcatcher.ch01
version: 0.1.0

dev:
  target: native
```

`id` is the app's bundle identifier on Apple platforms and its application ID on Android. Each chapter has its own, so two chapters installed side by side on a phone do not replace each other. [Chapter 9](tutorial-09-ship.md) sets the name, version and icon the stores will show.

## Open a window

`main.go` hands NSGo the root component:

```go
package main

import "github.com/nsgo-dev/nsgo"

func main() { nsgo.Run(Root) }
```

`Root` is an NSGo component: a function of a `*core.Scope` that returns the views to show. Star Catcher draws its playfield in a `core.Canvas` that fills the window, and shows the title in a native `core.Text` above it:

```go
func Root(cx *core.Scope) core.Node {
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Background: skyColor},
		Children: []core.Node{
			core.Canvas{
				TestID: "game-canvas",
				Label:  "Star Catcher playfield",
				Style:  fill(),
				Draw:   drawPlayfield,
			},
			core.View{
				PointerEvents: core.PointerEventsNone,
				Style: core.Style{
					Position:   core.Absolute,
					Top:        core.Pt(0),
					Left:       core.Pt(0),
					Right:      core.Pt(0),
					AlignItems: core.AlignCenter,
					Padding:    core.All(24),
					PaddingEnv: core.EnvAll(core.EnvSafeArea),
				},
				Children: []core.Node{
					core.Text{
						TestID:  "title",
						Content: "Star Catcher",
						Style:   core.Style{Color: textColor, FontSize: 28, FontWeight: core.FontWeightBold},
					},
				},
			},
		},
	}
}
```

- `fill()` returns a style that pins a child to all four edges of its parent, so the canvas covers the window at any size.
- `PaddingEnv: core.EnvAll(core.EnvSafeArea)` keeps the title clear of a phone's notch and home indicator.
- `PointerEventsNone` lets touches pass through the title to the playfield, which [chapter 3](tutorial-03-input.md) starts listening to.
- `TestID` names a view for tests, and `Label` names it for screen readers.

The canvas calls `Draw` with a `*core.Drawing` and its own size in points, and draws again whenever the size changes:

```go
func drawPlayfield(d *core.Drawing, size core.Size) {
	w, h := float32(size.Width), float32(size.Height)
	d.SetFillColor(skyColor).FillRect(0, 0, w, h)
	d.SetFillColor(groundColor).FillRect(0, h-groundHeight, w, groundHeight)
}
```

Run it:

```sh
go tool nsgo dev
```

A dark sky fills the window, with the ground along its bottom and the title at its top. Resize the window, and the ground stays at the bottom.

## Test it

NSGo's `nsgotest` package runs an app without a window: `nsgotest.Start` mounts it in a window of a size the test picks, `Find` looks up a view by its `TestID` or text and fails the test if there is none, and a canvas's `Ops` are the drawing operations it recorded.

```go
func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestShowsTitleOverPlayfield(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(360, 640))
	app.Find("text:Star Catcher")

	ops := app.Find("game-canvas").Ops
	if !hasFillColor(ops, skyColor) {
		t.Fatal("canvas did not paint the sky")
	}
	if !hasFillColor(ops, groundColor) {
		t.Fatal("canvas did not paint the ground")
	}
}
```

`TestMain` hands the main thread to the platform when the tests run on a real one, as `go tool nsgo test -target macos` does. The chapter's second test resizes the window with `app.Resize` and checks that the ground moved to the new bottom edge. Run the tests with:

```sh
go tool nsgo test ./...
```

## Next

The playfield is still a picture. [Chapter 2](tutorial-02-game-loop.md) makes it move with a fixed-step game loop.
