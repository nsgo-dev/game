package navbake_test

import (
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/navbake"
	"github.com/nsgo-dev/nsgo/core"
)

func TestMeshTiledNavMesh(t *testing.T) {
	src := navbake.Source{
		Outlines: [][]core.Point{{
			{X: 0, Y: 0}, {X: 120, Y: 0}, {X: 120, Y: 60}, {X: 0, Y: 60},
		}},
	}
	baked, err := navbake.Bake(src, navbake.Agent{Name: "hero", Radius: 2}, navbake.Options{})
	if err != nil {
		t.Fatalf("Bake failed: %v", err)
	}
	tm := baked.TiledNavMesh(32)
	if tm.TileCount() == 0 {
		t.Fatal("expected non-empty TiledNavMesh from baked 2D mesh")
	}
	start := core.Point{X: 10, Y: 30}
	goal := core.Point{X: 110, Y: 30}
	if _, ok := tm.FindPath(start, goal); !ok {
		t.Fatal("expected initial path across baked tiled mesh")
	}
	id, ok := tm.AddObstacle(ai.NavBoxObstacle(core.Point{X: 55, Y: 20}, core.Point{X: 65, Y: 40}))
	if !ok {
		t.Fatal("AddObstacle failed")
	}
	detour, ok := tm.FindPath(start, goal)
	if !ok || len(detour) <= 2 {
		t.Fatalf("expected detour around dynamic obstacle, got ok=%v path=%v", ok, detour)
	}
	tm.RemoveObstacle(id)
	restored, ok := tm.FindPath(start, goal)
	if !ok || len(restored) != 2 {
		t.Fatalf("expected straight restored path, got ok=%v path=%v", ok, restored)
	}
}
