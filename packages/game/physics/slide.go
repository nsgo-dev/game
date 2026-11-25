package physics

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/packages/game/internal/detmath"
	"github.com/nsgo-dev/nsgo/core"
)

const (
	// DefaultMaxSlides is the default maximum number of slide iterations performed by World.MoveAndSlide.
	DefaultMaxSlides = 4
	// DefaultFloorMaxAngle is the default maximum angle in radians between a contact normal and the up direction
	// for a surface to be classified as a floor (45 degrees, matching DefaultMaxSlopeAngle).
	DefaultFloorMaxAngle float32 = DefaultMaxSlopeAngle

	maxMinkVertices   = 2 * MaxPolygonVertices
	slideParEps       = 1e-12
	slideTouchEps     = 1e-9
	slideOneWayEps    = 1e-3
	slideDepenIters   = 4
	slideDepenNudge   = 1e-7
	slideAngleEps     = 1e-5
	slideSupportTol   = 1e-4
	slidePlaneSameCos = 1.0 - 1e-6
)

// MotionMode controls how World.MoveAndSlideWith classifies contact normals into floor, wall and ceiling states.
type MotionMode uint8

const (
	// MotionGrounded classifies contacts against UpDirection and FloorMaxAngle into floor, wall and ceiling
	// states (the zero value, for side-view platformers).
	MotionGrounded MotionMode = iota
	// MotionFloating treats every contact as a wall without floor or ceiling classification (for top-down games).
	MotionFloating
)

// MoveOptions configures World.MoveAndSlideWith. The zero value uses MotionGrounded with Up (0, -1),
// FloorMaxAngle DefaultFloorMaxAngle (45 degrees), MaxSlides DefaultMaxSlides (4), and collides against
// World.Bodies, World.Tiles (solid and one-way), and static RigidBody colliders in World.RigidWorld.
type MoveOptions struct {
	// Mode selects MotionGrounded (default) or MotionFloating (top-down, every hit is a wall).
	Mode MotionMode
	// UpX and UpY define the upward direction for floor, wall and ceiling classification in MotionGrounded mode
	// (and the pass-through direction for one-way colliders). When both are 0, defaults to (0, -1) (screen up).
	UpX, UpY float32
	// FloorMaxAngle is the steepest angle in radians between a contact normal and Up that counts as a floor
	// (or between a contact normal and -Up that counts as a ceiling). Values <= 0 default to DefaultFloorMaxAngle (45 degrees).
	FloorMaxAngle float32
	// MaxSlides is the maximum number of slide iterations per call. Values <= 0 default to DefaultMaxSlides (4).
	MaxSlides int
	// Mask optionally restricts which collider kinds are tested. Zero defaults to QueryAll
	// (World.Bodies, static RigidBody colliders, TileSolid and TileOneWay tiles).
	Mask QueryMask
	// RigidBodies supplies optional extra static RigidBody colliders to test in addition to World.RigidWorld.
	// Dynamic (non-static) rigid bodies are ignored.
	RigidBodies []*RigidBody
	// Exclude skips the listed Body pointers during movement in addition to the moving body itself.
	Exclude []*Body
}

// SlideCollision describes one contact encountered during World.MoveAndSlide or World.MoveAndSlideWith.
type SlideCollision struct {
	// Body is non-nil when the collision was with a Body in World.Bodies.
	Body *Body
	// RigidBody is non-nil when the collision was with a static RigidBody collider.
	RigidBody *RigidBody
	// Tile is true when the collision was with a tile in World.Tiles at (Col, Row).
	Tile      bool
	Col, Row  int
	TileFlags TileFlags
	// NormalX and NormalY form the unit surface normal pointing from the hit collider toward the moving body.
	NormalX, NormalY float32
	// Position is the world-space contact point on the colliding surface.
	Position core.Point
	// Remainder is the remaining displacement vector at the moment of impact before sliding.
	Remainder core.Point
}

// SlideResult summarizes the contacts, displacement and surface state from one World.MoveAndSlide call.
type SlideResult struct {
	// Collisions lists each contact encountered during the slide iterations, in order of impact.
	Collisions []SlideCollision
	// Travel is the net displacement actually moved by the body during the step.
	Travel core.Point
	// OnFloor reports whether a floor surface was contacted (normal within FloorMaxAngle of Up).
	OnFloor bool
	// OnWall reports whether a wall surface was contacted.
	OnWall bool
	// OnCeiling reports whether a ceiling surface was contacted (normal within FloorMaxAngle of -Up).
	OnCeiling bool
	// OnLeftWall reports whether a wall contact had its normal pointing toward the right side of Up.
	OnLeftWall bool
	// OnRightWall reports whether a wall contact had its normal pointing toward the left side of Up.
	OnRightWall bool
	// FloorNormal is the unit normal of the last floor contact, or zero when OnFloor is false.
	FloorNormal core.Point
	// WallNormal is the unit normal of the last wall contact, or zero when OnWall is false.
	WallNormal core.Point
	// CeilingNormal is the unit normal of the last ceiling contact, or zero when OnCeiling is false.
	CeilingNormal core.Point
}

// MoveAndSlide sweeps body's shape by (body.VX*dt, body.VY*dt) against static and kinematic bodies in w.Bodies,
// solid and one-way tiles in w.Tiles, and static RigidBody colliders in w.RigidWorld, filtered by Layer and Mask.
// It slides along each contact normal for up to DefaultMaxSlides iterations, updates body's position, velocity
// and contact flags (Grounded, OnCeiling, OnLeftWall, OnRightWall), and returns the collisions and surface state.
func (w *World) MoveAndSlide(body *Body, dt time.Duration) SlideResult {
	return w.MoveAndSlideWith(body, dt, MoveOptions{})
}

// MoveAndSlideWith is like MoveAndSlide with configurable motion mode, up direction, floor angle,
// slide iteration limit, query mask, extra static rigid bodies and exclusions.
func (w *World) MoveAndSlideWith(body *Body, dt time.Duration, opts MoveOptions) SlideResult {
	var grid TileGrid
	if w != nil {
		grid = w.Tiles
	}
	return w.moveAndSlideCore(body, dt, opts, grid)
}

