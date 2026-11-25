package physics3d

import (
	"cmp"
	"math"
	"slices"
)

// AreaOverrideMode controls how a trigger Body's AreaSettings combines its
// gravity or damping value with overlapping areas and the world or body default.
type AreaOverrideMode uint8

const (
	// AreaOverrideNone leaves the world or body default unchanged (zero value).
	AreaOverrideNone AreaOverrideMode = iota
	// AreaOverrideCombine sums this area's value with other equal-priority
	// Combine or Add areas (plus any higher-priority Add areas) and replaces
	// lower-priority areas and the world/body default.
	AreaOverrideCombine
	// AreaOverrideReplace replaces lower- and equal-priority areas and the
	// world/body default with this area's value. Higher-priority Add areas
	// still add on top.
	AreaOverrideReplace
	// AreaOverrideAdd adds this area's value on top of lower-priority areas
	// and the world/body default.
	AreaOverrideAdd
)

// GravityFalloff controls how point-gravity acceleration varies with distance
// from GravityPoint.
type GravityFalloff uint8

const (
	// GravityFalloffNone applies constant GravityStrength regardless of distance (zero value).
	GravityFalloffNone GravityFalloff = iota
	// GravityFalloffLinear decreases acceleration linearly from GravityStrength at
	// distance 0 to 0 at GravityUnitDistance (or the trigger shape's outer radius
	// when GravityUnitDistance <= 0).
	GravityFalloffLinear
	// GravityFalloffInverse scales acceleration as GravityStrength * (unitDist / dist).
	GravityFalloffInverse
	// GravityFalloffInverseSquare scales acceleration as GravityStrength * (unitDist / dist)^2.
	GravityFalloffInverseSquare
)

// AreaSettings configures gravity, damping, and buoyancy/fluid drag overrides
// for a 3D trigger Body (IsTrigger == true). The zero value has no physical effect.
type AreaSettings struct {
	// Priority orders overlapping areas: higher Priority takes precedence.
	// Equal priorities break ties deterministically by World body insertion order.
	Priority int

	// GravityMode controls whether and how this area overrides gravity.
	GravityMode AreaOverrideMode
	// Gravity is the directional gravity acceleration (units/s^2) used when
	// PointGravity is false.
	Gravity Vec3

	// PointGravity switches gravity from directional Gravity to a radial field
	// centered at GravityPoint.
	PointGravity bool
	// GravityPoint is the world-space point-gravity center.
	GravityPoint Vec3
	// GravityStrength is the acceleration magnitude (units/s^2) toward
	// GravityPoint (negative values repel).
	GravityStrength float32
	// GravityFalloff selects distance attenuation for PointGravity.
	GravityFalloff GravityFalloff
	// GravityUnitDistance is the reference distance for GravityFalloffInverse
	// and GravityFalloffInverseSquare (defaults to 1 when <= 0), or the cutoff
	// radius for GravityFalloffLinear (defaults to the trigger's outer radius
	// when <= 0).
	GravityUnitDistance float32

	// LinearDampingMode controls whether and how LinearDamping overrides a body's damping.
	LinearDampingMode AreaOverrideMode
	// LinearDamping is the linear damping coefficient (1/s) applied according to LinearDampingMode.
	LinearDamping float32

	// AngularDampingMode controls whether and how AngularDamping overrides a body's damping.
	AngularDampingMode AreaOverrideMode
	// AngularDamping is the angular damping coefficient (1/s) applied according to AngularDampingMode.
	AngularDamping float32

	// FluidDensity enables Archimedes buoyancy when > 0 (mass per unit volume, kg/m^3).
	// A body with implicit density Mass/Volume settles at submerged fraction
	// (Mass / Volume) / FluidDensity.
	FluidDensity float32
	// FluidLinearDrag is the linear fluid drag coefficient (1/s), scaled by the
	// submerged fraction of the body's bounds.
	FluidLinearDrag float32
	// FluidAngularDrag is the angular fluid drag coefficient (1/s), scaled by the
	// submerged fraction of the body's bounds.
	FluidAngularDrag float32
	// FluidVelocity is the fluid flow velocity (units/s) relative to which
	// FluidLinearDrag acts.
	FluidVelocity Vec3
}

