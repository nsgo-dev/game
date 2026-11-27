//go:build !release

package gamedebug_test

import (
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/game"
	"github.com/nsgo-dev/game/pkg/sprite"
	"github.com/nsgo-dev/game/pkg/tooling/gamedebug"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var testSheet = &sprite.Atlas{
	Image: core.ImageSource{URI: "asset:sheet.png"},
	Frames: map[string]sprite.Frame{
		"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		"coin": {Name: "coin", X: 16, Y: 0, W: 8, H: 10, OffsetX: 4, OffsetY: 3, Width: 16, Height: 16},
	},
}

func TestEnabledInDevBuild(t *testing.T) {
	if !gamedebug.Enabled() {
		t.Fatal("Enabled() = false in default build, want true")
	}
}

func TestToggleKeyShowsAndHidesOverlayAndShapes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		toggleKey string
		pressKey  string
	}{
		{name: "default F3", toggleKey: "", pressKey: "F3"},
		{name: "custom key", toggleKey: "f4", pressKey: "f4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dbg := gamedebug.New(gamedebug.Options{ToggleKey: tc.toggleKey})
			box := core.Bounds{Origin: core.Point{X: 10, Y: 20}, Size: core.Size{Width: 30, Height: 40}}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return core.View{
					Style: core.Style{Width: core.Pt(400), Height: core.Pt(300)},
					Children: []core.Node{
						core.Canvas{
							TestID: "game",
							Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
							Draw: func(d *core.Drawing, _ core.Size) {
								dbg.DrawShapes(d, func(s *gamedebug.ShapeOverlay) {
									s.DrawBox(d, box, core.RGB(0x22C55E))
								})
							},
						},
						dbg.View(cx),
					},
				}
			})

			if dbg.Visible() {
				t.Fatal("overlay started visible, want hidden")
			}
			if app.Exists("debug-overlay") {
				t.Fatal("debug-overlay exists while hidden")
			}
			if n := len(app.Find("game").Ops); n != 0 {
				t.Fatalf("game canvas recorded %d ops while hidden, want 0", n)
			}

			app.Key(tc.pressKey)
			if !dbg.Visible() {
				t.Fatalf("pressing %q did not show overlay", tc.pressKey)
			}
			if !app.Exists("debug-overlay") {
				t.Fatal("debug-overlay not mounted after toggle")
			}
			if got := app.Find("debug-overlay").Props.PointerEvents; got != core.PointerEventsNone {
				t.Fatalf("debug-overlay PointerEvents = %v, want PointerEventsNone", got)
			}
			if n := len(app.Find("game").Ops); n == 0 {
				t.Fatal("game canvas drew no debug shapes after toggle on")
			}

			app.Key(tc.pressKey)
			if dbg.Visible() {
				t.Fatalf("second %q press did not hide overlay", tc.pressKey)
			}
			if app.Exists("debug-overlay") {
				t.Fatal("debug-overlay still mounted after second toggle")
			}
			if n := len(app.Find("game").Ops); n != 0 {
				t.Fatalf("game canvas recorded %d ops after toggle off, want 0", n)
			}
		})
	}
}

