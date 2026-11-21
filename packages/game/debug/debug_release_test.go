//go:build release

package debug_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game"
	"github.com/nsgo-dev/game/packages/game/debug"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestReleaseBuildIsDisabledZeroCostStub(t *testing.T) {
	if debug.Enabled() {
		t.Fatal("Enabled() = true in release build, want false")
	}

	dbg := debug.New(debug.Options{
		ToggleKey: "F3",
		Visible:   true,
		TargetFPS: 60,
		LineWidth: 2,
		Scenes: func() []debug.SceneSnapshot {
			t.Error("Options.Scenes called in release build")
			return nil
		},
	})

	if dbg.Visible() {
		t.Fatal("Visible() = true in release build, want false")
	}
	dbg.SetVisible(true)
	dbg.Toggle()
	if dbg.Visible() {
		t.Fatal("Visible() became true after SetVisible/Toggle in release build")
	}

	var updates, draws int
	loop := &game.Loop{
		Rate:   60,
		Update: func(time.Duration) { updates++ },
		Draw:   func(float64) { draws++ },
	}
	dbg.WrapLoop(loop)

	sp := sprite.NewSprite(sprite.Frame{Name: "hero", W: 16, H: 16, Width: 16, Height: 16})
	batch := &sprite.Batch{Sprites: []*sprite.Sprite{&sp}}
	shapes := debug.ShapeOverlay{LineWidth: 2}
	box := core.Bounds{Origin: core.Point{X: 1, Y: 2}, Size: core.Size{Width: 10, Height: 12}}

	var viewNode core.Node
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		dbg.Attach(cx)
		loop.Run(cx)
		viewNode = dbg.View(cx)
		return core.View{
			Style: core.Style{Width: core.Pt(200), Height: core.Pt(200)},
			Children: []core.Node{
				core.Canvas{
					TestID: "canvas",
					Style:  core.Style{Width: core.Pt(200), Height: core.Pt(200)},
					Draw: func(d *core.Drawing, size core.Size) {
						dbg.Stats().DrawGraph(d, size, 60)
						dbg.DrawShapes(d, func(s *debug.ShapeOverlay) {
							t.Error("DrawShapes callback invoked in release build")
							s.DrawBox(d, box, core.RGB(0x22C55E))
						})
						dbg.DrawBox(d, box, core.RGB(0x22C55E))
						dbg.DrawCircle(d, 10, 10, 5, core.RGB(0x38BDF8))
						dbg.DrawSpriteBounds(d, &sp, core.RGB(0xF59E0B))
						dbg.DrawBatchBounds(d, batch, core.RGB(0xF59E0B))
						shapes.DrawBox(d, box, core.RGB(0x22C55E))
						shapes.DrawCircle(d, 10, 10, 5, core.RGB(0x38BDF8))
						shapes.DrawSpriteBounds(d, &sp, core.RGB(0xF59E0B))
						shapes.DrawBatchBounds(d, batch, core.RGB(0xF59E0B))
					},
				},
				viewNode,
			},
		}
	})

	if viewNode != nil {
		t.Fatalf("View(cx) = %#v in release build, want nil", viewNode)
	}
	if app.Exists("debug-overlay") {
		t.Fatal("debug-overlay exists in release build")
	}

	app.Key("F3")
	if dbg.Visible() || app.Exists("debug-overlay") {
		t.Fatal("pressing F3 showed overlay in release build")
	}

	app.Advance(100 * time.Millisecond)
	if updates == 0 || draws == 0 {
		t.Fatalf("loop did not run normally after WrapLoop: updates=%d draws=%d", updates, draws)
	}

	dbg.RecordFrame(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	dbg.SetScenes([]debug.SceneSnapshot{{Name: "world"}})
	dbg.RefreshScenes()

	stats := dbg.Stats()
	stats.Record(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	if stats.Len() != 0 || stats.FPS() != 0 || stats.MaxTotal() != 0 || stats.Latest() != (debug.FrameSample{}) || stats.At(0) != (debug.FrameSample{}) || stats.Average() != (debug.FrameSample{}) {
		t.Fatal("FrameStats recorded data in release build")
	}
	if out := stats.Samples(nil); len(out) != 0 {
		t.Fatalf("Samples() returned %d items in release build, want 0", len(out))
	}
	stats.Reset()

	if n := len(app.Find("canvas").Ops); n != 0 {
		t.Fatalf("canvas recorded %d ops in release build, want 0", n)
	}
}
