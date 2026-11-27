package voxel

import (
	"math"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
)

// AABBMoveResult reports the outcome of [MoveAndSlideAABB].
type AABBMoveResult struct {
	// Box is the resolved world-space AABB after axis-separated collision and
	// optional ledge step-up.
	Box math3d.AABB
	// Moved is the actual displacement applied to Box (Box.Min - initial.Min).
	Moved math3d.Vec3
	// Grounded reports whether the resolved box rests on a solid block underneath.
	Grounded bool
	// OnWall reports whether horizontal movement was blocked on X or Z.
	OnWall bool
	// OnCeiling reports whether upward movement was blocked by a solid block above.
	OnCeiling bool
	// SteppedUp reports whether the box automatically climbed a ledge up to stepHeight.
	SteppedUp bool
	// WallNormal is the horizontal unit normal of the last blocking wall face,
	// or the zero vector when OnWall is false.
	WallNormal math3d.Vec3
}

// MoveAndSlideAABB moves an axis-aligned box through solid blocks in w by delta
// using axis-separated swept AABB resolution (Y, then X, then Z).
//
// Because each axis sweeps strictly against faces perpendicular to that axis,
// flat floors and walls never snag on internal seams between adjacent blocks.
// When blocked horizontally while grounded and stepHeight > 0 (with delta.Y <= 0),
// MoveAndSlideAABB attempts an automatic step-up up to stepHeight (checking
// ceiling headroom and greater horizontal progress) so characters walk cleanly
// onto single-block ledges.
func MoveAndSlideAABB(w *World, box math3d.AABB, delta math3d.Vec3, stepHeight float32) AABBMoveResult {
	if w == nil || box.Empty() || !isFiniteVec3(box.Min) || !isFiniteVec3(box.Max) || !isFiniteVec3(delta) {
		return AABBMoveResult{Box: box}
	}

	vs := float64(w.voxelSize)
	if vs <= 0 {
		return AABBMoveResult{Box: box}
	}
	eps := math.Min(1e-4, vs*1e-4)

	start := aabb64{
		minX: float64(box.Min.X),
		minY: float64(box.Min.Y),
		minZ: float64(box.Min.Z),
		maxX: float64(box.Max.X),
		maxY: float64(box.Max.Y),
		maxZ: float64(box.Max.Z),
	}
	dx := float64(delta.X)
	dy := float64(delta.Y)
	dz := float64(delta.Z)

	// Check whether the box starts resting on solid ground.
	_, startedGrounded := sweepBoxAxisY(w, start, -2*eps, vs, eps)

	// 1. Resolve Y first so jumps clear ledge lips before horizontal motion and
	//    gravity lands the box before horizontal step-up checks.
	afterY := start
	groundedY := false
	onCeiling := false
	if dy != 0 {
		my, hitY := sweepBoxAxisY(w, start, dy, vs, eps)
		afterY.minY += my
		afterY.maxY += my
		if hitY {
			if dy < 0 {
				groundedY = true
			} else {
				onCeiling = true
			}
		}
	}

	// 2. Resolve X then Z from afterY.
	baseBox, hitX, hitZ, wallN := resolveHorizontalXZ(w, afterY, dx, dz, vs, eps)
	onWall := hitX || hitZ
	finalBox := baseBox
	steppedUp := false
	grounded := groundedY

	// 3. Automatic single-block ledge step-up when blocked horizontally while grounded.
	if onWall && stepHeight > 0 && (startedGrounded || groundedY) && dy <= 0 {
		stepH := float64(stepHeight)
		upMoved, _ := sweepBoxAxisY(w, afterY, stepH, vs, eps)
		if upMoved > eps {
			raised := afterY
			raised.minY += upMoved
			raised.maxY += upMoved

			stepBox, sHitX, sHitZ, sWallN := resolveHorizontalXZ(w, raised, dx, dz, vs, eps)
			baseDistSq := sq64(baseBox.minX-afterY.minX) + sq64(baseBox.minZ-afterY.minZ)
			stepDistSq := sq64(stepBox.minX-afterY.minX) + sq64(stepBox.minZ-afterY.minZ)
			if stepDistSq > baseDistSq+1e-8 {
				downMoved, hitLand := sweepBoxAxisY(w, stepBox, -(upMoved + 2*eps), vs, eps)
				landed := stepBox
				landed.minY += downMoved
				landed.maxY += downMoved
				if hitLand && landed.minY > afterY.minY+eps && landed.minY <= afterY.minY+stepH+2*eps {
					finalBox = landed
					steppedUp = true
					grounded = true
					onWall = sHitX || sHitZ
					wallN = sWallN
				}
			}
		}
	}

	// If not yet flagged grounded and not rising, probe immediately beneath the
	// resolved box so walking across flat ground with delta.Y == 0 stays grounded.
	if !grounded && dy <= 0 {
		if _, onGround := sweepBoxAxisY(w, finalBox, -2*eps, vs, eps); onGround {
			grounded = true
		}
	}

	outBox := math3d.AABB{
		Min: math3d.Vec3{X: float32(finalBox.minX), Y: float32(finalBox.minY), Z: float32(finalBox.minZ)},
		Max: math3d.Vec3{X: float32(finalBox.maxX), Y: float32(finalBox.maxY), Z: float32(finalBox.maxZ)},
	}
	return AABBMoveResult{
		Box:        outBox,
		Moved:      outBox.Min.Sub(box.Min),
		Grounded:   grounded,
		OnWall:     onWall,
		OnCeiling:  onCeiling,
		SteppedUp:  steppedUp,
		WallNormal: wallN,
	}
}