func (w *World) moveAndSlideCore(body *Body, dt time.Duration, opts MoveOptions, grid TileGrid) SlideResult {
	if body == nil {
		return SlideResult{}
	}
	body.Grounded = false
	body.OnCeiling = false
	body.OnLeftWall = false
	body.OnRightWall = false

	if body.Shape.Empty() || dt <= 0 {
		return SlideResult{}
	}
	secs := dt.Seconds()
	if math.IsNaN(secs) || math.IsInf(secs, 0) || secs <= 0 {
		return SlideResult{}
	}
	vx := float64(body.VX)
	vy := float64(body.VY)
	if math.IsNaN(vx) || math.IsInf(vx, 0) || math.IsNaN(vy) || math.IsInf(vy, 0) {
		return SlideResult{}
	}
	totalDX := float64(vx * secs)
	totalDY := float64(vy * secs)
	if math.IsNaN(totalDX) || math.IsInf(totalDX, 0) || math.IsNaN(totalDY) || math.IsInf(totalDY, 0) {
		return SlideResult{}
	}

	if w == nil && len(opts.RigidBodies) == 0 && grid == nil {
		translateBodyExact(body, totalDX, totalDY)
		return SlideResult{
			Travel: core.Point{X: core.Points(totalDX), Y: core.Points(totalDY)},
		}
	}

	baseHull, ok := shapeHull(body.Shape)
	if !ok || !baseHull.hasArea() {
		translateBodyExact(body, totalDX, totalDY)
		return SlideResult{
			Travel: core.Point{X: core.Points(totalDX), Y: core.Points(totalDY)},
		}
	}

	cfg := resolveSlideConfig(opts)
	res, offX, offY, outVX, outVY := w.sweepAndSlide(body, &baseHull, totalDX, totalDY, vx, vy, cfg, grid, opts.RigidBodies, opts.Exclude)

	translateBodyExact(body, offX, offY)
	body.VX = float32(outVX)
	body.VY = float32(outVY)
	body.Grounded = res.OnFloor
	body.OnCeiling = res.OnCeiling
	body.OnLeftWall = res.OnLeftWall
	body.OnRightWall = res.OnRightWall
	res.Travel = core.Point{X: core.Points(offX), Y: core.Points(offY)}
	return res
}

type slideConfig struct {
	mode      MotionMode
	upX, upY  float64
	cosFloor  float64
	maxSlides int
	mask      QueryMask
}

func resolveSlideConfig(opts MoveOptions) slideConfig {
	upX, upY := float64(opts.UpX), float64(opts.UpY)
	if math.IsNaN(upX) || math.IsInf(upX, 0) || math.IsNaN(upY) || math.IsInf(upY, 0) {
		upX, upY = 0, -1
	} else if l := detmath.Hypot(upX, upY); l > 0 {
		upX /= l
		upY /= l
	} else {
		upX, upY = 0, -1
	}

	floorAngle := float64(opts.FloorMaxAngle)
	if math.IsNaN(floorAngle) || math.IsInf(floorAngle, 0) || floorAngle <= 0 {
		floorAngle = float64(DefaultFloorMaxAngle)
	}
	if floorAngle > math.Pi*0.5 {
		floorAngle = math.Pi * 0.5
	}
	cosFloor := detmath.Cos(min(math.Pi*0.5, floorAngle+slideAngleEps))

	maxSlides := opts.MaxSlides
	if maxSlides <= 0 {
		maxSlides = DefaultMaxSlides
	}
	mask := opts.Mask
	if mask == 0 {
		mask = QueryAll
	}
	return slideConfig{
		mode:      opts.Mode,
		upX:       upX,
		upY:       upY,
		cosFloor:  cosFloor,
		maxSlides: maxSlides,
		mask:      mask,
	}
}

func (cfg *slideConfig) classify(res *SlideResult, nx, ny float64) {
	if cfg.mode == MotionFloating {
		res.OnWall = true
		res.WallNormal = core.Point{X: core.Points(nx), Y: core.Points(ny)}
		if nx > 1e-4 {
			res.OnLeftWall = true
		} else if nx < -1e-4 {
			res.OnRightWall = true
		}
		return
	}
	upDot := float64(nx*cfg.upX) + float64(ny*cfg.upY)
	if upDot >= cfg.cosFloor {
		res.OnFloor = true
		res.FloorNormal = core.Point{X: core.Points(nx), Y: core.Points(ny)}
		return
	}
	if -upDot >= cfg.cosFloor {
		res.OnCeiling = true
		res.CeilingNormal = core.Point{X: core.Points(nx), Y: core.Points(ny)}
		return
	}
	res.OnWall = true
	res.WallNormal = core.Point{X: core.Points(nx), Y: core.Points(ny)}
	// Right vector perpendicular to up: for standard up=(0,-1), right=(1,0).
	rightX := -cfg.upY
	rightY := cfg.upX
	sideDot := float64(nx*rightX) + float64(ny*rightY)
	if sideDot > 1e-4 {
		res.OnLeftWall = true
	} else if sideDot < -1e-4 {
		res.OnRightWall = true
	}
}

func translateBodyExact(b *Body, dx, dy float64) {
	if dx == 0 && dy == 0 {
		return
	}
	switch b.Shape.Kind {
	case ShapeBox:
		b.Shape.Box.X = float32(float64(b.Shape.Box.X) + dx)
		b.Shape.Box.Y = float32(float64(b.Shape.Box.Y) + dy)
	case ShapeCircle:
		b.Shape.Circle.X = float32(float64(b.Shape.Circle.X) + dx)
		b.Shape.Circle.Y = float32(float64(b.Shape.Circle.Y) + dy)
	default:
		b.Shape = b.Shape.Translated(float32(dx), float32(dy))
	}
}

func translateHull(h *hull, dx, dy float64) hull {
	out := *h
	if dx == 0 && dy == 0 {
		return out
	}
	for i := range out.count {
		out.v[i] = vec2{out.v[i].x + dx, out.v[i].y + dy}
	}
	return out
}

func bodyExcluded(target *Body, exclude []*Body) bool {
	for _, e := range exclude {
		if e == target {
			return true
		}
	}
	return false
}

