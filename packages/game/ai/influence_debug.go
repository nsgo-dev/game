//go:build !release

package ai

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// InfluenceHeatmapEnabled reports whether DrawHeatmap is compiled into this build.
// It is true in default builds and false when built with -tags release.
func InfluenceHeatmapEnabled() bool { return true }

// DrawHeatmap renders the influence layer as a translucent color heatmap in world coordinates,
// culling cells outside cam.VisibleBounds() when cam is non-nil.
//
// Like tilemap.Tilemap.Draw, DrawHeatmap expects d to already carry the camera's world-to-view transform
// (via cam.Apply(d)). Positive cells are tinted with opts.PositiveColor and negative cells with
// opts.NegativeColor, with alpha proportional to |value| / maxAbs up to opts.MaxAlpha.
// Under -tags release, DrawHeatmap compiles to a no-op stub.
func (l *InfluenceLayer) DrawHeatmap(d *core.Drawing, cam *tilemap.Camera, opts HeatmapOptions) {
	if d == nil || l == nil || l.cols == 0 || l.rows == 0 {
		return
	}
	size := l.cellSize()

	minCol, maxCol := 0, l.cols-1
	minRow, maxRow := 0, l.rows-1
	if cam != nil {
		vb := cam.VisibleBounds()
		if vb.Size.Width <= 0 || vb.Size.Height <= 0 {
			return
		}
		s := float64(size)
		minX := float64(vb.Origin.X) - float64(l.Origin.X)
		maxX := minX + float64(vb.Size.Width)
		minY := float64(vb.Origin.Y) - float64(l.Origin.Y)
		maxY := minY + float64(vb.Size.Height)

		minCol = max(0, clampGridIndex(math.Floor(minX/s), l.cols))
		maxCol = min(l.cols-1, clampGridIndex(math.Ceil(maxX/s), l.cols)-1)
		minRow = max(0, clampGridIndex(math.Floor(minY/s), l.rows))
		maxRow = min(l.rows-1, clampGridIndex(math.Ceil(maxY/s), l.rows)-1)
		if minCol > maxCol || minRow > maxRow {
			return
		}
	}

	maxAbs := opts.MaxAbs
	if !(maxAbs > 0) || math.IsInf(float64(maxAbs), 0) {
		maxAbs = 0
		for _, v := range l.values {
			if v < 0 {
				v = -v
			}
			if v > maxAbs {
				maxAbs = v
			}
		}
	}

	maxAlpha := opts.MaxAlpha
	if !(maxAlpha > 0) || math.IsInf(float64(maxAlpha), 0) {
		maxAlpha = 0.65
	} else if maxAlpha > 1 {
		maxAlpha = 1
	}

	threshold := opts.MinThreshold
	if !(threshold > 0) || math.IsInf(float64(threshold), 0) {
		threshold = 1e-4
	}

	posColor := opts.PositiveColor
	if posColor == 0 {
		posColor = core.RGB(0x22C55E)
	}
	negColor := opts.NegativeColor
	if negColor == 0 {
		negColor = core.RGB(0xEF4444)
	}

	posHex := uint32(posColor >> 8)
	posAlpha := float32(posColor&0xff) / 255
	negHex := uint32(negColor >> 8)
	negAlpha := float32(negColor&0xff) / 255

	saved := false
	if maxAbs > 0 {
		invMax := 1 / maxAbs
		for row := minRow; row <= maxRow; row++ {
			rowOff := row * l.cols
			y := l.Origin.Y + float32(row)*size
			for col := minCol; col <= maxCol; col++ {
				v := l.values[rowOff+col]
				av := v
				if av < 0 {
					av = -av
				}
				if av <= threshold {
					continue
				}
				t := av * invMax
				if t > 1 {
					t = 1
				}
				var colColor core.Color
				if v > 0 {
					colColor = core.RGBA(posHex, t*maxAlpha*posAlpha)
				} else {
					colColor = core.RGBA(negHex, t*maxAlpha*negAlpha)
				}
				if colColor&0xff == 0 {
					continue
				}
				if !saved {
					d.Save()
					saved = true
				}
				x := l.Origin.X + float32(col)*size
				d.SetFillColor(colColor).FillRect(x, y, size, size)
			}
		}
	}

	if opts.DrawGrid {
		gridCol := opts.GridColor
		if gridCol == 0 {
			gridCol = core.RGBA(0xFFFFFF, 0.2)
		}
		if gridCol&0xff != 0 {
			if !saved {
				d.Save()
				saved = true
			}
			x0 := l.Origin.X + float32(minCol)*size
			x1 := l.Origin.X + float32(maxCol+1)*size
			y0 := l.Origin.Y + float32(minRow)*size
			y1 := l.Origin.Y + float32(maxRow+1)*size
			d.SetStrokeColor(gridCol).SetLineWidth(1).BeginPath()
			for col := minCol; col <= maxCol+1; col++ {
				x := l.Origin.X + float32(col)*size
				d.MoveTo(x, y0).LineTo(x, y1)
			}
			for row := minRow; row <= maxRow+1; row++ {
				y := l.Origin.Y + float32(row)*size
				d.MoveTo(x0, y).LineTo(x1, y)
			}
			d.Stroke()
		}
	}

	if saved {
		d.Restore()
	}
}

func clampGridIndex(v float64, limit int) int {
	hi := float64(limit + 1)
	if v < -1 {
		return -1
	}
	if v > hi {
		return limit + 1
	}
	return int(v)
}
