package screen_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/nsgo-dev/game/packages/game/screen"
	"github.com/nsgo-dev/game/packages/game/sprite"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

var (
	win1280 = core.Size{Width: 1280, Height: 720}
	win1366 = core.Size{Width: 1366, Height: 768}
	win2532 = core.Size{Width: 2532, Height: 1170}
	winTall = core.Size{Width: 640, Height: 720}
)

func TestLayoutAndDrawAllModesAcrossWindows(t *testing.T) {
	black := core.RGB(0x000000)
	red := core.RGB(0xFF0000)

	type tc struct {
		name        string
		vp          screen.Viewport
		win         core.Size
		wantRect    core.Bounds
		wantVirtual core.Size
		wantSX      float32
		wantSY      float32
		wantBars    []core.Bounds
		hand        func(d *core.Drawing)
	}

	s1366Fit := float32(768.0 / 180.0)
	w1366Fit := float32(320.0 * 768.0 / 180.0)
	ox1366Fit := (1366 - w1366Fit) / 2
	s1366Fill := float32(1366.0 / 320.0)
	h1366Fill := float32(180.0 * 1366.0 / 320.0)
	oy1366Fill := (768 - h1366Fill) / 2
	expW1366 := float32(1366.0 / (768.0 / 180.0))
	expW2532 := float32(2532.0 / 6.5)

	cases := []tc{
		// ModeFit
		{
			name:        "fit/1280x720",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit},
			win:         win1280,
			wantRect:    core.Bounds{Size: win1280},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().Scale(4, 4).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "fit/1366x768",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit},
			win:         win1366,
			wantRect:    core.Bounds{Origin: core.Point{X: core.Points(ox1366Fit), Y: 0}, Size: core.Size{Width: core.Points(w1366Fit), Height: 768}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      s1366Fit, wantSY: s1366Fit,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: core.Points(ox1366Fit), Height: 768}},
				{Origin: core.Point{X: core.Points(ox1366Fit + w1366Fit), Y: 0}, Size: core.Size{Width: core.Points(ox1366Fit), Height: 768}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, ox1366Fit, 768).
					FillRect(ox1366Fit+w1366Fit, 0, ox1366Fit, 768).
					Restore()
				d.Save().
					BeginPath().Rect(ox1366Fit, 0, w1366Fit, 768).Clip().BeginPath().
					Translate(ox1366Fit, 0).Scale(s1366Fit, s1366Fit).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "fit/2532x1170",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit},
			win:         win2532,
			wantRect:    core.Bounds{Origin: core.Point{X: 226, Y: 0}, Size: core.Size{Width: 2080, Height: 1170}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      6.5, wantSY: 6.5,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 226, Height: 1170}},
				{Origin: core.Point{X: 2306, Y: 0}, Size: core.Size{Width: 226, Height: 1170}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, 226, 1170).
					FillRect(2306, 0, 226, 1170).
					Restore()
				d.Save().
					BeginPath().Rect(226, 0, 2080, 1170).Clip().BeginPath().
					Translate(226, 0).Scale(6.5, 6.5).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "fit/640x720_letterbox",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit},
			win:         winTall,
			wantRect:    core.Bounds{Origin: core.Point{X: 0, Y: 180}, Size: core.Size{Width: 640, Height: 360}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      2, wantSY: 2,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 640, Height: 180}},
				{Origin: core.Point{X: 0, Y: 540}, Size: core.Size{Width: 640, Height: 180}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, 640, 180).
					FillRect(0, 540, 640, 180).
					Restore()
				d.Save().
					BeginPath().Rect(0, 180, 640, 360).Clip().BeginPath().
					Translate(0, 180).Scale(2, 2).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},

		// ModeFill
		{
			name:        "fill/1280x720",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill},
			win:         win1280,
			wantRect:    core.Bounds{Size: win1280},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().Scale(4, 4).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "fill/1366x768",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill},
			win:         win1366,
			wantRect:    core.Bounds{Origin: core.Point{X: 0, Y: core.Points(oy1366Fill)}, Size: core.Size{Width: 1366, Height: core.Points(h1366Fill)}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      s1366Fill, wantSY: s1366Fill,
			hand: func(d *core.Drawing) {
				d.Save().
					BeginPath().Rect(0, 0, 1366, 768).Clip().BeginPath().
					Translate(0, oy1366Fill).Scale(s1366Fill, s1366Fill).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "fill/2532x1170",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill},
			win:         win2532,
			wantRect:    core.Bounds{Origin: core.Point{X: 0, Y: -127.125}, Size: core.Size{Width: 2532, Height: 1424.25}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      7.9125, wantSY: 7.9125,
			hand: func(d *core.Drawing) {
				d.Save().
					BeginPath().Rect(0, 0, 2532, 1170).Clip().BeginPath().
					Translate(0, -127.125).Scale(7.9125, 7.9125).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "fill/640x720_crops_sides",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill},
			win:         winTall,
			wantRect:    core.Bounds{Origin: core.Point{X: -320, Y: 0}, Size: core.Size{Width: 1280, Height: 720}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().
					BeginPath().Rect(0, 0, 640, 720).Clip().BeginPath().
					Translate(-320, 0).Scale(4, 4).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},

		// ModeStretch
		{
			name:        "stretch/1280x720",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeStretch},
			win:         win1280,
			wantRect:    core.Bounds{Size: win1280},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().Scale(4, 4).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "stretch/1366x768",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeStretch},
			win:         win1366,
			wantRect:    core.Bounds{Size: win1366},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      s1366Fill, wantSY: s1366Fit,
			hand: func(d *core.Drawing) {
				d.Save().Scale(s1366Fill, s1366Fit).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "stretch/2532x1170",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeStretch},
			win:         win2532,
			wantRect:    core.Bounds{Size: win2532},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      7.9125, wantSY: 6.5,
			hand: func(d *core.Drawing) {
				d.Save().Scale(7.9125, 6.5).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},

		// ModeExpand
		{
			name:        "expand/1280x720",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand},
			win:         win1280,
			wantRect:    core.Bounds{Size: win1280},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().Scale(4, 4).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "expand/1366x768",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand},
			win:         win1366,
			wantRect:    core.Bounds{Size: win1366},
			wantVirtual: core.Size{Width: core.Points(expW1366), Height: 180},
			wantSX:      s1366Fit, wantSY: s1366Fit,
			hand: func(d *core.Drawing) {
				d.Save().Scale(s1366Fit, s1366Fit).SetFillColor(red).FillRect(0, 0, expW1366, 180).Restore()
			},
		},
		{
			name:        "expand/2532x1170",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand},
			win:         win2532,
			wantRect:    core.Bounds{Size: win2532},
			wantVirtual: core.Size{Width: core.Points(expW2532), Height: 180},
			wantSX:      6.5, wantSY: 6.5,
			hand: func(d *core.Drawing) {
				d.Save().Scale(6.5, 6.5).SetFillColor(red).FillRect(0, 0, expW2532, 180).Restore()
			},
		},
		{
			name:        "expand/640x720_expands_height",
			vp:          screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand},
			win:         winTall,
			wantRect:    core.Bounds{Size: winTall},
			wantVirtual: core.Size{Width: 320, Height: 360},
			wantSX:      2, wantSY: 2,
			hand: func(d *core.Drawing) {
				d.Save().Scale(2, 2).SetFillColor(red).FillRect(0, 0, 320, 360).Restore()
			},
		},

		// IntegerScale
		{
			name:        "integer/1280x720",
			vp:          screen.Viewport{Width: 320, Height: 180, IntegerScale: true},
			win:         win1280,
			wantRect:    core.Bounds{Size: win1280},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			hand: func(d *core.Drawing) {
				d.Save().Scale(4, 4).SetFillColor(red).FillRect(0, 0, 320, 180).Restore()
			},
		},
		{
			name:        "integer/1366x768",
			vp:          screen.Viewport{Width: 320, Height: 180, IntegerScale: true},
			win:         win1366,
			wantRect:    core.Bounds{Origin: core.Point{X: 43, Y: 24}, Size: core.Size{Width: 1280, Height: 720}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      4, wantSY: 4,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 1366, Height: 24}},
				{Origin: core.Point{X: 0, Y: 744}, Size: core.Size{Width: 1366, Height: 24}},
				{Origin: core.Point{X: 0, Y: 24}, Size: core.Size{Width: 43, Height: 720}},
				{Origin: core.Point{X: 1323, Y: 24}, Size: core.Size{Width: 43, Height: 720}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, 1366, 24).
					FillRect(0, 744, 1366, 24).
					FillRect(0, 24, 43, 720).
					FillRect(1323, 24, 43, 720).
					Restore()
				d.Save().
					BeginPath().Rect(43, 24, 1280, 720).Clip().BeginPath().
					Translate(43, 24).Scale(4, 4).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "integer/2532x1170",
			vp:          screen.Viewport{Width: 320, Height: 180, IntegerScale: true},
			win:         win2532,
			wantRect:    core.Bounds{Origin: core.Point{X: 306, Y: 45}, Size: core.Size{Width: 1920, Height: 1080}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      6, wantSY: 6,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 2532, Height: 45}},
				{Origin: core.Point{X: 0, Y: 1125}, Size: core.Size{Width: 2532, Height: 45}},
				{Origin: core.Point{X: 0, Y: 45}, Size: core.Size{Width: 306, Height: 1080}},
				{Origin: core.Point{X: 2226, Y: 45}, Size: core.Size{Width: 306, Height: 1080}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, 2532, 45).
					FillRect(0, 1125, 2532, 45).
					FillRect(0, 45, 306, 1080).
					FillRect(2226, 45, 306, 1080).
					Restore()
				d.Save().
					BeginPath().Rect(306, 45, 1920, 1080).Clip().BeginPath().
					Translate(306, 45).Scale(6, 6).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
		{
			name:        "integer/200x90_fractional_fallback",
			vp:          screen.Viewport{Width: 320, Height: 180, IntegerScale: true},
			win:         core.Size{Width: 200, Height: 90},
			wantRect:    core.Bounds{Origin: core.Point{X: 20, Y: 0}, Size: core.Size{Width: 160, Height: 90}},
			wantVirtual: core.Size{Width: 320, Height: 180},
			wantSX:      0.5, wantSY: 0.5,
			wantBars: []core.Bounds{
				{Origin: core.Point{X: 0, Y: 0}, Size: core.Size{Width: 20, Height: 90}},
				{Origin: core.Point{X: 180, Y: 0}, Size: core.Size{Width: 20, Height: 90}},
			},
			hand: func(d *core.Drawing) {
				d.Save().SetFillColor(black).
					FillRect(0, 0, 20, 90).
					FillRect(180, 0, 20, 90).
					Restore()
				d.Save().
					BeginPath().Rect(20, 0, 160, 90).Clip().BeginPath().
					Translate(20, 0).Scale(0.5, 0.5).
					SetFillColor(red).FillRect(0, 0, 320, 180).
					Restore()
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pl := c.vp.Layout(c.win)
			assertBoundsNear(t, "Rect", pl.Rect, c.wantRect)
			assertSizeNear(t, "VirtualSize", pl.VirtualSize, c.wantVirtual)
			assertNear(t, "ScaleX", pl.ScaleX, c.wantSX)
			assertNear(t, "ScaleY", pl.ScaleY, c.wantSY)
			if len(pl.Bars) != len(c.wantBars) {
				t.Fatalf("got %d bars %+v, want %d %+v", len(pl.Bars), pl.Bars, len(c.wantBars), c.wantBars)
			}
			for i := range c.wantBars {
				assertBoundsNear(t, "Bar", pl.Bars[i], c.wantBars[i])
			}

			var gotSize core.Size
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				return core.View{Children: []core.Node{
					core.Canvas{
						TestID: "vp",
						Style:  core.Style{Width: core.Pt(c.win.Width), Height: core.Pt(c.win.Height)},
						Draw: func(d *core.Drawing, size core.Size) {
							c.vp.Draw(d, size, func(d *core.Drawing, vSize core.Size) {
								gotSize = vSize
								d.SetFillColor(red).FillRect(0, 0, float32(vSize.Width), float32(vSize.Height))
							})
						},
					},
					core.Canvas{
						TestID: "hand",
						Style:  core.Style{Width: core.Pt(c.win.Width), Height: core.Pt(c.win.Height)},
						Draw: func(d *core.Drawing, _ core.Size) {
							c.hand(d)
						},
					},
				}}
			})
			assertSizeNear(t, "callback size", gotSize, c.wantVirtual)
			sameOps(t, app.FindMounted("vp").Ops, app.FindMounted("hand").Ops)
		})
	}
}