// worldHasBodyColliders reports whether w has any static/kinematic bodies or static rigid bodies
// other than self and exclude.
func (w *World) worldHasBodyColliders(self *Body, exclude []*Body) bool {
	if w == nil {
		return false
	}
	for _, other := range w.Bodies {
		if other != nil && other != self && !other.Shape.Empty() && !bodyExcluded(other, exclude) {
			return true
		}
	}
	if w.RigidWorld != nil {
		for _, rb := range w.RigidWorld.Bodies {
			if rb != nil && rb.Static && !rb.Shape.Empty() {
				return true
			}
		}
	}
	return false
}

type sweepHit struct {
	hit        bool
	t          float64
	nx, ny     float64
	posX, posY float64
	pushDepth  float64
	body       *Body
	rigid      *RigidBody
	tile       bool
	col, row   int
	tileFlags  TileFlags
}

func (w *World) sweepAndSlide(
	body *Body,
	baseHull *hull,
	totalDX, totalDY float64,
	vx, vy float64,
	cfg slideConfig,
	grid TileGrid,
	extraRigid []*RigidBody,
	exclude []*Body,
) (res SlideResult, offX, offY, outVX, outVY float64) {
	outVX, outVY = vx, vy

	// 1. Pre-sweep depenetration against solid (non-one-way) colliders so float32 rounding
	// from a previous step or an encroaching collider does not leave the mover stuck inside.
	for range slideDepenIters {
		cur := translateHull(baseHull, offX, offY)
		c, found := w.deepestSolidOverlap(body, &cur, cfg.mask, grid, extraRigid, exclude)
		if !found || c.depth <= 0 {
			break
		}
		push := c.depth
		// Add a tiny float64 margin for non-axis-aligned normals so the sweep starts cleanly on the outside.
		if c.nx != 0 && c.ny != 0 {
			push += slideDepenNudge
		}
		offX += float64(c.nx * push)
		offY += float64(c.ny * push)
	}

	remX, remY := totalDX, totalDY
	planes := make([]vec2, 0, cfg.maxSlides)

	for range cfg.maxSlides {
		remLen := detmath.Hypot(remX, remY)
		if remLen <= slideTouchEps {
			break
		}
		cur := translateHull(baseHull, offX, offY)
		hit := w.sweepAll(body, &cur, remX, remY, cfg, grid, extraRigid, exclude)
		if !hit.hit {
			offX += remX
			offY += remY
			break
		}

		// Advance to the exact time of impact (plus any tiny one-way top recovery if grazing).
		if hit.t > 0 {
			offX += float64(remX * hit.t)
			offY += float64(remY * hit.t)
		}
		if hit.pushDepth > 0 {
			offX += float64(hit.nx * hit.pushDepth)
			offY += float64(hit.ny * hit.pushDepth)
		}

		leftFrac := 1.0 - hit.t
		remX = float64(remX * leftFrac)
		remY = float64(remY * leftFrac)

		res.Collisions = append(res.Collisions, SlideCollision{
			Body:      hit.body,
			RigidBody: hit.rigid,
			Tile:      hit.tile,
			Col:       hit.col,
			Row:       hit.row,
			TileFlags: hit.tileFlags,
			NormalX:   float32(hit.nx),
			NormalY:   float32(hit.ny),
			Position:  core.Point{X: core.Points(hit.posX), Y: core.Points(hit.posY)},
			Remainder: core.Point{X: core.Points(remX), Y: core.Points(remY)},
		})
		cfg.classify(&res, hit.nx, hit.ny)

		// Slide remaining displacement along the hit tangent.
		dot := float64(remX*hit.nx) + float64(remY*hit.ny)
		if dot < 0 {
			remX -= float64(dot * hit.nx)
			remY -= float64(dot * hit.ny)
		}
		// Slide velocity along the hit tangent.
		vdot := float64(outVX*hit.nx) + float64(outVY*hit.ny)
		if vdot < 0 {
			outVX -= float64(vdot * hit.nx)
			outVY -= float64(vdot * hit.ny)
		}

		// 2D crease check: if sliding along the new normal pushes back into any distinct earlier
		// contact plane from this step, the body is wedged in a corner and must stop without oscillation.
		remAfterLen := detmath.Hypot(remX, remY)
		vAfterLen := detmath.Hypot(outVX, outVY)
		for _, prev := range planes {
			if float64(prev.x*hit.nx)+float64(prev.y*hit.ny) >= slidePlaneSameCos {
				continue
			}
			if remAfterLen > 0 && float64(remX*prev.x)+float64(remY*prev.y) < -1e-9*remAfterLen {
				remX, remY = 0, 0
				remAfterLen = 0
			}
			if vAfterLen > 0 && float64(outVX*prev.x)+float64(outVY*prev.y) < -1e-9*vAfterLen {
				outVX, outVY = 0, 0
				vAfterLen = 0
			}
		}
		planes = append(planes, vec2{hit.nx, hit.ny})
		if remAfterLen <= slideTouchEps {
			break
		}
	}

	return res, offX, offY, outVX, outVY
}

type overlapRes struct {
	nx, ny float64
	depth  float64
}

