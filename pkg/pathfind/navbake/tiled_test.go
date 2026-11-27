package navbake_test

import (
	"bytes"
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/pathfind/navbake"
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
	id, ok := tm.AddObstacle(pathfind.NavBoxObstacle(core.Point{X: 55, Y: 20}, core.Point{X: 65, Y: 40}))
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

func TestMeshTiledNavMeshPreservesBakedOffMeshLinks(t *testing.T) {
	// Three islands:
	//   West [0,30]x[0,30]
	//   Mid  [46,76]x[0,30]
	//   East [92,122]x[0,30]
	// Connected by:
	//   - one-way cross-tile jump West -> Mid
	//   - bidirectional cross-tile ladder Mid <-> East
	//   - intra-island cheap teleport across West
	src := navbake.Source{
		Outlines: [][]core.Point{
			{{X: 0, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 30}, {X: 0, Y: 30}},
			{{X: 46, Y: 0}, {X: 76, Y: 0}, {X: 76, Y: 30}, {X: 46, Y: 30}},
			{{X: 92, Y: 0}, {X: 122, Y: 0}, {X: 122, Y: 30}, {X: 92, Y: 30}},
		},
		Links: []navbake.OffMeshLink{
			{
				ID:            10,
				Start:         navbake.Point{28, 15},
				End:           navbake.Point{48, 15},
				Radius:        1,
				Bidirectional: false,
				Cost:          6,
				Area:          pathfind.NavAreaJump,
				Flags:         uint32(pathfind.NavFlagJump),
				UserData:      "jump",
			},
			{
				ID:            20,
				Start:         navbake.Point{74, 15},
				End:           navbake.Point{94, 15},
				Radius:        1,
				Bidirectional: true,
				Area:          pathfind.NavAreaClimb,
				Flags:         uint32(pathfind.NavFlagClimb),
				UserData:      "ladder",
			},
			{
				ID:            30,
				Start:         navbake.Point{5, 5},
				End:           navbake.Point{25, 25},
				Bidirectional: false,
				Cost:          1,
				Area:          pathfind.NavAreaTeleport,
				Flags:         uint32(pathfind.NavFlagTeleport),
				UserData:      "warp",
			},
		},
	}

	baked, err := navbake.Bake(src, navbake.Agent{Name: "hero", Radius: 1}, navbake.Options{})
	if err != nil {
		t.Fatalf("Bake failed: %v", err)
	}

	// Verify YAML save/load round-trip preserves links identically before building TiledNavMesh.
	encoded1, err := baked.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	parsed, err := navbake.Parse(encoded1)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	encoded2, err := parsed.Encode()
	if err != nil {
		t.Fatalf("re-Encode failed: %v", err)
	}
	if !bytes.Equal(encoded1, encoded2) {
		t.Fatalf("YAML round-trip changed:\n%s\n---\n%s", encoded1, encoded2)
	}

	nav := parsed.NavMesh()
	tm := parsed.TiledNavMesh(24)

	if len(tm.OffMeshLinks()) != len(nav.OffMeshLinks()) || len(tm.OffMeshLinks()) != 3 {
		t.Fatalf("OffMeshLinks count = %d (nav=%d), want 3", len(tm.OffMeshLinks()), len(nav.OffMeshLinks()))
	}

	// 1. Multi-island paths West -> East must match NavMesh identically:
	//    - from (6,15): warp(30) + jump(10) + ladder(20) is strictly cheaper than walking to jump
	//    - from (15,15): walking directly to jump(10) + ladder(20) is cheaper than detouring to warp
	goal := core.Point{X: 116, Y: 15}
	for _, tc := range []struct {
		name      string
		start     core.Point
		wantLinks []int
	}{
		{"chained_warp_jump_ladder", core.Point{X: 6, Y: 15}, []int{30, 10, 20}},
		{"walk_jump_ladder", core.Point{X: 15, Y: 15}, []int{10, 20}},
	} {
		navRes := nav.FindPathWithOptions(tc.start, goal, pathfind.PathOptions{})
		tiledRes := tm.FindPathWithOptions(tc.start, goal, pathfind.PathOptions{})
		if !navRes.Found || !tiledRes.Found {
			t.Fatalf("%s: expected multi-island path: nav=%v tiled=%v", tc.name, navRes.Found, tiledRes.Found)
		}
		if len(navRes.Links) != len(tc.wantLinks) || len(tiledRes.Links) != len(tc.wantLinks) {
			t.Fatalf("%s: expected %d links: nav=%+v tiled=%+v", tc.name, len(tc.wantLinks), navRes.Links, tiledRes.Links)
		}
		if len(navRes.Path) != len(tiledRes.Path) {
			t.Fatalf("%s: path mismatch: nav=%v tiled=%v", tc.name, navRes.Path, tiledRes.Path)
		}
		for i := range navRes.Path {
			dx := float64(navRes.Path[i].X - tiledRes.Path[i].X)
			dy := float64(navRes.Path[i].Y - tiledRes.Path[i].Y)
			if math.Hypot(dx, dy) > 1e-2 {
				t.Fatalf("%s: waypoint[%d] mismatch: nav=%v tiled=%v", tc.name, i, navRes.Path[i], tiledRes.Path[i])
			}
		}
		for i, wantID := range tc.wantLinks {
			if navRes.Links[i].LinkID != wantID || tiledRes.Links[i].LinkID != wantID ||
				navRes.Links[i].UserData != tiledRes.Links[i].UserData ||
				navRes.Links[i].Area != tiledRes.Links[i].Area ||
				navRes.Links[i].Flags != tiledRes.Links[i].Flags ||
				navRes.Links[i].StartIndex != tiledRes.Links[i].StartIndex ||
				navRes.Links[i].EndIndex != tiledRes.Links[i].EndIndex {
				t.Fatalf("%s: link[%d] mismatch: nav=%+v tiled=%+v", tc.name, i, navRes.Links[i], tiledRes.Links[i])
			}
		}
	}
	start := core.Point{X: 15, Y: 15}

	// 2. Bidirectional ladder allows East -> Mid, while one-way jump blocks East -> West.
	revLadder := tm.FindPathWithOptions(goal, core.Point{X: 52, Y: 15}, pathfind.PathOptions{})
	if !revLadder.Found || len(revLadder.Links) != 1 || revLadder.Links[0].LinkID != 20 {
		t.Fatalf("expected reverse bidirectional ladder East->Mid, got %+v", revLadder)
	}
	if revJump := tm.FindPathWithOptions(goal, start, pathfind.PathOptions{}); revJump.Found {
		t.Fatalf("one-way jump unexpectedly allowed reverse traversal East->West: %+v", revJump)
	}

	// 3. Intra-island cheap teleport on West island is chosen over walking.
	warpRes := tm.FindPathWithOptions(core.Point{X: 3, Y: 5}, core.Point{X: 27, Y: 25}, pathfind.PathOptions{})
	if !warpRes.Found || len(warpRes.Links) != 1 || warpRes.Links[0].LinkID != 30 || warpRes.Links[0].UserData != "warp" {
		t.Fatalf("expected intra-island cheap teleport, got %+v", warpRes)
	}

	// 4. NavFilter respects baked link areas and flags.
	noJump := pathfind.DefaultNavFilter()
	noJump.ExcludeArea(pathfind.NavAreaJump)
	if r := tm.FindPathWithFilterAndOptions(start, goal, noJump, pathfind.PathOptions{}); r.Found {
		t.Fatalf("ExcludeArea(NavAreaJump) should block jump link, got %+v", r)
	}
	noClimb := pathfind.DefaultNavFilter()
	noClimb.ExcludeFlags |= pathfind.NavFlagClimb
	if r := tm.FindPathWithFilterAndOptions(start, goal, noClimb, pathfind.PathOptions{}); r.Found {
		t.Fatalf("ExcludeFlags(NavFlagClimb) should block ladder link, got %+v", r)
	}
	walkOnly := pathfind.NavFilter{IncludeFlags: pathfind.NavFlagWalk, ExcludeFlags: pathfind.NavFlagDisabled}
	if r := tm.FindPathWithFilterAndOptions(start, goal, walkOnly, pathfind.PathOptions{}); r.Found {
		t.Fatalf("walk-only filter should block jump/ladder links, got %+v", r)
	}

	// 5. Dynamic obstacle carving on West island keeps the jump link attached and detours cleanly.
	obsID, ok := tm.AddObstacle(pathfind.NavBoxObstacle(core.Point{X: 18, Y: 10}, core.Point{X: 23, Y: 20}))
	if !ok {
		t.Fatal("AddObstacle failed")
	}
	detourRes := tm.FindPathWithOptions(start, goal, pathfind.PathOptions{})
	if !detourRes.Found || len(detourRes.Links) != 2 || detourRes.Links[0].LinkID != 10 {
		t.Fatalf("expected detour path still crossing baked jump and ladder links, got %+v", detourRes)
	}
	tm.RemoveObstacle(obsID)

	// 6. Nil mesh safety.
	nilTM := (*navbake.Mesh)(nil).TiledNavMesh(16)
	if nilTM.TileCount() != 0 || len(nilTM.OffMeshLinks()) != 0 {
		t.Fatalf("expected empty TiledNavMesh from nil Mesh, got tiles=%d links=%d", nilTM.TileCount(), len(nilTM.OffMeshLinks()))
	}
}
