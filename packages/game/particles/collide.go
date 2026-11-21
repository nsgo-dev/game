package particles

import "math"

// CollisionMode controls how a particle responds when it hits a Collider.
type CollisionMode uint8

const (
	// CollideNone disables collision response.
	CollideNone CollisionMode = iota
	// CollideStop halts the particle at the contact point with zero velocity.
	CollideStop
	// CollideBounce reflects the particle's velocity using Bounce (restitution) and Friction.
	CollideBounce
	// CollideKill immediately expires the particle upon impact.
	CollideKill
)

// String returns the canonical string name of the collision mode.
func (m CollisionMode) String() string {
	switch m {
	case CollideStop:
		return "stop"
	case CollideBounce:
		return "bounce"
	case CollideKill:
		return "kill"
	default:
		return "none"
	}
}

// Collider resolves a particle's movement from (prevX, prevY) to (x, y) with the given radius.
// When hit is true, (hitX, hitY) is the resolved particle center position and (nx, ny) is the unit surface normal pointing out of the obstacle.
type Collider interface {
	ResolveParticle(prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32)
}

// CollisionConfig configures particle collision detection and response on an Emitter.
type CollisionConfig struct {
	// Enabled turns collision checking on during Update when true and Collider is non-nil.
	Enabled bool
	// Mode controls the particle's response on impact.
	Mode CollisionMode
	// Bounce is the restitution factor applied to the normal velocity component in CollideBounce mode.
	Bounce float32
	// Friction damps tangential velocity on impact in CollideBounce mode, clamped to [0, 1].
	Friction float32
	// Radius is the particle's collision radius in points (>= 0).
	Radius float32
	// Collider tests and resolves particle contacts. Not serialized in JSON configs.
	Collider Collider
}

// TileGridCollider resolves particle collisions against a uniform 2D tile grid via a SolidAt lookup callback.
type TileGridCollider struct {
	// TileSize is the width and height of one tile in points. Non-positive disables collisions.
	TileSize float32
	// OriginX and OriginY are the world coordinates of tile (0, 0)'s top-left corner.
	OriginX float32
	OriginY float32
	// SolidAt reports whether the tile at (col, row) is solid.
	SolidAt func(col, row int) bool
}

// ResolveParticle implements Collider with zero heap allocations.
func (c *TileGridCollider) ResolveParticle(prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32) {
	if c == nil || c.TileSize <= 0 || c.SolidAt == nil {
		return false, x, y, 0, 0
	}
	if radius < 0 {
		radius = 0
	}
	invSize := 1 / c.TileSize

	if radius == 0 {
		return c.resolvePoint(prevX, prevY, x, y, invSize)
	}
	return c.resolveCircle(prevX, prevY, x, y, radius, invSize)
}