func (w *World) deepestSolidOverlap(
	body *Body,
	cur *hull,
	mask QueryMask,
	grid TileGrid,
	extraRigid []*RigidBody,
	exclude []*Body,
) (overlapRes, bool) {
	aMinX, aMinY, aMaxX, aMaxY := cur.bounds()
	var best overlapRes
	found := false

	consider := func(nx, ny, depth float64) {
		if depth > best.depth {
			best = overlapRes{nx: nx, ny: ny, depth: depth}
			found = true
		}
	}

	if w != nil && mask&QueryBodies != 0 {
		for _, other := range w.Bodies {
			if other == nil || other == body || other.OneWay || other.Shape.Empty() || bodyExcluded(other, exclude) {
				continue
			}
			if !CanCollide(body, other) {
				continue
			}
			if !boundsOverlap(aMinX, aMinY, aMaxX, aMaxY, other.Shape.Bounds()) {
				continue
			}
			if nx, ny, depth, ok := overlapHullShape(cur, other.Shape); ok {
				consider(nx, ny, depth)
			}
		}
	}

	if mask&QueryBodies != 0 {
		checkRigid := func(rb *RigidBody) {
			if rb == nil || !rb.Static || rb.Shape.Empty() {
				return
			}
			if !layersInteract(body.Layer, body.Mask, rb.Layer, rb.Mask) {
				return
			}
			if rb.Shape.Kind == ShapeChain {
				ch := staticRigidChain(rb)
				if ch == nil || !boundsOverlap(aMinX, aMinY, aMaxX, aMaxY, ch.Bounds()) {
					return
				}
				if nx, ny, depth, ok := overlapHullChain(cur, ch); ok {
					consider(nx, ny, depth)
				}
				return
			}
			hb, ok := staticRigidHull(rb)
			if !ok {
				return
			}
			bMinX, bMinY, bMaxX, bMaxY := hb.bounds()
			if bMaxX < aMinX || aMaxX < bMinX || bMaxY < aMinY || aMaxY < bMinY {
				return
			}
			m := collideHulls(cur, &hb)
			if m.count > 0 {
				if d := m.maxDepth(); d > 0 {
					consider(-m.nx, -m.ny, d)
				}
			}
		}
		if w != nil && w.RigidWorld != nil {
			for _, rb := range w.RigidWorld.Bodies {
				checkRigid(rb)
			}
		}
		for _, rb := range extraRigid {
			checkRigid(rb)
		}
	}

	if grid != nil && mask&QueryTiles != 0 {
		if w == nil || layersInteract(body.Layer, body.Mask, w.TileLayer, w.TileMask) {
			tileW, tileH := grid.TileSize()
			cols, rows := grid.GridSize()
			if tileW > 0 && tileH > 0 && cols > 0 && rows > 0 {
				tw, th := float64(tileW), float64(tileH)
				ox, oy := tileGridOrigin(grid)
				minCol := max(0, int(math.Floor((aMinX-ox)/tw)))
				maxCol := min(cols-1, int(math.Ceil((aMaxX-ox)/tw))-1)
				minRow := max(0, int(math.Floor((aMinY-oy)/th)))
				maxRow := min(rows-1, int(math.Ceil((aMaxY-oy)/th))-1)
				for row := minRow; row <= maxRow; row++ {
					for col := minCol; col <= maxCol; col++ {
						if grid.TileFlagsAt(col, row)&TileSolid == 0 {
							continue
						}
						tb := Box{
							X: float32(ox + float64(float64(col)*tw)),
							Y: float32(oy + float64(float64(row)*th)),
							W: tileW,
							H: tileH,
						}
						hb := boxHull(tb)
						m := collideHulls(cur, &hb)
						if m.count == 0 {
							continue
						}
						d := m.maxDepth()
						if d <= 0 {
							continue
						}
						nx, ny, ok := clampTileNormal(grid, col, row, cols, rows, -m.nx, -m.ny)
						if !ok {
							continue
						}
						consider(nx, ny, d)
					}
				}
			}
		}
	}

	return best, found
}

func (w *World) sweepAll(
	body *Body,
	cur *hull,
	dx, dy float64,
	cfg slideConfig,
	grid TileGrid,
	extraRigid []*RigidBody,
	exclude []*Body,
) sweepHit {
	dLen := detmath.Hypot(dx, dy)
	if dLen == 0 {
		return sweepHit{}
	}
	aMinX, aMinY, aMaxX, aMaxY := cur.bounds()
	sMinX := min(aMinX, aMinX+dx) - slideOneWayEps
	sMaxX := max(aMaxX, aMaxX+dx) + slideOneWayEps
	sMinY := min(aMinY, aMinY+dy) - slideOneWayEps
	sMaxY := max(aMaxY, aMaxY+dy) + slideOneWayEps

	var best sweepHit
	keep := func(cand sweepHit) {
		if !cand.hit {
			return
		}
		if !best.hit || cand.t < best.t {
			best = cand
		}
	}

	// 1. World.Bodies in slice order.
	if w != nil && cfg.mask&QueryBodies != 0 {
		for _, other := range w.Bodies {
			if other == nil || other == body || other.Shape.Empty() || bodyExcluded(other, exclude) {
				continue
			}
			if other.OneWay && body.OneWayDrop {
				continue
			}
			if !CanCollide(body, other) {
				continue
			}
			if !boundsOverlap(sMinX, sMinY, sMaxX, sMaxY, other.Shape.Bounds()) {
				continue
			}
			cand := sweepHullVsShape(cur, other.Shape, dx, dy, dLen, other.OneWay, cfg)
			if cand.hit {
				cand.body = other
				keep(cand)
			}
		}
	}

	// 2. Static RigidBody colliders (World.RigidWorld then opts.RigidBodies).
	if cfg.mask&QueryBodies != 0 {
		checkRigid := func(rb *RigidBody) {
			if rb == nil || !rb.Static || rb.Shape.Empty() {
				return
			}
			if !layersInteract(body.Layer, body.Mask, rb.Layer, rb.Mask) {
				return
			}
			if rb.Shape.Kind == ShapeChain {
				ch := staticRigidChain(rb)
				if ch == nil || !boundsOverlap(sMinX, sMinY, sMaxX, sMaxY, ch.Bounds()) {
					return
				}
				cand := sweepHullVsChain(cur, ch, dx, dy, dLen, false, cfg)
				if cand.hit {
					cand.rigid = rb
					keep(cand)
				}
				return
			}
			hb, ok := staticRigidHull(rb)
			if !ok {
				return
			}
			bMinX, bMinY, bMaxX, bMaxY := hb.bounds()
			if bMaxX < sMinX || sMaxX < bMinX || bMaxY < sMinY || sMaxY < bMinY {
				return
			}
			cand := sweepHullVsHull(cur, &hb, dx, dy, dLen, false, cfg)
			if cand.hit {
				cand.rigid = rb
				keep(cand)
			}
		}
		if w != nil && w.RigidWorld != nil {
			for _, rb := range w.RigidWorld.Bodies {
				checkRigid(rb)
			}
		}
		for _, rb := range extraRigid {
			checkRigid(rb)
		}
	}

	// 3. TileGrid tiles in row-major order.
	if grid != nil && cfg.mask&(QueryTiles|QueryOneWay) != 0 {
		if w == nil || layersInteract(body.Layer, body.Mask, w.TileLayer, w.TileMask) {
			tileW, tileH := grid.TileSize()
			cols, rows := grid.GridSize()
			if tileW > 0 && tileH > 0 && cols > 0 && rows > 0 {
				tw, th := float64(tileW), float64(tileH)
				ox, oy := tileGridOrigin(grid)
				minCol := max(0, int(math.Floor((sMinX-ox)/tw)))
				maxCol := min(cols-1, int(math.Ceil((sMaxX-ox)/tw))-1)
				minRow := max(0, int(math.Floor((sMinY-oy)/th)))
				maxRow := min(rows-1, int(math.Ceil((sMaxY-oy)/th))-1)
				for row := minRow; row <= maxRow; row++ {
					for col := minCol; col <= maxCol; col++ {
						flags := grid.TileFlagsAt(col, row)
						isSolid := flags&TileSolid != 0 && cfg.mask&QueryTiles != 0
						isOneWay := !isSolid && flags&TileOneWay != 0 && cfg.mask&QueryOneWay != 0 && !body.OneWayDrop
						if !isSolid && !isOneWay {
							continue
						}
						tb := Box{
							X: float32(ox + float64(float64(col)*tw)),
							Y: float32(oy + float64(float64(row)*th)),
							W: tileW,
							H: tileH,
						}
						hb := boxHull(tb)
						cand := sweepHullVsHull(cur, &hb, dx, dy, dLen, isOneWay, cfg)
						if !cand.hit {
							continue
						}
						if isSolid {
							nx, ny, ok := clampTileNormal(grid, col, row, cols, rows, cand.nx, cand.ny)
							if !ok {
								continue
							}
							if float64(nx*dx)+float64(ny*dy) >= -slideParEps*dLen {
								continue
							}
							cand.nx, cand.ny = nx, ny
						}
						cand.tile = true
						cand.col = col
						cand.row = row
						cand.tileFlags = flags
						keep(cand)
					}
				}
			}
		}
	}

	return best
}

