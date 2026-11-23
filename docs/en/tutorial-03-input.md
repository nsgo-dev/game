# Tutorial 3: Input

Let the player move the basket from lane to lane by touching or clicking a side of the playfield, or with the arrow keys, through `game/input`'s named actions, sampled once per step.

The finished code is in [`examples/tutorial/ch03-input`](../../examples/tutorial/ch03-input/).

## Moves in the world

The world takes the player's move as an argument of `step`: -1 for a lane to the left, 1 for a lane to the right, 0 to stay. The basket heads for its lane and glides there at `basketSpeed`:

```go
func (w *world) step(dt time.Duration, move int) {
	secs := float32(dt.Seconds())
	w.basketLane = min(max(w.basketLane+move, 0), laneCount-1)
	w.prevX = w.basketX
	target := laneX(w.basketLane)
	if d := target - w.basketX; d > 0 {
		w.basketX = min(w.basketX+basketSpeed*secs, target)
	} else if d < 0 {
		w.basketX = max(w.basketX-basketSpeed*secs, target)
	}
	// The star falls as before…
}
```

The world still knows nothing about touches or keys, so tests move the basket by passing moves, and so will the bot in [chapter 9](tutorial-09-ship.md).

## Actions from touches and clicks

`input.Actions` maps names, such as `"left"`, to the touches and mouse buttons that press them. Star Catcher binds each half of the playfield:

```go
var (
	leftHalf  = core.Bounds{Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
	rightHalf = core.Bounds{Origin: core.Point{X: virtualWidth / 2}, Size: core.Size{Width: virtualWidth / 2, Height: virtualHeight}}
)

s.actions.MapPoint = func(p core.Point) (core.Point, bool) { return viewport.ViewToGame(s.size, p) }
s.actions.Bind("left", input.TouchRegion(leftHalf), input.MouseRegion(leftHalf, core.MouseButtonLeft))
s.actions.Bind("right", input.TouchRegion(rightHalf), input.MouseRegion(rightHalf, core.MouseButtonLeft))
```

- The regions are in virtual points, the same on every screen. `MapPoint` converts a touch from the view's points with the viewport, and reports `false` for one on a bar, which then presses nothing.
- `s.size` is the canvas's size, which `draw` records each frame.
- `Attach` wires the actions to the view's touch, press and mouse handlers:

```go
v := core.View{TestID: "game-view", /* … */}
s.actions.Attach(&v)
```

## Keys

A hardware keyboard reaches the game through key bindings on its scope. Each one triggers an action, which latches it for the next step, so a key and a touch move the basket the same way:

```go
repeat := core.KeyOptions{Repeat: true}
for _, key := range []string{"left", "a"} {
	cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("left") })
}
for _, key := range []string{"right", "d"} {
	cx.OnKeyWith(key, repeat, func() { s.actions.Trigger("right") })
}
```

`Repeat` fires the binding again on the keyboard's auto-repeat, so holding an arrow sweeps the basket across the lanes.

## Sample once per step

`Step` takes a snapshot of the actions at the start of each update, and `JustPressed` reports an action that went down since the last one. Each new press moves the basket one lane:

```go
func (s *starCatcher) update(dt time.Duration) {
	s.actions.Step()
	move := 0
	if s.actions.JustPressed("left") {
		move--
	}
	if s.actions.JustPressed("right") {
		move++
	}
	s.world.step(dt, move)
}
```

Sampling in the step, not in the touch handler, keeps input in time with the simulation: a tap that starts and ends between two steps is still latched for one step, and a touch held for a second moves the basket once, not sixty times.

Run the chapter, and move the basket with a click or a touch on each side, or with the arrow keys.

## Test touches and keys

`nsgotest` sends the same events a platform would. `app.Key` presses a key; `app.Touch` puts a finger down on a view and returns it, so the test can hold it while time passes and lift it later:

```go
func TestTouchingASideMovesTheBasket(t *testing.T) {
	var s *starCatcher
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		s = newStarCatcher(cx)
		return s.view()
	}, nsgotest.Size(480, 640))

	// The 480 by 640 window shows the playfield at twice its size, so x 400 is in its right half.
	finger := app.Touch("game-view", core.Point{X: 400, Y: 300})
	app.Advance(time.Second / 2)
	finger.Lift()
	app.Advance(time.Second / 4)
	if s.world.basketLane != laneCount/2+1 {
		t.Fatalf("after a held touch the basket heads for lane %d, want one lane right, %d", s.world.basketLane, laneCount/2+1)
	}
}
```

The test builds the game itself instead of calling `Root`, so it keeps the `*starCatcher` and can look at its world. Holding the finger for half a second still moves the basket a single lane, which is what `JustPressed` promises.

## Next

The game is drawn with rectangles and circles. [Chapter 4](tutorial-04-sprites.md) draws it with sprites.