func TestBarColorCustomAndTransparent(t *testing.T) {
	slate := core.RGB(0x1A1B26)
	green := core.RGB(0x00FF00)

	custom := screen.Viewport{Width: 320, Height: 180, BarColor: slate}
	noBars := screen.Viewport{Width: 320, Height: 180, BarColor: core.Transparent}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Children: []core.Node{
			core.Canvas{
				TestID: "custom",
				Style:  core.Style{Width: core.Pt(win2532.Width), Height: core.Pt(win2532.Height)},
				Draw: func(d *core.Drawing, size core.Size) {
					custom.Draw(d, size, func(d *core.Drawing, s core.Size) {
						d.SetFillColor(green).FillRect(0, 0, float32(s.Width), float32(s.Height))
					})
				},
			},
			core.Canvas{
				TestID: "custom_hand",
				Style:  core.Style{Width: core.Pt(win2532.Width), Height: core.Pt(win2532.Height)},
				Draw: func(d *core.Drawing, _ core.Size) {
					d.Save().SetFillColor(slate).
						FillRect(0, 0, 226, 1170).
						FillRect(2306, 0, 226, 1170).
						Restore()
					d.Save().
						BeginPath().Rect(226, 0, 2080, 1170).Clip().BeginPath().
						Translate(226, 0).Scale(6.5, 6.5).
						SetFillColor(green).FillRect(0, 0, 320, 180).
						Restore()
				},
			},
			core.Canvas{
				TestID: "transparent",
				Style:  core.Style{Width: core.Pt(win2532.Width), Height: core.Pt(win2532.Height)},
				Draw: func(d *core.Drawing, size core.Size) {
					noBars.Draw(d, size, func(d *core.Drawing, s core.Size) {
						d.SetFillColor(green).FillRect(0, 0, float32(s.Width), float32(s.Height))
					})
				},
			},
			core.Canvas{
				TestID: "transparent_hand",
				Style:  core.Style{Width: core.Pt(win2532.Width), Height: core.Pt(win2532.Height)},
				Draw: func(d *core.Drawing, _ core.Size) {
					// Still clipped to the game rect, with no bar fills recorded.
					d.Save().
						BeginPath().Rect(226, 0, 2080, 1170).Clip().BeginPath().
						Translate(226, 0).Scale(6.5, 6.5).
						SetFillColor(green).FillRect(0, 0, 320, 180).
						Restore()
				},
			},
		}}
	})
	sameOps(t, app.FindMounted("custom").Ops, app.FindMounted("custom_hand").Ops)
	sameOps(t, app.FindMounted("transparent").Ops, app.FindMounted("transparent_hand").Ops)
}