// clampTileNormal removes normal components pointing into adjacent solid tiles so flat tile seams never snag.
func clampTileNormal(grid TileGrid, col, row, cols, rows int, nx, ny float64) (float64, float64, bool) {
	if nx < 0 && col > 0 && grid.TileFlagsAt(col-1, row)&TileSolid != 0 {
		nx = 0
	}
	if nx > 0 && col+1 < cols && grid.TileFlagsAt(col+1, row)&TileSolid != 0 {
		nx = 0
	}
	if ny < 0 && row > 0 && grid.TileFlagsAt(col, row-1)&TileSolid != 0 {
		ny = 0
	}
	if ny > 0 && row+1 < rows && grid.TileFlagsAt(col, row+1)&TileSolid != 0 {
		ny = 0
	}
	l := detmath.Hypot(nx, ny)
	if l <= 1e-9 {
		return 0, 0, false
	}
	return nx / l, ny / l, true
}

func boundsOverlap(minX, minY, maxX, maxY float64, b Box) bool {
	if b.W < 0 || b.H < 0 {
		return false
	}
	bMinX, bMinY := float64(b.X), float64(b.Y)
	bMaxX, bMaxY := bMinX+float64(b.W), bMinY+float64(b.H)
	return !(bMaxX < minX || maxX < bMinX || bMaxY < minY || maxY < bMinY)
}

func staticRigidHull(rb *RigidBody) (hull, bool) {
	switch rb.Shape.Kind {
	case ShapeBox:
		if rb.Shape.Box.Empty() {
			return hull{}, false
		}
		h := orientedBoxHull(float64(rb.X), float64(rb.Y), float64(rb.Shape.Box.W)*0.5, float64(rb.Shape.Box.H)*0.5, float64(rb.Angle))
		return h, h.finite()
	case ShapeCircle:
		if rb.Shape.Circle.Empty() {
			return hull{}, false
		}
		h := pointHull(float64(rb.X), float64(rb.Y), float64(rb.Shape.Circle.R))
		return h, h.finite()
	case ShapePolygon, ShapeCapsule, ShapeSegment:
		if rb.hasLocal {
			s, _ := rb.poseShape()
			return shapeHull(s)
		}
		return shapeHull(rb.Shape)
	}
	return hull{}, false
}

func staticRigidChain(rb *RigidBody) *Chain {
	if rb.Shape.Kind != ShapeChain || rb.Shape.Chain.Empty() {
		return nil
	}
	if rb.hasLocal {
		s, _ := rb.poseShape()
		if s.Chain.Empty() {
			return nil
		}
		return s.Chain
	}
	return rb.Shape.Chain
}

func overlapHullShape(cur *hull, s Shape) (nx, ny, depth float64, ok bool) {
	if s.Kind == ShapeChain {
		return overlapHullChain(cur, s.Chain)
	}
	hb, valid := shapeHull(s)
	if !valid {
		return 0, 0, 0, false
	}
	m := collideHulls(cur, &hb)
	if m.count == 0 {
		return 0, 0, 0, false
	}
	d := m.maxDepth()
	if d <= 0 {
		return 0, 0, 0, false
	}
	return -m.nx, -m.ny, d, true
}

func overlapHullChain(cur *hull, c *Chain) (nx, ny, depth float64, ok bool) {
	if c.Empty() {
		return 0, 0, 0, false
	}
	for i := range c.SegmentCount() {
		m, hit := chainSegmentManifold(c, i, cur)
		if !hit {
			continue
		}
		if d := m.maxDepth(); d > depth {
			// chainSegmentManifold normal points from chain toward cur.
			nx, ny, depth, ok = m.nx, m.ny, d, true
		}
	}
	return nx, ny, depth, ok
}

func sweepHullVsShape(cur *hull, s Shape, dx, dy, dLen float64, oneWay bool, cfg slideConfig) sweepHit {
	if s.Kind == ShapeChain {
		return sweepHullVsChain(cur, s.Chain, dx, dy, dLen, oneWay, cfg)
	}
	hb, ok := shapeHull(s)
	if !ok {
		return sweepHit{}
	}
	return sweepHullVsHull(cur, &hb, dx, dy, dLen, oneWay, cfg)
}

