package scene3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/scene3d"
)

func TestMeshColliderBoundsCacheFollowsTriangleChanges(t *testing.T) {
	m := &scene3d.MeshCollider{Triangles: []math3d.Vec3{{}, {X: 1}, {Y: 1}}}
	if got := m.LocalBounds(); got.Max.X != 1 {
		t.Fatalf("bounds = %+v", got)
	}
	m.Triangles = []math3d.Vec3{{}, {X: 3}, {Y: 1}}
	if got := m.LocalBounds(); got.Max.X != 3 {
		t.Fatalf("bounds after replacing Triangles = %+v, want max X 3", got)
	}
	m.Triangles[1].X = 5
	m.MarkDirty()
	if got := m.LocalBounds(); got.Max.X != 5 {
		t.Fatalf("bounds after in-place edit and MarkDirty = %+v, want max X 5", got)
	}
}

func TestDeepHierarchyVisibilityIsFilteredPerQuery(t *testing.T) {
	sc := scene3d.NewScene()
	parent := sc.Root()
	var chain []*scene3d.Node
	for range 200 {
		n := scene3d.NewNode("link")
		n.SetLocalBounds(math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}})
		if err := parent.AddChild(n); err != nil {
			t.Fatal(err)
		}
		chain = append(chain, n)
		parent = n
	}
	box := math3d.AABB{Min: math3d.Vec3{X: -2, Y: -2, Z: -2}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2}}
	if got := len(sc.QueryAABB(box)); got != 200 {
		t.Fatalf("visible chain query = %d, want 200", got)
	}
	chain[100].SetVisible(false)
	if got := len(sc.QueryAABB(box)); got != 100 {
		t.Fatalf("after hiding link 100 = %d, want 100", got)
	}
	chain[100].SetVisible(true)
	chain[150].Enabled = false
	if got := len(sc.QueryAABB(box)); got != 150 {
		t.Fatalf("after disabling link 150 = %d, want 150", got)
	}
}

func BenchmarkDeepHierarchyQuery(b *testing.B) {
	sc := scene3d.NewScene()
	parent := sc.Root()
	for range 6000 {
		n := scene3d.NewNode("link")
		n.SetLocalBounds(math3d.AABB{Min: math3d.Vec3{X: -1, Y: -1, Z: -1}, Max: math3d.Vec3{X: 1, Y: 1, Z: 1}})
		_ = parent.AddChild(n)
		parent = n
	}
	box := math3d.AABB{Min: math3d.Vec3{X: -2, Y: -2, Z: -2}, Max: math3d.Vec3{X: 2, Y: 2, Z: 2}}
	sc.QueryAABB(box)
	b.ResetTimer()
	for b.Loop() {
		sc.QueryAABB(box)
	}
}
