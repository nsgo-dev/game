package physics

import (
	"math"
	"time"
)

// TileFlags marks how a tile cell participates in collision resolution and queries.
type TileFlags uint32

const (
	// TileSolid blocks movement from all four directions.
	TileSolid TileFlags = 1 << 0
	// TileOneWay blocks downward movement only when the body starts at or above the tile's top edge, unless Body.OneWayDrop is set.
	TileOneWay TileFlags = 1 << 1
)

// TileGrid exposes a regular grid of collision tiles to physics resolution and queries without depending on a specific tilemap type.
type TileGrid interface {
	// TileSize returns the width and height of one tile in world coordinates.
	TileSize() (width, height float32)
	// GridSize returns the number of columns and rows in the grid.
	GridSize() (cols, rows int)
	// TileFlagsAt returns the collision flags of the tile at (col, row), or 0 when out of bounds or empty.
	TileFlagsAt(col, row int) TileFlags
}

// Grid is a row-major TileGrid backed by a flat slice of TileFlags.
type Grid struct {
	Cols, Rows            int
	TileWidth, TileHeight float32
	Cells                 []TileFlags
}

// NewGrid returns a Grid with cols columns and rows rows of tiles sized tileWidth by tileHeight.
func NewGrid(cols, rows int, tileWidth, tileHeight float32) *Grid {
	if cols < 0 {
		cols = 0
	}
	if rows < 0 {
		rows = 0
	}
	return &Grid{
		Cols:       cols,
		Rows:       rows,
		TileWidth:  tileWidth,
		TileHeight: tileHeight,
		Cells:      make([]TileFlags, cols*rows),
	}
}

// TileSize returns the tile width and height.
func (g *Grid) TileSize() (width, height float32) {
	return g.TileWidth, g.TileHeight
}

// GridSize returns the grid dimensions in tiles.
func (g *Grid) GridSize() (cols, rows int) {
	return g.Cols, g.Rows
}

// TileFlagsAt returns the flags at (col, row), or 0 when out of bounds.
func (g *Grid) TileFlagsAt(col, row int) TileFlags {
	if col < 0 || row < 0 || col >= g.Cols || row >= g.Rows {
		return 0
	}
	idx := row*g.Cols + col
	if idx < 0 || idx >= len(g.Cells) {
		return 0
	}
	return g.Cells[idx]
}

// Set writes flags to (col, row), ignoring coordinates outside the grid.
func (g *Grid) Set(col, row int, flags TileFlags) {
	if col < 0 || row < 0 || col >= g.Cols || row >= g.Rows {
		return
	}
	need := g.Cols * g.Rows
	if len(g.Cells) < need {
		cells := make([]TileFlags, need)
		copy(cells, g.Cells)
		g.Cells = cells
	}
	g.Cells[row*g.Cols+col] = flags
}

// FillRect sets flags on all cells in the rectangle [col, col+w) x [row, row+h).
func (g *Grid) FillRect(col, row, w, h int, flags TileFlags) {
	for r := row; r < row+h; r++ {
		for c := col; c < col+w; c++ {
			g.Set(c, r, flags)
		}
	}
}

// Body is a movable physics shape with velocity, collision filtering and tile contact state.
type Body struct {
	ID       int
	Tag      string
	Shape    Shape
	VX, VY   float32
	Layer    LayerMask
	Mask     LayerMask
	Grounded bool
	// OnCeiling, OnLeftWall and OnRightWall report tile contacts from the last MoveAndSlide call.
	OnCeiling   bool
	OnLeftWall  bool
	OnRightWall bool
	// OneWayDrop lets the body fall through TileOneWay platforms during MoveAndSlide.
	OneWayDrop bool
	UserData   any
}

// NewBoxBody returns a Body whose Shape is a Box at (x, y) with size (w, h).
func NewBoxBody(x, y, w, h float32) *Body {
	return &Body{Shape: BoxShape(Box{X: x, Y: y, W: w, H: h})}
}