func sweepHullVsChain(cur *hull, c *Chain, dx, dy, dLen float64, oneWay bool, cfg slideConfig) sweepHit {
	if c.Empty() {
		return sweepHit{}
	}
	nPts := len(c.Points)
	var best sweepHit
	for i := range c.SegmentCount() {
		p0 := vec2{float64(c.Points[i].X), float64(c.Points[i].Y)}
		p1 := vec2{float64(c.Points[(i+1)%nPts].X), float64(c.Points[(i+1)%nPts].Y)}
		seg := segmentHull(p0, p1, 0)
		if seg.count != 2 || !seg.finite() {
			continue
		}
		cand := sweepHullVsHull(cur, &seg, dx, dy, dLen, oneWay, cfg)
		if !cand.hit {
			continue
		}
		// Filter ghost collisions at interior chain vertices using the same normal cone rule as chainSegmentManifold.
		nx, ny, ok := filterChainSweepNormal(c, i, p0, p1, seg.n[0], cand.nx, cand.ny, dx, dy, dLen)
		if !ok {
			continue
		}
		cand.nx, cand.ny = nx, ny
		if oneWay && float64(nx*cfg.upX)+float64(ny*cfg.upY) < cfg.cosFloor {
			continue
		}
		if !best.hit || cand.t < best.t {
			best = cand
		}
	}
	return best
}

func filterChainSweepNormal(c *Chain, i int, p0, p1, nSeg vec2, nx, ny, dx, dy, dLen float64) (float64, float64, bool) {
	nPts := len(c.Points)
	dir := vec2{p1.x - p0.x, p1.y - p0.y}
	length := detmath.Hypot(dir.x, dir.y)
	if length == 0 {
		return 0, 0, false
	}
	dir = vec2{dir.x / length, dir.y / length}
	mn := vec2{nx, ny}
	side := 1.0
	if dot2(mn, nSeg) < 0 {
		side = -1
	}
	nF := vec2{nSeg.x * side, nSeg.y * side}
	if dot2(mn, nF) >= 1-chainNormalTolerance {
		return nx, ny, true
	}
	var vertex, ghost vec2
	hasGhost := false
	atEnd := dot2(dir, mn) > 0
	if atEnd {
		vertex = p1
		if c.Loop || i+2 < nPts {
			ghost = vec2{float64(c.Points[(i+2)%nPts].X), float64(c.Points[(i+2)%nPts].Y)}
			hasGhost = true
		}
	} else {
		vertex = p0
		if c.Loop || i > 0 {
			ghost = vec2{float64(c.Points[(i+nPts-1)%nPts].X), float64(c.Points[(i+nPts-1)%nPts].Y)}
			hasGhost = true
		}
	}
	if !hasGhost {
		return nx, ny, true
	}
	toGhost := vec2{ghost.x - vertex.x, ghost.y - vertex.y}
	gl := detmath.Hypot(toGhost.x, toGhost.y)
	if gl > 0 && dot2(nF, toGhost) < -1e-9*gl {
		t := vec2{toGhost.x / gl, toGhost.y / gl}
		if atEnd {
			if dot2(mn, t) <= chainNormalTolerance {
				return nx, ny, true
			}
		} else if dot2(mn, vec2{-t.x, -t.y}) >= -chainNormalTolerance {
			return nx, ny, true
		}
		return 0, 0, false
	}
	// Flat or concave interior vertex: snap normal to face normal and keep only if moving into that face.
	if float64(nF.x*dx)+float64(nF.y*dy) >= -slideParEps*dLen {
		return 0, 0, false
	}
	return nF.x, nF.y, true
}

func sweepHullVsHull(ha, hb *hull, dx, dy, dLen float64, oneWay bool, cfg slideConfig) sweepHit {
	mink := minkowskiDiff(ha, hb)
	t, nx, ny, hit, startedInside := sweepMinkowski(&mink, dx, dy, dLen)
	if startedInside {
		m := collideHulls(ha, hb)
		if m.count == 0 {
			return sweepHit{}
		}
		depth := m.maxDepth()
		// collideHulls normal points from A toward B; invert to point from B toward A.
		nx, ny = -m.nx, -m.ny
		if oneWay {
			// A one-way collider only blocks if the mover was at or barely within its top edge and moves against it.
			if depth > slideOneWayEps {
				return sweepHit{}
			}
			if float64(nx*cfg.upX)+float64(ny*cfg.upY) < cfg.cosFloor {
				return sweepHit{}
			}
			if float64(nx*dx)+float64(ny*dy) >= -slideParEps*dLen {
				return sweepHit{}
			}
			posX, posY := contactPointAtSweep(ha, hb, nx, ny)
			return sweepHit{
				hit:       true,
				t:         0,
				nx:        nx,
				ny:        ny,
				posX:      posX,
				posY:      posY,
				pushDepth: depth,
			}
		}
		// Solid collider with remaining overlap: block if moving further into it.
		if float64(nx*dx)+float64(ny*dy) >= -slideParEps*dLen {
			return sweepHit{}
		}
		posX, posY := contactPointAtSweep(ha, hb, nx, ny)
		return sweepHit{
			hit:  true,
			t:    0,
			nx:   nx,
			ny:   ny,
			posX: posX,
			posY: posY,
		}
	}
	if !hit {
		return sweepHit{}
	}
	if oneWay && float64(nx*cfg.upX)+float64(ny*cfg.upY) < cfg.cosFloor {
		return sweepHit{}
	}
	haT := translateHull(ha, float64(dx*t), float64(dy*t))
	posX, posY := contactPointAtSweep(&haT, hb, nx, ny)
	return sweepHit{
		hit:  true,
		t:    t,
		nx:   nx,
		ny:   ny,
		posX: posX,
		posY: posY,
	}
}

// minkHull is the Minkowski difference B - A = (coreB + (-coreA)) + disk(rA + rB), holding up to 16 core vertices.
type minkHull struct {
	v     [maxMinkVertices]vec2
	n     [maxMinkVertices]vec2
	count int
	r     float64
}

