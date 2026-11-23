# Tutorial 2: The Game Loop

Give the game a world that moves in fixed steps, drive it with a `game.Loop`, draw it at a virtual resolution that `screen.Viewport` scales to any window, and test the world without one.

The finished code is in [`examples/tutorial/ch02-game-loop`](../../examples/tutorial/ch02-game-loop/).

## A world that only steps change

Put the game's state in a type of its own, and change it only in one method, `step`, that moves it on by a fixed time:

```go
type world struct {
	basketX float32

	starLane     int
	starY, prevY float32
}

func (w *world) step(dt time.Duration) {
	w.prevY = w.starY
	w.starY += fallSpeed * float32(dt.Seconds())
	if w.starY > virtualHeight+itemSize {
		w.respawn((w.starLane + 2) % laneCount)
	}
}
```

The world knows nothing about windows or frames, which pays off three ways: a test can step it a thousand times in a millisecond, the game plays the same on a 60 Hz and a 120 Hz display, and in [chapter 5](tutorial-05-rules.md) the same seed and moves will always play the same game.

`prevY` keeps where the star was a step ago. Drawing uses it to place the star between steps:

```go
func (w *world) starAt(alpha float32) float32 { return w.prevY + (w.starY-w.prevY)*alpha }
```

## Drive it with game.Loop

`game.Loop` runs `Update` at a fixed rate on the display's frames, and `Draw` once a frame in between:

```go
func newStarCatcher(cx *core.Scope) *starCatcher {
	s := &starCatcher{world: newWorld(), frame: core.NewSignal(cx, uint64(0))}
	loop := &game.Loop{
		Rate:   60,
		Update: s.update,
		Draw: func(alpha float64) {
			s.alpha = alpha
			s.frame.Update(func(n uint64) uint64 { return n + 1 })
		},
	}
	loop.Run(cx)
	return s
}

func (s *starCatcher) update(dt time.Duration) { s.world.step(dt) }
```

- Each display frame, the loop adds the time since the last one and runs `Update` once for every whole sixtieth of a second it holds, so `dt` is always 1/60 s.
- `Draw` gets `alpha`, how far the frame falls between the last step and the next, from 0 up to 1.
- The canvas reads `frame` in its `Draw`, so setting it each frame redraws only the canvas, without rebuilding any views.
- The loop stops when the component goes, and runs no updates while the app is in the background.

## A virtual resolution

Phones, tablets, desktop windows and browser tabs all have different sizes and shapes. Rather than lay the game out for each one, Star Catcher plays on a virtual canvas 240 points wide and 320 tall, split into five lanes, and lets a `screen.Viewport` fit it to the window:

```go
var viewport = screen.Viewport{
	Width:    virtualWidth,
	Height:   virtualHeight,
	Mode:     screen.ModeFit,
	BarColor: barColor,
}

func (s *starCatcher) draw(d *core.Drawing, size core.Size) {
	_ = s.frame.Get()
	viewport.Draw(d, size, s.drawWorld)
}
```

`ModeFit` scales the playfield as large as fits, keeps its shape, and fills the rest with bars of `BarColor`. `viewport.Draw` scales and clips the drawing, then calls `drawWorld`, which draws in virtual points only:

```go
func (s *starCatcher) drawWorld(d *core.Drawing, _ core.Size) {
	w := s.world
	d.SetFillColor(skyColor).FillRect(0, 0, virtualWidth, virtualHeight)
	// The lanes and the ground…
	d.SetFillColor(basketColor).FillRect(w.basketX-basketWidth/2, basketY, basketWidth, basketHeight)

	y := w.starAt(float32(s.alpha))
	d.SetFillColor(starColor).BeginPath().Ellipse(laneX(w.starLane), y, itemSize/2, itemSize/2).Fill()
}
```

Run the chapter. A star falls down a lane, and starts again two lanes over once it has passed the ground. Make the window wide, and bars appear beside the playfield.

## Test the world and the loop

The world's tests step it by hand. A second of steps must move the star by `fallSpeed`:

```go
func TestStarFallsAtFallSpeed(t *testing.T) {
	w := newWorld()
	start := w.starY
	for range 60 {
		w.step(step)
	}
	if got, want := w.starY-start, float32(fallSpeed); !near(got, want) {
		t.Fatalf("star fell %v points in a second, want %v", got, want)
	}
}
```

An app test checks that the loop really runs: `app.Advance` moves the test's clock on and runs the display frames in that time, so after a second the star the canvas drew is lower:

```go
func TestLoopMovesTheStarOnScreen(t *testing.T) {
	app := nsgotest.Start(t, Root, nsgotest.Size(240, 320))
	before := starEllipse(t, app.Find("game-canvas").Ops)

	app.Advance(time.Second)

	after := starEllipse(t, app.Find("game-canvas").Ops)
	if after.Y <= before.Y {
		t.Fatalf("star at y %v after a second, want below %v", after.Y, before.Y)
	}
}
```

Neither test sleeps: the test's clock moves only when the test says so, so the tests are as fast and as repeatable as the world.

## Next

The basket sits still. [Chapter 3](tutorial-03-input.md) lets the player move it.
