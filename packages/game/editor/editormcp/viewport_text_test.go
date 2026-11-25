package editormcp

import (
	"bytes"
	"image"
	"image/png"
	"math"
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

func TestRenderOverlayTextPNGGuardsNonFiniteAndHugeScale(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	basePNG := buf.Bytes()

	nan := float32(math.NaN())
	inf := float32(math.Inf(1))

	// Non-finite positions and transforms must be skipped safely without panicking or hanging.
	badOps := []core.DrawOp{
		{Kind: core.DrawFillColor, Color: core.RGBA(0xFFFFFF, 1)},
		{Kind: core.DrawFillText, X: nan, Y: 10, Text: "nan"},
		{Kind: core.DrawFillText, X: inf, Y: 10, Text: "inf"},
		{Kind: core.DrawFillText, X: 1e20, Y: 10, Text: "huge-pos"},
		{Kind: core.DrawSave},
		{Kind: core.DrawScale, X: inf, Y: 1},
		{Kind: core.DrawFillText, X: 4, Y: 16, Text: "inf-scale"},
		{Kind: core.DrawRestore},
	}
	if _, ok := renderOverlayTextPNG(basePNG, badOps); ok {
		t.Fatal("expected non-finite / out-of-range text ops to be skipped")
	}

	// Extreme finite scale must be clamped to maxOverlayTextPixScale rather than overflowing or hanging.
	hugeScaleOps := []core.DrawOp{
		{Kind: core.DrawFillColor, Color: core.RGBA(0xFFFFFF, 1)},
		{Kind: core.DrawScale, X: 1e30, Y: 1e30},
		{Kind: core.DrawFillText, X: 0, Y: 0, Text: "A"},
	}
	if _, ok := renderOverlayTextPNG(basePNG, hugeScaleOps); !ok {
		t.Fatal("expected clamped huge-scale text op at origin to render cleanly without overflow")
	}
}