func TestViewToGameAndGameToViewRoundTrip(t *testing.T) {
	viewports := []struct {
		name string
		vp   screen.Viewport
	}{
		{"fit", screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}},
		{"fill", screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill}},
		{"stretch", screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeStretch}},
		{"expand", screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand}},
		{"integer", screen.Viewport{Width: 320, Height: 180, IntegerScale: true}},
	}
	windows := []core.Size{win1280, win1366, win2532, winTall}

	for _, vc := range viewports {
		for _, win := range windows {
			pl := vc.vp.Layout(win)
			// Sample interior points inside the visible intersection of window and game rect.
			visX0 := max(core.Points(0), pl.Rect.Origin.X)
			visY0 := max(core.Points(0), pl.Rect.Origin.Y)
			visX1 := min(win.Width, pl.Rect.Origin.X+pl.Rect.Size.Width)
			visY1 := min(win.Height, pl.Rect.Origin.Y+pl.Rect.Size.Height)
			viewSamples := []core.Point{
				{X: visX0 + (visX1-visX0)*0.5, Y: visY0 + (visY1-visY0)*0.5},
				{X: visX0 + (visX1-visX0)*0.25, Y: visY0 + (visY1-visY0)*0.75},
				{X: visX0 + (visX1-visX0)*0.8, Y: visY0 + (visY1-visY0)*0.2},
			}
			for _, v := range viewSamples {
				g, ok := vc.vp.ViewToGame(win, v)
				if !ok {
					t.Fatalf("%s in %v: visible view point %v rejected by ViewToGame", vc.name, win, v)
				}
				back := vc.vp.GameToView(win, g)
				assertNear(t, "roundtrip X", float32(back.X), float32(v.X))
				assertNear(t, "roundtrip Y", float32(back.Y), float32(v.Y))
			}

			// Every bar's center must return ok == false.
			for _, bar := range pl.Bars {
				center := core.Point{
					X: bar.Origin.X + bar.Size.Width/2,
					Y: bar.Origin.Y + bar.Size.Height/2,
				}
				if got, ok := vc.vp.ViewToGame(win, center); ok {
					t.Fatalf("%s in %v: bar point %v mapped to %v, want ok == false", vc.name, win, center, got)
				}
			}

			// Points outside the window must return ok == false.
			outside := []core.Point{
				{X: -1, Y: 10},
				{X: 10, Y: -1},
				{X: win.Width, Y: 10},
				{X: 10, Y: win.Height},
			}
			for _, p := range outside {
				if got, ok := vc.vp.ViewToGame(win, p); ok {
					t.Fatalf("%s in %v: outside point %v mapped to %v, want ok == false", vc.name, win, p, got)
				}
			}
		}
	}

	// Verify exact boundary between left/right pillarboxes and game rect in ModeFit on 2532x1170.
	fit := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}
	if pt, ok := fit.ViewToGame(win2532, core.Point{X: 226, Y: 0}); !ok || pt.X != 0 || pt.Y != 0 {
		t.Fatalf("left edge of game rect = (%v, %v), want ((0, 0), true)", pt, ok)
	}
	if _, ok := fit.ViewToGame(win2532, core.Point{X: 225.5, Y: 500}); ok {
		t.Fatal("press on left pillarbox returned ok == true")
	}
	if _, ok := fit.ViewToGame(win2532, core.Point{X: 2306, Y: 500}); ok {
		t.Fatal("press on right pillarbox returned ok == true")
	}
}

