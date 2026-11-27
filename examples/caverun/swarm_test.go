package main

import (
	"image/color"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/game/pkg/screen"
	"github.com/nsgo-dev/nsgo/core"
)

const fixedStep = time.Second / 60

func TestFireflySwarmDraws1000MovingSpritesInOneAtlasCall(t *testing.T) {
	app, g := startCaveRun(t)
	startPlaying(t, app, g)

	// The pause menu's button turns the swarm on.
	app.Press("id:pause-btn")
	app.Settle()
	if !slices.Contains(app.Texts(), "Fireflies: Off") {
		t.Fatalf("pause menu shows %q, want a Fireflies: Off button", app.Texts())
	}
	app.Press("id:swarm-btn")
	app.Settle()
	if !slices.Contains(app.Texts(), "Fireflies: 1000") {
		t.Fatalf("pause menu shows %q after pressing swarm-btn, want Fireflies: 1000", app.Texts())
	}
	app.Press("id:resume-btn")
	app.Advance(testFrame)

	before := make([][2]float32, swarmSize)
	for i, f := range g.level.swarm.flies {
		before[i] = [2]float32{f.x, f.y}
	}
	app.Advance(time.Second)

	moved := 0
	for i, f := range g.level.swarm.flies {
		if f.x != before[i][0] || f.y != before[i][1] {
			moved++
		}
	}
	if moved != swarmSize {
		t.Fatalf("%d of %d fireflies moved in 1s, want all of them", moved, swarmSize)
	}
	if got := len(g.level.batch.Sprites); got < swarmSize+10 {
		t.Fatalf("level batch holds %d sprites, want the %d fireflies and the level's own", got, swarmSize)
	}

	// The whole swarm goes to the drawing as one DrawAtlas, and every firefly lands inside the virtual canvas.
	ops := app.Find("id:game-canvas").Ops
	largest := 0
	for _, op := range ops {
		if op.Kind == core.DrawAtlas && op.Atlas != nil && len(*op.Atlas) > largest {
			largest = len(*op.Atlas)
		}
	}
	if largest < swarmSize {
		t.Fatalf("largest DrawAtlas drew %d sprites, want the %d fireflies in one call", largest, swarmSize)
	}
	view := g.level.cam.VisibleBounds()
	for i := range g.level.swarm.sprites {
		s := &g.level.swarm.sprites[i]
		if float64(s.X) < float64(view.Origin.X)-float64(swarmMargin)-1 || float64(s.X) > float64(view.Origin.X+view.Size.Width)+float64(swarmMargin)+1 {
			t.Fatalf("firefly %d at x=%.1f is outside the view %v", i, s.X, view)
		}
		if float64(s.Y) < float64(view.Origin.Y)-float64(swarmMargin)-1 || float64(s.Y) > float64(view.Origin.Y+view.Size.Height)+float64(swarmMargin)+1 {
			t.Fatalf("firefly %d at y=%.1f is outside the view %v", i, s.Y, view)
		}
	}
	if got, want := app.Find("id:hud-sprites").Text, "SPRITES 1012"; got != want {
		t.Fatalf("hud-sprites = %q, want %q", got, want)
	}
}

func TestFireflySwarmIsDeterministic(t *testing.T) {
	run := func() [][2]float32 {
		assets, err := loadGameAssets()
		if err != nil {
			t.Fatal(err)
		}
		s := newFireflySwarm(assets.atlas, virtualWidth, virtualHeight)
		for range 600 {
			s.update(fixedStep)
		}
		out := make([][2]float32, len(s.flies))
		for i, f := range s.flies {
			out[i] = [2]float32{f.x, f.y}
		}
		return out
	}
	a, b := run(), run()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("firefly %d ended at %v and %v in two runs of the same seed", i, a[i], b[i])
		}
		if x, y := a[i][0], a[i][1]; x < -swarmMargin || x >= virtualWidth+swarmMargin || y < -swarmMargin || y >= virtualHeight+swarmMargin {
			t.Fatalf("firefly %d drifted out of the wrapped view to (%.1f, %.1f)", i, x, y)
		}
	}
}

// newSwarmLevel returns a level with the firefly swarm on, stepped and drawn past any first-use growth, and a drawing to draw it into.
func newSwarmLevel(tb testing.TB) (*levelState, *core.Drawing, screen.Viewport) {
	tb.Helper()
	assets, err := loadGameAssets()
	if err != nil {
		tb.Fatal(err)
	}
	l, err := newLevelState(assets, levelCallbacks{})
	if err != nil {
		tb.Fatal(err)
	}
	l.setSwarm(true)
	vp := screen.Viewport{Width: virtualWidth, Height: virtualHeight, PixelSnap: true}
	d := &core.Drawing{Ops: make([]core.DrawOp, 0, 1024)}
	for range 30 {
		l.update(fixedStep, stepInput{})
		d.Ops = d.Ops[:0]
		l.draw(d, 0.5, vp)
	}
	if got := l.spriteCount(); got < swarmSize {
		tb.Fatalf("level draws %d sprites, want at least %d", got, swarmSize)
	}
	return l, d, vp
}

