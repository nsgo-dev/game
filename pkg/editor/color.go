package editor

import (
	"github.com/nsgo-dev/game/pkg/editor/theme"
	"github.com/nsgo-dev/nsgo/core"
)

// Palette returns the active theme.Palette for cx when non-nil (following cx.Dark(), which tracks Scope.SetAppearance, Scope.SetSubtreeAppearance, and live system appearance changes), or e.Dark() otherwise.
func (e *Editor) Palette(cx *core.Scope) theme.Palette {
	if cx != nil {
		return theme.PaletteFor(cx.Dark())
	}
	return e.activePalette()
}

// activePalette returns the active theme.Palette for e, defaulting to dark when e is nil.
func (e *Editor) activePalette() theme.Palette {
	if e == nil {
		return theme.Dark()
	}
	return theme.PaletteFor(e.Dark())
}

// colorFromRGBA8 packs dynamic raster pixel or computed data channels into a core.Color.
func colorFromRGBA8(r, g, b, a uint8) core.Color {
	return core.Color(uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a))
}
