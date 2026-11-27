package gametest_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/nsgo-dev/game/pkg/a11y"
	"github.com/nsgo-dev/game/pkg/gametest"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
)

func TestRenderOpsColorVisionSimulationAndCorrection(t *testing.T) {
	pal := a11y.PaletteFor(a11y.VisionNormal)
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "swatches",
			Style:  core.Style{Width: core.Pt(32), Height: core.Pt(16)},
			Draw: func(d *core.Drawing, _ core.Size) {
				d.SetFillColor(pal.Player).FillRect(0, 0, 16, 16)
				d.SetFillColor(pal.Enemy).FillRect(16, 0, 16, 16)
			},
		}
	}, nsgotest.Size(32, 16))
	app.Settle()

	ops := app.Find("id:swatches").Drawing.Ops
	raw := gametest.RenderOps(32, 16, ops)
	sim := gametest.RenderOps(32, 16, ops, gametest.RenderOptions{
		ColorVision: a11y.VisionDeuteranopia,
	})

	// Left swatch pixel matches a11y.Simulate(pal.Player, VisionDeuteranopia).
	wantPlayer := a11y.Simulate(pal.Player, a11y.VisionDeuteranopia)
	wr, wg, wb, _ := wantPlayer.Components()
	px := sim.RGBAAt(4, 4)
	if int(px.R) != int(wr*255+0.5) || int(px.G) != int(wg*255+0.5) || int(px.B) != int(wb*255+0.5) {
		t.Fatalf("simulated left swatch = %v, want ~(%d,%d,%d)", px, int(wr*255+0.5), int(wg*255+0.5), int(wb*255+0.5))
	}

	// Okabe-Ito Player vs Enemy stays distinguishable under every deficiency.
	left := image.Rect(0, 0, 16, 16)
	right := image.Rect(16, 0, 32, 16)
	for _, mode := range []a11y.ColorVision{
		a11y.VisionProtanopia,
		a11y.VisionDeuteranopia,
		a11y.VisionTritanopia,
	} {
		gametest.AssertColorDistinguishable(t, mode, pal.Player, pal.Enemy, 40)
		gametest.AssertRegionsDistinguishable(t, raw, left, right, mode, 40)
	}

	// RenderOps with ColorVisionCorrect applies daltonization, raising simulated distance for red/green.
	rgApp := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.Canvas{
			TestID: "rg",
			Style:  core.Style{Width: core.Pt(32), Height: core.Pt(16)},
			Draw: func(d *core.Drawing, _ core.Size) {
				d.SetFillColor(core.RGB(0xCC3333)).FillRect(0, 0, 16, 16)
				d.SetFillColor(core.RGB(0x3E7B2E)).FillRect(16, 0, 16, 16)
			},
		}
	}, nsgotest.Size(32, 16))
	rgApp.Settle()
	rgOps := rgApp.Find("id:rg").Drawing.Ops

	rgUncorr := gametest.RenderOps(32, 16, rgOps, gametest.RenderOptions{
		ColorVision: a11y.VisionProtanopia,
	})
	rgCorr := gametest.RenderOps(32, 16, rgOps, gametest.RenderOptions{
		ColorVision:        a11y.VisionProtanopia,
		ColorVisionCorrect: true,
	})
	rgCorrSim := gametest.SimulateColorVision(rgCorr, a11y.VisionProtanopia, 1.0)

	dBefore := gametest.ColorDistance(gametest.AverageRegionColor(rgUncorr, left), gametest.AverageRegionColor(rgUncorr, right))
	dAfter := gametest.ColorDistance(gametest.AverageRegionColor(rgCorrSim, left), gametest.AverageRegionColor(rgCorrSim, right))
	if dAfter <= dBefore+10 {
		t.Fatalf("expected corrected red/green distance %.1f > uncorrected %.1f + 10", dAfter, dBefore)
	}

	// AverageRegionColor handles empty/transparent regions cleanly.
	blank := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if got := gametest.AverageRegionColor(blank, image.Rect(0, 0, 4, 4)); got != core.Transparent {
		t.Fatalf("AverageRegionColor(blank) = %#08x, want Transparent", uint32(got))
	}
	blank.SetRGBA(1, 1, color.RGBA{200, 100, 50, 255})
	if got := gametest.AverageRegionColor(blank, image.Rect(0, 0, 4, 4)); got>>8 != 0xC86432 {
		t.Fatalf("AverageRegionColor(single opaque) RGB = %#06x, want 0xc86432", uint32(got>>8))
	}
}
