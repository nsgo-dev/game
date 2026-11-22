package render3d

import (
	"testing"

	"github.com/nsgo-dev/game/packages/game/math3d"
)

func TestOcclusionBufferRejectsAnotherView(t *testing.T) {
	// #546: a buffer rasterized for view A must not cull against view B.
	a := math3d.OrthoNO(-2, 2, -2, 2, 0.1, 100)
	bView := math3d.PerspectiveNO(math3d.DegToRad(60), 1, 0.1, 100)
	occ := NewOcclusionBuffer(8, 8)
	occ.hasGeometry = true
	occ.rasterSet = true
	occ.rasterView = a
	bounds := math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -2.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: -1.5}}
	if !occ.TestAABB(bounds, bView) {
		t.Fatal("stale occlusion buffer culled an item for another view")
	}
}