type aabb64 struct {
	minX, minY, minZ float64
	maxX, maxY, maxZ float64
}

func sq64(x float64) float64 { return x * x }

func resolveHorizontalXZ(w *World, b aabb64, dx, dz, vs, eps float64) (out aabb64, hitX, hitZ bool, wallN math3d.Vec3) {
	out = b
	if dx != 0 {
		mx, hx := sweepBoxAxisX(w, out, dx, vs, eps)
		out.minX += mx
		out.maxX += mx
		if hx {
			hitX = true
			if dx > 0 {
				wallN = math3d.Vec3{X: -1}
			} else {
				wallN = math3d.Vec3{X: 1}
			}
		}
	}
	if dz != 0 {
		mz, hz := sweepBoxAxisZ(w, out, dz, vs, eps)
		out.minZ += mz
		out.maxZ += mz
		if hz {
			hitZ = true
			if dz > 0 {
				wallN = math3d.Vec3{Z: -1}
			} else {
				wallN = math3d.Vec3{Z: 1}
			}
		}
	}
	return out, hitX, hitZ, wallN
}

func spanRange(lo, hi, vs, eps float64) (int, int) {
	a := lo + eps
	b := hi - eps
	if a > b {
		mid := (lo + hi) * 0.5
		i := int(math.Floor(mid / vs))
		return i, i
	}
	return int(math.Floor(a / vs)), int(math.Floor(b / vs))
}

func sweepBoxAxisY(w *World, b aabb64, dy, vs, eps float64) (float64, bool) {
	if dy == 0 {
		return 0, false
	}
	x0, x1 := spanRange(b.minX, b.maxX, vs, eps)
	z0, z1 := spanRange(b.minZ, b.maxZ, vs, eps)

	if dy < 0 {
		yStart := int(math.Floor((b.minY - eps) / vs))
		yEnd := int(math.Floor((b.minY + dy) / vs))
		for by := yStart; by >= yEnd; by-- {
			blockTop := float64(by+1) * vs
			if blockTop > b.minY+eps {
				continue
			}
			for bz := z0; bz <= z1; bz++ {
				for bx := x0; bx <= x1; bx++ {
					if w.isSolidXYZ(bx, by, bz) {
						allowed := blockTop - b.minY
						if allowed > 0 {
							allowed = 0
						}
						if allowed >= dy {
							return allowed, true
						}
						return dy, true
					}
				}
			}
		}
		return dy, false
	}

	yStart := int(math.Floor((b.maxY + eps) / vs))
	yEnd := int(math.Floor((b.maxY + dy) / vs))
	for by := yStart; by <= yEnd; by++ {
		blockBot := float64(by) * vs
		if blockBot < b.maxY-eps {
			continue
		}
		for bz := z0; bz <= z1; bz++ {
			for bx := x0; bx <= x1; bx++ {
				if w.isSolidXYZ(bx, by, bz) {
					allowed := blockBot - b.maxY
					if allowed < 0 {
						allowed = 0
					}
					if allowed <= dy {
						return allowed, true
					}
					return dy, true
				}
			}
		}
	}
	return dy, false
}