func TestFrameStatsRingBufferAndZeroAllocs(t *testing.T) {
	var stats gamedebug.FrameStats
	if stats.Len() != 0 || stats.FPS() != 0 || stats.MaxTotal() != 0 {
		t.Fatalf("empty stats = (len=%d, fps=%v, max=%v), want all zero", stats.Len(), stats.FPS(), stats.MaxTotal())
	}

	// Write more than FrameHistory samples to verify ring wrap-around.
	totalWrites := gamedebug.FrameHistory + 35
	for i := 1; i <= totalWrites; i++ {
		tot := time.Duration(i) * time.Millisecond
		upd := time.Duration(i) * 200 * time.Microsecond
		drw := time.Duration(i) * 100 * time.Microsecond
		stats.Record(tot, upd, drw)
	}

	if stats.Len() != gamedebug.FrameHistory {
		t.Fatalf("Len() = %d, want %d", stats.Len(), gamedebug.FrameHistory)
	}
	oldestWant := time.Duration(totalWrites-gamedebug.FrameHistory+1) * time.Millisecond
	newestWant := time.Duration(totalWrites) * time.Millisecond
	if got := stats.At(0).Total; got != oldestWant {
		t.Fatalf("At(0).Total = %v, want %v", got, oldestWant)
	}
	if got := stats.Latest().Total; got != newestWant {
		t.Fatalf("Latest().Total = %v, want %v", got, newestWant)
	}
	if got := stats.MaxTotal(); got != newestWant {
		t.Fatalf("MaxTotal() = %v, want %v", got, newestWant)
	}

	buf := make([]gamedebug.FrameSample, 0, gamedebug.FrameHistory)
	samples := stats.Samples(buf)
	if len(samples) != gamedebug.FrameHistory || samples[0].Total != oldestWant || samples[len(samples)-1].Total != newestWant {
		t.Fatalf("Samples order mismatch: len=%d first=%v last=%v", len(samples), samples[0].Total, samples[len(samples)-1].Total)
	}

	// Verify zero per-frame allocations.
	allocs := testing.AllocsPerRun(100, func() {
		stats.Record(16*time.Millisecond, 4*time.Millisecond, 3*time.Millisecond)
	})
	if allocs != 0 {
		t.Fatalf("FrameStats.Record allocated %.1f times per call, want 0", allocs)
	}

	stats.Reset()
	if stats.Len() != 0 {
		t.Fatalf("Len() after Reset = %d, want 0", stats.Len())
	}
}

func TestWrapLoopMeasuresUpdateAndDrawCosts(t *testing.T) {
	now := time.Unix(100, 0)
	dbg := gamedebug.New(gamedebug.Options{
		Now: func() time.Time { return now },
	})

	var updates, draws int
	loop := &game.Loop{
		Rate: 60,
		Update: func(dt time.Duration) {
			updates++
			now = now.Add(4 * time.Millisecond)
		},
		Draw: func(alpha float64) {
			draws++
			now = now.Add(3 * time.Millisecond)
		},
	}
	dbg.WrapLoop(loop)

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		loop.Run(cx)
		return core.View{}
	})

	// Three 60 Hz display frames (~50 ms): each runs 1 update (4 ms) and 1 draw (3 ms).
	app.Advance(50 * time.Millisecond)
	if updates != 3 || draws != 3 {
		t.Fatalf("got %d updates and %d draws, want 3 and 3", updates, draws)
	}
	stats := dbg.Stats()
	if stats.Len() != 3 {
		t.Fatalf("recorded %d frames, want 3", stats.Len())
	}
	for i := range stats.Len() {
		sample := stats.At(i)
		if sample.Update != 4*time.Millisecond || sample.Draw != 3*time.Millisecond || sample.Total != 7*time.Millisecond {
			t.Fatalf("frame %d = %+v, want Update=4ms Draw=3ms Total=7ms", i, sample)
		}
	}

	// A hitch running 3 fixed updates in one frame accumulates all 3 update durations.
	app.FastForward(50 * time.Millisecond)
	latest := stats.Latest()
	if latest.Update != 12*time.Millisecond || latest.Draw != 3*time.Millisecond || latest.Total != 15*time.Millisecond {
		t.Fatalf("hitch frame = %+v, want Update=12ms Draw=3ms Total=15ms", latest)
	}
}

func TestFrameGraphDrawsStackedBarsAndReferenceLine(t *testing.T) {
	dbg := gamedebug.New(gamedebug.Options{Visible: true})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style: core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{
				dbg.View(cx),
			},
		}
	})

	app.Post(func() {
		for range 5 {
			dbg.RecordFrame(16667*time.Microsecond, 4*time.Millisecond, 3*time.Millisecond)
		}
	})
	app.Settle()

	readout := app.Find("debug-frame-stats").Text
	if !strings.Contains(readout, "60.0 fps") || !strings.Contains(readout, "upd 4.00") || !strings.Contains(readout, "drw 3.00") {
		t.Fatalf("frame readout = %q, want 60.0 fps with upd 4.00 and drw 3.00", readout)
	}

	ops := app.Find("debug-frame-graph").Ops
	var fillRects, strokeLines, refTexts int
	var sawRefLineAtMid bool
	for i, op := range ops {
		switch op.Kind {
		case core.DrawRect:
			if i+1 < len(ops) && ops[i+1].Kind == core.DrawFill {
				fillRects++
			}
		case core.DrawLineTo:
			strokeLines++
			// Canvas height is 64; 16.67 ms reference line on a 33.33 ms scale sits at y = 32.
			if math.Abs(float64(op.Y-32)) < 0.5 && math.Abs(float64(op.X-240)) < 0.5 {
				sawRefLineAtMid = true
			}
		case core.DrawFillText:
			if strings.Contains(op.Text, "16.7ms") {
				refTexts++
			}
		}
	}
	// Background + 5 frames * (idle + update + draw) = 16 filled rects.
	if fillRects < 11 {
		t.Fatalf("graph recorded %d filled rects, want at least background + stacked update/draw bars", fillRects)
	}
	if strokeLines == 0 || !sawRefLineAtMid {
		t.Fatalf("graph missing 16.67ms reference line at y=32 (lines=%d, mid=%v)", strokeLines, sawRefLineAtMid)
	}
	if refTexts == 0 {
		t.Fatal("graph missing 16.7ms reference label")
	}
}

