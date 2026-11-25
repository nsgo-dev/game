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

// Draw paints team's fog over the grid in world coordinates, as of the last Update: one filled path of Style.Unseen over unseen cells and one of Style.Explored over explored cells. Visible cells stay uncovered.
//
// Like tilemap.Tilemap.Draw, Draw expects d to already carry the camera's transform (cam.Apply(d)), and uses cam only to skip cells outside cam.VisibleBounds(); a nil cam draws the whole grid. Draw it after the tilemap and units so it covers them. Orthogonal grids emit horizontal runs of rectangles; isometric and staggered grids emit aligned tile diamonds; hexagonal grids emit aligned pointy-top or flat-top hexagons matching Tilemap's projection.
func (f *Fog) Draw(d *core.Drawing, team Team, cam *tilemap.Camera, style Style) {
	if f.width == 0 || f.height == 0 || f.TileWidth <= 0 || f.TileHeight <= 0 {
		return
	}
	if f.Orientation != tilemap.OrientationOrthogonal {
		f.drawProjected(d, team, cam, style)
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
		ox, oy := float64(f.Origin.X), float64(f.Origin.Y)
		minX, minY := float64(vb.Origin.X)-ox, float64(vb.Origin.Y)-oy
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

func (f *Fog) hexSide() float32 {
	if f.HexSideLength > 0 {
		return f.HexSideLength
	}
	if f.StaggerAxis == tilemap.StaggerX {
		return f.TileWidth / 2
	}
	return f.TileHeight / 2
}

func (f *Fog) drawProjected(d *core.Drawing, team Team, cam *tilemap.Camera, style Style) {
	minCol, maxCol := 0, f.width-1
	minRow, maxRow := 0, f.height-1
	hasCam := cam != nil
	var vbMinX, vbMinY, vbMaxX, vbMaxY float64
	tw, th := float64(f.TileWidth), float64(f.TileHeight)

	if hasCam {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		vbMinX = float64(vb.Origin.X)
		vbMinY = float64(vb.Origin.Y)
		vbMaxX = vbMinX + float64(vb.Size.Width)
		vbMaxY = vbMinY + float64(vb.Size.Height)
		lxMin := vbMinX - float64(f.Origin.X)
		lyMin := vbMinY - float64(f.Origin.Y)
		lxMax := vbMaxX - float64(f.Origin.X)
		lyMax := vbMaxY - float64(f.Origin.Y)

		switch f.Orientation {
		case tilemap.OrientationIsometric:
			dMin := (lxMin - tw) / (tw / 2)
			dMax := lxMax / (tw / 2)
			sMin := (lyMin - th) / (th / 2)
			sMax := lyMax / (th / 2)
			minCol = max(minCol, clampInt(math.Floor((sMin+dMin)/2)))
			maxCol = min(maxCol, clampInt(math.Ceil((sMax+dMax)/2)))
			minRow = max(minRow, clampInt(math.Floor((sMin-dMax)/2)))
			maxRow = min(maxRow, clampInt(math.Ceil((sMax-dMin)/2)))

		case tilemap.OrientationStaggered, tilemap.OrientationHexagonal:
			var stepX, stepY float64
			if f.Orientation == tilemap.OrientationStaggered {
				if f.StaggerAxis == tilemap.StaggerX {
					stepX, stepY = tw/2, th
				} else {
					stepX, stepY = tw, th/2
				}
			} else {
				s := float64(f.hexSide())
				if f.StaggerAxis == tilemap.StaggerX {
					stepX, stepY = (tw+s)/2, th
				} else {
					stepX, stepY = tw, (th+s)/2
				}
			}
			if stepX <= 0 || stepY <= 0 {
				return
			}
			if f.StaggerAxis == tilemap.StaggerX {
				minCol = max(minCol, clampInt(math.Floor((lxMin-tw)/stepX)))
				maxCol = min(maxCol, clampInt(math.Ceil(lxMax/stepX)))
				minRow = max(minRow, clampInt(math.Floor((lyMin-th*1.5)/stepY)))
				maxRow = min(maxRow, clampInt(math.Ceil(lyMax/stepY)))
			} else {
				minCol = max(minCol, clampInt(math.Floor((lxMin-tw*1.5)/stepX)))
				maxCol = min(maxCol, clampInt(math.Ceil(lxMax/stepX)))
				minRow = max(minRow, clampInt(math.Floor((lyMin-th)/stepY)))
				maxRow = min(maxRow, clampInt(math.Ceil(lyMax/stepY)))
			}
		}
		if minCol > maxCol || minRow > maxRow {
			return
		}
	}

	m := f.projectionMap()
	l := f.teams[team]
	saved := false
	if style.Unseen&0xff != 0 {
		saved = f.fillProjectedPolygons(d, &m, l, Unseen, style.Unseen, minCol, maxCol, minRow, maxRow, hasCam, vbMinX, vbMinY, vbMaxX, vbMaxY, saved)
	}
	if style.Explored&0xff != 0 && l != nil {
		saved = f.fillProjectedPolygons(d, &m, l, Explored, style.Explored, minCol, maxCol, minRow, maxRow, hasCam, vbMinX, vbMinY, vbMaxX, vbMaxY, saved)
	}
	if saved {
		d.Restore()
	}
}

func (f *Fog) fillProjectedPolygons(d *core.Drawing, m *tilemap.Tilemap, l *layer, want State, color core.Color, minCol, maxCol, minRow, maxRow int, hasCam bool, vbMinX, vbMinY, vbMaxX, vbMaxY float64, saved bool) bool {
	tw, th := f.TileWidth, f.TileHeight
	hw, hh := tw/2, th/2
	side := f.hexSide()
	hexDX := (tw - side) / 2
	hexDY := (th - side) / 2
	begun := false

	for row := minRow; row <= maxRow; row++ {
		base := row * f.width
		for col := minCol; col <= maxCol; col++ {
			if stateOf(l, base+col) != want {
				continue
			}
			orig := m.TileOrigin(col, row)
			ox, oy := float32(orig.X), float32(orig.Y)
			if hasCam {
				if float64(ox+tw) <= vbMinX || float64(ox) >= vbMaxX || float64(oy+th) <= vbMinY || float64(oy) >= vbMaxY {
					continue
				}
			}
			if !begun {
				if !saved {
					d.Save()
					saved = true
				}
				d.SetFillColor(color).BeginPath()
				begun = true
			}
			switch f.Orientation {
			case tilemap.OrientationHexagonal:
				if f.StaggerAxis == tilemap.StaggerX {
					d.MoveTo(ox+hexDX, oy).
						LineTo(ox+tw-hexDX, oy).
						LineTo(ox+tw, oy+hh).
						LineTo(ox+tw-hexDX, oy+th).
						LineTo(ox+hexDX, oy+th).
						LineTo(ox, oy+hh).
						ClosePath()
				} else {
					d.MoveTo(ox+hw, oy).
						LineTo(ox+tw, oy+hexDY).
						LineTo(ox+tw, oy+th-hexDY).
						LineTo(ox+hw, oy+th).
						LineTo(ox, oy+th-hexDY).
						LineTo(ox, oy+hexDY).
						ClosePath()
				}
			default: // OrientationIsometric, OrientationStaggered
				d.MoveTo(ox+hw, oy).
					LineTo(ox+tw, oy+hh).
					LineTo(ox+hw, oy+th).
					LineTo(ox, oy+hh).
					ClosePath()
			}
		}
	}
	if begun {
		d.Fill()
	}
	return saved
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
	ox, oy := float32(f.Origin.X), float32(f.Origin.Y)
	begun := false
	for row := minRow; row <= maxRow; row++ {
		base := row * f.width
		y := oy + float32(row)*f.TileHeight
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
			d.Rect(ox+float32(col)*f.TileWidth, y, float32(end-col)*f.TileWidth, f.TileHeight)
			col = end
		}
	}
	if begun {
		d.Fill()
	}
	return saved
}
