package navbake3d

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
)

func TestGeometryTriangleIndexKeepsTileCandidatesDeterministic(t *testing.T) {
	var g Geometry
	for i := 0; i < 100; i++ {
		x := float32(i * 40)
		g.AddTriangle(math3d.Vec3{X: x, Y: 0}, math3d.Vec3{X: x + 10, Y: 0}, math3d.Vec3{X: x, Y: 0, Z: 10}, 1)
	}
	b := math3d.AABB{Min: math3d.Vec3{X: 78, Y: -1, Z: -1}, Max: math3d.Vec3{X: 122, Y: 1, Z: 11}}
	got := g.trianglesInBounds(b)
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Fatalf("candidates = %v, want [2 3]", got)
	}
	for i := range 10 {
		if again := g.trianglesInBounds(b); len(again) != len(got) || again[0] != got[0] || again[1] != got[1] {
			t.Fatalf("run %d candidates = %v, want %v", i, again, got)
		}
	}
}