func (c *TileGridCollider) resolvePoint(prevX, prevY, x, y, invSize float32) (hit bool, hitX, hitY, nx, ny float32) {
	pcol := fastFloor((prevX - c.OriginX) * invSize)
	prow := fastFloor((prevY - c.OriginY) * invSize)
	col := fastFloor((x - c.OriginX) * invSize)
	row := fastFloor((y - c.OriginY) * invSize)

	if pcol == col && prow == row {
		if !c.SolidAt(col, row) {
			return false, x, y, 0, 0
		}
		tx := c.OriginX + float32(col)*c.TileSize
		ty := c.OriginY + float32(row)*c.TileSize
		return resolveRect(tx, ty, c.TileSize, c.TileSize, prevX, prevY, x, y, 0)
	}

	// 2D grid DDA from (prevX, prevY) to (x, y) to catch the first solid tile crossed.
	dx := x - prevX
	dy := y - prevY

	var stepX, stepY int
	var tMaxX, tMaxY, tDeltaX, tDeltaY float32

	if dx > 0 {
		stepX = 1
		nextX := c.OriginX + float32(pcol+1)*c.TileSize
		tMaxX = (nextX - prevX) / dx
		tDeltaX = c.TileSize / dx
	} else if dx < 0 {
		stepX = -1
		nextX := c.OriginX + float32(pcol)*c.TileSize
		tMaxX = (nextX - prevX) / dx
		tDeltaX = -c.TileSize / dx
	} else {
		tMaxX = math.MaxFloat32
		tDeltaX = math.MaxFloat32
	}

	if dy > 0 {
		stepY = 1
		nextY := c.OriginY + float32(prow+1)*c.TileSize
		tMaxY = (nextY - prevY) / dy
		tDeltaY = c.TileSize / dy
	} else if dy < 0 {
		stepY = -1
		nextY := c.OriginY + float32(prow)*c.TileSize
		tMaxY = (nextY - prevY) / dy
		tDeltaY = -c.TileSize / dy
	} else {
		tMaxY = math.MaxFloat32
		tDeltaY = math.MaxFloat32
	}

	curCol, curRow := pcol, prow
	const maxSteps = 64
	for range maxSteps {
		var tHit float32
		var hitNX, hitNY float32
		if tMaxX < tMaxY {
			if tMaxX > 1 {
				break
			}
			tHit = tMaxX
			tMaxX += tDeltaX
			curCol += stepX
			hitNX = float32(-stepX)
		} else {
			if tMaxY > 1 {
				break
			}
			tHit = tMaxY
			tMaxY += tDeltaY
			curRow += stepY
			hitNY = float32(-stepY)
		}
		if c.SolidAt(curCol, curRow) {
			if tHit < 0 {
				tHit = 0
			}
			hx := prevX + dx*tHit
			hy := prevY + dy*tHit
			// Snap to exact tile face to avoid floating-point drift into the tile.
			if hitNX < 0 {
				hx = c.OriginX + float32(curCol)*c.TileSize
			} else if hitNX > 0 {
				hx = c.OriginX + float32(curCol+1)*c.TileSize
			}
			if hitNY < 0 {
				hy = c.OriginY + float32(curRow)*c.TileSize
			} else if hitNY > 0 {
				hy = c.OriginY + float32(curRow+1)*c.TileSize
			}
			return true, hx, hy, hitNX, hitNY
		}
		if curCol == col && curRow == row {
			break
		}
	}

	if c.SolidAt(col, row) {
		tx := c.OriginX + float32(col)*c.TileSize
		ty := c.OriginY + float32(row)*c.TileSize
		return resolveRect(tx, ty, c.TileSize, c.TileSize, prevX, prevY, x, y, 0)
	}
	return false, x, y, 0, 0
}

func (c *TileGridCollider) resolveCircle(prevX, prevY, x, y, radius, invSize float32) (hit bool, hitX, hitY, nx, ny float32) {
	curX, curY := x, y
	var nxSum, nySum float32
	anyHit := false

	// Two passes resolve single faces first (deepest penetration to avoid internal tile seam snags)
	// and handle inside corners cleanly in the second pass.
	for range 2 {
		minCol := fastFloor((curX - radius - c.OriginX) * invSize)
		maxCol := fastFloor((curX + radius - c.OriginX) * invSize)
		minRow := fastFloor((curY - radius - c.OriginY) * invSize)
		maxRow := fastFloor((curY + radius - c.OriginY) * invSize)

		// Guard against pathological radii.
		if maxCol-minCol > 16 {
			maxCol = minCol + 16
		}
		if maxRow-minRow > 16 {
			maxRow = minRow + 16
		}

		bestFound := false
		bestDistSq := float32(math.MaxFloat32)
		var bestHX, bestHY, bestNX, bestNY float32

		for tr := minRow; tr <= maxRow; tr++ {
			ty := c.OriginY + float32(tr)*c.TileSize
			for tc := minCol; tc <= maxCol; tc++ {
				if !c.SolidAt(tc, tr) {
					continue
				}
				tx := c.OriginX + float32(tc)*c.TileSize
				cx := clampRange(curX, tx, tx+c.TileSize)
				cy := clampRange(curY, ty, ty+c.TileSize)
				dx := curX - cx
				dy := curY - cy
				distSq := dx*dx + dy*dy
				if distSq > radius*radius {
					continue
				}
				if distSq < bestDistSq {
					h, hx, hy, hnx, hny := resolveRect(tx, ty, c.TileSize, c.TileSize, prevX, prevY, curX, curY, radius)
					if h {
						bestFound = true
						bestDistSq = distSq
						bestHX, bestHY = hx, hy
						bestNX, bestNY = hnx, hny
					}
				}
			}
		}

		if !bestFound {
			break
		}
		anyHit = true
		curX, curY = bestHX, bestHY
		nxSum += bestNX
		nySum += bestNY
	}

	if anyHit {
		nnx, nny := normDir(nxSum, nySum)
		if nnx == 0 && nny == 0 {
			nnx, nny = 0, -1
		}
		return true, curX, curY, nnx, nny
	}

	// Swept fallback if the particle moved more than TileSize in a single step.
	moveDx, moveDy := x-prevX, y-prevY
	if moveDx*moveDx+moveDy*moveDy > c.TileSize*c.TileSize {
		if h, hx, hy, hnx, hny := c.resolvePoint(prevX, prevY, x, y, invSize); h {
			return true, hx + hnx*radius, hy + hny*radius, hnx, hny
		}
	}

	return false, x, y, 0, 0
}