func TestCanvasPressesMapThroughViewToGame(t *testing.T) {
	vp := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}
	var hits []core.Point
	var barHits int

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "game",
			Style:  core.Style{Width: core.Pt(win2532.Width), Height: core.Pt(win2532.Height)},
			OnPress: func(ev core.PressEvent) {
				if pt, ok := vp.ViewToGame(win2532, ev.Position); ok {
					hits = append(hits, pt)
				} else {
					barHits++
				}
			},
			Draw: func(d *core.Drawing, size core.Size) {
				vp.Draw(d, size, func(d *core.Drawing, s core.Size) {
					d.FillRect(0, 0, float32(s.Width), float32(s.Height))
				})
			},
		}
	})

	// Press on left pillarbox bar (ignored).
	app.PressAt("game", core.Point{X: 100, Y: 585})
	// Press on exact center of the window -> virtual center (160, 90).
	app.PressAt("game", core.Point{X: 1266, Y: 585})
	// Press on right pillarbox bar (ignored).
	app.PressAt("game", core.Point{X: 2400, Y: 585})

	if barHits != 2 {
		t.Fatalf("got %d bar presses, want 2", barHits)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d game hits %+v, want 1", len(hits), hits)
	}
	assertNear(t, "hit X", float32(hits[0].X), 160)
	assertNear(t, "hit Y", float32(hits[0].Y), 90)
}

