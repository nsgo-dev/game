package physics

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

// DefaultMaxSlopeAngle is the default maximum walkable slope angle (45 degrees in radians).
const DefaultMaxSlopeAngle float32 = math.Pi / 4

// Slope is a line segment representing a ramp or slanted surface for CharacterBody movement.
// The walkable side is the upward-facing normal (NormalY <= 0 in screen coordinates).
type Slope struct {
	A, B core.Point
}

func (s Slope) ordered() (ax, ay, bx, by float64, ok bool) {
	ax, ay = float64(s.A.X), float64(s.A.Y)
	bx, by = float64(s.B.X), float64(s.B.Y)
	if ax == bx && ay == by {
		return 0, 0, 0, 0, false
	}
	if ax > bx || (ax == bx && ay > by) {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return ax, ay, bx, by, true
}

// Angle returns the slope inclination from horizontal in radians, in [0, math.Pi/2].
func (s Slope) Angle() float32 {
	ax, ay, bx, by, ok := s.ordered()
	if !ok {
		return 0
	}
	return float32(detmath.Atan2(math.Abs(by-ay), bx-ax))
}

// Normal returns the upward-facing unit surface normal of s (NormalY <= 0).
func (s Slope) Normal() core.Point {
	ax, ay, bx, by, ok := s.ordered()
	if !ok {
		return core.Point{X: 0, Y: -1}
	}
	dx := bx - ax
	dy := by - ay
	length := detmath.Hypot(dx, dy)
	if length == 0 {
		return core.Point{X: 0, Y: -1}
	}
	// Tangent is (dx, dy)/L with dx >= 0; rotating 90 deg counter-clockwise in screen space
	// (X right, Y down) gives (dy, -dx)/L whose Y component is <= 0 (pointing up).
	return core.Point{
		X: core.Points(dy / length),
		Y: core.Points(-dx / length),
	}
}

func slopeYAt(ax, ay, bx, by, x float64) float64 {
	dx := bx - ax
	if dx == 0 {
		return min(ay, by)
	}
	t := (x - ax) / dx
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return ay + float64(t*(by-ay))
}

// MovingPlatform is a kinematic platform body with its per-step displacement (DeltaX, DeltaY).
// Callers update the platform's Body position and record its displacement in DeltaX and DeltaY
// (or call Move) before stepping CharacterBody.Move.
type MovingPlatform struct {
	Body           *Body
	DeltaX, DeltaY float32
	OneWay         bool
}

// Move records (dx, dy) as the platform's per-step displacement and translates Body accordingly.
func (p *MovingPlatform) Move(dx, dy float32) {
	if p == nil {
		return
	}
	p.DeltaX = dx
	p.DeltaY = dy
	if p.Body != nil {
		p.Body.Translate(dx, dy)
	}
}

// CharacterBody wraps a Body with kinematic platformer movement supporting tiles,
// walkable and steep slopes, downhill snapping, and moving platforms.
type CharacterBody struct {
	Body *Body
	// MaxSlopeAngle is the steepest walkable slope angle in radians.
	// Values <= 0 default to DefaultMaxSlopeAngle (45 degrees).
	MaxSlopeAngle float32
	// SnapDist is the maximum downward distance to snap onto a walkable slope while walking downhill.
	SnapDist float32
	// Grounded reports whether the character is supported by a tile, walkable slope, or platform.
	Grounded bool
	// GroundNormal is the unit surface normal of the ground supporting the character, or zero when airborne.
	GroundNormal core.Point
	// Riding is the MovingPlatform currently carrying the character, or nil.
	Riding *MovingPlatform
	// World is an optional physics World whose GravityX/GravityY and Triggers
	// override gravity and damping for this character during Move.
	World *World
	// GravityScale multiplies the effective gravity applied when World is set.
	// NewCharacterBody initializes it to 1; on a zero-initialized CharacterBody
	// literal it defaults to 1 unless SetGravityScale(0) is called.
	GravityScale float32

	gravityScaleSet bool
}

// NewCharacterBody returns a CharacterBody wrapping body with DefaultMaxSlopeAngle and a default SnapDist of 8.
func NewCharacterBody(body *Body) *CharacterBody {
	return &CharacterBody{
		Body:            body,
		MaxSlopeAngle:   DefaultMaxSlopeAngle,
		SnapDist:        8,
		GravityScale:    1,
		gravityScaleSet: true,
	}
}

// EffectiveGravity returns the scaled gravity acceleration acting on c from
// c.World and any overlapping Triggers, or (0, 0) when c.World is nil.
func (c *CharacterBody) EffectiveGravity() (gx, gy float32) {
	if c == nil || c.Body == nil || c.World == nil || c.Body.Shape.Empty() {
		return 0, 0
	}
	eff := c.World.AreaEffectAtShape(c.Body.Shape, c.Body.Layer, c.Body.Mask)
	scale := c.EffectiveGravityScale()
	return eff.GravityX * scale, eff.GravityY * scale
}

// Move advances c by carrying it with any riding MovingPlatform and then moving by (Body.VX*dt, Body.VY*dt),
// resolving solid and one-way tiles, moving platforms, walkable slopes (up to MaxSlopeAngle),
// downhill slope snapping (up to SnapDist), and blocking steeper slopes.
// When c.World is non-nil, Move integrates effective gravity (world gravity combined
// with overlapping Trigger.Area overrides, scaled by GravityScale) along with area
// linear damping and fluid linear drag into Body.VX/Body.VY before moving.
func (c *CharacterBody) Move(grid TileGrid, slopes []Slope, platforms []*MovingPlatform, dt time.Duration) MoveResult {
	if c == nil || c.Body == nil {
		return MoveResult{}
	}
	b := c.Body
	wasGrounded := c.Grounded
	prevRiding := c.Riding

	c.Grounded = false
	c.GroundNormal = core.Point{}
	c.Riding = nil
	b.Grounded = false
	b.OnCeiling = false
	b.OnLeftWall = false
	b.OnRightWall = false

	if b.Shape.Empty() || dt <= 0 {
		return MoveResult{}
	}

	secs := dt.Seconds()
	if c.World != nil && !math.IsNaN(secs) && !math.IsInf(secs, 0) && secs > 0 {
		eff := c.World.AreaEffectAtShape(b.Shape, b.Layer, b.Mask)
		gScale := float64(c.EffectiveGravityScale())
		vx := float64(b.VX) + float64(float64(float64(eff.GravityX)*gScale)*secs)
		vy := float64(b.VY) + float64(float64(float64(eff.GravityY)*gScale)*secs)
		if eff.FluidLinearDrag > 0 {
			fvx, fvy := float64(eff.FluidVelX), float64(eff.FluidVelY)
			factor := 1.0 / (1.0 + float64(float64(eff.FluidLinearDrag)*secs))
			vx = fvx + float64((vx-fvx)*factor)
			vy = fvy + float64((vy-fvy)*factor)
		}
		if eff.HasLinearDamping && eff.LinearDamping > 0 {
			factor := 1.0 / (1.0 + float64(float64(eff.LinearDamping)*secs))
			vx *= factor
			vy *= factor
		}
		b.VX = float32(vx)
		b.VY = float32(vy)
	}

	maxSlope := float64(c.MaxSlopeAngle)
	if maxSlope <= 0 {
		maxSlope = float64(DefaultMaxSlopeAngle)
	}
	snapDist := float64(c.SnapDist)
	if snapDist < 0 {
		snapDist = 0
	}

	var hasGrid bool
	var tw, th, ox, oy float64
	var cols, rows int
	if grid != nil {
		tileW, tileH := grid.TileSize()
		gc, gr := grid.GridSize()
		if tileW > 0 && tileH > 0 && gc > 0 && gr > 0 {
			hasGrid = true
			tw, th = float64(tileW), float64(tileH)
			ox, oy = tileGridOrigin(grid)
			cols, rows = gc, gr
		}
	}

	box := b.Shape.Bounds()
	x, y := float64(box.X), float64(box.Y)
	w, h := float64(box.W), float64(box.H)

	var contacts []TileContact
	addContact := func(tc TileContact) {
		for _, existing := range contacts {
			if existing.Col == tc.Col && existing.Row == tc.Row &&
				existing.NormalX == tc.NormalX && existing.NormalY == tc.NormalY {
				return
			}
		}
		contacts = append(contacts, tc)
	}

	resolveTilesX := func(dx float64) bool {
		if !hasGrid || dx == 0 {
			return false
		}
		yEps := spanEpsilon(y, h)
		minRow := max(0, int(math.Floor((y-oy+yEps)/th)))
		maxRow := min(rows-1, int(math.Ceil((y-oy+h-yEps)/th))-1)
		xEps := spanEpsilon(x, w)
		minCol := max(0, int(math.Floor((x-ox+xEps)/tw)))
		maxCol := min(cols-1, int(math.Ceil((x-ox+w-xEps)/tw))-1)
		if minRow > maxRow || minCol > maxCol {
			return false
		}
		if dx > 0 {
			hitCol := -1
			for col := minCol; col <= maxCol; col++ {
				for row := minRow; row <= maxRow; row++ {
					if flags := grid.TileFlagsAt(col, row); flags&TileSolid != 0 {
						hitCol = col
						addContact(TileContact{Col: col, Row: row, Flags: flags, NormalX: -1})
					}
				}
				if hitCol >= 0 {
					x = ox + float64(float64(hitCol)*tw) - w
					b.OnRightWall = true
					return true
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
					x = ox + float64(hitCol+1)*tw
					b.OnLeftWall = true
					return true
				}
			}
		}
		return false
	}

	resolveTilesY := func(prevBottom, dy float64) (hitFloor, hitCeil bool) {
		if !hasGrid || dy == 0 {
			return false, false
		}
		xEps := spanEpsilon(x, w)
		minCol := max(0, int(math.Floor((x-ox+xEps)/tw)))
		maxCol := min(cols-1, int(math.Ceil((x-ox+w-xEps)/tw))-1)
		yEps := spanEpsilon(y, h)
		minRow := max(0, int(math.Floor((y-oy+yEps)/th)))
		maxRow := min(rows-1, int(math.Ceil((y-oy+h-yEps)/th))-1)
		if minCol > maxCol || minRow > maxRow {
			return false, false
		}
		if dy > 0 {
			hitRow := -1
			for row := minRow; row <= maxRow; row++ {
				tileTop := oy + float64(float64(row)*th)
				oneWayEps := max(1e-4, float64(1e-5*math.Abs(tileTop)))
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
					y = oy + float64(float64(hitRow)*th) - h
					return true, false
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
					y = oy + float64(hitRow+1)*th
					b.OnCeiling = true
					return false, true
				}
			}
		}
		return false, false
	}

	overlapsSolidTile := func(tx, ty float64) bool {
		if !hasGrid {
			return false
		}
		xEps := spanEpsilon(tx, w)
		yEps := spanEpsilon(ty, h)
		minCol := max(0, int(math.Floor((tx-ox+xEps)/tw)))
		maxCol := min(cols-1, int(math.Ceil((tx-ox+w-xEps)/tw))-1)
		minRow := max(0, int(math.Floor((ty-oy+yEps)/th)))
		maxRow := min(rows-1, int(math.Ceil((ty-oy+h-yEps)/th))-1)
		for row := minRow; row <= maxRow; row++ {
			for col := minCol; col <= maxCol; col++ {
				if grid.TileFlagsAt(col, row)&TileSolid != 0 {
					return true
				}
			}
		}
		return false
	}

	// 1. Carry character with the platform it was riding, unless jumping upward off it.
	var carriedPlatform *MovingPlatform
	if prevRiding != nil && prevRiding.Body != nil && !prevRiding.Body.Shape.Empty() && CanCollide(b, prevRiding.Body) {
		for _, p := range platforms {
			if p == prevRiding {
				carriedPlatform = p
				break
			}
		}
	}
	if carriedPlatform != nil && b.VY >= 0 {
		cdx := float64(carriedPlatform.DeltaX)
		cdy := float64(carriedPlatform.DeltaY)
		if !math.IsNaN(cdx) && !math.IsInf(cdx, 0) && cdx != 0 {
			x += cdx
			resolveTilesX(cdx)
		}
		if !math.IsNaN(cdy) && !math.IsInf(cdy, 0) && cdy != 0 {
			prevBottom := y + h
			y += cdy
			resolveTilesY(prevBottom, cdy)
		}
	}

	// Handle platforms that swept into the character while not already riding them.
	for _, p := range platforms {
		if p == nil || p == carriedPlatform || p.Body == nil || p.Body == b || p.Body.Shape.Empty() {
			continue
		}
		if !CanCollide(b, p.Body) {
			continue
		}
		pb := p.Body.Bounds()
		pLeft, pTop := float64(pb.X), float64(pb.Y)
		pRight, pBottom := float64(pb.X+pb.W), float64(pb.Y+pb.H)
		xEps := spanEpsilon(x, w)
		yEps := spanEpsilon(y, h)

		// Rising platform catches character's feet.
		if p.DeltaY < 0 && x+w > pLeft+xEps && x < pRight-xEps {
			if !p.OneWay || !b.OneWayDrop {
				prevPTop := pTop - float64(p.DeltaY)
				if y+h <= prevPTop+1e-3 && y+h > pTop {
					prevBottom := y + h
					y = pTop - h
					resolveTilesY(prevBottom, -1)
					c.Grounded = true
					c.GroundNormal = core.Point{X: 0, Y: -1}
					c.Riding = p
					if b.VY > 0 {
						b.VY = 0
					}
				}
			}
		}

		// Horizontally moving solid platform pushes character out.
		if !p.OneWay && p.DeltaX != 0 && y+h > pTop+yEps && y < pBottom-yEps {
			if p.DeltaX > 0 {
				prevPRight := pRight - float64(p.DeltaX)
				if x >= prevPRight-1e-3 && x < pRight {
					x = pRight
					resolveTilesX(1)
				}
			} else {
				prevPLeft := pLeft - float64(p.DeltaX)
				if x+w <= prevPLeft+1e-3 && x+w > pLeft {
					x = pLeft - w
					resolveTilesX(-1)
				}
			}
		}
	}

	// 2. Subdivide character's own displacement to prevent tunneling through tiles, platforms, or slopes.
	totalDX := float64(b.VX) * secs
	totalDY := float64(b.VY) * secs
	if math.IsNaN(totalDX) || math.IsInf(totalDX, 0) {
		totalDX = 0
	}
	if math.IsNaN(totalDY) || math.IsInf(totalDY, 0) {
		totalDY = 0
	}

	steps := 1
	if hasGrid {
		if sx := int(math.Ceil(math.Abs(totalDX) / (tw * 0.5))); sx > steps {
			steps = sx
		}
		if sy := int(math.Ceil(math.Abs(totalDY) / (th * 0.5))); sy > steps {
			steps = sy
		}
	}
	if len(slopes) > 0 || len(platforms) > 0 {
		maxStepX := max(2.0, w*0.5)
		maxStepY := max(2.0, h*0.5)
		if sx := int(math.Ceil(math.Abs(totalDX) / maxStepX)); sx > steps {
			steps = sx
		}
		if sy := int(math.Ceil(math.Abs(totalDY) / maxStepY)); sy > steps {
			steps = sy
		}
	}

	stepDX := totalDX / float64(steps)
	stepDY := totalDY / float64(steps)
	const angleEps = 1e-5

	for range steps {
		// 2a. Horizontal step.
		if stepDX != 0 {
			prevX := x
			prevY := y
			prevBottom := y + h
			x += stepDX

			if resolveTilesX(stepDX) {
				b.VX = 0
				stepDX = 0
			}

			// Resolve horizontally against solid moving platforms.
			if stepDX != 0 {
				yEps := spanEpsilon(y, h)
				for _, p := range platforms {
					if p == nil || p.OneWay || p.Body == nil || p.Body == b || p.Body.Shape.Empty() {
						continue
					}
					if !CanCollide(b, p.Body) {
						continue
					}
					pb := p.Body.Bounds()
					pLeft, pTop := float64(pb.X), float64(pb.Y)
					pRight, pBottom := float64(pb.X+pb.W), float64(pb.Y+pb.H)
					if y+h <= pTop+yEps || y >= pBottom-yEps {
						continue
					}
					if stepDX > 0 && x+w > pLeft && prevX+w <= pLeft+1e-3 {
						x = pLeft - w
						b.OnRightWall = true
						b.VX = 0
						stepDX = 0
						break
					}
					if stepDX < 0 && x < pRight && prevX >= pRight-1e-3 {
						x = pRight
						b.OnLeftWall = true
						b.VX = 0
						stepDX = 0
						break
					}
				}
			}

			// Resolve horizontally against steep (unwalkable) slopes.
			if stepDX != 0 {
				for _, s := range slopes {
					ax, ay, bx, by, ok := s.ordered()
					if !ok {
						continue
					}
					angle := detmath.Atan2(math.Abs(by-ay), bx-ax)
					if angle <= maxSlope+angleEps {
						continue
					}
					minSY, maxSY := min(ay, by), max(ay, by)
					if y+h <= minSY+1e-4 || y >= maxSY-1e-4 {
						continue
					}
					if bx == ax {
						if stepDX > 0 && prevX+w <= ax+1e-3 && x+w > ax {
							x = ax - w
							b.OnRightWall = true
							b.VX = 0
							stepDX = 0
						} else if stepDX < 0 && prevX >= ax-1e-3 && x < ax {
							x = ax
							b.OnLeftWall = true
							b.VX = 0
							stepDX = 0
						}
						continue
					}
					ovL := max(x, ax)
					ovR := min(x+w, bx)
					if ovL > ovR {
						continue
					}
					surfY := min(slopeYAt(ax, ay, bx, by, ovL), slopeYAt(ax, ay, bx, by, ovR))
					bottom := y + h
					if bottom <= surfY+1e-4 {
						continue
					}
					t := (bottom - ay) / (by - ay)
					t = min(max(t, 0), 1)
					xHit := ax + float64(t*(bx-ax))
					if by < ay && stepDX > 0 && prevX+w <= xHit+1e-2 && x+w > xHit {
						x = xHit - w
						b.OnRightWall = true
						b.VX = 0
						stepDX = 0
					} else if by > ay && stepDX < 0 && prevX >= xHit-1e-2 && x < xHit {
						x = xHit
						b.OnLeftWall = true
						b.VX = 0
						stepDX = 0
					}
				}
			}

			// Climb walkable slopes smoothly when not jumping upward.
			if b.VY >= 0 {
				bestSurfY := math.Inf(1)
				var bestNormal core.Point
				climbed := false

				for _, s := range slopes {
					ax, ay, bx, by, ok := s.ordered()
					if !ok || bx == ax {
						continue
					}
					angle := detmath.Atan2(math.Abs(by-ay), bx-ax)
					if angle > maxSlope+angleEps {
						continue
					}
					ovL := max(x, ax)
					ovR := min(x+w, bx)
					if ovL > ovR {
						continue
					}
					surfY := min(slopeYAt(ax, ay, bx, by, ovL), slopeYAt(ax, ay, bx, by, ovR))
					if y+h <= surfY+1e-5 {
						continue
					}
					// Verify character approached from on/above the slope rather than jumping from below.
					var refY float64
					switch {
					case prevX+w <= ax+1e-4:
						refY = ay
					case prevX >= bx-1e-4:
						refY = by
					default:
						pOvL := max(prevX, ax)
						pOvR := min(prevX+w, bx)
						refY = min(slopeYAt(ax, ay, bx, by, pOvL), slopeYAt(ax, ay, bx, by, pOvR))
					}
					climbTol := max(0.5, float64(math.Abs(x-prevX)*detmath.Tan(maxSlope))+0.5)
					if prevBottom <= refY+climbTol && surfY < bestSurfY {
						bestSurfY = surfY
						bestNormal = s.Normal()
						climbed = true
					}
				}

				if climbed {
					newY := bestSurfY - h
					if overlapsSolidTile(x, newY) {
						// Blocked by ceiling while trying to climb slope: revert horizontal step.
						x = prevX
						y = prevY
						if stepDX > 0 {
							b.OnRightWall = true
						} else if stepDX < 0 {
							b.OnLeftWall = true
						}
						b.VX = 0
						stepDX = 0
					} else {
						y = newY
						c.Grounded = true
						c.GroundNormal = bestNormal
						c.Riding = nil
						if b.VY > 0 {
							b.VY = 0
						}
						if stepDY > 0 {
							stepDY = 0
						}
					}
				}
			}
		}

		// 2b. Vertical step.
		if stepDY != 0 {
			prevBottom := y + h
			prevTop := y
			y += stepDY

			hitFloor, hitCeil := resolveTilesY(prevBottom, stepDY)
			if hitFloor {
				c.Grounded = true
				c.GroundNormal = core.Point{X: 0, Y: -1}
				c.Riding = nil
				b.VY = 0
				stepDY = 0
			} else if hitCeil {
				b.VY = 0
				stepDY = 0
			}

			// Resolve vertically against moving platforms.
			xEps := spanEpsilon(x, w)
			if stepDY > 0 || (hitFloor && totalDY > 0) {
				for _, p := range platforms {
					if p == nil || p.Body == nil || p.Body == b || p.Body.Shape.Empty() {
						continue
					}
					if !CanCollide(b, p.Body) {
						continue
					}
					pb := p.Body.Bounds()
					pLeft, pTop := float64(pb.X), float64(pb.Y)
					pRight := float64(pb.X + pb.W)
					if x+w <= pLeft+xEps || x >= pRight-xEps {
						continue
					}
					oneWayEps := max(1e-3, 1e-5*math.Abs(pTop))
					solid := !p.OneWay
					oneWay := p.OneWay && !b.OneWayDrop && prevBottom <= pTop+oneWayEps
					if (solid || oneWay) && prevBottom <= pTop+oneWayEps && y+h >= pTop-1e-4 {
						if pTop-h <= y+1e-4 {
							y = pTop - h
							c.Grounded = true
							c.GroundNormal = core.Point{X: 0, Y: -1}
							c.Riding = p
							b.VY = 0
							stepDY = 0
						}
					}
				}
			} else if stepDY < 0 {
				for _, p := range platforms {
					if p == nil || p.OneWay || p.Body == nil || p.Body == b || p.Body.Shape.Empty() {
						continue
					}
					if !CanCollide(b, p.Body) {
						continue
					}
					pb := p.Body.Bounds()
					pLeft, pRight := float64(pb.X), float64(pb.X+pb.W)
					pBottom := float64(pb.Y + pb.H)
					if x+w <= pLeft+xEps || x >= pRight-xEps {
						continue
					}
					if prevTop >= pBottom-1e-3 && y < pBottom {
						y = pBottom
						b.OnCeiling = true
						b.VY = 0
						stepDY = 0
					}
				}
			}

			// Resolve downward motion against slopes.
			if totalDY > 0 {
				for _, s := range slopes {
					ax, ay, bx, by, ok := s.ordered()
					if !ok || bx == ax {
						continue
					}
					ovL := max(x, ax)
					ovR := min(x+w, bx)
					if ovL > ovR {
						continue
					}
					yL := slopeYAt(ax, ay, bx, by, ovL)
					yR := slopeYAt(ax, ay, bx, by, ovR)
					surfY := min(yL, yR)
					maxY := max(yL, yR)
					angle := detmath.Atan2(math.Abs(by-ay), bx-ax)

					if angle <= maxSlope+angleEps {
						if prevBottom <= maxY+1e-2 && y+h >= surfY-1e-4 && surfY-h <= y+1e-4 {
							y = surfY - h
							c.Grounded = true
							c.GroundNormal = s.Normal()
							c.Riding = nil
							b.VY = 0
							stepDY = 0
						}
					} else if prevBottom <= maxY+1e-2 && y+h > surfY+1e-4 {
						// Falling onto a steep slope: push horizontally downhill without grounding.
						t := ((y + h) - ay) / (by - ay)
						t = min(max(t, 0), 1)
						xHit := ax + float64(t*(bx-ax))
						if by < ay {
							x = min(x, xHit-w)
						} else if by > ay {
							x = max(x, xHit)
						}
					}
				}
			}
		}

		if stepDX == 0 && stepDY == 0 {
			break
		}
	}

	// 3. Maintain ground contact when resting flush (e.g. VY == 0 on a moving platform)
	// and snap down onto walkable slopes while walking downhill.
	const restEps = 1e-3
	if b.VY >= 0 && !c.Grounded {
		xEps := spanEpsilon(x, w)

		// 3a. Check resting flush on a moving platform (prioritize previous riding platform).
		checkPlatformRest := func(p *MovingPlatform) bool {
			if p == nil || p.Body == nil || p.Body == b || p.Body.Shape.Empty() {
				return false
			}
			if !CanCollide(b, p.Body) || (p.OneWay && b.OneWayDrop) {
				return false
			}
			pb := p.Body.Bounds()
			pLeft, pTop := float64(pb.X), float64(pb.Y)
			pRight := float64(pb.X + pb.W)
			if x+w <= pLeft+xEps || x >= pRight-xEps {
				return false
			}
			if math.Abs((y+h)-pTop) <= restEps {
				y = pTop - h
				c.Grounded = true
				c.GroundNormal = core.Point{X: 0, Y: -1}
				c.Riding = p
				b.VY = 0
				return true
			}
			return false
		}

		if !checkPlatformRest(carriedPlatform) {
			for _, p := range platforms {
				if p == carriedPlatform {
					continue
				}
				if checkPlatformRest(p) {
					break
				}
			}
		}

		// 3b. Check walkable slopes (resting flush or within SnapDist while walking downhill).
		if !c.Grounded && len(slopes) > 0 {
			snapLimit := restEps
			if wasGrounded && snapDist > snapLimit {
				snapLimit = snapDist
			}
			bestSurfY := math.Inf(1)
			var bestNormal core.Point
			snapped := false
			for _, s := range slopes {
				ax, ay, bx, by, ok := s.ordered()
				if !ok || bx == ax {
					continue
				}
				angle := math.Atan2(math.Abs(by-ay), bx-ax)
				if angle > maxSlope+angleEps {
					continue
				}
				ovL := max(x, ax)
				ovR := min(x+w, bx)
				if ovL > ovR {
					continue
				}
				surfY := min(slopeYAt(ax, ay, bx, by, ovL), slopeYAt(ax, ay, bx, by, ovR))
				gap := surfY - (y + h)
				if gap >= -restEps && gap <= snapLimit && surfY < bestSurfY {
					candY := surfY - h
					if !overlapsSolidTile(x, candY) {
						bestSurfY = surfY
						bestNormal = s.Normal()
						snapped = true
					}
				}
			}
			if snapped {
				y = bestSurfY - h
				c.Grounded = true
				c.GroundNormal = bestNormal
				c.Riding = nil
				b.VY = 0
			}
		}

		// 3c. Check resting flush on solid or one-way tiles.
		if !c.Grounded && hasGrid {
			bottom := y + h
			row := int(math.Floor((bottom - oy + restEps) / th))
			if row >= 0 && row < rows {
				tileTop := oy + float64(float64(row)*th)
				if math.Abs(bottom-tileTop) <= restEps {
					minCol := max(0, int(math.Floor((x-ox+xEps)/tw)))
					maxCol := min(cols-1, int(math.Ceil((x-ox+w-xEps)/tw))-1)
					for col := minCol; col <= maxCol; col++ {
						flags := grid.TileFlagsAt(col, row)
						if flags&TileSolid != 0 || (flags&TileOneWay != 0 && !b.OneWayDrop) {
							y = float64(tileTop) - h
							c.Grounded = true
							c.GroundNormal = core.Point{X: 0, Y: -1}
							b.VY = 0
							break
						}
					}
				}
			}
		}
	}

	// 4. Write resolved position and ground flags back to Body.
	switch b.Shape.Kind {
	case ShapeBox:
		b.Shape.Box.X = float32(x)
		b.Shape.Box.Y = float32(y)
	case ShapeCircle:
		b.Shape.Circle.X = float32(x + float64(b.Shape.Circle.R))
		b.Shape.Circle.Y = float32(y + float64(b.Shape.Circle.R))
	}
	b.Grounded = c.Grounded

	return MoveResult{
		Contacts:    contacts,
		Grounded:    c.Grounded,
		OnCeiling:   b.OnCeiling,
		OnLeftWall:  b.OnLeftWall,
		OnRightWall: b.OnRightWall,
	}
}
