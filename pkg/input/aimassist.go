package input

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/game/pkg/pathfind"
)

// AimTarget2D is a target that can be selected by AimAssist. Radius is the
// target's world-space radius and Priority breaks ties between targets at the
// same angle.
type AimTarget2D struct {
	ID       string
	Position math2d.Vec2
	Radius   float32
	Priority int
}

// AimTarget3D is a target that can be selected by AimAssist. Radius is the
// target's world-space radius and Priority breaks ties between targets at the
// same angle.
type AimTarget3D struct {
	ID       string
	Position pathfind.Vec3
	Radius   float32
	Priority int
}

// AimResult2D describes one fixed-step aim-assist evaluation.
type AimResult2D struct {
	Direction math2d.Vec2
	Target    AimTarget2D
	Found     bool
	// Friction is the multiplier to apply to look input for this step.
	Friction float32
}

// AimResult3D describes one fixed-step aim-assist evaluation.
type AimResult3D struct {
	Direction pathfind.Vec3
	Target    AimTarget3D
	Found     bool
	// Friction is the multiplier to apply to look input for this step.
	Friction float32
}

// AimAssist applies deterministic friction, magnetism and optional snap to a
// fixed-step aim direction. Strength is in [0, 1]. Angles are radians. Zero
// values use the documented defaults, so AimAssist{Strength: 1} is useful by
// itself. A line-of-sight callback may reject targets before they are chosen.
type AimAssist struct {
	Strength float32
	// InnerRadius is the angular radius in which friction is applied.
	InnerRadius float32
	// OuterRadius is the maximum angular radius for magnetism and selection.
	OuterRadius float32
	// Magnetism is the fraction of the remaining angle corrected per step.
	Magnetism float32
	// Friction is the maximum reduction in look speed inside InnerRadius.
	Friction float32
	// Snap enables snapping to a target inside SnapAngle.
	Snap      bool
	SnapAngle float32
}

const (
	defaultAimInnerRadius = float32(0.12)
	defaultAimOuterRadius = float32(0.5)
	defaultAimMagnetism   = float32(0.35)
	defaultAimFriction    = float32(0.5)
	defaultAimSnapAngle   = float32(0.08)
)

// Evaluate2D evaluates assist for a 2D aim direction. It is safe to call from
// a fixed-step update and does not retain or mutate targets.
func (a AimAssist) Evaluate2D(origin, direction math2d.Vec2, targets []AimTarget2D, visible func(from, to math2d.Vec2) bool) AimResult2D {
	direction = direction.Normalize()
	result := AimResult2D{Direction: direction, Friction: 1}
	if lenSq := direction.LenSq(); lenSq == 0 || !finiteFloat32(lenSq) || len(targets) == 0 {
		return result
	}
	cfg := a.normalized()
	var best AimTarget2D
	bestAngle, bestDistance := float32(math.MaxFloat32), float32(math.MaxFloat32)
	found := false
	for _, target := range targets {
		offset := target.Position.Sub(origin)
		distance := offset.Len()
		if distance == 0 || !finiteFloat32(distance) {
			continue
		}
		toTarget := offset.Scale(1 / distance)
		angle := absAngle2D(direction, toTarget)
		limit := cfg.OuterRadius
		if target.Radius > 0 {
			limit += float32(math.Atan(float64(target.Radius / distance)))
		}
		if angle > limit {
			continue
		}
		// A worse candidate cannot win, so skip its line-of-sight query: the callback is usually a raycast.
		if found && (target.Priority < best.Priority || (target.Priority == best.Priority && (angle > bestAngle || (angle == bestAngle && distance >= bestDistance)))) {
			continue
		}
		if visible != nil && !visible(origin, target.Position) {
			continue
		}
		// Priority is authoritative, then angle, then distance; on a full tie the earlier target in the slice is kept, so the input order is the final deterministic tie-breaker.
		best, bestAngle, bestDistance, found = target, angle, distance, true
	}
	if !found {
		return result
	}
	result.Target, result.Found = best, true
	offset := best.Position.Sub(origin)
	targetDirection := offset.Normalize()
	inner := cfg.InnerRadius
	if best.Radius > 0 {
		inner += float32(math.Atan(float64(best.Radius / maxf(best.Position.Sub(origin).Len(), 1e-6))))
	}
	if bestAngle < inner {
		result.Friction = 1 - cfg.Friction*cfg.Strength*(1-bestAngle/maxf(inner, 1e-6))
		if result.Friction < 0 {
			result.Friction = 0
		}
	}
	if cfg.Snap && bestAngle <= cfg.SnapAngle*cfg.Strength {
		result.Direction = targetDirection
	} else {
		amount := cfg.Magnetism * cfg.Strength
		if cfg.OuterRadius > 0 {
			amount *= 1 - minf(bestAngle/cfg.OuterRadius, 1)
		}
		result.Direction = direction.Lerp(targetDirection, amount).Normalize()
	}
	return result
}

