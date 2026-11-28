package main

import (
	"math"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// aimTarget3D is the template's pinned-module adapter for the shared input
// aim-assist contract. It keeps generated projects buildable before the next
// game module release carries input.AimAssist.
type aimTarget3D struct {
	Position math3d.Vec3
	Radius   float32
	Priority int
}

type aimAssist3D struct {
	Strength, OuterRadius, Magnetism float32
}

func (a aimAssist3D) evaluate(origin, direction math3d.Vec3, targets []aimTarget3D, visible func(math3d.Vec3, math3d.Vec3) bool) math3d.Vec3 {
	direction = direction.Norm()
	if a.Strength <= 0 || direction.LenSq() == 0 {
		return direction
	}
	outer, magnetism := a.OuterRadius, a.Magnetism
	if outer <= 0 {
		outer = 0.5
	}
	if magnetism <= 0 {
		magnetism = 0.35
	}
	var best aimTarget3D
	bestAngle := float32(math.MaxFloat32)
	found := false
	for _, target := range targets {
		offset := target.Position.Sub(origin)
		distance := offset.Len()
		if distance == 0 || (visible != nil && !visible(origin, target.Position)) {
			continue
		}
		toTarget := offset.Scale(1 / distance)
		dot := float64(direction.Dot(toTarget))
		dot = math.Max(-1, math.Min(1, dot))
		angle := float32(math.Acos(dot))
		limit := outer
		if target.Radius > 0 {
			limit += float32(math.Atan(float64(target.Radius / distance)))
		}
		if angle > limit || (found && (target.Priority < best.Priority || (target.Priority == best.Priority && angle >= bestAngle))) {
			continue
		}
		best, bestAngle, found = target, angle, true
	}
	if !found {
		return direction
	}
	amount := magnetism * a.Strength * (1 - float32(math.Min(1, float64(bestAngle/outer))))
	return direction.Lerp(best.Position.Sub(origin).Norm(), amount).Norm()
}