func TestIntegerScaleAndPixelSnapAcrossScales(t *testing.T) {
	sheet := &sprite.Atlas{
		Image: core.ImageSource{URI: "asset:pixel.png"},
		Frames: map[string]sprite.Frame{
			"hero": {Name: "hero", X: 0, Y: 0, W: 16, H: 16, Width: 16, Height: 16},
		},
	}

	// Verify package-level Snap / SnapPoint and Viewport.Snap / SnapPoint.
	if x, y := screen.Snap(10.4, 20.6); x != 10 || y != 21 {
		t.Fatalf("Snap(10.4, 20.6) = (%v, %v), want (10, 21)", x, y)
	}
	if x, y := screen.Snap(-0.4, -1.6); x != 0 || math.Signbit(float64(x)) || y != -2 {
		t.Fatalf("Snap(-0.4, -1.6) = (%v, %v), want (0, -2)", x, y)
	}
	if pt := screen.SnapPoint(core.Point{X: 3.7, Y: 8.2}); pt != (core.Point{X: 4, Y: 8}) {
		t.Fatalf("SnapPoint = %v, want {4 8}", pt)
	}

	unsnapped := screen.Viewport{Width: 320, Height: 180, PixelSnap: false}
	if x, y := unsnapped.Snap(10.4, 20.6); x != 10.4 || y != 20.6 {
		t.Fatalf("unsnapped.Snap = (%v, %v), want unchanged", x, y)
	}
	if pt := unsnapped.SnapPoint(core.Point{X: 3.7, Y: 8.2}); pt != (core.Point{X: 3.7, Y: 8.2}) {
		t.Fatalf("unsnapped.SnapPoint = %v, want unchanged", pt)
	}

	// Verify viewport origin snaps to whole points on odd-sized windows.
	oddWin := core.Size{Width: 321, Height: 181}
	snappedVP := screen.Viewport{Width: 320, Height: 180, IntegerScale: true, PixelSnap: true}
	plOdd := snappedVP.Layout(oddWin)
	if plOdd.Rect.Origin.X != 1 || plOdd.Rect.Origin.Y != 1 {
		t.Fatalf("snapped odd origin = %v, want {1 1}", plOdd.Rect.Origin)
	}

	// Verify pixel-art sprites draw at whole virtual pixels across 1x, 2x, 3x, 4x scales.
	scales := []struct {
		scale int
		win   core.Size
	}{
		{1, core.Size{Width: 340, Height: 200}},
		{2, core.Size{Width: 680, Height: 400}},
		{3, core.Size{Width: 1000, Height: 600}},
		{4, win1366},
	}

	for _, sc := range scales {
		vp := screen.Viewport{Width: 320, Height: 180, IntegerScale: true, PixelSnap: true}
		pl := vp.Layout(sc.win)
		if pl.ScaleX != float32(sc.scale) || pl.ScaleY != float32(sc.scale) {
			t.Fatalf("scale %dx in %v: got (%v, %v)", sc.scale, sc.win, pl.ScaleX, pl.ScaleY)
		}
		if float64(pl.Rect.Origin.X) != math.Round(float64(pl.Rect.Origin.X)) ||
			float64(pl.Rect.Origin.Y) != math.Round(float64(pl.Rect.Origin.Y)) {
			t.Fatalf("scale %dx origin %v not snapped to whole points", sc.scale, pl.Rect.Origin)
		}

		// Sub-pixel sprite position snaps to whole virtual pixel (12, 25).
		sx, sy := vp.Snap(12.37, 24.68)
		s := sprite.NewSprite(sheet.Frames["hero"])
		s.X, s.Y = sx, sy
		batch := &sprite.Batch{Atlas: sheet, Sprites: []*sprite.Sprite{&s}}

		app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
			return core.View{Children: []core.Node{
				core.Canvas{
					TestID: "snapped",
					Style:  core.Style{Width: core.Pt(sc.win.Width), Height: core.Pt(sc.win.Height)},
					Draw: func(d *core.Drawing, size core.Size) {
						vp.Draw(d, size, func(d *core.Drawing, _ core.Size) {
							batch.Draw(d)
						})
					},
				},
				core.Canvas{
					TestID: "hand",
					Style:  core.Style{Width: core.Pt(sc.win.Width), Height: core.Pt(sc.win.Height)},
					Draw: func(d *core.Drawing, _ core.Size) {
						if len(pl.Bars) > 0 {
							d.Save().SetFillColor(core.RGB(0x000000))
							for _, b := range pl.Bars {
								d.FillRect(float32(b.Origin.X), float32(b.Origin.Y), float32(b.Size.Width), float32(b.Size.Height))
							}
							d.Restore()
						}
						d.Save()
						if len(pl.Bars) > 0 {
							d.BeginPath().Rect(float32(pl.Rect.Origin.X), float32(pl.Rect.Origin.Y), float32(pl.Rect.Size.Width), float32(pl.Rect.Size.Height)).Clip().BeginPath()
						}
						if pl.Rect.Origin.X != 0 || pl.Rect.Origin.Y != 0 {
							d.Translate(float32(pl.Rect.Origin.X), float32(pl.Rect.Origin.Y))
						}
						if pl.ScaleX != 1 || pl.ScaleY != 1 {
							d.Scale(pl.ScaleX, pl.ScaleY)
						}
						d.DrawImageFrom(sheet.Image, 0, 0, 16, 16, 12, 25, 16, 16)
						d.Restore()
					},
				},
			}}
		})
		sameOps(t, app.Find("snapped").Ops, app.Find("hand").Ops)
	}
}