func (m *minkHull) edgeCount() int {
	if m.count >= 3 {
		return m.count
	}
	if m.count == 2 {
		return 2
	}
	return 0
}

func (m *minkHull) edge(i int) (vec2, vec2) {
	return m.v[i], m.v[(i+1)%m.count]
}

func (m *minkHull) finish() {
	if m.count == 2 && m.v[0] == m.v[1] {
		m.count = 1
	}
	for i := range m.edgeCount() {
		a, b := m.edge(i)
		ex, ey := b.x-a.x, b.y-a.y
		if l := detmath.Hypot(ex, ey); l > 0 {
			m.n[i] = vec2{ey / l, -ex / l}
		} else {
			m.n[i] = vec2{}
		}
	}
}

func (m *minkHull) coreContainsOrigin() bool {
	if m.count < 3 {
		return false
	}
	for i := range m.count {
		// Origin is inside half-space i iff dot(n[i], 0 - v[i]) <= 0 <=> dot(n[i], v[i]) >= 0.
		if dot2(m.n[i], m.v[i]) < 0 {
			return false
		}
	}
	return true
}

func (m *minkHull) closestCoreToOrigin() (q vec2, dist float64) {
	if m.coreContainsOrigin() {
		return vec2{}, 0
	}
	if m.count == 1 {
		return m.v[0], detmath.Hypot(m.v[0].x, m.v[0].y)
	}
	best := math.Inf(1)
	edges := m.count
	if m.count == 2 {
		edges = 1
	}
	for i := range edges {
		a, b := m.edge(i)
		cand := closestOnSegment(a, b, vec2{})
		d := detmath.Hypot(cand.x, cand.y)
		if d < best {
			best, q = d, cand
		}
	}
	return q, best
}

func edgeHalf(e vec2) int {
	if e.y > 0 || (e.y == 0 && e.x > 0) {
		return 0
	}
	return 1
}

func compareEdgeAngle(a, b vec2) int {
	ha, hb := edgeHalf(a), edgeHalf(b)
	if ha != hb {
		if ha < hb {
			return -1
		}
		return 1
	}
	c := float64(a.x*b.y) - float64(a.y*b.x)
	la := float64(a.x*a.x) + float64(a.y*a.y)
	lb := float64(b.x*b.x) + float64(b.y*b.y)
	if float64(c*c) <= float64(float64(1e-18*la)*lb) {
		return 0
	}
	if c > 0 {
		return -1
	}
	return 1
}

// minkowskiDiff computes the Minkowski difference B - A = (coreB + (-coreA)) + disk(a.r + b.r).
func minkowskiDiff(a, b *hull) minkHull {
	m := minkHull{r: a.r + b.r}
	if a.count == 1 {
		ax, ay := a.v[0].x, a.v[0].y
		m.count = b.count
		for i := range b.count {
			m.v[i] = vec2{b.v[i].x - ax, b.v[i].y - ay}
		}
		for i := range b.edgeCount() {
			m.n[i] = b.n[i]
		}
		return m
	}
	if b.count == 1 {
		bx, by := b.v[0].x, b.v[0].y
		m.count = a.count
		for i := range a.count {
			m.v[i] = vec2{bx - a.v[i].x, by - a.v[i].y}
		}
		for i := range a.edgeCount() {
			m.n[i] = vec2{-a.n[i].x, -a.n[i].y}
		}
		return m
	}

	var na [MaxPolygonVertices]vec2
	startA := 0
	for i := range a.count {
		na[i] = vec2{-a.v[i].x, -a.v[i].y}
		if na[i].y < na[startA].y || (na[i].y == na[startA].y && na[i].x < na[startA].x) {
			startA = i
		}
	}
	startB := 0
	for i := 1; i < b.count; i++ {
		if b.v[i].y < b.v[startB].y || (b.v[i].y == b.v[startB].y && b.v[i].x < b.v[startB].x) {
			startB = i
		}
	}

	nB, nA := b.count, a.count
	i, j := 0, 0
	for i < nB || j < nA {
		vb := b.v[(startB+i)%nB]
		va := na[(startA+j)%nA]
		m.v[m.count] = vec2{vb.x + va.x, vb.y + va.y}
		m.count++
		if i == nB {
			j++
			continue
		}
		if j == nA {
			i++
			continue
		}
		vbNext := b.v[(startB+i+1)%nB]
		vaNext := na[(startA+j+1)%nA]
		eb := vec2{vbNext.x - vb.x, vbNext.y - vb.y}
		ea := vec2{vaNext.x - va.x, vaNext.y - va.y}
		cmp := compareEdgeAngle(eb, ea)
		if cmp < 0 {
			i++
		} else if cmp > 0 {
			j++
		} else {
			i++
			j++
		}
	}
	m.finish()
	return m
}

// sweepMinkowski casts the origin along (dx, dy) for t in [0, 1] against the rounded Minkowski difference m.
// A hit requires entering the interior of m; grazing touches moving parallel or outward return miss.
func sweepMinkowski(m *minkHull, dx, dy, dLen float64) (t, nx, ny float64, hit, startedInside bool) {
	if m.r == 0 {
		if m.count < 3 {
			return 0, 0, 0, false, false
		}
		tNear := math.Inf(-1)
		tFar := math.Inf(1)
		var hitNX, hitNY float64
		allInside := true
		for i := range m.count {
			n := m.n[i]
			num := dot2(n, m.v[i])
			if num <= slideTouchEps {
				allInside = false
			}
			den := float64(n.x*dx) + float64(n.y*dy)
			if math.Abs(den) <= slideParEps*dLen {
				if num <= slideTouchEps {
					return 0, 0, 0, false, false
				}
				continue
			}
			ti := num / den
			if den < 0 {
				if ti > tNear {
					tNear = ti
					hitNX, hitNY = n.x, n.y
				}
			} else if ti < tFar {
				tFar = ti
			}
		}
		if allInside {
			return 0, 0, 0, false, true
		}
		if tNear >= tFar || tFar <= slideTouchEps || tNear > 1 {
			return 0, 0, 0, false, false
		}
		if tNear < 0 {
			if tNear >= -1e-7 {
				return 0, hitNX, hitNY, true, false
			}
			return 0, 0, 0, false, true
		}
		return tNear, hitNX, hitNY, true, false
	}

	// Rounded hull (m.r > 0).
	q, coreDist := m.closestCoreToOrigin()
	if coreDist < m.r-1e-7 {
		return 0, 0, 0, false, true
	}
	if coreDist <= m.r+1e-7 && coreDist > 0 {
		onx, ony := -q.x/coreDist, -q.y/coreDist
		if float64(onx*dx)+float64(ony*dy) < -slideParEps*dLen {
			return 0, onx, ony, true, false
		}
		return 0, 0, 0, false, false
	}

	var best rayPiece
	for i := range m.count {
		if p, ok := raycastDiskStrict(dx, dy, m.v[i], m.r); ok {
			best.keep(p.t, p.nx, p.ny)
		}
	}
	for i := range m.edgeCount() {
		n := m.n[i]
		if float64(n.x*dx)+float64(n.y*dy) >= -slideParEps*dLen {
			continue
		}
		a, b := m.edge(i)
		oa := vec2{a.x + float64(n.x*m.r), a.y + float64(n.y*m.r)}
		ob := vec2{b.x + float64(n.x*m.r), b.y + float64(n.y*m.r)}
		if p, ok := raycastLine(0, 0, dx, dy, oa, ob); ok {
			best.keep(p.t, n.x, n.y)
		}
	}
	if !best.ok {
		return 0, 0, 0, false, false
	}
	return best.t, best.nx, best.ny, true, false
}