func TestOverlayDrawStatsAndResetStats(t *testing.T) {
	dbg := gamedebug.New(gamedebug.Options{Visible: true})
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{
			Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
			Children: []core.Node{dbg.View(cx)},
		}
	})

	if dbg.HasDrawStats() || app.Exists("debug-draw-stats") {
		t.Fatal("draw stats should be absent before SetDrawStats")
	}

	want := gamedebug.DrawStats{DrawCalls: 7, Sprites: 42, Batches: 2, Triangles: 84, Vertices: 168}
	app.Post(func() {
		dbg.RecordFrame(16*time.Millisecond, 3*time.Millisecond, 2*time.Millisecond)
		dbg.SetDrawStats(want)
	})
	app.Settle()

	if !dbg.HasDrawStats() || dbg.DrawStats() != want {
		t.Fatalf("DrawStats() = %+v (has=%v), want %+v", dbg.DrawStats(), dbg.HasDrawStats(), want)
	}
	if !app.Exists("debug-draw-stats") {
		t.Fatal("debug-draw-stats section not mounted after SetDrawStats")
	}
	foundDraw := false
	for _, txt := range app.Texts() {
		if strings.Contains(txt, "7 calls") && strings.Contains(txt, "42 sprites") && strings.Contains(txt, "84 tris") {
			foundDraw = true
			break
		}
	}
	if !foundDraw {
		t.Fatalf("overlay texts %v missing draw stats summary", app.Texts())
	}

	app.Post(dbg.ResetStats)
	app.Settle()
	if dbg.Stats().Len() != 0 || dbg.HasDrawStats() || dbg.DrawStats() != (gamedebug.DrawStats{}) {
		t.Fatalf("ResetStats did not clear frame/draw stats: len=%d has=%v ds=%+v", dbg.Stats().Len(), dbg.HasDrawStats(), dbg.DrawStats())
	}
	if app.Exists("debug-draw-stats") {
		t.Fatal("debug-draw-stats still mounted after ResetStats")
	}
}