func TestSafeAreaKeepsHUDClearOfNotchAndEdges(t *testing.T) {
	insets := core.Edges{Top: 59, Bottom: 34, Left: 44, Right: 44}

	// Pure SafeRect in view coordinates.
	gotViewSafe := screen.SafeRect(win2532, insets)
	wantViewSafe := core.Bounds{
		Origin: core.Point{X: 44, Y: 59},
		Size:   core.Size{Width: 2444, Height: 1077},
	}
	assertBoundsNear(t, "SafeRect", gotViewSafe, wantViewSafe)

	// Insets larger than window clamp cleanly to non-negative size.
	collapsed := screen.SafeRect(core.Size{Width: 100, Height: 80}, core.Edges{Top: 60, Bottom: 50, Left: 70, Right: 70})
	if collapsed.Size.Width != 0 || collapsed.Size.Height != 0 {
		t.Fatalf("over-inset SafeRect size = %v, want zero", collapsed.Size)
	}

	// Verify SafeGameRect across modes on 2532x1170 with phone notch insets.
	// ModeFit: pillarboxes (226pt each) already absorb Left=44 and Right=44, so X stays [0, 320], while Y trims top/bottom.
	fitVP := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFit}
	fitSafe := fitVP.SafeGameRect(win2532, insets)
	assertBoundsNear(t, "fit SafeGameRect", fitSafe, core.Bounds{
		Origin: core.Point{X: 0, Y: core.Points(59.0 / 6.5)},
		Size:   core.Size{Width: 320, Height: core.Points(1077.0 / 6.5)},
	})

	// ModeFill: all 4 insets fall inside the visible slice of virtual space.
	fillVP := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeFill}
	fillSafe := fillVP.SafeGameRect(win2532, insets)
	assertBoundsNear(t, "fill SafeGameRect", fillSafe, core.Bounds{
		Origin: core.Point{X: core.Points(44.0 / 7.9125), Y: core.Points((59.0 + 127.125) / 7.9125)},
		Size:   core.Size{Width: core.Points(2444.0 / 7.9125), Height: core.Points(1077.0 / 7.9125)},
	})

	// ModeExpand: all 4 insets apply inside the expanded virtual width (389.538 x 180).
	expandVP := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand}
	expandSafe := expandVP.SafeGameRect(win2532, insets)
	assertBoundsNear(t, "expand SafeGameRect", expandSafe, core.Bounds{
		Origin: core.Point{X: core.Points(44.0 / 6.5), Y: core.Points(59.0 / 6.5)},
		Size:   core.Size{Width: core.Points(2444.0 / 6.5), Height: core.Points(1077.0 / 6.5)},
	})

	// End-to-end under nsgotest with SetEnvironment reading env.Window() and env.SafeArea().
	var envSafeRect core.Bounds
	var hudRect core.Bounds
	hudVP := screen.Viewport{Width: 320, Height: 180, Mode: screen.ModeExpand}

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		env := cx.Env()
		return core.Canvas{
			TestID: "hud",
			Style:  core.Style{Width: core.Pct(100), Height: core.Pct(100)},
			Draw: func(d *core.Drawing, size core.Size) {
				envSafeRect = screen.SafeRectForEnv(env)
				hudRect = hudVP.SafeGameRectForEnv(env)
				hudVP.Draw(d, size, func(d *core.Drawing, _ core.Size) {
					// Draw a top-left HUD score box anchored inside hudRect.
					d.SetFillColor(core.RGB(0xFFFFFF)).
						FillRect(float32(hudRect.Origin.X), float32(hudRect.Origin.Y), 40, 12)
				})
			},
		}
	})

	app.Resize(win2532.Width, win2532.Height)
	app.SetEnvironment(core.Environment{SafeArea: insets})

	assertBoundsNear(t, "SafeRectForEnv", envSafeRect, wantViewSafe)
	assertBoundsNear(t, "SafeGameRectForEnv", hudRect, expandSafe)

	// Transform the HUD box's corners back to view coordinates and verify they stay completely clear of the notch and home indicator.
	tl := hudVP.GameToView(win2532, hudRect.Origin)
	br := hudVP.GameToView(win2532, core.Point{
		X: hudRect.Origin.X + hudRect.Size.Width,
		Y: hudRect.Origin.Y + hudRect.Size.Height,
	})
	if tl.X < insets.Left-1e-3 || tl.Y < insets.Top-1e-3 {
		t.Fatalf("HUD top-left %v intrudes into safe-area insets %+v", tl, insets)
	}
	if br.X > win2532.Width-insets.Right+1e-3 || br.Y > win2532.Height-insets.Bottom+1e-3 {
		t.Fatalf("HUD bottom-right %v intrudes into safe-area insets %+v", br, insets)
	}
}

