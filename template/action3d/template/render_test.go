package main

import (
	"image"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

// frameStats summarizes a frame the GPU drew.
type frameStats struct {
	distinct, blue, notSky int
}

func analyze(img *image.RGBA) frameStats {
	sr, sg, sb, _ := skyColor.Components()
	sky := [3]int{int(sr * 255), int(sg * 255), int(sb * 255)}
	colors := map[[3]uint8]bool{}
	var s frameStats
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			colors[[3]uint8{c.R, c.G, c.B}] = true
			if abs(int(c.R)-sky[0])+abs(int(c.G)-sky[1])+abs(int(c.B)-sky[2]) > 24 {
				s.notSky++
			}
			// The hero's blue tunic, far bluer than the slate sky.
			if int(c.B) > int(c.R)+70 && int(c.B) > int(c.G)+25 {
				s.blue++
			}
		}
	}
	s.distinct = len(colors)
	return s
}

func abs(v int) int { return max(v, -v) }

func startRendered(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *play) {
	t.Helper()
	var p *play
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		var node core.Node
		p, node = newPlay(cx)
		return node
	}, append([]nsgotest.Option{nsgotest.Size(96, 60), nsgotest.ManualFrames()}, opts...)...)
	app.Advance(time.Millisecond)
	if !p.render.ready() || p.render.frames == 0 {
		t.Fatalf("the renderer is not drawing: ready %v, %d frames, status %q", p.render.ready(), p.render.frames, p.gpuStatus.Get())
	}
	return app, p
}

func checkFrame(t *testing.T, app *nsgotest.App) frameStats {
	t.Helper()
	img, err := app.GPUReadPixels("id:game-gpu")
	if err != nil {
		t.Fatalf("GPUReadPixels: %v", err)
	}
	s := analyze(img)
	total := img.Bounds().Dx() * img.Bounds().Dy()
	if s.notSky < total/3 || s.distinct < 40 || s.blue == 0 {
		t.Fatalf("the frame shows too little of the level: %d of %d pixels lit, %d colors, %d of the hero's", s.notSky, total, s.distinct, s.blue)
	}
	return s
}

// TestTheLevelDrawsOnFullAndWebGL2GPUs draws the opening view, the hero in front of the courtyard, with render3d's lit PBR pipeline on a full GPU and on a WebGL 2 one without compute.
func TestTheLevelDrawsOnFullAndWebGL2GPUs(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []nsgotest.Option
	}{{"full", nil}, {"webgl2", []nsgotest.Option{nsgotest.NoGPUCompute()}}} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := startRendered(t, tc.opts...)
			checkFrame(t, app)
			if app.Exists("id:gpu-status") {
				t.Fatalf("the HUD reports a GPU problem: %s", app.Find("id:gpu-status").Text)
			}
		})
	}
}

func TestTheViewSurvivesALostDevice(t *testing.T) {
	app, p := startRendered(t)
	checkFrame(t, app)
	app.TriggerGPUDeviceLost("id:game-gpu", nsgotest.GPULostContextLost, "test")
	app.Advance(time.Millisecond)
	app.RestoreGPUDevice("id:game-gpu")
	frames := p.render.frames
	app.Advance(time.Millisecond)
	if p.render.frames == frames {
		t.Fatal("no frames drawn after the device came back")
	}
	checkFrame(t, app)
}

func BenchmarkScene(b *testing.B) {
	g := newTestGame(b)
	var items []drawItem
	for b.Loop() {
		items = g.scene(0.5, items[:0])
	}
}