// raycastDiskStrict intersects ray (0,0) -> (dx,dy) with the interior of disk (c, r), ignoring grazing tangents.
func raycastDiskStrict(dx, dy float64, c vec2, r float64) (rayPiece, bool) {
	ox, oy := -c.x, -c.y
	a := float64(dx*dx) + float64(dy*dy)
	if a == 0 {
		return rayPiece{}, false
	}
	b := float64(ox*dx) + float64(oy*dy)
	if b >= 0 {
		return rayPiece{}, false
	}
	disc := float64(b*b) - float64(a*(float64(ox*ox)+float64(oy*oy)-float64(r*r)))
	if disc <= 0 {
		return rayPiece{}, false
	}
	t := (-b - math.Sqrt(disc)) / a
	if t < 0 || t > 1 {
		return rayPiece{}, false
	}
	nx, ny := (ox+float64(dx*t))/r, (oy+float64(dy*t))/r
	return rayPiece{t: t, nx: nx, ny: ny, ok: true}, true
}

// contactPointAtSweep computes the world-space contact point between touching hulls ha and hb
// along unit normal (nx, ny) pointing from hb toward ha.
func contactPointAtSweep(ha, hb *hull, nx, ny float64) (float64, float64) {
	tx, ty := -ny, nx
	// Support of hb along +n.
	maxB := math.Inf(-1)
	for i := range hb.count {
		d := float64(hb.v[i].x*nx) + float64(hb.v[i].y*ny)
		if d > maxB {
			maxB = d
		}
	}
	bMinT, bMaxT := math.Inf(1), math.Inf(-1)
	bCount := 0
	var bRef vec2
	for i := range hb.count {
		d := float64(hb.v[i].x*nx) + float64(hb.v[i].y*ny)
		if d >= maxB-slideSupportTol {
			p := vec2{hb.v[i].x + float64(nx*hb.r), hb.v[i].y + float64(ny*hb.r)}
			pt := float64(p.x*tx) + float64(p.y*ty)
			if pt < bMinT {
				bMinT = pt
			}
			if pt > bMaxT {
				bMaxT = pt
			}
			bRef = p
			bCount++
		}
	}

	// Support of ha along -n.
	minA := math.Inf(1)
	for i := range ha.count {
		d := float64(ha.v[i].x*nx) + float64(ha.v[i].y*ny)
		if d < minA {
			minA = d
		}
	}
	aMinT, aMaxT := math.Inf(1), math.Inf(-1)
	aCount := 0
	var aRef vec2
	for i := range ha.count {
		d := float64(ha.v[i].x*nx) + float64(ha.v[i].y*ny)
		if d <= minA+slideSupportTol {
			p := vec2{ha.v[i].x - float64(nx*ha.r), ha.v[i].y - float64(ny*ha.r)}
			pt := float64(p.x*tx) + float64(p.y*ty)
			if pt < aMinT {
				aMinT = pt
			}
			if pt > aMaxT {
				aMaxT = pt
			}
			aRef = p
			aCount++
		}
	}

	if bCount == 1 && aCount == 1 {
		return float64(bRef.x+aRef.x) * 0.5, float64(bRef.y+aRef.y) * 0.5
	}
	if bCount == 1 {
		return bRef.x, bRef.y
	}
	if aCount == 1 {
		return aRef.x, aRef.y
	}
	lo := max(aMinT, bMinT)
	hi := min(aMaxT, bMaxT)
	midT := float64(lo+hi) * 0.5
	if lo > hi {
		midT = float64(aMinT+aMaxT) * 0.5
	}
	bSurfN := maxB + hb.r
	return float64(nx*bSurfN) + float64(tx*midT), float64(ny*bSurfN) + float64(ty*midT)
}

// checkFloorRest tests whether cur is resting flush (within restEps) on a floor-facing body,
// static rigid body, or tile along -up, returning the floor normal and whether support was found.
func (w *World) checkFloorRest(
	body *Body,
	cur *hull,
	restEps float64,
	cfg slideConfig,
	grid TileGrid,
	extraRigid []*RigidBody,
	exclude []*Body,
) (nx, ny float64, ok bool) {
	if cfg.mode == MotionFloating {
		return 0, 0, false
	}
	dx := -float64(cfg.upX * restEps)
	dy := -float64(cfg.upY * restEps)
	dLen := detmath.Hypot(dx, dy)
	if dLen == 0 {
		return 0, 0, false
	}
	hit := w.sweepAll(body, cur, dx, dy, cfg, grid, extraRigid, exclude)
	if !hit.hit {
		return 0, 0, false
	}
	if float64(hit.nx*cfg.upX)+float64(hit.ny*cfg.upY) >= cfg.cosFloor {
		return hit.nx, hit.ny, true
	}
	return 0, 0, false
}