// Active reports whether a enables any gravity, damping, or fluid effect.
func (a AreaSettings) Active() bool {
	return a.GravityMode != AreaOverrideNone ||
		a.LinearDampingMode != AreaOverrideNone ||
		a.AngularDampingMode != AreaOverrideNone ||
		a.FluidDensity > 0 ||
		a.FluidLinearDrag > 0 ||
		a.FluidAngularDrag > 0
}

// AreaEffect holds the resolved gravity, damping, and fluid state at a point or
// shape from all overlapping active trigger bodies.
type AreaEffect struct {
	Gravity           Vec3
	LinearDamping     float32
	AngularDamping    float32
	HasLinearDamping  bool
	HasAngularDamping bool
	SubmergedFraction float32
	BuoyancyAccel     Vec3
	FluidLinearDrag   float32
	FluidAngularDrag  float32
	FluidVelocity     Vec3
}

// SetGravityScale sets the per-body gravity multiplier on b, including 0 for
// weightless bodies. Non-finite values are ignored.
func (b *Body) SetGravityScale(scale float32) {
	if b == nil || !isFinite32(scale) {
		return
	}
	b.GravityScale = scale
	b.gravityScaleSet = true
}

// EffectiveGravityScale returns b's active gravity multiplier.
// For a zero-initialized Body literal whose GravityScale has not been set,
// this defaults to 1.
func (b *Body) EffectiveGravityScale() float32 {
	if b == nil || !isFinite32(b.GravityScale) {
		return 1
	}
	if b.GravityScale == 0 && !b.gravityScaleSet {
		return 1
	}
	return b.GravityScale
}

// SetGravityScale sets the per-character gravity multiplier on c, including 0
// for weightless characters. Non-finite values are ignored.
func (c *CharacterController) SetGravityScale(scale float32) {
	if c == nil || !isFinite32(scale) {
		return
	}
	c.GravityScale = scale
	c.gravityScaleSet = true
}

// EffectiveGravityScale returns c's active gravity multiplier, defaulting to 1
// when unset on a zero-initialized CharacterController.
func (c *CharacterController) EffectiveGravityScale() float32 {
	if c == nil || !isFinite32(c.GravityScale) {
		return 1
	}
	if c.GravityScale == 0 && !c.gravityScaleSet {
		return 1
	}
	return c.GravityScale
}

// GravityAt returns the effective gravity acceleration at world point p in w,
// combining w.Gravity with any overlapping active trigger areas.
func (w *World) GravityAt(p Vec3) Vec3 {
	if w == nil {
		return Vec3{}
	}
	entries := w.collectActiveAreaTriggers()
	if len(entries) == 0 {
		return w.Gravity
	}
	var matching []activeAreaEntry
	for _, e := range entries {
		if shapeContainsPoint(e.worldShape, p) {
			matching = append(matching, e)
		}
	}
	return resolveMatchedAreas3D(matching, w.Gravity, 0, 0, p, Box{}, false, 0, 0).Gravity
}

// AreaEffectAtShape resolves all active trigger areas in w overlapping shape
// (filtered by layer and mask) using w.Gravity as the base gravity.
func (w *World) AreaEffectAtShape(shape Shape, layer, mask LayerMask) AreaEffect {
	if w == nil {
		return AreaEffect{}
	}
	entries := w.collectActiveAreaTriggers()
	if len(entries) == 0 {
		return AreaEffect{Gravity: w.Gravity}
	}
	bounds := shape.Bounds()
	var matching []activeAreaEntry
	for _, e := range entries {
		if !canCollideMasks(e.body.Layer, e.body.Mask, layer, mask) {
			continue
		}
		if !aabbOverlap(bounds, e.bounds) {
			continue
		}
		if TestShapes(e.worldShape, shape).Hit {
			matching = append(matching, e)
		}
	}
	return resolveMatchedAreas3D(matching, w.Gravity, 0, 0, bounds.Center, bounds, true, shapeVolume3D(shape), 0)
}

func sanitize32f64(v float32) float64 {
	if !isFinite32(v) {
		return 0
	}
	return float64(v)
}

type activeAreaEntry struct {
	body       *Body
	worldShape Shape
	bounds     Box
	order      int
}

