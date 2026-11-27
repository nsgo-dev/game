package navbake3d_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d/navbake3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func TestBakeTileAndTiledNavMesh(t *testing.T) {
	var g navbake3d.Geometry
	// Flat walkable floor spanning [0, 32] x [0, 16] at y=0 (2 tiles of size 16).
	a := math3d.Vec3{X: 0, Y: 0, Z: 0}
	b := math3d.Vec3{X: 0, Y: 0, Z: 16}
	c := math3d.Vec3{X: 32, Y: 0, Z: 16}
	d := math3d.Vec3{X: 32, Y: 0, Z: 0}
	g.AddTriangle(a, b, c, navbake3d.LayerAll)
	g.AddTriangle(a, c, d, navbake3d.LayerAll)

	settings := navbake3d.Settings{CellSize: 0.5, CellHeight: 0.2}
	agent := navbake3d.AgentType{Name: "hero", Radius: 0.5, Height: 2, MaxClimb: 0.5, MaxSlope: 45}

	const tileSize = 16
	tile0, err := navbake3d.BakeTile(&g, settings, agent, world3d.ChunkCoord{X: 0, Z: 0}, tileSize, math3d.Vec3{})
	if err != nil {
		t.Fatalf("BakeTile (0,0) failed: %v", err)
	}
	tile1, err := navbake3d.BakeTile(&g, settings, agent, world3d.ChunkCoord{X: 1, Z: 0}, tileSize, math3d.Vec3{})
	if err != nil {
		t.Fatalf("BakeTile (1,0) failed: %v", err)
	}
	if tile0.BasePolygonCount() == 0 || tile1.BasePolygonCount() == 0 {
		t.Fatalf("expected non-empty baked tiles, got %d and %d", tile0.BasePolygonCount(), tile1.BasePolygonCount())
	}

	tm := pathfind3d.NewTiledNavMesh(pathfind3d.TiledNavMeshConfig{TileSize: tileSize})
	tm.AddTile(world3d.ChunkCoord{X: 0, Z: 0}, tile0)
	tm.AddTile(world3d.ChunkCoord{X: 1, Z: 0}, tile1)

	start := math3d.Vec3{X: 3, Y: 0, Z: 8}
	goal := math3d.Vec3{X: 29, Y: 0, Z: 8}
	if _, ok := tm.FindPath(start, goal); !ok {
		t.Fatal("expected cross-tile path across independently voxel-baked 3D tiles")
	}

	// Also verify Mesh.TiledNavMesh convenience bridge.
	full, err := navbake3d.BakeMesh(&g, settings, agent)
	if err != nil {
		t.Fatalf("BakeMesh failed: %v", err)
	}
	tmFull := full.TiledNavMesh(tileSize)
	if _, ok := tmFull.FindPath(start, goal); !ok {
		t.Fatal("expected path on Mesh.TiledNavMesh")
	}
}
