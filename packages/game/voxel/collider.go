package voxel

import "github.com/nsgo-dev/game/packages/game/physics3d"

// worldVoxelGrid adapts a [*World] to [physics3d.VoxelGrid],
// [physics3d.VoxelBoundsGrid], and [physics3d.VoxelEditNotifier].
type worldVoxelGrid struct {
	world *World
}

func (g worldVoxelGrid) VoxelSize() float32 {
	if g.world == nil {
		return 0
	}
	return g.world.voxelSize
}

func (g worldVoxelGrid) IsSolid(x, y, z int) bool {
	if g.world == nil {
		return false
	}
	return g.world.isSolidXYZ(x, y, z)
}

func (g worldVoxelGrid) VoxelBounds() (physics3d.Vec3, physics3d.Vec3, bool) {
	if g.world == nil {
		return physics3d.Vec3{}, physics3d.Vec3{}, false
	}
	b := g.world.Bounds()
	if b.Empty() {
		return physics3d.Vec3{}, physics3d.Vec3{}, false
	}
	return physics3d.Vec3{X: b.Min.X, Y: b.Min.Y, Z: b.Min.Z},
		physics3d.Vec3{X: b.Max.X, Y: b.Max.Y, Z: b.Max.Z},
		true
}

func (g worldVoxelGrid) WatchVoxelEdits(onBlock func(x, y, z int), onReset func()) func() {
	if g.world == nil {
		return func() {}
	}
	return g.world.watchSolidEdits(onBlock, onReset)
}

// NewCollider returns a [*physics3d.VoxelCollider] backed directly by w.
//
// Overlap, raycast, and sweep queries read solid blocks directly from w without
// building a triangle mesh, so [World.SetBlock] edits take effect immediately
// with zero rebuild. When added to a [*physics3d.World] via
// [physics3d.World.AddVoxelCollider], solid-block edits automatically wake
// sleeping dynamic bodies near the edited voxel.
func NewCollider(w *World) *physics3d.VoxelCollider {
	if w == nil {
		return physics3d.NewVoxelCollider(nil)
	}
	return physics3d.NewVoxelCollider(worldVoxelGrid{world: w})
}

// Collider returns a [*physics3d.VoxelCollider] backed directly by w.
// It is equivalent to [NewCollider](w).
func (w *World) Collider() *physics3d.VoxelCollider {
	return NewCollider(w)
}