func (w *World) collectActiveAreaTriggers() []activeAreaEntry {
	w.bp.ensureInit(w)
	var out []activeAreaEntry
	for _, pid := range w.bp.dynamicList {
		p := &w.bp.proxies[pid]
		b := p.body
		if b == nil || !b.IsTrigger || !b.Area.Active() {
			continue
		}
		ws := p.worldShape
		wb := p.worldBounds
		if b.Pos != p.cachedPos || b.Rot != p.cachedRot || !shapesEqual(b.Shape, p.cachedShape) {
			ws = b.WorldShape()
			wb = ws.Bounds()
		}
		out = append(out, activeAreaEntry{
			body:       b,
			worldShape: ws,
			bounds:     wb,
			order:      int(p.order),
		})
	}
	slices.SortStableFunc(out, func(a, b activeAreaEntry) int {
		if c := cmp.Compare(b.body.Area.Priority, a.body.Area.Priority); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	return out
}

func shapeContainsPoint(s Shape, p Vec3) bool {
	switch s.Kind {
	case ShapeBox:
		he := s.Box.HalfExtents
		if he.X <= 0 || he.Y <= 0 || he.Z <= 0 {
			return false
		}
		d := p.Sub(s.Box.Center)
		if rot, ok := s.orientedBox(); ok {
			d = rot.Conjugate().RotateVec(d)
		}
		return abs32(d.X) <= he.X && abs32(d.Y) <= he.Y && abs32(d.Z) <= he.Z
	case ShapeSphere:
		r := s.Sphere.Radius
		if r <= 0 {
			return false
		}
		return p.Sub(s.Sphere.Center).LenSq() <= r*r
	case ShapeCapsule:
		r := s.Capsule.Radius
		if r <= 0 {
			return false
		}
		closest := closestPointOnSegment(s.Capsule.A, s.Capsule.B, p)
		return p.Sub(closest).LenSq() <= r*r
	case ShapeConvexHull:
		g := s.Hull.geom
		if g == nil {
			return false
		}
		d := toDV(normalizedQuat(s.Rot).Conjugate().RotateVec(p.Sub(s.Hull.Center)))
		for _, f := range g.faces {
			if f.n.dot(d) > f.d {
				return false
			}
		}
		return true
	case ShapeCylinder:
		c := s.Cylinder
		axis := toDV(c.B.Sub(c.A))
		l := axis.length()
		if c.Radius <= 0 || l <= 1e-12 {
			return false
		}
		u := axis.scale(1 / l)
		d := toDV(p.Sub(c.A))
		t := d.dot(u)
		if t < 0 || t > l {
			return false
		}
		r := float64(c.Radius)
		return d.sub(u.scale(t)).lenSq() <= r*r
	default:
		return false
	}
}

func shapeOuterRadius3D(s Shape) float64 {
	switch s.Kind {
	case ShapeSphere:
		return float64(s.Sphere.Radius)
	case ShapeBox:
		he := s.Box.HalfExtents
		return math.Sqrt(float64(float64(he.X)*float64(he.X)) + float64(float64(he.Y)*float64(he.Y)) + float64(float64(he.Z)*float64(he.Z)))
	case ShapeCapsule:
		segLen := float64(s.Capsule.B.Sub(s.Capsule.A).Len())
		return float64(0.5*segLen) + float64(s.Capsule.Radius)
	case ShapeConvexHull:
		if s.Hull.geom == nil {
			return 0
		}
		return s.Hull.geom.radius
	case ShapeCylinder:
		h := 0.5 * float64(s.Cylinder.Height())
		r := float64(s.Cylinder.Radius)
		return math.Sqrt(float64(h*h) + float64(r*r))
	default:
		return 1
	}
}

func shapeVolume3D(s Shape) float64 {
	switch s.Kind {
	case ShapeBox:
		he := s.Box.HalfExtents
		if he.X <= 0 || he.Y <= 0 || he.Z <= 0 {
			return 0
		}
		return 8.0 * float64(he.X) * float64(he.Y) * float64(he.Z)
	case ShapeSphere:
		r := float64(s.Sphere.Radius)
		if r <= 0 {
			return 0
		}
		return (4.0 / 3.0) * math.Pi * r * r * r
	case ShapeCapsule:
		r := float64(s.Capsule.Radius)
		if r <= 0 {
			return 0
		}
		segLen := float64(s.Capsule.B.Sub(s.Capsule.A).Len())
		return float64(math.Pi*r*r*segLen) + float64((4.0/3.0)*math.Pi*r*r*r)
	case ShapeConvexHull:
		return float64(s.Hull.Volume())
	case ShapeCylinder:
		r := float64(s.Cylinder.Radius)
		if r <= 0 {
			return 0
		}
		return math.Pi * r * r * float64(s.Cylinder.Height())
	default:
		return 0
	}
}

func boundsOverlapFraction3D(bodyBounds, areaBounds Box) float64 {
	bMin, bMax := bodyBounds.Min(), bodyBounds.Max()
	aMin, aMax := areaBounds.Min(), areaBounds.Max()
	bx := float64(bMax.X) - float64(bMin.X)
	by := float64(bMax.Y) - float64(bMin.Y)
	bz := float64(bMax.Z) - float64(bMin.Z)
	bodyVol := bx * by * bz
	if bodyVol <= 0 {
		return 0
	}
	ix := min(float64(bMax.X), float64(aMax.X)) - max(float64(bMin.X), float64(aMin.X))
	iy := min(float64(bMax.Y), float64(aMax.Y)) - max(float64(bMin.Y), float64(aMin.Y))
	iz := min(float64(bMax.Z), float64(aMax.Z)) - max(float64(bMin.Z), float64(aMin.Z))
	if ix <= 0 || iy <= 0 || iz <= 0 {
		return 0
	}
	frac := (ix * iy * iz) / bodyVol
	if frac > 1 {
		return 1
	}
	return frac
}

func areaGravityVector3D(e activeAreaEntry, p Vec3) (float64, float64, float64) {
	a := e.body.Area
	if !a.PointGravity {
		return sanitize32f64(a.Gravity.X), sanitize32f64(a.Gravity.Y), sanitize32f64(a.Gravity.Z)
	}
	strength := sanitize32f64(a.GravityStrength)
	if strength == 0 {
		return 0, 0, 0
	}
	dx := sanitize32f64(a.GravityPoint.X) - float64(p.X)
	dy := sanitize32f64(a.GravityPoint.Y) - float64(p.Y)
	dz := sanitize32f64(a.GravityPoint.Z) - float64(p.Z)
	dist := math.Sqrt(float64(float64(dx*dx)+float64(dy*dy)) + float64(dz*dz))
	if dist <= 1e-6 {
		return 0, 0, 0
	}
	nx, ny, nz := dx/dist, dy/dist, dz/dist

	var mag float64
	switch a.GravityFalloff {
	case GravityFalloffNone:
		mag = strength
	case GravityFalloffLinear:
		cutoff := sanitize32f64(a.GravityUnitDistance)
		if cutoff <= 0 {
			cutoff = shapeOuterRadius3D(e.worldShape)
		}
		if cutoff <= 0 || dist >= cutoff {
			return 0, 0, 0
		}
		mag = strength * (1.0 - dist/cutoff)
	case GravityFalloffInverse:
		unit := sanitize32f64(a.GravityUnitDistance)
		if unit <= 0 {
			unit = 1
		}
		rEff := max(dist, max(1e-6, 1e-3*unit))
		mag = strength * (unit / rEff)
	case GravityFalloffInverseSquare:
		unit := sanitize32f64(a.GravityUnitDistance)
		if unit <= 0 {
			unit = 1
		}
		rEff := max(dist, max(1e-6, 1e-3*unit))
		ratio := unit / rEff
		mag = strength * ratio * ratio
	default:
		mag = strength
	}
	return nx * mag, ny * mag, nz * mag
}

func (w *World) evaluateBodyAreas(b *Body, entries []activeAreaEntry) AreaEffect {
	baseLin := max(0, sanitize32f64(b.effectiveLinearDamping()))
	baseAng := max(0, sanitize32f64(b.effectiveAngularDamping()))
	if len(entries) == 0 {
		return AreaEffect{
			Gravity:        w.Gravity,
			LinearDamping:  float32(baseLin),
			AngularDamping: float32(baseAng),
		}
	}
	ws := b.WorldShape()
	wb := ws.Bounds()
	var matching []activeAreaEntry
	for _, e := range entries {
		if e.body == b {
			continue
		}
		if !CanCollide(e.body, b) || w.isPairIgnored(e.body.ID, b.ID) {
			continue
		}
		if !aabbOverlap(wb, e.bounds) {
			continue
		}
		if TestShapes(e.worldShape, ws).Hit {
			matching = append(matching, e)
		}
	}
	return resolveMatchedAreas3D(
		matching,
		w.Gravity,
		baseLin, baseAng,
		b.Pos,
		wb, true,
		shapeVolume3D(b.Shape),
		float64(b.InvMass),
	)
}

func (w *World) evaluateShapeAreasForCharacter(shape Shape, ignoreBodyID int, mask LayerMask) AreaEffect {
	if w == nil {
		return AreaEffect{}
	}
	entries := w.collectActiveAreaTriggers()
	if len(entries) == 0 {
		return AreaEffect{Gravity: w.Gravity}
	}
	wb := shape.Bounds()
	var matching []activeAreaEntry
	for _, e := range entries {
		if e.body.ID == ignoreBodyID {
			continue
		}
		if !canCollideMasks(e.body.Layer, e.body.Mask, LayerAll, mask) {
			continue
		}
		if !aabbOverlap(wb, e.bounds) {
			continue
		}
		if TestShapes(e.worldShape, shape).Hit {
			matching = append(matching, e)
		}
	}
	return resolveMatchedAreas3D(matching, w.Gravity, 0, 0, wb.Center, wb, true, shapeVolume3D(shape), 0)
}

func resolveScalarOverride3D(areas []activeAreaEntry, base float64, getMode func(*Body) AreaOverrideMode, getVal func(*Body) float64) (val float64, overridden bool) {
	acc := 0.0
	useBase := true
	stoppedAtPriority := false
	combinePriority := 0
	for _, e := range areas {
		if stoppedAtPriority && e.body.Area.Priority < combinePriority {
			break
		}
		mode := getMode(e.body)
		if mode == AreaOverrideNone {
			continue
		}
		overridden = true
		if stoppedAtPriority {
			if mode == AreaOverrideCombine || mode == AreaOverrideAdd {
				acc += getVal(e.body)
			}
			continue
		}
		switch mode {
		case AreaOverrideAdd:
			acc += getVal(e.body)
		case AreaOverrideCombine:
			acc += getVal(e.body)
			useBase = false
			stoppedAtPriority = true
			combinePriority = e.body.Area.Priority
		case AreaOverrideReplace:
			acc += getVal(e.body)
			return acc, true
		}
	}
	if useBase {
		acc += base
	}
	return acc, overridden
}

func resolveMatchedAreas3D(
	areas []activeAreaEntry,
	baseGravity Vec3,
	baseLinDamp, baseAngDamp float64,
	pos Vec3,
	bodyBounds Box,
	hasBounds bool,
	bodyVol, invMass float64,
) AreaEffect {
	if len(areas) == 0 {
		return AreaEffect{
			Gravity:        baseGravity,
			LinearDamping:  float32(baseLinDamp),
			AngularDamping: float32(baseAngDamp),
		}
	}

	// 1. Resolve effective gravity vector in descending priority order.
	gxAcc, gyAcc, gzAcc := 0.0, 0.0, 0.0
	useBaseG := true
	stoppedG := false
	combinePrioG := 0
	for _, e := range areas {
		if stoppedG && e.body.Area.Priority < combinePrioG {
			break
		}
		mode := e.body.Area.GravityMode
		if mode == AreaOverrideNone {
			continue
		}
		agx, agy, agz := areaGravityVector3D(e, pos)
		if stoppedG {
			if mode == AreaOverrideCombine || mode == AreaOverrideAdd {
				gxAcc += agx
				gyAcc += agy
				gzAcc += agz
			}
			continue
		}
		switch mode {
		case AreaOverrideAdd:
			gxAcc += agx
			gyAcc += agy
			gzAcc += agz
		case AreaOverrideCombine:
			gxAcc += agx
			gyAcc += agy
			gzAcc += agz
			useBaseG = false
			stoppedG = true
			combinePrioG = e.body.Area.Priority
		case AreaOverrideReplace:
			gxAcc += agx
			gyAcc += agy
			gzAcc += agz
			useBaseG = false
			goto doneGravity
		}
	}
doneGravity:
	if useBaseG {
		gxAcc += float64(baseGravity.X)
		gyAcc += float64(baseGravity.Y)
		gzAcc += float64(baseGravity.Z)
	}

	// 2. Resolve linear and angular damping overrides.
	linDamp, hasLin := resolveScalarOverride3D(
		areas,
		baseLinDamp,
		func(b *Body) AreaOverrideMode { return b.Area.LinearDampingMode },
		func(b *Body) float64 { return sanitize32f64(b.Area.LinearDamping) },
	)
	if linDamp < 0 {
		linDamp = 0
	}
	angDamp, hasAng := resolveScalarOverride3D(
		areas,
		baseAngDamp,
		func(b *Body) AreaOverrideMode { return b.Area.AngularDampingMode },
		func(b *Body) float64 { return sanitize32f64(b.Area.AngularDamping) },
	)
	if angDamp < 0 {
		angDamp = 0
	}

	// 3. Resolve fluid buoyancy and drag from submerged bounds fraction.
	remFrac := 1.0
	totalSubFrac := 0.0
	buoyancyMass := 0.0
	totalLinDrag := 0.0
	totalAngDrag := 0.0
	velWeightSum := 0.0
	fluidVelXSum, fluidVelYSum, fluidVelZSum := 0.0, 0.0, 0.0

	for _, e := range areas {
		if remFrac <= 0 {
			break
		}
		a := e.body.Area
		fd := sanitize32f64(a.FluidDensity)
		flDrag := max(0, sanitize32f64(a.FluidLinearDrag))
		faDrag := max(0, sanitize32f64(a.FluidAngularDrag))
		if fd <= 0 && flDrag <= 0 && faDrag <= 0 {
			continue
		}
		frac := 1.0
		if hasBounds {
			frac = boundsOverlapFraction3D(bodyBounds, e.bounds)
		}
		if frac <= 0 {
			continue
		}
		used := min(frac, remFrac)
		remFrac -= used
		totalSubFrac += used
		if fd > 0 && bodyVol > 0 {
			buoyancyMass += float64(fd * float64(used*bodyVol))
		}
		if flDrag > 0 {
			wDrag := float64(flDrag * used)
			totalLinDrag += wDrag
			velWeightSum += wDrag
			fluidVelXSum += float64(sanitize32f64(a.FluidVelocity.X) * wDrag)
			fluidVelYSum += float64(sanitize32f64(a.FluidVelocity.Y) * wDrag)
			fluidVelZSum += float64(sanitize32f64(a.FluidVelocity.Z) * wDrag)
		}
		if faDrag > 0 {
			totalAngDrag += float64(faDrag * used)
		}
	}

	var buoyX, buoyY, buoyZ float64
	if buoyancyMass > 0 && invMass > 0 {
		buoyScale := buoyancyMass * invMass
		buoyX = -gxAcc * buoyScale
		buoyY = -gyAcc * buoyScale
		buoyZ = -gzAcc * buoyScale
	}
	var fvx, fvy, fvz float64
	if velWeightSum > 0 {
		fvx = fluidVelXSum / velWeightSum
		fvy = fluidVelYSum / velWeightSum
		fvz = fluidVelZSum / velWeightSum
	}

	return AreaEffect{
		Gravity:           Vec3{X: float32(gxAcc), Y: float32(gyAcc), Z: float32(gzAcc)},
		LinearDamping:     float32(linDamp),
		AngularDamping:    float32(angDamp),
		HasLinearDamping:  hasLin,
		HasAngularDamping: hasAng,
		SubmergedFraction: float32(totalSubFrac),
		BuoyancyAccel:     Vec3{X: float32(buoyX), Y: float32(buoyY), Z: float32(buoyZ)},
		FluidLinearDrag:   float32(totalLinDrag),
		FluidAngularDrag:  float32(totalAngDrag),
		FluidVelocity:     Vec3{X: float32(fvx), Y: float32(fvy), Z: float32(fvz)},
	}
}