// RectCollider resolves particle collisions against an axis-aligned rectangle [X, X+W] x [Y, Y+H].
type RectCollider struct {
	X, Y, W, H float32
}

// ResolveParticle implements Collider with zero heap allocations.
func (c *RectCollider) ResolveParticle(prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32) {
	if c == nil || c.W <= 0 || c.H <= 0 {
		return false, x, y, 0, 0
	}
	if radius < 0 {
		radius = 0
	}
	return resolveRect(c.X, c.Y, c.W, c.H, prevX, prevY, x, y, radius)
}

func resolveRect(rx, ry, rw, rh, prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32) {
	rx0, rx1 := rx, rx+rw
	ry0, ry1 := ry, ry+rh
	ex0, ex1 := rx0-radius, rx1+radius
	ey0, ey1 := ry0-radius, ry1+radius

	cx := clampRange(x, rx0, rx1)
	cy := clampRange(y, ry0, ry1)
	dx := x - cx
	dy := y - cy
	distSq := dx*dx + dy*dy

	insideCore := x > rx0 && x < rx1 && y > ry0 && y < ry1

	if !insideCore && distSq <= radius*radius {
		if distSq > 1e-12 {
			dist := float32(math.Sqrt(float64(distSq)))
			nx = dx / dist
			ny = dy / dist
			return true, cx + nx*radius, cy + ny*radius, nx, ny
		}
		// Exactly on the rect boundary (typically radius == 0).
		if h, hx, hy, snx, sny := slabIntersect(ex0, ey0, ex1, ey1, prevX, prevY, x, y); h {
			return true, hx, hy, snx, sny
		}
		return pushOutAABB(ex0, ey0, ex1, ey1, x, y)
	}

	if insideCore {
		if h, hx, hy, snx, sny := slabIntersect(ex0, ey0, ex1, ey1, prevX, prevY, x, y); h {
			return true, hx, hy, snx, sny
		}
		return pushOutAABB(ex0, ey0, ex1, ey1, x, y)
	}

	// Swept test to prevent tunneling through thin rectangles.
	if prevX != x || prevY != y {
		if h, hx, hy, snx, sny := slabIntersect(ex0, ey0, ex1, ey1, prevX, prevY, x, y); h {
			return true, hx, hy, snx, sny
		}
	}

	return false, x, y, 0, 0
}

func slabIntersect(minX, minY, maxX, maxY, prevX, prevY, x, y float32) (hit bool, hitX, hitY, nx, ny float32) {
	// If already strictly inside at prev, let pushOut handle it.
	if prevX > minX && prevX < maxX && prevY > minY && prevY < maxY {
		return false, x, y, 0, 0
	}

	dx := x - prevX
	dy := y - prevY

	tMin := float32(0)
	tMax := float32(1)
	var hitNX, hitNY float32

	if dx == 0 {
		if prevX < minX || prevX > maxX {
			return false, x, y, 0, 0
		}
	} else {
		invD := 1 / dx
		t1 := (minX - prevX) * invD
		t2 := (maxX - prevX) * invD
		n1 := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			n1 = 1
		}
		if t1 > tMin {
			tMin = t1
			hitNX = n1
			hitNY = 0
		}
		if t2 < tMax {
			tMax = t2
		}
		if tMin > tMax {
			return false, x, y, 0, 0
		}
	}

	if dy == 0 {
		if prevY < minY || prevY > maxY {
			return false, x, y, 0, 0
		}
	} else {
		invD := 1 / dy
		t1 := (minY - prevY) * invD
		t2 := (maxY - prevY) * invD
		n1 := float32(-1)
		if t1 > t2 {
			t1, t2 = t2, t1
			n1 = 1
		}
		if t1 > tMin {
			tMin = t1
			hitNX = 0
			hitNY = n1
		}
		if t2 < tMax {
			tMax = t2
		}
		if tMin > tMax {
			return false, x, y, 0, 0
		}
	}

	if hitNX == 0 && hitNY == 0 {
		// Started directly on a boundary face and moved inward.
		switch {
		case prevX <= minX && dx > 0:
			hitNX = -1
		case prevX >= maxX && dx < 0:
			hitNX = 1
		case prevY <= minY && dy > 0:
			hitNY = -1
		case prevY >= maxY && dy < 0:
			hitNY = 1
		default:
			return false, x, y, 0, 0
		}
	}

	return true, prevX + dx*tMin, prevY + dy*tMin, hitNX, hitNY
}