// Evaluate3D is the 3D counterpart of Evaluate2D.
func (a AimAssist) Evaluate3D(origin, direction pathfind.Vec3, targets []AimTarget3D, visible func(from, to pathfind.Vec3) bool) AimResult3D {
	direction = aimNorm3(direction)
	result := AimResult3D{Direction: direction, Friction: 1}
	if lenSq := aimLenSq3(direction); lenSq == 0 || !finiteFloat32(lenSq) || len(targets) == 0 {
		return result
	}
	cfg := a.normalized()
	var best AimTarget3D
	bestAngle, bestDistance := float32(math.MaxFloat32), float32(math.MaxFloat32)
	found := false
	for _, target := range targets {
		offset := aimSub3(target.Position, origin)
		distance := aimLen3(offset)
		if distance == 0 || !finiteFloat32(distance) {
			continue
		}
		toTarget := aimScale3(offset, 1/distance)
		angle := absAngle3D(direction, toTarget)
		limit := cfg.OuterRadius
		if target.Radius > 0 {
			limit += float32(math.Atan(float64(target.Radius / distance)))
		}
		if angle > limit {
			continue
		}
		// A worse candidate cannot win, so skip its line-of-sight query: the callback is usually a raycast.
		if found && (target.Priority < best.Priority || (target.Priority == best.Priority && (angle > bestAngle || (angle == bestAngle && distance >= bestDistance)))) {
			continue
		}
		if visible != nil && !visible(origin, target.Position) {
			continue
		}
		best, bestAngle, bestDistance, found = target, angle, distance, true
	}
	if !found {
		return result
	}
	result.Target, result.Found = best, true
	offset := aimSub3(best.Position, origin)
	targetDirection := aimNorm3(offset)
	inner := cfg.InnerRadius
	if best.Radius > 0 {
		inner += float32(math.Atan(float64(best.Radius / maxf(aimLen3(aimSub3(best.Position, origin)), 1e-6))))
	}
	if bestAngle < inner {
		result.Friction = 1 - cfg.Friction*cfg.Strength*(1-bestAngle/maxf(inner, 1e-6))
		if result.Friction < 0 {
			result.Friction = 0
		}
	}
	if cfg.Snap && bestAngle <= cfg.SnapAngle*cfg.Strength {
		result.Direction = targetDirection
	} else {
		amount := cfg.Magnetism * cfg.Strength
		if cfg.OuterRadius > 0 {
			amount *= 1 - minf(bestAngle/cfg.OuterRadius, 1)
		}
		result.Direction = aimNorm3(aimLerp3(direction, targetDirection, amount))
	}
	return result
}

// Assist2D returns the assisted direction and is a shorthand for Evaluate2D.
func (a AimAssist) Assist2D(origin, direction math2d.Vec2, targets []AimTarget2D, visible func(from, to math2d.Vec2) bool) math2d.Vec2 {
	return a.Evaluate2D(origin, direction, targets, visible).Direction
}

// Assist3D returns the assisted direction and is a shorthand for Evaluate3D.
func (a AimAssist) Assist3D(origin, direction pathfind.Vec3, targets []AimTarget3D, visible func(from, to pathfind.Vec3) bool) pathfind.Vec3 {
	return a.Evaluate3D(origin, direction, targets, visible).Direction
}

func (a AimAssist) normalized() AimAssist {
	a.Strength = clamp01(a.Strength)
	if a.InnerRadius <= 0 || !finiteFloat32(a.InnerRadius) {
		a.InnerRadius = defaultAimInnerRadius
	}
	if a.OuterRadius <= 0 || !finiteFloat32(a.OuterRadius) {
		a.OuterRadius = defaultAimOuterRadius
	}
	if a.OuterRadius < a.InnerRadius {
		a.OuterRadius = a.InnerRadius
	}
	if a.Magnetism <= 0 || !finiteFloat32(a.Magnetism) {
		a.Magnetism = defaultAimMagnetism
	}
	if a.Magnetism > 1 {
		a.Magnetism = 1
	}
	if a.Friction <= 0 || !finiteFloat32(a.Friction) {
		a.Friction = defaultAimFriction
	}
	if a.Friction > 1 {
		a.Friction = 1
	}
	if a.SnapAngle <= 0 || !finiteFloat32(a.SnapAngle) {
		a.SnapAngle = defaultAimSnapAngle
	}
	return a
}

func absAngle2D(a, b math2d.Vec2) float32 {
	return float32(math.Abs(math.Atan2(float64(a.Cross(b)), float64(a.Dot(b)))))
}
func absAngle3D(a, b pathfind.Vec3) float32 {
	dot := float64(aimDot3(a, b))
	if dot > 1 {
		dot = 1
	}
	if dot < -1 {
		dot = -1
	}
	return float32(math.Acos(dot))
}
func aimSub3(a, b pathfind.Vec3) pathfind.Vec3 {
	return pathfind.Vec3{X: a.X - b.X, Y: a.Y - b.Y, Z: a.Z - b.Z}
}
func aimScale3(v pathfind.Vec3, s float32) pathfind.Vec3 {
	return pathfind.Vec3{X: v.X * s, Y: v.Y * s, Z: v.Z * s}
}
func aimDot3(a, b pathfind.Vec3) float32 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }
func aimLenSq3(v pathfind.Vec3) float32  { return aimDot3(v, v) }
func aimLen3(v pathfind.Vec3) float32    { return float32(math.Sqrt(float64(aimLenSq3(v)))) }
func aimNorm3(v pathfind.Vec3) pathfind.Vec3 {
	l := aimLen3(v)
	if l == 0 || !finiteFloat32(l) {
		return pathfind.Vec3{}
	}
	return aimScale3(v, 1/l)
}
func aimLerp3(a, b pathfind.Vec3, t float32) pathfind.Vec3 {
	return pathfind.Vec3{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t, Z: a.Z + (b.Z-a.Z)*t}
}

func clamp01(v float32) float32 {
	if !finiteFloat32(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
func finiteFloat32(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
