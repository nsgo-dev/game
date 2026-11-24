# Loop

`game.Loop` drives fixed-step updates and interpolated frame draws on an NSGo scope's display frames, pausing automatically while the app is in the background.

```go
import "github.com/nsgo-dev/game/packages/game"
```

## How the fixed step works

Displays run at different refresh rates (60 Hz, 90 Hz, 120 Hz, or variable). If a game moves physics by raw frame delta times, jump heights and collisions change with the display rate.

`Loop` decouples simulation from rendering:

```go
type Loop struct {
	Rate     int                    // updates a second, 60 when 0
	MaxSteps int                    // most updates one frame runs, max(1, Rate/10) when 0
	Update   func(dt time.Duration) // moves the game on by dt, always 1/Rate seconds
	Draw     func(alpha float64)    // draws the frame, alpha in [0, 1)
}

func (l *Loop) Run(cx *core.Scope) (stop func())
```

On each display frame from NSGo's `RequestAnimationFrame`:

1. The loop adds the elapsed time since the previous frame to an accumulator.
2. While the accumulator holds at least one whole step (`time.Second / time.Duration(Rate)`), it subtracts one step and calls `Update(step)`. A single display frame may run zero, one or several updates.
3. Once fewer than one whole step remains, it calls `Draw(alpha)` with `alpha = float64(acc) / float64(step)` in `[0, 1)`.

`Run` reads `Rate`, `MaxSteps`, `Update` and `Draw` on every frame, so you can swap handlers or adjust the rate while the loop is running.

## Interpolating with alpha

When the display rate does not evenly divide `Rate` (for example, a 60 Hz simulation on a 120 Hz display), drawing the raw state from `Update` produces visible stutter because some frames run zero updates and some run one.

Keep the previous and current state in `Update`, pass `alpha` into a `core.Signal[float64]` from `Draw`, and interpolate inside `core.Canvas.Draw`:

```go
func Ball(cx *core.Scope) core.Node {
	var x, last float32
	alpha := core.NewSignal(cx, 0.0)

	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			last = x
			x += 120 * float32(dt.Seconds())
		},
		Draw: func(a float64) {
			alpha.Set(a)
		},
	}
	loop.Run(cx)

	return core.Canvas{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1},
		Draw: func(d *core.Drawing, _ core.Size) {
			at := last + (x-last)*float32(alpha.Get())
			d.SetFillColor(core.RGB(0xff5a1f)).FillRect(at, 40, 16, 16)
		},
	}
}
```

Because `alpha` is read only inside `Canvas.Draw`, setting it redraws the canvas without re-evaluating or committing the surrounding view tree.

## Hitch protection and MaxSteps

If a single frame takes much longer than usual (a debugger pause, a slow frame, or a device stall), catching up every missed step would cause an even slower next frame — the classic "spiral of death".

`MaxSteps` caps how many `Update` calls one display frame may execute:

- When `MaxSteps <= 0`, it defaults to `max(1, Rate/10)` — one tenth of a second of simulation (6 steps at 60 Hz, 12 steps at 120 Hz).
- When a frame reaches `MaxSteps`, the loop drops the remaining whole steps (`acc %= step`) and moves straight to `Draw`. The game briefly slows down instead of freezing.

Set `MaxSteps` explicitly when you need a tighter bound (for example, `MaxSteps: 3` for heavy physics):

```go
loop := &game.Loop{
	Rate:     60,
	MaxSteps: 3,
	Update:   stepWorld,
	Draw:     renderWorld,
}
```

## Frame time

Each frame adds the time between its timestamp and the previous frame's, exactly as `RequestAnimationFrame` gives it. Where the platform stamps its display frames, that is the display's own frame time, so a frame's length is the display's rather than when the engine got to the frame; elsewhere it is the graph clock's. The first frame after the loop starts or resumes adds the time from then to its timestamp, which may lie a frame ahead where the display stamps a frame with when it will show, so the loop's time is the shown frame's. A frame stamped before the time the loop started or resumed adds nothing, so resuming never moves the accumulator backward, and the next frame adds the time from then.

Set `FrameTime` to read each frame's length, the time it adds, ahead of the frame's updates; a frame that adds nothing passes 0. Frame-time statistics, such as [quality](quality.md)'s `Selector`, read it. The first frame back from the background measures from the time the window showed again, not the time away, and `FrameTime` may call the `stop` function `Run` returned, which ends the frame before any update. It also fires while `Paused` is set, so a caller that should only measure frames that simulate skips them itself.

```go
loop := &game.Loop{
	FrameTime: func(dt time.Duration) { selector.Observe(dt) },
	Update:    stepWorld,
	Draw:      renderWorld,
}
```

## Pausing and syncing subsystems