func TestSwarmAddsNoAllocations(t *testing.T) {
	l, d, vp := newSwarmLevel(t)
	if allocs := testing.AllocsPerRun(60, func() { l.swarm.update(fixedStep) }); allocs != 0 {
		t.Fatalf("moving %d fireflies allocated %v times per step, want 0", swarmSize, allocs)
	}
	if allocs := testing.AllocsPerRun(60, func() { l.update(fixedStep, stepInput{}) }); allocs != 0 {
		t.Fatalf("a fixed step with the swarm allocated %v times, want 0", allocs)
	}
	l.setSwarm(false)
	if allocs := testing.AllocsPerRun(60, func() { l.update(fixedStep, stepInput{}) }); allocs != 0 {
		t.Fatalf("a fixed step without the swarm allocated %v times, want 0", allocs)
	}

	// NSGo v0.3.0's Drawing copies the image source of every DrawImageFrom and DrawAtlas, and the sprite data of every DrawAtlas, onto the heap (internal/driver/canvas.go:995, internal/driver/atlas.go:34 and :52), so the tilemap's tiles allocate on every frame. The swarm adds only its own DrawAtlas's copies, however many fireflies it holds (nsgo-dev/nsgo#2269).
	draw := func() float64 {
		for range 30 {
			l.update(fixedStep, stepInput{})
			d.Ops = d.Ops[:0]
			l.draw(d, 0.5, vp)
		}
		return testing.AllocsPerRun(60, func() {
			d.Ops = d.Ops[:0]
			l.draw(d, 0.5, vp)
		})
	}
	drawWithout := draw()
	l.setSwarm(true)
	drawWith := draw()
	if extra := drawWith - drawWithout; extra > maxSwarmDrawAllocs {
		t.Fatalf("drawing the swarm's %d fireflies added %v allocations per frame (%v with, %v without), want at most %d", swarmSize, extra, drawWith, drawWithout, maxSwarmDrawAllocs)
	}
}

// maxSwarmDrawAllocs bounds the allocations the swarm adds to a frame's drawing: NSGo's copies for the one DrawAtlas that draws it, and for the run of level sprites it splits in two.
const maxSwarmDrawAllocs = 6

// BenchmarkLevel1000Sprites measures one fixed step, and one step plus the frame's drawing, of the level with the 1000-firefly swarm on.
func BenchmarkLevel1000Sprites(b *testing.B) {
	l, d, vp := newSwarmLevel(b)
	b.Run("Step", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			l.update(fixedStep, stepInput{})
		}
		b.ReportMetric(float64(l.spriteCount()), "sprites")
	})
	b.Run("StepAndDraw", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			l.update(fixedStep, stepInput{})
			d.Ops = d.Ops[:0]
			l.draw(d, 0.5, vp)
		}
		b.ReportMetric(float64(l.spriteCount()), "sprites")
	})
}

// TestScreenshot renders the game view a moment into a run with the firefly swarm on, from the canvas's recorded drawing, and compares it with screenshot.png, which the README shows. Run it with -update-golden to rewrite the picture after a visual change.
func TestScreenshot(t *testing.T) {
	app, g := startCaveRun(t)
	app.Post(g.toggleSwarm)
	startPlaying(t, app, g)
	c := &keyControls{app: app}
	c.move(1)
	app.Advance(1200 * time.Millisecond)
	c.move(0)
	c.jump(true)
	app.Advance(10 * testFrame)

	canvas := app.Find("id:game-canvas")
	w, h := int(canvas.Frame.Size.Width), int(canvas.Frame.Size.Height)
	img := gametest.RenderOps(w, h, canvas.Ops, gametest.RenderOptions{Background: color.RGBA{R: 0x0b, G: 0x0d, B: 0x17, A: 0xff}})
	c.jump(false)

	diff, mismatched, err := gametest.CompareGolden(img, "screenshot.png", gametest.ShouldUpdateGolden())
	if err != nil {
		t.Fatal(err)
	}
	// Different CPUs may round a blended edge pixel differently; anything more is a real change.
	if limit := w * h / 1000; mismatched > limit {
		dir := t.TempDir()
		_ = gametest.WritePNG(dir+"/screenshot.got.png", img)
		_ = gametest.WritePNG(dir+"/screenshot.diff.png", diff)
		t.Fatalf("screenshot differs from screenshot.png in %d pixels, more than %d (got and diff in %s); rerun with -update-golden if the change is intended", mismatched, limit, dir)
	}
}
