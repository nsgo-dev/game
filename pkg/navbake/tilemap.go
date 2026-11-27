package navbake

import (
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// FromTilemap returns the source of a tilemap's navigation mesh in world space: the map's area is the outline and every cell whose flags match mask (across all tile layers, as Tilemap.FlagsAt combines them) is an obstacle, such as mask = tilemap.TileSolid.
//
// Cells are the shapes the map's Orientation gives them: squares on an orthogonal map, diamonds on an isometric or staggered one and hexagons on a hexagonal one, so the mesh lines up with the tiles drawn. Neighbouring solid cells merge into one obstacle when the mesh is baked.
func FromTilemap(m *tilemap.Tilemap, mask tilemap.TileFlags) (Source, error) {
	if m == nil {
		return Source{}, errors.New("navbake: nil tilemap")
	}
	if m.Width <= 0 || m.Height <= 0 || !(m.TileWidth > 0) || !(m.TileHeight > 0) {
		return Source{}, fmt.Errorf("navbake: tilemap of %dx%d tiles of %vx%v has no area", m.Width, m.Height, m.TileWidth, m.TileHeight)
	}
	if mask == 0 {
		return Source{}, errors.New("navbake: empty tile flag mask")
	}
	var src Source
	switch m.Orientation {
	case tilemap.OrientationOrthogonal, tilemap.OrientationIsometric:
		// The cells tile a parallelogram, which is the outline.
		src.Outlines = [][]core.Point{{
			latticePoint(m, 0, 0), latticePoint(m, m.Width, 0), latticePoint(m, m.Width, m.Height), latticePoint(m, 0, m.Height),
		}}
	default:
		// Staggered and hexagonal maps have ragged edges: every cell is part of the outline.
		for row := range m.Height {
			for col := range m.Width {
				src.Outlines = append(src.Outlines, cellPolygon(m, col, row))
			}
		}
	}
	for row := range m.Height {
		for col := range m.Width {
			if m.FlagsAt(col, row)&mask != 0 {
				src.Obstacles = append(src.Obstacles, cellPolygon(m, col, row))
			}
		}
	}
	return src, nil
}

// latticePoint returns the world position of grid corner (c, r) of an orthogonal or isometric map, where cell (col, row) spans corners (col, row) to (col+1, row+1).
func latticePoint(m *tilemap.Tilemap, c, r int) core.Point {
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	if m.Orientation == tilemap.OrientationIsometric {
		return pt(float64(c-r)*tw/2+tw/2, float64(c+r)*th/2)
	}
	return pt(float64(c)*tw, float64(r)*th)
}

// cellPolygon returns the outline of cell (col, row) in world space.
func cellPolygon(m *tilemap.Tilemap, col, row int) []core.Point {
	switch m.Orientation {
	case tilemap.OrientationOrthogonal, tilemap.OrientationIsometric:
		return []core.Point{latticePoint(m, col, row), latticePoint(m, col+1, row), latticePoint(m, col+1, row+1), latticePoint(m, col, row+1)}
	}
	o := m.TileOrigin(col, row)
	x, y := float64(o.X), float64(o.Y)
	tw, th := float64(m.TileWidth), float64(m.TileHeight)
	if m.Orientation == tilemap.OrientationHexagonal {
		if m.StaggerAxis == tilemap.StaggerX {
			s := hexSide(m, tw)
			return []core.Point{
				pt(x+(tw-s)/2, y), pt(x+(tw+s)/2, y), pt(x+tw, y+th/2),
				pt(x+(tw+s)/2, y+th), pt(x+(tw-s)/2, y+th), pt(x, y+th/2),
			}
		}
		s := hexSide(m, th)
		return []core.Point{
			pt(x+tw/2, y), pt(x+tw, y+(th-s)/2), pt(x+tw, y+(th+s)/2),
			pt(x+tw/2, y+th), pt(x, y+(th+s)/2), pt(x, y+(th-s)/2),
		}
	}
	// A staggered map's cell is the diamond inside its box.
	return []core.Point{pt(x+tw/2, y), pt(x+tw, y+th/2), pt(x+tw/2, y+th), pt(x, y+th/2)}
}

// hexSide returns the length of a hexagonal cell's straight side along its stagger axis, defaulting as the tilemap package does to half the cell's size along that axis.
func hexSide(m *tilemap.Tilemap, size float64) float64 {
	if m.HexSideLength > 0 {
		return float64(m.HexSideLength)
	}
	return size / 2
}

func pt(x, y float64) core.Point {
	return core.Point{X: core.Points(x), Y: core.Points(y)}
}