`SetPaused(true)` (or setting `Paused = true`) stops `Update` from advancing while `Draw` keeps running, and unpausing resumes without a catch-up backlog. Set `OnPause` to keep subsystems such as [`sound.Mixer`](sound.md#pausing-with-the-game) in sync with the loop's paused state:

```go
mixer.SetPauseExempt("ui", true)
loop := &game.Loop{
	Rate:    60,
	OnPause: mixer.SetPaused,
	Update:  stepWorld,
	Draw:    renderWorld,
}
loop.Run(cx)
```

- `SetPaused` calls `OnPause` immediately whenever the paused state changes (repeating the same value does nothing).
- Editing `Paused` directly calls `OnPause` on the next frame, or in the same frame when `Update` changes it.
- If `Paused` is already `true` when `Run` starts, `Run` calls `OnPause(true)`.
- Backgrounding does not call `OnPause`; both `Loop.Run` and `sound.NewMixer` watch `core.Env.Lifecycle` directly so backgrounding pauses and resumes everything in sync without clearing an open pause menu.

## Background pause and stopping

`Loop.Run` watches `cx.Env().Lifecycle()`:

- **`core.LifecycleBackground`** (minimized, hidden or covered on desktop; app backgrounded on iOS or Android): the loop cancels its pending frame request and runs no updates.
- **`core.LifecycleActive` or `core.LifecycleInactive`** (visible, even without focus on desktop): the loop runs normally. When returning from the background, it resets its clock anchor to `cx.Now()`, so time spent away produces zero catch-up steps.

The loop ends permanently when either:

- The `cx` scope is disposed (for example, navigating away or hiding the component with `core.Show`), or
- You call the `stop` function returned by `Run`.

Calling `stop()` is safe to repeat and safe from inside `Update`: if `Update` stops the loop, no further updates and no `Draw` run for that frame, and returning from the background will not restart it.

```go
var stop func()
loop := &game.Loop{
	Update: func(dt time.Duration) {
		if gameOver {
			stop()
		}
	},
	Draw: func(alpha float64) {
		frame.Set(alpha)
	},
}
stop = loop.Run(cx)
```

## Immersive mode on phones

Set `Immersive` to hide a phone's system bars while the game plays and bring them back in menus. The loop calls NSGo's `mobile.Immersive`: Android hides its status and navigation bars and shows them briefly on an edge swipe, iOS hides the status bar and lets the home indicator fade, and HarmonyOS hides its status bar, navigation bar and navigation indicator. Desktop and web targets ignore it.

```go
loop := &game.Loop{
	Immersive: true,
	Update:    stepWorld,
	Draw:      renderWorld,
}
loop.Run(cx)

// Opening the pause menu shows the bars on the next frame; closing it hides them again.
openMenu := func() { loop.SetPaused(true) }
closeMenu := func() { loop.SetPaused(false) }
```

- The bars hide while the loop runs and is not `Paused`, and come back while it is `Paused`, when `stop` is called and when `cx` is disposed, so a menu screen that replaces the game's view brings them back too.
- The loop hides them as it starts or returns from the background, applies a change to `Immersive` or `Paused` on its next frame (in the same frame when `Update` pauses), and asks the platform only when the state changes. It leaves the bars as they are while the app is in the background.
- Hiding and showing the bars can change the safe-area insets the platform reports (HarmonyOS, for one, keeps only the display cutout while they are hidden), so lay HUDs out with [`screen.Viewport.SafeGameRectForEnv`](screen.md#safe-area-for-mobile-huds), which follows the insets as they change.
- Loops in the same window share the bars: they stay hidden while any loop with `Immersive` set plays, and come back once the last of them pauses or ends. A scene that starts its own loop while the previous scene's loop is still alive, as under a scene transition, keeps the bars hidden when the old loop is disposed, and a pushed scene's loop keeps them hidden when the scene below pauses its own. A menu shown as an NSGo page or dialog can also set `StatusBar: core.StatusBarSetting{Immersive: core.ToggleOff}`, which overrides the window's setting while it is shown.

## Testing with nsgotest

`nsgotest` runs display frames deterministically on a virtual clock (one frame every 16.667 ms, 60 frames a second), so you can test gameplay loops, `MaxSteps` hitches and background pauses without sleeping.

```go
package mygame_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestLoopSimulation(t *testing.T) {
	updates := 0
	l := &game.Loop{
		Rate:     60,
		MaxSteps: 4,
		Update: func(dt time.Duration) {
			updates++
		},
	}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		l.Run(cx)
		return core.View{}
	})

	// Advance steps frame by frame for one full second.
	app.Advance(time.Second)
	if updates != 60 {
		t.Fatalf("got %d updates, want 60", updates)
	}

	// FastForward jumps in a single frame, exercising MaxSteps.
	app.FastForward(500 * time.Millisecond)
	if updates != 64 {
		t.Fatalf("got %d updates after hitch, want 64", updates)
	}

	// Background pauses the loop with no catch-up on resume.
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleBackground})
	app.Advance(time.Second)
	app.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	if updates != 64 {
		t.Fatalf("got %d updates while backgrounded, want 64", updates)
	}
}
```