func sweepBoxAxisX(w *World, b aabb64, dx, vs, eps float64) (float64, bool) {
	if dx == 0 {
		return 0, false
	}
	y0, y1 := spanRange(b.minY, b.maxY, vs, eps)
	z0, z1 := spanRange(b.minZ, b.maxZ, vs, eps)

	if dx < 0 {
		xStart := int(math.Floor((b.minX - eps) / vs))
		xEnd := int(math.Floor((b.minX + dx) / vs))
		for bx := xStart; bx >= xEnd; bx-- {
			blockMax := float64(bx+1) * vs
			if blockMax > b.minX+eps {
				continue
			}
			for by := y0; by <= y1; by++ {
				for bz := z0; bz <= z1; bz++ {
					if w.isSolidXYZ(bx, by, bz) {
						allowed := blockMax - b.minX
						if allowed > 0 {
							allowed = 0
						}
						if allowed >= dx {
							return allowed, true
						}
						return dx, true
					}
				}
			}
		}
		return dx, false
	}

	xStart := int(math.Floor((b.maxX + eps) / vs))
	xEnd := int(math.Floor((b.maxX + dx) / vs))
	for bx := xStart; bx <= xEnd; bx++ {
		blockMin := float64(bx) * vs
		if blockMin < b.maxX-eps {
			continue
		}
		for by := y0; by <= y1; by++ {
			for bz := z0; bz <= z1; bz++ {
				if w.isSolidXYZ(bx, by, bz) {
					allowed := blockMin - b.maxX
					if allowed < 0 {
						allowed = 0
					}
					if allowed <= dx {
						return allowed, true
					}
					return dx, true
				}
			}
		}
	}
	return dx, false
}

func sweepBoxAxisZ(w *World, b aabb64, dz, vs, eps float64) (float64, bool) {
	if dz == 0 {
		return 0, false
	}
	x0, x1 := spanRange(b.minX, b.maxX, vs, eps)
	y0, y1 := spanRange(b.minY, b.maxY, vs, eps)

	if dz < 0 {
		zStart := int(math.Floor((b.minZ - eps) / vs))
		zEnd := int(math.Floor((b.minZ + dz) / vs))
		for bz := zStart; bz >= zEnd; bz-- {
			blockMax := float64(bz+1) * vs
			if blockMax > b.minZ+eps {
				continue
			}
			for by := y0; by <= y1; by++ {
				for bx := x0; bx <= x1; bx++ {
					if w.isSolidXYZ(bx, by, bz) {
						allowed := blockMax - b.minZ
						if allowed > 0 {
							allowed = 0
						}
						if allowed >= dz {
							return allowed, true
						}
						return dz, true
					}
				}
			}
		}
		return dz, false
	}

	zStart := int(math.Floor((b.maxZ + eps) / vs))
	zEnd := int(math.Floor((b.maxZ + dz) / vs))
	for bz := zStart; bz <= zEnd; bz++ {
		blockMin := float64(bz) * vs
		if blockMin < b.maxZ-eps {
			continue
		}
		for by := y0; by <= y1; by++ {
			for bx := x0; bx <= x1; bx++ {
				if w.isSolidXYZ(bx, by, bz) {
					allowed := blockMin - b.maxZ
					if allowed < 0 {
						allowed = 0
					}
					if allowed <= dz {
						return allowed, true
					}
					return dz, true
				}
			}
		}
	}
	return dz, false
}

// CharacterMoveResult summarizes the outcome of [CharacterMover.Move].
type CharacterMoveResult struct {
	Pos        math3d.Vec3
	Grounded   bool
	OnWall     bool
	OnCeiling  bool
	SteppedUp  bool
	WallNormal math3d.Vec3
}