func TestShapesAlignAcrossCameraZoomsAndRotations(t *testing.T) {
	green := core.RGB(0x22C55E)
	cyan := core.RGB(0x38BDF8)
	amber := core.RGB(0xF59E0B)

	hero := sprite.NewSprite(testSheet.Frames["hero"])
	hero.X, hero.Y = 50, 50
	hero.OriginX, hero.OriginY = 8, 8
	hero.ScaleX, hero.ScaleY = 2, 2

	rotated := sprite.NewSprite(testSheet.Frames["hero"])
	rotated.X, rotated.Y = 100, 100
	rotated.OriginX, rotated.OriginY = 8, 8
	rotated.Rotation = math.Pi / 2

	trimmed := sprite.NewSprite(testSheet.Frames["coin"])
	trimmed.X, trimmed.Y = 200, 10
	trimmed.FlipX = true

	hidden := sprite.NewSprite(testSheet.Frames["hero"])
	hidden.Alpha = 0

	batch := &sprite.Batch{
		Atlas:   testSheet,
		Sprites: []*sprite.Sprite{&hero, &rotated, &trimmed, &hidden},
	}

	zooms := []float32{0.5, 1.0, 3.0}
	for _, zoom := range zooms {
		t.Run(zoomName(zoom), func(t *testing.T) {
			dbg := gamedebug.New(gamedebug.Options{Visible: true, LineWidth: 1.5})
			box := core.Bounds{Origin: core.Point{X: 12, Y: 18}, Size: core.Size{Width: 24, Height: 32}}

			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return core.View{
					Children: []core.Node{
						core.Canvas{
							TestID: "world",
							Style:  core.Style{Width: core.Pt(400), Height: core.Pt(300)},
							Draw: func(d *core.Drawing, _ core.Size) {
								// Active camera transform: translate + uniform zoom + rotation.
								d.Translate(100, 80).Scale(zoom, zoom).Rotate(0.25)
								dbg.DrawBox(d, box, green)
								dbg.DrawCircle(d, 40, 60, 15, cyan)
								dbg.DrawBatchBounds(d, batch, amber)
							},
						},
					},
				}
			})

			ops := app.Find("world").Ops
			wantLW := float32(1.5) / zoom

			var lineWidths []float32
			var rects []core.DrawOp
			var arcs []core.DrawOp
			var moveTos, lineTos int
			var saves, restores int
			for _, op := range ops {
				switch op.Kind {
				case core.DrawSave:
					saves++
				case core.DrawRestore:
					restores++
				case core.DrawLineWidth:
					lineWidths = append(lineWidths, op.W)
				case core.DrawRect:
					rects = append(rects, op)
				case core.DrawArc:
					arcs = append(arcs, op)
				case core.DrawMoveTo:
					moveTos++
				case core.DrawLineTo:
					lineTos++
				}
			}

			if saves != restores || saves == 0 {
				t.Fatalf("unbalanced save/restore: saves=%d restores=%d", saves, restores)
			}
			// 1 box + 1 circle + 3 visible sprites (hidden skipped) = 5 shapes.
			if len(lineWidths) != 5 {
				t.Fatalf("got %d line width ops, want 5", len(lineWidths))
			}
			for i, lw := range lineWidths {
				if math.Abs(float64(lw-wantLW)) > 1e-4 {
					t.Fatalf("shape %d line width = %v at zoom %v, want %v", i, lw, zoom, wantLW)
				}
			}

			// Rects: 1 DrawBox + hero (scaled 2x about center -> [34,34,32,32]) + trimmed coin (untrimmed 16x16 at [200,10]).
			if len(rects) != 3 {
				t.Fatalf("got %d rect ops, want 3: %+v", len(rects), rects)
			}
			assertRect(t, "box", rects[0], 12, 18, 24, 32)
			assertRect(t, "hero", rects[1], 34, 34, 32, 32)
			assertRect(t, "coin", rects[2], 200, 10, 16, 16)

			// Circle: center (40, 60), radius 15.
			if len(arcs) != 1 {
				t.Fatalf("got %d arc ops, want 1", len(arcs))
			}
			if !near32(arcs[0].X, 40) || !near32(arcs[0].Y, 60) || !near32(arcs[0].R, 15) {
				t.Fatalf("arc = %+v, want (40, 60, r=15)", arcs[0])
			}

			// Rotated sprite: 1 MoveTo + 3 LineTo forming the 4-corner oriented quad.
			if moveTos != 1 || lineTos != 3 {
				t.Fatalf("rotated sprite path had %d MoveTo and %d LineTo, want 1 and 3", moveTos, lineTos)
			}
		})
	}
}

