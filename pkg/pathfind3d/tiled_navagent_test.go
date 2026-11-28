package pathfind3d

import (
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
	"testing"
	"time"
)

func TestTiledNavAgentReplansWhenWatchedTileChanges(t *testing.T) {
	tm := NewTiledNavMesh(TiledNavMeshConfig{TileSize: 16})
	tile := tm.BakeTile(world3d.ChunkCoord{X: 0, Z: 0}, []NavPolygonInput{{Vertices: []math3d.Vec3{{X: 0, Z: 0}, {X: 16, Z: 0}, {X: 16, Z: 8}, {X: 0, Z: 8}}}})
	if !tm.SetTile(world3d.ChunkCoord{}, tile) {
		t.Fatal("SetTile failed")
	}
	a := NewTiledNavAgent(tm, math3d.Vec3{X: 2, Z: 4}, TiledNavAgentConfig{MaxSpeed: 4})
	a.SetTarget(math3d.Vec3{X: 14, Z: 4})
	if a.State() != TiledNavAgentMoving || len(a.Corridor()) == 0 {
		t.Fatalf("agent did not find corridor: state %v", a.State())
	}
	before := a.ReplanCount()
	if !tm.RemoveTile(world3d.ChunkCoord{}) {
		t.Fatal("RemoveTile failed")
	}
	if !a.NeedsReplan() {
		t.Fatal("tile change did not invalidate agent")
	}
	a.Step(time.Second / 60)
	if a.ReplanCount() <= before || a.State() != TiledNavAgentUnreachable {
		t.Fatalf("after unload: replans=%d state=%v", a.ReplanCount(), a.State())
	}
}