func TestDegenerateInputsAndModeString(t *testing.T) {
	vp := screen.Viewport{Width: 320, Height: 180}
	if pl := vp.Layout(core.Size{}); pl.ScaleX != 0 || pl.ScaleY != 0 || len(pl.Bars) != 0 {
		t.Fatalf("zero window Layout = %+v, want zero", pl)
	}
	if pl := (screen.Viewport{}).Layout(win1280); pl.ScaleX != 0 || pl.ScaleY != 0 {
		t.Fatalf("zero viewport Layout = %+v, want zero", pl)
	}
	if _, ok := vp.ViewToGame(core.Size{}, core.Point{X: 10, Y: 10}); ok {
		t.Fatal("ViewToGame on zero window returned ok == true")
	}
	if pt := vp.GameToView(core.Size{}, core.Point{X: 10, Y: 10}); pt != (core.Point{}) {
		t.Fatalf("GameToView on zero window = %v, want zero", pt)
	}
	if b := screen.SafeRectForEnv(nil); b != (core.Bounds{}) {
		t.Fatalf("SafeRectForEnv(nil) = %v, want zero", b)
	}
	if b := vp.SafeGameRectForEnv(nil); b != (core.Bounds{}) {
		t.Fatalf("SafeGameRectForEnv(nil) = %v, want zero", b)
	}
	if b := vp.SafeGameRect(core.Size{}, core.Edges{Top: 10}); b != (core.Bounds{}) {
		t.Fatalf("SafeGameRect on zero window = %v, want zero", b)
	}

	// Drawing with zero size or nil fn is a no-op and leaves the drawing intact.
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "noop",
			Style:  core.Style{Width: core.Pt(100), Height: core.Pt(100)},
			Draw: func(d *core.Drawing, _ core.Size) {
				vp.Draw(d, core.Size{}, func(*core.Drawing, core.Size) {
					t.Error("fn should not be called for zero window")
				})
				vp.Draw(d, win1280, nil)
			},
		}
	})
	if len(app.Find("noop").Ops) != 0 {
		t.Fatalf("expected 0 ops, got %+v", app.Find("noop").Ops)
	}

	wantNames := map[screen.Mode]string{
		screen.ModeFit:     "fit",
		screen.ModeFill:    "fill",
		screen.ModeStretch: "stretch",
		screen.ModeExpand:  "expand",
		screen.Mode(99):    "unknown",
	}
	for m, want := range wantNames {
		if got := m.String(); got != want {
			t.Errorf("Mode(%d).String() = %q, want %q", m, got, want)
		}
	}
}