func TestSceneInspectorPushAndReactiveLiveCounts(t *testing.T) {
	t.Run("SetScenes", func(t *testing.T) {
		dbg := gamedebug.New(gamedebug.Options{Visible: true})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.View{
				Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Children: []core.Node{dbg.View(cx)},
			}
		})

		if !slices.Contains(app.Texts(), "Scenes: none") {
			t.Fatalf("initial texts = %v, want Scenes: none", app.Texts())
		}

		app.Post(func() {
			dbg.SetScenes([]gamedebug.SceneSnapshot{
				{
					Name: "world",
					Layers: []gamedebug.LayerSnapshot{
						{Name: "terrain", Space: "world", Objects: 12},
						{Name: "actors", Space: "world", Objects: 5},
						{Name: "hud", Space: "screen", Objects: 3},
					},
				},
			})
		})
		app.Settle()

		texts := app.Texts()
		for _, want := range []string{"Scenes (1)", "world", "terrain (world)", "12", "actors (world)", "5", "hud (screen)", "3"} {
			if !slices.Contains(texts, want) {
				t.Fatalf("texts %v missing %q", texts, want)
			}
		}

		// Simulate removing objects and adding an overlay scene.
		app.Post(func() {
			dbg.SetScenes([]gamedebug.SceneSnapshot{
				{
					Name: "world",
					Layers: []gamedebug.LayerSnapshot{
						{Name: "actors", Space: "world", Objects: 2},
					},
				},
				{
					Name: "pause",
					Layers: []gamedebug.LayerSnapshot{
						{Name: "menu", Space: "screen", Objects: 4},
					},
				},
			})
		})
		app.Settle()

		texts = app.Texts()
		for _, want := range []string{"Scenes (2)", "world", "actors (world)", "2", "pause", "menu (screen)", "4"} {
			if !slices.Contains(texts, want) {
				t.Fatalf("updated texts %v missing %q", texts, want)
			}
		}
		if slices.Contains(texts, "terrain (world)") {
			t.Fatalf("stale layer terrain (world) still present in %v", texts)
		}
	})

	t.Run("ReactiveSignalProvider", func(t *testing.T) {
		var actorsCount *core.Signal[int]
		var dbg *gamedebug.Overlay
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			actorsCount = core.NewSignal(cx, 7)
			dbg = gamedebug.New(gamedebug.Options{
				Visible: true,
				Scenes: func() []gamedebug.SceneSnapshot {
					return []gamedebug.SceneSnapshot{
						{
							Name: "arena",
							Layers: []gamedebug.LayerSnapshot{
								{Name: "entities", Space: "world", Objects: actorsCount.Get()},
							},
						},
					}
				},
			})
			return core.View{
				Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Children: []core.Node{dbg.View(cx)},
			}
		})

		if !slices.Contains(app.Texts(), "7") || !slices.Contains(app.Texts(), "entities (world)") {
			t.Fatalf("initial reactive texts = %v, want arena/entities/7", app.Texts())
		}

		app.Post(func() { actorsCount.Set(19) })
		app.Settle()
		if !slices.Contains(app.Texts(), "19") {
			t.Fatalf("after adding objects texts = %v, want 19", app.Texts())
		}

		app.Post(func() { actorsCount.Set(1) })
		app.Settle()
		if !slices.Contains(app.Texts(), "1") || slices.Contains(app.Texts(), "19") {
			t.Fatalf("after removing objects texts = %v, want 1", app.Texts())
		}
	})

	t.Run("PlainStateProviderUpdatesOnFrame", func(t *testing.T) {
		bullets := 3
		dbg := gamedebug.New(gamedebug.Options{
			Visible: true,
			Scenes: func() []gamedebug.SceneSnapshot {
				return []gamedebug.SceneSnapshot{
					{
						Name: "shooter",
						Layers: []gamedebug.LayerSnapshot{
							{Name: "bullets", Space: "world", Objects: bullets},
						},
					},
				}
			},
		})
		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.View{
				Style:    core.Style{Width: core.Pt(400), Height: core.Pt(300)},
				Children: []core.Node{dbg.View(cx)},
			}
		})

		if !slices.Contains(app.Texts(), "3") {
			t.Fatalf("initial texts = %v, want 3", app.Texts())
		}

		app.Post(func() {
			bullets = 42
			dbg.RecordFrame(16*time.Millisecond, 2*time.Millisecond, 2*time.Millisecond)
		})
		app.Settle()
		if !slices.Contains(app.Texts(), "42") {
			t.Fatalf("after frame texts = %v, want 42", app.Texts())
		}
	})
}

func zoomName(z float32) string {
	switch z {
	case 0.5:
		return "0.5x"
	case 1.0:
		return "1x"
	case 3.0:
		return "3x"
	default:
		return "zoom"
	}
}

func near32(a, b float32) bool {
	return math.Abs(float64(a-b)) < 1e-4
}

func assertRect(t *testing.T, label string, op core.DrawOp, x, y, w, h float32) {
	t.Helper()
	if !near32(op.X, x) || !near32(op.Y, y) || !near32(op.W, w) || !near32(op.H, h) {
		t.Fatalf("%s rect = (%v, %v, %v, %v), want (%v, %v, %v, %v)", label, op.X, op.Y, op.W, op.H, x, y, w, h)
	}
}
