package scene

import (
	"math"

	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// NewParallaxLayer returns a visible SpaceWorld Layer with the given draw order and parallax scroll factors.
//
// Factors of 0 keep the layer fixed against camera translation, 0.5 scrolls at half the camera's speed, 1.0 matches the world camera, and values greater than 1.0 scroll faster for foreground layers.
func NewParallaxLayer(name string, order int, parallaxX, parallaxY float32) *Layer {
	return &Layer{
		Name:        name,
		Space:       SpaceWorld,
		Visible:     true,
		Order:       order,
		ParallaxX:   parallaxX,
		ParallaxY:   parallaxY,
		parallaxSet: true,
	}
}

// SetParallax sets the layer's horizontal and vertical parallax scroll factors and returns l.
//
// Calling SetParallax marks parallax factors as explicitly configured so zero factors lock an axis instead of defaulting to 1.
func (l *Layer) SetParallax(x, y float32) *Layer {
	if l == nil {
		return nil
	}
	l.ParallaxX = x
	l.ParallaxY = y
	l.parallaxSet = true
	return l
}

func (l *Layer) parallaxFactors() (float32, float32) {
	if l.parallaxSet {
		return l.ParallaxX, l.ParallaxY
	}
	px, py := l.ParallaxX, l.ParallaxY
	if px == 0 {
		px = 1
	}
	if py == 0 {
		py = 1
	}
	return px, py
}

// parallaxCamera returns the effective camera for l under cam, scaling camera position (and shake) by l's parallax factors and applying OffsetX/OffsetY.
func (l *Layer) parallaxCamera(cam *tilemap.Camera) tilemap.Camera {
	px, py := l.parallaxFactors()
	pcam := cam.Parallax(px, py)
	pcam.X -= l.OffsetX
	pcam.Y -= l.OffsetY
	return pcam
}

func (l *Layer) repeatRanges(vb core.Bounds) (minCol, maxCol, minRow, maxRow int, repeatX, repeatY bool) {
	if l.RepeatX && l.RepeatWidth > 0 && vb.Size.Width > 0 {
		w := float64(l.RepeatWidth)
		minX := float64(vb.Origin.X)
		maxX := minX + float64(vb.Size.Width)
		minCol = int(math.Floor(minX / w))
		maxCol = int(math.Ceil(maxX/w)) - 1
		if maxCol < minCol {
			maxCol = minCol
		}
		repeatX = true
	}
	if l.RepeatY && l.RepeatHeight > 0 && vb.Size.Height > 0 {
		h := float64(l.RepeatHeight)
		minY := float64(vb.Origin.Y)
		maxY := minY + float64(vb.Size.Height)
		minRow = int(math.Floor(minY / h))
		maxRow = int(math.Ceil(maxY/h)) - 1
		if maxRow < minRow {
			maxRow = minRow
		}
		repeatY = true
	}
	return minCol, maxCol, minRow, maxRow, repeatX, repeatY
}

func (l *Layer) drawWithParallax(d *core.Drawing, cam *tilemap.Camera, entries []drawEntry) {
	if l.Space == SpaceWorld && cam != nil {
		pcam := l.parallaxCamera(cam)
		d.Save()
		pcam.Apply(d)

		minCol, maxCol, minRow, maxRow, rx, ry := l.repeatRanges(pcam.VisibleBounds())
		if !rx && !ry {
			l.drawEntries(d, entries, &pcam)
		} else {
			for row := minRow; row <= maxRow; row++ {
				var ty float32
				if ry {
					ty = float32(row) * l.RepeatHeight
				}
				for col := minCol; col <= maxCol; col++ {
					var tx float32
					if rx {
						tx = float32(col) * l.RepeatWidth
					}
					tileCam := pcam
					tileCam.X -= tx
					tileCam.Y -= ty
					if tx != 0 || ty != 0 {
						d.Save()
						d.Translate(tx, ty)
					}
					l.drawEntries(d, entries, &tileCam)
					if tx != 0 || ty != 0 {
						d.Restore()
					}
				}
			}
		}

		d.Restore()
		return
	}

	hasOffset := l.OffsetX != 0 || l.OffsetY != 0
	if hasOffset {
		d.Save()
		d.Translate(l.OffsetX, l.OffsetY)
	}
	l.drawEntries(d, entries, nil)
	if hasOffset {
		d.Restore()
	}
}