func pushOutAABB(minX, minY, maxX, maxY, x, y float32) (bool, float32, float32, float32, float32) {
	dLeft := x - minX
	dRight := maxX - x
	dTop := y - minY
	dBottom := maxY - y

	best := dLeft
	hx, hy := minX, y
	nx, ny := float32(-1), float32(0)

	if dRight < best {
		best = dRight
		hx, hy = maxX, y
		nx, ny = 1, 0
	}
	if dTop < best {
		best = dTop
		hx, hy = x, minY
		nx, ny = 0, -1
	}
	if dBottom < best {
		hx, hy = x, maxY
		nx, ny = 0, 1
	}
	return true, hx, hy, nx, ny
}

// CircleCollider resolves particle collisions against a solid circle centered at (X, Y) with Radius.
type CircleCollider struct {
	X, Y, Radius float32
}

// ResolveParticle implements Collider with zero heap allocations.
func (c *CircleCollider) ResolveParticle(prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32) {
	if c == nil || c.Radius <= 0 {
		return false, x, y, 0, 0
	}
	if radius < 0 {
		radius = 0
	}
	r := c.Radius + radius
	rSq := r * r

	dx := x - c.X
	dy := y - c.Y
	distSq := dx*dx + dy*dy
	if distSq <= rSq {
		if distSq > 1e-12 {
			dist := float32(math.Sqrt(float64(distSq)))
			nx = dx / dist
			ny = dy / dist
			return true, c.X + nx*r, c.Y + ny*r, nx, ny
		}
		nx, ny = normDir(prevX-x, prevY-y)
		if nx == 0 && ny == 0 {
			nx, ny = 0, -1
		}
		return true, c.X + nx*r, c.Y + ny*r, nx, ny
	}

	// Swept segment-vs-circle test to prevent tunneling.
	vx := x - prevX
	vy := y - prevY
	a := vx*vx + vy*vy
	if a > 1e-12 {
		fx := prevX - c.X
		fy := prevY - c.Y
		cc := fx*fx + fy*fy - rSq
		if cc > 0 {
			b := 2 * (fx*vx + fy*vy)
			disc := b*b - 4*a*cc
			if disc >= 0 {
				t := (-b - float32(math.Sqrt(float64(disc)))) / (2 * a)
				if t >= 0 && t <= 1 {
					hx := prevX + t*vx
					hy := prevY + t*vy
					nx, ny = normDir(hx-c.X, hy-c.Y)
					if nx == 0 && ny == 0 {
						nx, ny = 0, -1
					}
					return true, c.X + nx*r, c.Y + ny*r, nx, ny
				}
			}
		}
	}

	return false, x, y, 0, 0
}

// MultiCollider combines multiple Colliders and reports the earliest/first hit.
type MultiCollider []Collider

// ResolveParticle implements Collider with zero heap allocations.
func (m MultiCollider) ResolveParticle(prevX, prevY, x, y, radius float32) (hit bool, hitX, hitY, nx, ny float32) {
	if len(m) == 0 {
		return false, x, y, 0, 0
	}
	bestDistSq := float32(math.MaxFloat32)
	for _, col := range m {
		if col == nil {
			continue
		}
		h, hx, hy, hnx, hny := col.ResolveParticle(prevX, prevY, x, y, radius)
		if !h {
			continue
		}
		ddx := hx - prevX
		ddy := hy - prevY
		dSq := ddx*ddx + ddy*ddy
		if !hit || dSq < bestDistSq {
			hit = true
			bestDistSq = dSq
			hitX, hitY = hx, hy
			nx, ny = hnx, hny
		}
	}
	if !hit {
		return false, x, y, 0, 0
	}
	return hit, hitX, hitY, nx, ny
}

func fastFloor(v float32) int {
	i := int(v)
	if float32(i) > v {
		i--
	}
	return i
}

func clampRange(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
