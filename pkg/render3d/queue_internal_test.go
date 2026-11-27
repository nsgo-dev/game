package render3d

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
)

func TestLODHistoryEvictsUnseenIDs(t *testing.T) {
	// #546: hysteresis history for transient IDs must not accumulate forever.
	vp := math3d.OrthoNO(-2, 2, -2, 2, 0.1, 100)
	var q Queue
	q.LODHysteresis = 0.1
	levels := []LODLevel{{MeshID: 1, MaxDistance: 10}, {MeshID: 2}}
	item := func(id uint64) DrawItem {
		return DrawItem{ID: id, MeshID: 1, MaterialID: 1, PipelineID: 1,
			WorldBounds: math3d.AABB{Min: math3d.Vec3{X: -0.5, Y: -0.5, Z: -1.5}, Max: math3d.Vec3{X: 0.5, Y: 0.5, Z: -0.5}},
			Triangles:   12, Vertices: 8, LODs: levels}
	}
	q.Build(vp, math3d.Vec3Zero(), []DrawItem{item(1), item(2)})
	if len(q.prevLOD) != 2 {
		t.Fatalf("history = %d, want 2", len(q.prevLOD))
	}
	q.Build(vp, math3d.Vec3Zero(), []DrawItem{item(3)})
	if len(q.prevLOD) != 1 {
		t.Fatalf("history after eviction = %d, want 1", len(q.prevLOD))
	}
	if _, ok := q.prevLOD[3]; !ok {
		t.Error("current ID 3 is missing from history")
	}
}