// CharacterMover drives a simple axis-aligned box character directly against a
// voxel [World] using axis-separated AABB move-and-slide with gravity, jumping,
// and automatic single-block ledge step-up, without needing a [physics3d.World].
//
// Pos is the world-space bottom-center of the character's box (the feet).
type CharacterMover struct {
	// Pos is the world-space bottom-center of the character's AABB (feet).
	Pos math3d.Vec3
	// HalfWidth is the half-extent along X.
	HalfWidth float32
	// HalfDepth is the half-extent along Z.
	HalfDepth float32
	// Height is the full vertical height of the AABB along +Y.
	Height float32
	// StepHeight is the maximum vertical ledge height climbed automatically while
	// grounded. NewCharacterMover defaults this to 1.0 (one default voxel); set
	// to 0 to disable automatic step-up.
	StepHeight float32
	// Gravity is the vertical acceleration in units/s^2 (negative for -Y).
	// NewCharacterMover defaults this to -9.81.
	Gravity float32
	// Vel is the character's current velocity in units per second.
	Vel math3d.Vec3
	// Grounded reports whether the character ended the last Move on solid blocks.
	Grounded bool
	// OnWall reports whether the last Move was blocked horizontally.
	OnWall bool
	// OnCeiling reports whether the last Move hit a ceiling block above.
	OnCeiling bool
	// WallNormal is the horizontal unit normal of the last blocking wall hit.
	WallNormal math3d.Vec3
}

// NewCharacterMover returns a CharacterMover centered horizontally at pos
// (feet at pos.Y) with square horizontal footprint [-halfWidth, +halfWidth],
// vertical height, default StepHeight of 1.0, and Gravity of -9.81.
func NewCharacterMover(pos math3d.Vec3, halfWidth, height float32) *CharacterMover {
	return &CharacterMover{
		Pos:        pos,
		HalfWidth:  halfWidth,
		HalfDepth:  halfWidth,
		Height:     height,
		StepHeight: DefaultVoxelSize,
		Gravity:    -9.81,
	}
}

// Bounds returns the character's current world-space AABB.
func (c *CharacterMover) Bounds() math3d.AABB {
	hw := c.HalfWidth
	hd := c.HalfDepth
	if hd <= 0 {
		hd = hw
	}
	return math3d.AABB{
		Min: math3d.Vec3{X: c.Pos.X - hw, Y: c.Pos.Y, Z: c.Pos.Z - hd},
		Max: math3d.Vec3{X: c.Pos.X + hw, Y: c.Pos.Y + c.Height, Z: c.Pos.Z + hd},
	}
}

// Move advances the character through w for dt using axis-separated AABB
// move-and-slide. wishMove.X and wishMove.Z set horizontal velocity directly;
// a non-zero wishMove.Y sets Vel.Y (such as for a jump) while wishMove.Y == 0
// lets Gravity govern vertical motion.
func (c *CharacterMover) Move(w *World, wishMove math3d.Vec3, dt time.Duration) CharacterMoveResult {
	if c == nil || w == nil || dt <= 0 || c.HalfWidth <= 0 || c.Height <= 0 {
		if c == nil {
			return CharacterMoveResult{}
		}
		return CharacterMoveResult{
			Pos:        c.Pos,
			Grounded:   c.Grounded,
			OnWall:     c.OnWall,
			OnCeiling:  c.OnCeiling,
			WallNormal: c.WallNormal,
		}
	}

	dtSec := float32(dt.Seconds())
	wasGrounded := c.Grounded

	c.Vel.X = wishMove.X
	c.Vel.Z = wishMove.Z
	jumping := false
	if wishMove.Y != 0 {
		c.Vel.Y = wishMove.Y
		if wishMove.Y > 0 {
			jumping = true
		}
	} else if c.Vel.Y > 0 {
		jumping = true
	}

	c.Vel.Y += c.Gravity * dtSec
	if wasGrounded && !jumping && c.Vel.Y < 0 {
		c.Vel.Y = c.Gravity * dtSec
	}

	stepH := c.StepHeight
	if jumping {
		stepH = 0
	}

	res := MoveAndSlideAABB(w, c.Bounds(), c.Vel.Scale(dtSec), stepH)
	c.Pos = math3d.Vec3{
		X: (res.Box.Min.X + res.Box.Max.X) * 0.5,
		Y: res.Box.Min.Y,
		Z: (res.Box.Min.Z + res.Box.Max.Z) * 0.5,
	}
	c.Grounded = res.Grounded
	c.OnWall = res.OnWall
	c.OnCeiling = res.OnCeiling
	c.WallNormal = res.WallNormal

	if (res.Grounded && c.Vel.Y < 0) || (res.OnCeiling && c.Vel.Y > 0) {
		c.Vel.Y = 0
	}

	return CharacterMoveResult{
		Pos:        c.Pos,
		Grounded:   c.Grounded,
		OnWall:     c.OnWall,
		OnCeiling:  c.OnCeiling,
		SteppedUp:  res.SteppedUp,
		WallNormal: c.WallNormal,
	}
}
