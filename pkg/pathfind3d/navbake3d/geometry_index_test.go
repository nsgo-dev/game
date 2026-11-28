package navbake3d

import (
	"math"
	"reflect"
	"slices"
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func TestGeometryTriangleIndexKeepsTileCandidatesDeterministic(t *testing.T) {
	var g Geometry
	for i := 0; i < 100; i++ {
		x := float32(i * 40)
		g.AddTriangle(math3d.Vec3{X: x, Y: 0}, math3d.Vec3{X: x + 10, Y: 0}, math3d.Vec3{X: x, Y: 0, Z: 10}, 1)
	}
	b := math3d.AABB{Min: math3d.Vec3{X: 78, Y: -1, Z: -1}, Max: math3d.Vec3{X: 122, Y: 1, Z: 11}}
	got := g.trianglesInBounds(b)
	if !slices.Equal(got, []int{2, 3}) {
		t.Fatalf("candidates = %v, want [2 3]", got)
	}
	for i := range 10 {
		if again := g.trianglesInBounds(b); !slices.Equal(again, got) {
			t.Fatalf("run %d candidates = %v, want %v", i, again, got)
		}
	}
}

func TestGeometryIndexKeepsLargeTrianglesOutOfTheGrid(t *testing.T) {
	var g Geometry
	// One triangle of a 100 km ground plane would cover millions of cells.
	g.AddTriangle(math3d.Vec3{X: -50000}, math3d.Vec3{X: 50000}, math3d.Vec3{Z: 50000}, 1)
	g.AddTriangle(math3d.Vec3{X: 1, Y: 1, Z: 1}, math3d.Vec3{X: 2, Y: 1, Z: 1}, math3d.Vec3{X: 1, Y: 1, Z: 2}, 1)
	if len(g.large) != 1 || g.large[0] != 0 {
		t.Fatalf("large = %v, want [0]", g.large)
	}
	if n := len(g.grid); n != 1 {
		t.Fatalf("grid holds %d cells, want 1", n)
	}
	near := math3d.AABB{Min: math3d.Vec3{X: 0, Y: -1, Z: 0}, Max: math3d.Vec3{X: 4, Y: 2, Z: 4}}
	if got := g.trianglesInBounds(near); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("candidates near the origin = %v, want [0 1]", got)
	}
	far := math3d.AABB{Min: math3d.Vec3{X: 1000, Y: -1, Z: 1000}, Max: math3d.Vec3{X: 1010, Y: 2, Z: 1010}}
	if got := g.trianglesInBounds(far); !slices.Equal(got, []int{0}) {
		t.Fatalf("candidates far away = %v, want [0]", got)
	}
	huge := math3d.AABB{Min: math3d.Vec3{X: -math.MaxFloat32, Y: float32(math.Inf(-1)), Z: -math.MaxFloat32}, Max: math3d.Vec3{X: math.MaxFloat32, Y: float32(math.Inf(1)), Z: math.MaxFloat32}}
	if got := g.trianglesInBounds(huge); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("candidates in the whole float range = %v, want [0 1]", got)
	}
}

func TestBakeTileWithIndexMatchesFullScan(t *testing.T) {
	var g Geometry
	g.AddTriangle(math3d.Vec3{}, math3d.Vec3{Z: 16}, math3d.Vec3{X: 32, Z: 16}, LayerAll)
	g.AddTriangle(math3d.Vec3{}, math3d.Vec3{X: 32, Z: 16}, math3d.Vec3{X: 32}, LayerAll)
	g.addBox(math3d.Vec3{X: 8, Y: 0.5, Z: 8}, math3d.Vec3{X: 1, Y: 0.5, Z: 1}, math3d.QuatIdentity(), LayerAll)
	// Unrelated geometry elsewhere in the level, including a tall wall that would raise the tile's height range if it were counted.
	for i := range 200 {
		x := float32(200 + 20*(i%20))
		z := float32(200 + 20*(i/20))
		g.AddTriangle(math3d.Vec3{X: x, Y: 50, Z: z}, math3d.Vec3{X: x, Y: 50, Z: z + 5}, math3d.Vec3{X: x + 5, Y: 50, Z: z}, LayerAll)
	}
	settings := Settings{CellSize: 0.5, CellHeight: 0.2}
	agent := AgentType{Name: "hero", Radius: 0.5, Height: 2, MaxClimb: 0.5, MaxSlope: 45}

	full := g
	// Without grid cells every query takes the linear scan over all triangles, the behavior before the index.
	full.grid = nil
	full.large = nil
	for _, coord := range []world3d.ChunkCoord{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 12, Z: 12}} {
		indexed, err := BakeTile(&g, settings, agent, coord, 16, math3d.Vec3{})
		if err != nil {
			t.Fatalf("BakeTile %v with the index: %v", coord, err)
		}
		scanned, err := BakeTile(&full, settings, agent, coord, 16, math3d.Vec3{})
		if err != nil {
			t.Fatalf("BakeTile %v with a full scan: %v", coord, err)
		}
		if !reflect.DeepEqual(indexed, scanned) {
			t.Fatalf("BakeTile %v differs between the index and a full scan", coord)
		}
	}
}
