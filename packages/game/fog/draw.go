package fog

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Style is the paint Draw lays over cells a team cannot see now.
type Style struct {
	// Unseen fills cells the team has never seen; a zero alpha leaves them uncovered.
	Unseen core.Color
	// Explored fills cells the team has seen before but cannot see now; a zero alpha leaves them uncovered.
	Explored core.Color
}

// DefaultStyle returns opaque black over unseen cells and half-transparent black over explored ones, so remembered terrain shows dimmed.
func DefaultStyle() Style {
	return Style{Unseen: core.RGB(0x000000), Explored: core.RGBA(0x000000, 0.55)}
}

// Draw paints team's fog over the grid in world coordinates, as of the last Update: one filled path of Style.Unseen over unseen cells and one of Style.Explored over explored cells, each built from horizontal runs of cells. Visible cells stay uncovered.
//
// Like tilemap.Tilemap.Draw, Draw expects d to already carry the camera's transform (cam.Apply(d)), and uses cam only to skip cells outside cam.VisibleBounds(); a nil cam draws the whole grid. Draw it after the tilemap and units so it covers them. The grid's cell (col, row) covers world points from (col*TileWidth, row*TileHeight), so Draw suits orthogonal maps.
func (f *Fog) Draw(d *core.Drawing, team Team, cam *tilemap.Camera, style Style) {
	if f.width == 0 || f.height == 0 || f.TileWidth <= 0 || f.TileHeight <= 0 {
		return
	}
	minCol, maxCol := 0, f.width-1
	minRow, maxRow := 0, f.height-1
	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		tw, th := float64(f.TileWidth), float64(f.TileHeight)
		minX, minY := float64(vb.Origin.X), float64(vb.Origin.Y)
		maxX, maxY := minX+float64(vb.Size.Width), minY+float64(vb.Size.Height)
		minCol = max(minCol, clampInt(math.Floor(minX/tw)))
		maxCol = min(maxCol, clampInt(math.Ceil(maxX/tw))-1)
		minRow = max(minRow, clampInt(math.Floor(minY/th)))
		maxRow = min(maxRow, clampInt(math.Ceil(maxY/th))-1)
		if minCol > maxCol || minRow > maxRow {
			return
		}
	}
	l := f.teams[team]
	saved := false
	if style.Unseen&0xff != 0 {
		saved = f.fillRuns(d, l, Unseen, style.Unseen, minCol, maxCol, minRow, maxRow, saved)
	}
	if style.Explored&0xff != 0 && l != nil {
		saved = f.fillRuns(d, l, Explored, style.Explored, minCol, maxCol, minRow, maxRow, saved)
	}
	if saved {
		d.Restore()
	}
}

// clampInt converts v to an int no further from zero than MaxCells, so cameras far outside the grid cannot overflow.
func clampInt(v float64) int {
	return int(math.Max(-MaxCells, math.Min(MaxCells, v)))
}

// stateOf returns l's state at index i, where a nil l is a team with nothing seen.
func stateOf(l *layer, i int) State {
	switch {
	case l == nil:
		return Unseen
	case bit(l.visible, i):
		return Visible
	case bit(l.explored, i):
		return Explored
	}
	return Unseen
}

// fillRuns fills one path made of a rectangle per horizontal run of cells in state want, saving d's state before its first fill unless saved, and reports whether d's state is saved.
func (f *Fog) fillRuns(d *core.Drawing, l *layer, want State, color core.Color, minCol, maxCol, minRow, maxRow int, saved bool) bool {
	begun := false
	for row := minRow; row <= maxRow; row++ {
		base := row * f.width
		y := float32(row) * f.TileHeight
		for col := minCol; col <= maxCol; {
			if stateOf(l, base+col) != want {
				col++
				continue
			}
			end := col + 1
			for end <= maxCol && stateOf(l, base+end) == want {
				end++
			}
			if !begun {
				if !saved {
					d.Save()
					saved = true
				}
				d.SetFillColor(color).BeginPath()
				begun = true
			}
			d.Rect(float32(col)*f.TileWidth, y, float32(end-col)*f.TileWidth, f.TileHeight)
			col = end
		}
	}
	if begun {
		d.Fill()
	}
	return saved
}