func assertNear(t *testing.T, label string, got, want float32) {
	t.Helper()
	if math.Abs(float64(got-want)) > 1e-3 {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func assertSizeNear(t *testing.T, label string, got, want core.Size) {
	t.Helper()
	assertNear(t, label+".Width", float32(got.Width), float32(want.Width))
	assertNear(t, label+".Height", float32(got.Height), float32(want.Height))
}

func assertBoundsNear(t *testing.T, label string, got, want core.Bounds) {
	t.Helper()
	assertNear(t, label+".Origin.X", float32(got.Origin.X), float32(want.Origin.X))
	assertNear(t, label+".Origin.Y", float32(got.Origin.Y), float32(want.Origin.Y))
	assertNear(t, label+".Size.Width", float32(got.Size.Width), float32(want.Size.Width))
	assertNear(t, label+".Size.Height", float32(got.Size.Height), float32(want.Size.Height))
}

// sameOps fails t unless got and want are the same operations, their numbers within float rounding.
func sameOps(t *testing.T, got, want []core.DrawOp) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d ops, want %d:\n%+v\nwant:\n%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		near := func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }
		gf := []float32{g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY}
		wf := []float32{w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY}
		for j := range gf {
			if !near(gf[j], wf[j]) {
				t.Fatalf("op %d is %+v, want %+v", i, g, w)
			}
		}
		if g.Kind != w.Kind || !reflect.DeepEqual(g.Image, w.Image) {
			t.Fatalf("op %d is %+v, want %+v", i, g, w)
		}
		g.X, g.Y, g.X1, g.Y1, g.X2, g.Y2, g.W, g.H, g.R, g.A, g.A0, g.A1, g.RY = w.X, w.Y, w.X1, w.Y1, w.X2, w.Y2, w.W, w.H, w.R, w.A, w.A0, w.A1, w.RY
		g.Image = w.Image
		if !reflect.DeepEqual(g, w) {
			t.Fatalf("op %d is %+v, want %+v", i, got[i], want[i])
		}
	}
}