// NewCircleBody returns a Body whose Shape is a Circle centered at (x, y) with radius r.
func NewCircleBody(x, y, r float32) *Body {
	return &Body{Shape: CircleShape(Circle{X: x, Y: y, R: r})}
}

// Bounds returns the axis-aligned bounding box of b's shape.
func (b *Body) Bounds() Box { return b.Shape.Bounds() }

// Translate shifts b's shape by (dx, dy).
func (b *Body) Translate(dx, dy float32) {
	b.Shape = b.Shape.Translated(dx, dy)
}

// TileContact records one tile hit during MoveAndSlide.
// (NormalX, NormalY) points away from the tile surface into the body.
type TileContact struct {
	Col, Row         int
	Flags            TileFlags
	NormalX, NormalY float32
}

// MoveResult summarizes the tile contacts and surface flags from one MoveAndSlide call.
type MoveResult struct {
	Contacts    []TileContact
	Grounded    bool
	OnCeiling   bool
	OnLeftWall  bool
	OnRightWall bool
}

// MoveAndSlide advances b by (VX*dt, VY*dt) and resolves axis-aligned collisions against grid,
// resolving X before Y. It zeroes velocity along blocked axes, updates b's Grounded, OnCeiling,
// OnLeftWall and OnRightWall flags, and subdivides large displacements so fast bodies do not
// tunnel through single-tile walls or platforms.
func MoveAndSlide(b *Body, grid TileGrid, dt time.Duration) MoveResult {
	if b == nil {
		return MoveResult{}
	}
	b.Grounded = false
	b.OnCeiling = false
	b.OnLeftWall = false
	b.OnRightWall = false

	if b.Shape.Empty() || dt <= 0 {
		return MoveResult{}
	}

	secs := dt.Seconds()
	totalDX := float64(b.VX) * secs
	totalDY := float64(b.VY) * secs
	if math.IsNaN(totalDX) || math.IsInf(totalDX, 0) || math.IsNaN(totalDY) || math.IsInf(totalDY, 0) {
		return MoveResult{}
	}

	if grid == nil {
		b.Translate(float32(totalDX), float32(totalDY))
		return MoveResult{}
	}
	tileW, tileH := grid.TileSize()
	cols, rows := grid.GridSize()
	if tileW <= 0 || tileH <= 0 || cols <= 0 || rows <= 0 {
		b.Translate(float32(totalDX), float32(totalDY))
		return MoveResult{}
	}

	tw, th := float64(tileW), float64(tileH)
	box := b.Shape.Bounds()
	x, y := float64(box.X), float64(box.Y)
	w, h := float64(box.W), float64(box.H)

	// Subdivide any displacement exceeding half a tile to prevent tunneling through 1-tile barriers.
	steps := 1
	if sx := int(math.Ceil(math.Abs(totalDX) / (tw * 0.5))); sx > steps {
		steps = sx
	}
	if sy := int(math.Ceil(math.Abs(totalDY) / (th * 0.5))); sy > steps {
		steps = sy
	}

	stepDX := totalDX / float64(steps)
	stepDY := totalDY / float64(steps)

	var contacts []TileContact
	addContact := func(c TileContact) {
		for _, existing := range contacts {
			if existing.Col == c.Col && existing.Row == c.Row &&
				existing.NormalX == c.NormalX && existing.NormalY == c.NormalY {
				return
			}
		}
		contacts = append(contacts, c)
	}

	for range steps {
		// 1. Move and resolve horizontally against TileSolid cells.
		if stepDX != 0 {
			x += stepDX
			yEps := spanEpsilon(y, h)
			minRow := max(0, int(math.Floor((y+yEps)/th)))
			maxRow := min(rows-1, int(math.Ceil((y+h-yEps)/th))-1)
			xEps := spanEpsilon(x, w)
			minCol := max(0, int(math.Floor((x+xEps)/tw)))
			maxCol := min(cols-1, int(math.Ceil((x+w-xEps)/tw))-1)

			if minRow <= maxRow && minCol <= maxCol {
				if stepDX > 0 {
					hitCol := -1
					for col := minCol; col <= maxCol; col++ {
						for row := minRow; row <= maxRow; row++ {
							if flags := grid.TileFlagsAt(col, row); flags&TileSolid != 0 {
								hitCol = col
								addContact(TileContact{Col: col, Row: row, Flags: flags, NormalX: -1})
							}
						}
						if hitCol >= 0 {
							x = float64(hitCol)*tw - w
							b.OnRightWall = true
							b.VX = 0
							stepDX = 0
							break
						}
					}
				} else {
					hitCol := -1
					for col := maxCol; col >= minCol; col-- {
						for row := minRow; row <= maxRow; row++ {
							if flags := grid.TileFlagsAt(col, row); flags&TileSolid != 0 {
								hitCol = col
								addContact(TileContact{Col: col, Row: row, Flags: flags, NormalX: 1})
							}
						}
						if hitCol >= 0 {
							x = float64(hitCol+1) * tw
							b.OnLeftWall = true
							b.VX = 0
							stepDX = 0
							break
						}
					}
				}
			}
		}

		// 2. Move and resolve vertically against TileSolid and TileOneWay cells.
		if stepDY != 0 {
			prevBottom := y + h
			y += stepDY
			xEps := spanEpsilon(x, w)
			minCol := max(0, int(math.Floor((x+xEps)/tw)))
			maxCol := min(cols-1, int(math.Ceil((x+w-xEps)/tw))-1)
			yEps := spanEpsilon(y, h)
			minRow := max(0, int(math.Floor((y+yEps)/th)))
			maxRow := min(rows-1, int(math.Ceil((y+h-yEps)/th))-1)

			if minCol <= maxCol && minRow <= maxRow {
				if stepDY > 0 {
					hitRow := -1
					for row := minRow; row <= maxRow; row++ {
						tileTop := float64(row) * th
						oneWayEps := max(1e-4, 1e-5*math.Abs(tileTop))
						for col := minCol; col <= maxCol; col++ {
							flags := grid.TileFlagsAt(col, row)
							solid := flags&TileSolid != 0
							oneWay := flags&TileOneWay != 0 && !b.OneWayDrop && prevBottom <= tileTop+oneWayEps
							if solid || oneWay {
								hitRow = row
								addContact(TileContact{Col: col, Row: row, Flags: flags, NormalY: -1})
							}
						}
						if hitRow >= 0 {
							y = float64(hitRow)*th - h
							b.Grounded = true
							b.VY = 0
							stepDY = 0
							break
						}
					}
				} else {
					hitRow := -1
					for row := maxRow; row >= minRow; row-- {
						for col := minCol; col <= maxCol; col++ {
							if flags := grid.TileFlagsAt(col, row); flags&TileSolid != 0 {
								hitRow = row
								addContact(TileContact{Col: col, Row: row, Flags: flags, NormalY: 1})
							}
						}
						if hitRow >= 0 {
							y = float64(hitRow+1) * th
							b.OnCeiling = true
							b.VY = 0
							stepDY = 0
							break
						}
					}
				}
			}
		}

		if stepDX == 0 && stepDY == 0 {
			break
		}
	}

	switch b.Shape.Kind {
	case ShapeBox:
		b.Shape.Box.X = float32(x)
		b.Shape.Box.Y = float32(y)
	case ShapeCircle:
		b.Shape.Circle.X = float32(x + float64(b.Shape.Circle.R))
		b.Shape.Circle.Y = float32(y + float64(b.Shape.Circle.R))
	}

	return MoveResult{
		Contacts:    contacts,
		Grounded:    b.Grounded,
		OnCeiling:   b.OnCeiling,
		OnLeftWall:  b.OnLeftWall,
		OnRightWall: b.OnRightWall,
	}
}

// spanEpsilon returns a small inward tolerance so a body resting flush on a tile seam does not snag on adjacent tiles due to float32 rounding.
func spanEpsilon(origin, size float64) float64 {
	return min(size*0.25, max(1e-4, 1e-5*(math.Abs(origin)+math.Abs(size))))
}
