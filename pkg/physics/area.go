package physics

import (
	"cmp"
	"math"
	"slices"

	"github.com/nsgo-dev/game/internal/detmath"
)

// AreaOverrideMode controls how a Trigger's AreaSettings combines its gravity
// or damping value with overlapping areas and the world or body default.
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
// from (GravityPointX, GravityPointY).
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
// for a 2D Trigger. The zero value has no physical effect.
type AreaSettings struct {
	// Priority orders overlapping areas: higher Priority takes precedence.
	// Equal priorities break ties deterministically by trigger slice order.
	Priority int

	// GravityMode controls whether and how this area overrides gravity.
	GravityMode AreaOverrideMode
	// GravityX and GravityY are the directional gravity acceleration (units/s^2)
	// used when PointGravity is false.
	GravityX, GravityY float32

	// PointGravity switches gravity from directional (GravityX, GravityY) to a
	// radial field centered at (GravityPointX, GravityPointY).
	PointGravity bool
	// GravityPointX and GravityPointY are the world-space point-gravity center.
	GravityPointX, GravityPointY float32
	// GravityStrength is the acceleration magnitude (units/s^2) toward the point
	// center (negative values repel).
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

	// FluidDensity enables Archimedes buoyancy when > 0 (mass per unit area in 2D).
	// A body with implicit density Mass/Area settles at submerged fraction
	// (Mass / Area) / FluidDensity.
	FluidDensity float32
	// FluidLinearDrag is the linear fluid drag coefficient (1/s), scaled by the
	// submerged fraction of the body's bounds.
	FluidLinearDrag float32
	// FluidAngularDrag is the angular fluid drag coefficient (1/s), scaled by the
	// submerged fraction of the body's bounds.
	FluidAngularDrag float32
	// FluidVelX and FluidVelY are the fluid flow velocity (units/s) relative to
	// which FluidLinearDrag acts.
	FluidVelX, FluidVelY float32
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
// shape from all overlapping active Triggers.
type AreaEffect struct {
	GravityX, GravityY float32
	LinearDamping      float32
	AngularDamping     float32
	HasLinearDamping   bool
	HasAngularDamping  bool
	SubmergedFraction  float32
	BuoyancyAccelX     float32
	BuoyancyAccelY     float32
	FluidLinearDrag    float32
	FluidAngularDrag   float32
	FluidVelX          float32
	FluidVelY          float32
}

// SetGravityScale sets the per-body gravity multiplier on b, including 0 for
// weightless bodies. Non-finite values are ignored.
func (b *RigidBody) SetGravityScale(scale float32) {
	if b == nil || !isFinite32(scale) {
		return
	}
	b.GravityScale = scale
	b.gravityScaleSet = true
}

// EffectiveGravityScale returns b's active gravity multiplier.
// For a zero-initialized RigidBody literal whose GravityScale has not been set,
// this defaults to 1.
func (b *RigidBody) EffectiveGravityScale() float32 {
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
func (c *CharacterBody) SetGravityScale(scale float32) {
	if c == nil || !isFinite32(scale) {
		return
	}
	c.GravityScale = scale
	c.gravityScaleSet = true
}

// EffectiveGravityScale returns c's active gravity multiplier, defaulting to 1
// when unset on a zero-initialized CharacterBody.
func (c *CharacterBody) EffectiveGravityScale() float32 {
	if c == nil || !isFinite32(c.GravityScale) {
		return 1
	}
	if c.GravityScale == 0 && !c.gravityScaleSet {
		return 1
	}
	return c.GravityScale
}

// AddTrigger appends non-nil triggers to w.Triggers so their Area settings
// apply to overlapping RigidBodies during Step.
func (w *RigidWorld) AddTrigger(triggers ...*Trigger) {
	if w == nil {
		return
	}
	for _, tr := range triggers {
		if tr != nil {
			w.ensureTriggerID(tr)
			w.Triggers = append(w.Triggers, tr)
		}
	}
}

// RemoveTrigger deletes the first occurrence of tr from w.Triggers, preserving
// the order of remaining triggers.
func (w *RigidWorld) RemoveTrigger(tr *Trigger) bool {
	if w == nil {
		return false
	}
	for i, existing := range w.Triggers {
		if existing == tr {
			w.Triggers = append(w.Triggers[:i], w.Triggers[i+1:]...)
			return true
		}
	}
	return false
}

// GravityAt returns the effective gravity acceleration at world point (x, y)
// in w, combining w.GravityX/GravityY with any overlapping active Triggers.
func (w *RigidWorld) GravityAt(x, y float32) (gx, gy float32) {
	if w == nil {
		return 0, 0
	}
	eff := evaluateAreasAtPoint(w.Triggers, w.GravityX, w.GravityY, x, y, LayerAll, LayerAll)
	return eff.GravityX, eff.GravityY
}

// AreaEffectAtShape resolves all active Triggers in w overlapping shape (filtered
// by layer and mask) using w.GravityX/GravityY as the base gravity.
func (w *RigidWorld) AreaEffectAtShape(shape Shape, layer, mask LayerMask) AreaEffect {
	if w == nil {
		return AreaEffect{}
	}
	return evaluateAreasAtShape(w.Triggers, w.GravityX, w.GravityY, shape, layer, mask, 0, 0, 0, 0)
}

// GravityAt returns the effective gravity acceleration at world point (x, y)
// in w, combining w.GravityX/GravityY with any overlapping active Triggers.
func (w *World) GravityAt(x, y float32) (gx, gy float32) {
	if w == nil {
		return 0, 0
	}
	eff := evaluateAreasAtPoint(w.Triggers, w.GravityX, w.GravityY, x, y, LayerAll, LayerAll)
	return eff.GravityX, eff.GravityY
}

// AreaEffectAtShape resolves all active Triggers in w overlapping shape (filtered
// by layer and mask) using w.GravityX/GravityY as the base gravity.
func (w *World) AreaEffectAtShape(shape Shape, layer, mask LayerMask) AreaEffect {
	if w == nil {
		return AreaEffect{}
	}
	return evaluateAreasAtShape(w.Triggers, w.GravityX, w.GravityY, shape, layer, mask, 0, 0, 0, 0)
}

func isFinite32(v float32) bool {
	return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
}

func sanitize32(v float32) float64 {
	if !isFinite32(v) {
		return 0
	}
	return float64(v)
}

type activeAreaEntry struct {
	tr    *Trigger
	order int
}

func collectActiveTriggers(triggers []*Trigger) []activeAreaEntry {
	var out []activeAreaEntry
	for i, tr := range triggers {
		if tr == nil || tr.Shape.Empty() || !tr.Area.Active() {
			continue
		}
		out = append(out, activeAreaEntry{tr: tr, order: i})
	}
	slices.SortStableFunc(out, func(a, b activeAreaEntry) int {
		if c := cmp.Compare(b.tr.Area.Priority, a.tr.Area.Priority); c != 0 {
			return c
		}
		return cmp.Compare(a.order, b.order)
	})
	return out
}

func triggerOuterRadius(s Shape) float64 {
	switch s.Kind {
	case ShapeCircle:
		return float64(s.Circle.R)
	case ShapeBox:
		return 0.5 * detmath.Hypot(float64(s.Box.W), float64(s.Box.H))
	case ShapePolygon, ShapeCapsule, ShapeSegment, ShapeChain:
		b := s.Bounds()
		return 0.5 * detmath.Hypot(float64(b.W), float64(b.H))
	default:
		return 1
	}
}

func areaGravityVector(tr *Trigger, px, py float64) (float64, float64) {
	a := tr.Area
	if !a.PointGravity {
		return sanitize32(a.GravityX), sanitize32(a.GravityY)
	}
	strength := sanitize32(a.GravityStrength)
	if strength == 0 {
		return 0, 0
	}
	dx := sanitize32(a.GravityPointX) - px
	dy := sanitize32(a.GravityPointY) - py
	dist := detmath.Hypot(dx, dy)
	if dist <= 1e-6 {
		return 0, 0
	}
	nx, ny := dx/dist, dy/dist

	var mag float64
	switch a.GravityFalloff {
	case GravityFalloffNone:
		mag = strength
	case GravityFalloffLinear:
		cutoff := sanitize32(a.GravityUnitDistance)
		if cutoff <= 0 {
			cutoff = triggerOuterRadius(tr.Shape)
		}
		if cutoff <= 0 || dist >= cutoff {
			return 0, 0
		}
		mag = strength * (1.0 - dist/cutoff)
	case GravityFalloffInverse:
		unit := sanitize32(a.GravityUnitDistance)
		if unit <= 0 {
			unit = 1
		}
		rEff := max(dist, max(1e-6, 1e-3*unit))
		mag = strength * (unit / rEff)
	case GravityFalloffInverseSquare:
		unit := sanitize32(a.GravityUnitDistance)
		if unit <= 0 {
			unit = 1
		}
		rEff := max(dist, max(1e-6, 1e-3*unit))
		ratio := unit / rEff
		mag = strength * ratio * ratio
	default:
		mag = strength
	}
	return nx * mag, ny * mag
}

func evaluateAreasAtPoint(triggers []*Trigger, baseGX, baseGY, x, y float32, layer, mask LayerMask) AreaEffect {
	entries := collectActiveTriggers(triggers)
	if len(entries) == 0 {
		return AreaEffect{GravityX: baseGX, GravityY: baseGY}
	}
	var matching []*Trigger
	for _, e := range entries {
		if !layersInteract(e.tr.Layer, e.tr.Mask, layer, mask) {
			continue
		}
		if e.tr.Shape.Contains(x, y) {
			matching = append(matching, e.tr)
		}
	}
	return resolveMatchedAreas(matching, float64(baseGX), float64(baseGY), 0, 0, float64(x), float64(y), 0, 0, 0, 0, 0, 0)
}

func shapeArea2D(s Shape) float64 {
	switch s.Kind {
	case ShapeBox:
		if s.Box.Empty() {
			return 0
		}
		return float64(s.Box.W) * float64(s.Box.H)
	case ShapeCircle:
		if s.Circle.Empty() {
			return 0
		}
		r := float64(s.Circle.R)
		return math.Pi * r * r
	default:
		return float64(s.Area())
	}
}

func boundsOverlapFraction2D(bMinX, bMinY, bMaxX, bMaxY float64, areaBounds Box) float64 {
	bw := bMaxX - bMinX
	bh := bMaxY - bMinY
	bodyBoundsArea := bw * bh
	if bodyBoundsArea <= 0 || areaBounds.Empty() {
		return 0
	}
	aMinX, aMinY := float64(areaBounds.X), float64(areaBounds.Y)
	aMaxX, aMaxY := aMinX+float64(areaBounds.W), aMinY+float64(areaBounds.H)
	ix := min(bMaxX, aMaxX) - max(bMinX, aMinX)
	iy := min(bMaxY, aMaxY) - max(bMinY, aMinY)
	if ix <= 0 || iy <= 0 {
		return 0
	}
	frac := (ix * iy) / bodyBoundsArea
	if frac > 1 {
		return 1
	}
	return frac
}

func evaluateAreasAtShape(triggers []*Trigger, baseGX, baseGY float32, shape Shape, layer, mask LayerMask, baseLinDamp, baseAngDamp, bodyArea, invMass float64) AreaEffect {
	if shape.Empty() {
		return AreaEffect{
			GravityX:       baseGX,
			GravityY:       baseGY,
			LinearDamping:  float32(baseLinDamp),
			AngularDamping: float32(baseAngDamp),
		}
	}
	entries := collectActiveTriggers(triggers)
	if len(entries) == 0 {
		return AreaEffect{
			GravityX:       baseGX,
			GravityY:       baseGY,
			LinearDamping:  float32(baseLinDamp),
			AngularDamping: float32(baseAngDamp),
		}
	}
	bounds := shape.Bounds()
	cx, cy := bounds.Center()
	bMinX, bMinY := float64(bounds.X), float64(bounds.Y)
	bMaxX, bMaxY := bMinX+float64(bounds.W), bMinY+float64(bounds.H)

	var matching []*Trigger
	for _, e := range entries {
		if !layersInteract(e.tr.Layer, e.tr.Mask, layer, mask) {
			continue
		}
		if TestShapes(e.tr.Shape, shape).Hit {
			matching = append(matching, e.tr)
		}
	}
	return resolveMatchedAreas(matching, float64(baseGX), float64(baseGY), baseLinDamp, baseAngDamp, float64(cx), float64(cy), bMinX, bMinY, bMaxX, bMaxY, bodyArea, invMass)
}

func (b *RigidBody) syncedShape() Shape {
	s := b.Shape
	switch s.Kind {
	case ShapeBox:
		s.Box.X = b.X - float32(s.Box.W*0.5)
		s.Box.Y = b.Y - float32(s.Box.H*0.5)
	case ShapeCircle:
		s.Circle.X = b.X
		s.Circle.Y = b.Y
	}
	return s
}

func rigidBodyOverlapsTrigger(b *RigidBody, tr *Trigger) bool {
	if b.Shape.Kind == ShapeBox && b.Angle != 0 {
		tb := tr.Shape.Bounds()
		tcx, tcy := tb.Center()
		trBody := RigidBody{
			Shape: tr.Shape,
			X:     tcx,
			Y:     tcy,
		}
		_, hit := collideRigidBodies(b, &trBody)
		return hit
	}
	return TestShapes(tr.Shape, b.syncedShape()).Hit
}

func (w *RigidWorld) evaluateRigidBodyAreas(b *RigidBody, entries []activeAreaEntry) AreaEffect {
	baseGX, baseGY := float64(w.GravityX), float64(w.GravityY)
	baseLin := max(0, sanitize32(b.LinearDamping))
	baseAng := max(0, sanitize32(b.AngularDamping))
	if len(entries) == 0 || b.Shape.Empty() {
		return AreaEffect{
			GravityX:       w.GravityX,
			GravityY:       w.GravityY,
			LinearDamping:  float32(baseLin),
			AngularDamping: float32(baseAng),
		}
	}
	bMinX, bMinY, bMaxX, bMaxY := b.worldAABB()
	var matching []*Trigger
	for _, e := range entries {
		if !layersInteract(e.tr.Layer, e.tr.Mask, b.Layer, b.Mask) {
			continue
		}
		tb := e.tr.Shape.Bounds()
		if bMaxX <= float64(tb.X) || float64(tb.X+tb.W) <= bMinX || bMaxY <= float64(tb.Y) || float64(tb.Y+tb.H) <= bMinY {
			continue
		}
		if rigidBodyOverlapsTrigger(b, e.tr) {
			matching = append(matching, e.tr)
		}
	}
	return resolveMatchedAreas(
		matching,
		baseGX, baseGY,
		baseLin, baseAng,
		float64(b.X), float64(b.Y),
		bMinX, bMinY, bMaxX, bMaxY,
		shapeArea2D(b.Shape),
		float64(b.InvMass),
	)
}

func resolveScalarOverride(areas []*Trigger, base float64, getMode func(*Trigger) AreaOverrideMode, getVal func(*Trigger) float64) (val float64, overridden bool) {
	acc := 0.0
	useBase := true
	stoppedAtPriority := false
	combinePriority := 0
	for _, tr := range areas {
		if stoppedAtPriority && tr.Area.Priority < combinePriority {
			break
		}
		mode := getMode(tr)
		if mode == AreaOverrideNone {
			continue
		}
		overridden = true
		if stoppedAtPriority {
			if mode == AreaOverrideCombine || mode == AreaOverrideAdd {
				acc += getVal(tr)
			}
			continue
		}
		switch mode {
		case AreaOverrideAdd:
			acc += getVal(tr)
		case AreaOverrideCombine:
			acc += getVal(tr)
			useBase = false
			stoppedAtPriority = true
			combinePriority = tr.Area.Priority
		case AreaOverrideReplace:
			acc += getVal(tr)
			return acc, true
		}
	}
	if useBase {
		acc += base
	}
	return acc, overridden
}

func resolveMatchedAreas(
	areas []*Trigger,
	baseGX, baseGY float64,
	baseLinDamp, baseAngDamp float64,
	px, py float64,
	bMinX, bMinY, bMaxX, bMaxY float64,
	bodyArea, invMass float64,
) AreaEffect {
	if len(areas) == 0 {
		return AreaEffect{
			GravityX:       float32(baseGX),
			GravityY:       float32(baseGY),
			LinearDamping:  float32(baseLinDamp),
			AngularDamping: float32(baseAngDamp),
		}
	}

	// 1. Resolve effective gravity vector in descending priority order.
	gxAcc, gyAcc := 0.0, 0.0
	useBaseG := true
	stoppedG := false
	combinePrioG := 0
	for _, tr := range areas {
		if stoppedG && tr.Area.Priority < combinePrioG {
			break
		}
		mode := tr.Area.GravityMode
		if mode == AreaOverrideNone {
			continue
		}
		agx, agy := areaGravityVector(tr, px, py)
		if stoppedG {
			if mode == AreaOverrideCombine || mode == AreaOverrideAdd {
				gxAcc += agx
				gyAcc += agy
			}
			continue
		}
		switch mode {
		case AreaOverrideAdd:
			gxAcc += agx
			gyAcc += agy
		case AreaOverrideCombine:
			gxAcc += agx
			gyAcc += agy
			useBaseG = false
			stoppedG = true
			combinePrioG = tr.Area.Priority
		case AreaOverrideReplace:
			gxAcc += agx
			gyAcc += agy
			useBaseG = false
			goto doneGravity
		}
	}
doneGravity:
	if useBaseG {
		gxAcc += baseGX
		gyAcc += baseGY
	}

	// 2. Resolve linear and angular damping overrides.
	linDamp, hasLin := resolveScalarOverride(
		areas,
		baseLinDamp,
		func(tr *Trigger) AreaOverrideMode { return tr.Area.LinearDampingMode },
		func(tr *Trigger) float64 { return sanitize32(tr.Area.LinearDamping) },
	)
	if linDamp < 0 {
		linDamp = 0
	}
	angDamp, hasAng := resolveScalarOverride(
		areas,
		baseAngDamp,
		func(tr *Trigger) AreaOverrideMode { return tr.Area.AngularDampingMode },
		func(tr *Trigger) float64 { return sanitize32(tr.Area.AngularDamping) },
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
	fluidVelXSum, fluidVelYSum := 0.0, 0.0

	hasBounds := bMaxX > bMinX && bMaxY > bMinY
	for _, tr := range areas {
		if remFrac <= 0 {
			break
		}
		a := tr.Area
		fd := sanitize32(a.FluidDensity)
		flDrag := max(0, sanitize32(a.FluidLinearDrag))
		faDrag := max(0, sanitize32(a.FluidAngularDrag))
		if fd <= 0 && flDrag <= 0 && faDrag <= 0 {
			continue
		}
		frac := 1.0
		if hasBounds {
			frac = boundsOverlapFraction2D(bMinX, bMinY, bMaxX, bMaxY, tr.Shape.Bounds())
		}
		if frac <= 0 {
			continue
		}
		used := min(frac, remFrac)
		remFrac -= used
		totalSubFrac += used
		if fd > 0 && bodyArea > 0 {
			buoyancyMass += float64(fd * float64(used*bodyArea))
		}
		if flDrag > 0 {
			wDrag := float64(flDrag * used)
			totalLinDrag += wDrag
			velWeightSum += wDrag
			fluidVelXSum += float64(sanitize32(a.FluidVelX) * wDrag)
			fluidVelYSum += float64(sanitize32(a.FluidVelY) * wDrag)
		}
		if faDrag > 0 {
			totalAngDrag += float64(faDrag * used)
		}
	}

	var buoyX, buoyY float64
	if buoyancyMass > 0 && invMass > 0 {
		buoyScale := buoyancyMass * invMass
		buoyX = -gxAcc * buoyScale
		buoyY = -gyAcc * buoyScale
	}
	var fvx, fvy float64
	if velWeightSum > 0 {
		fvx = fluidVelXSum / velWeightSum
		fvy = fluidVelYSum / velWeightSum
	}

	return AreaEffect{
		GravityX:          float32(gxAcc),
		GravityY:          float32(gyAcc),
		LinearDamping:     float32(linDamp),
		AngularDamping:    float32(angDamp),
		HasLinearDamping:  hasLin,
		HasAngularDamping: hasAng,
		SubmergedFraction: float32(totalSubFrac),
		BuoyancyAccelX:    float32(buoyX),
		BuoyancyAccelY:    float32(buoyY),
		FluidLinearDrag:   float32(totalLinDrag),
		FluidAngularDrag:  float32(totalAngDrag),
		FluidVelX:         float32(fvx),
		FluidVelY:         float32(fvy),
	}
}
