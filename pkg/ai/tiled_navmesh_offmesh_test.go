package ai_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func TestTiledNavMeshOffMeshOneWayDropCrossTileNeverReversed(t *testing.T) {
	// Ledge [0,10]x[0,10] in tile (0,0) and floor [20,30]x[0,10] in tile (1,0)
	// with tileSize=16. Collinear path along y=5 verifies link endpoints are preserved.
	polys := [][]core.Point{
		rect2D(0, 0, 10, 10),
		rect2D(20, 0, 30, 10),
	}
	tm := ai.BuildTiledNavMesh(polys, 16)
	untiled := ai.BuildNavMesh(polys)

	linkCfg := ai.NavOffMeshLink{
		Start:         pt2(9, 5),
		End:           pt2(21, 5),
		Radius:        0.5,
		Bidirectional: false,
		Flags:         uint32(ai.NavFlagJump),
		ID:            101,
		Area:          ai.NavAreaJump,
		UserData:      "drop",
	}
	if !tm.AddOffMeshLink(linkCfg) {
		t.Fatal("TiledNavMesh.AddOffMeshLink returned false")
	}
	if !untiled.AddOffMeshLink(linkCfg) {
		t.Fatal("NavMesh.AddOffMeshLink returned false")
	}

	start := pt2(2, 5)
	goal := pt2(28, 5)

	res := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	uRes := untiled.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !res.Found || res.Partial {
		t.Fatalf("expected complete path down ledge, got %+v", res)
	}
	if !res.HasLinks() || len(res.Links) != 1 {
		t.Fatalf("expected 1 off-mesh link in result, got %+v", res.Links)
	}
	link := res.Links[0]
	if link.StartIndex != 1 || link.EndIndex != 2 {
		t.Fatalf("link indices = [%d, %d], want [1, 2] (path=%v)", link.StartIndex, link.EndIndex, res.Path)
	}
	if link.FromPoly.Tile != (ai.NavTileCoord{X: 0, Y: 0}) || link.ToPoly.Tile != (ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatalf("link tiles = %v->%v, want (0,0)->(1,0)", link.FromPoly.Tile, link.ToPoly.Tile)
	}
	if !link.FromPoly.Valid() || !link.ToPoly.Valid() {
		t.Fatalf("link poly refs must be valid: %+v", link)
	}
	if link.Start != pt2(9, 5) || link.End != pt2(21, 5) {
		t.Fatalf("link endpoints = %v->%v, want (9,5)->(21,5)", link.Start, link.End)
	}
	if res.Path[link.StartIndex] != link.Start || res.Path[link.EndIndex] != link.End {
		t.Fatalf("path[%d]=%v path[%d]=%v do not match link %v->%v",
			link.StartIndex, res.Path[link.StartIndex], link.EndIndex, res.Path[link.EndIndex], link.Start, link.End)
	}
	if link.Radius != 0.5 || link.Area != ai.NavAreaJump || link.LinkID != 101 || link.UserData != "drop" {
		t.Fatalf("link metadata mismatch: %+v", link)
	}
	if link.Flags != (uint32(ai.NavFlagJump) | ai.NavLinkOffMesh) {
		t.Fatalf("link flags = %#x, want %#x", link.Flags, uint32(ai.NavFlagJump)|ai.NavLinkOffMesh)
	}
	if _, isLink := res.LinkAt(0); isLink {
		t.Fatal("segment 0 (walking approach) should not be marked as a link")
	}
	if got, isLink := res.LinkAt(1); !isLink || got.LinkID != 101 {
		t.Fatalf("LinkAt(1) = (%+v, %v), want link 101", got, isLink)
	}
	if _, isLink := res.LinkAt(2); isLink {
		t.Fatal("segment 2 (walking exit) should not be marked as a link")
	}

	// Waypoints must match untiled NavMesh.
	if len(res.Path) != len(uRes.Path) {
		t.Fatalf("path mismatch: tiled=%v untiled=%v", res.Path, uRes.Path)
	}
	for i := range uRes.Path {
		if !approxPt2D(res.Path[i], uRes.Path[i], 1e-3) {
			t.Fatalf("waypoint[%d] mismatch: tiled=%v untiled=%v", i, res.Path[i], uRes.Path[i])
		}
	}

	// Both endpoint tiles must be stamped on res.Tiles.
	if !res.CrossesTile(ai.NavTileCoord{X: 0, Y: 0}) || !res.CrossesTile(ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatalf("expected both endpoint tiles in res.Tiles, got %+v", res.Tiles)
	}

	// One-way drop link must NEVER be used in reverse (floor -> ledge).
	if rev := tm.FindPathWithOptions(goal, start, ai.PathOptions{}); rev.Found {
		t.Fatalf("reverse path unexpectedly succeeded across one-way drop link: %+v", rev)
	}
	if _, ok := tm.FindPath(goal, start); ok {
		t.Fatal("FindPath reverse unexpectedly succeeded")
	}

	// Surface Raycast must not traverse off-mesh links across the gap.
	hit, obstructed := tm.Raycast(pt2(5, 5), pt2(25, 5))
	if !obstructed || !hit.Hit || math.Abs(float64(hit.Point.X-10)) > 0.1 {
		t.Fatalf("expected raycast to stop at ledge edge x=10, got %+v obstructed=%v", hit, obstructed)
	}
}

func TestTiledNavMeshOffMeshTeleportCostVsWalking(t *testing.T) {
	// 1. Disconnected islands across several tiles.
	disconnected := ai.BuildTiledNavMesh([][]core.Point{
		rect2D(0, 0, 10, 10),
		rect2D(100, 0, 110, 10),
	}, 20)
	if !disconnected.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt2(8, 5),
		End:           pt2(102, 5),
		Bidirectional: true,
		Cost:          5,
		Area:          ai.NavAreaTeleport,
		Flags:         uint32(ai.NavFlagTeleport),
		ID:            1,
		UserData:      "teleport",
	}) {
		t.Fatal("AddOffMeshLink failed on disconnected islands")
	}
	res := disconnected.FindPathWithOptions(pt2(2, 5), pt2(108, 5), ai.PathOptions{})
	if !res.Found || len(res.Links) != 1 || res.Links[0].UserData != "teleport" {
		t.Fatalf("expected teleport across disconnected islands, got %+v", res)
	}
	rev := disconnected.FindPathWithOptions(pt2(108, 5), pt2(2, 5), ai.PathOptions{})
	if !rev.Found || len(rev.Links) != 1 || rev.Links[0].Start != pt2(102, 5) || rev.Links[0].End != pt2(8, 5) {
		t.Fatalf("expected bidirectional reverse teleport, got %+v", rev)
	}

	// 2. Two islands connected by a walking bridge AND a teleporter behind start.
	makeConnected := func(teleportCost float32) *ai.TiledNavMesh {
		tm := ai.BuildTiledNavMesh([][]core.Point{
			rect2D(-10, 0, 0, 10),
			rect2D(0, 0, 10, 10),
			rect2D(10, 0, 100, 10),
			rect2D(100, 0, 110, 10),
		}, 20)
		if !tm.AddOffMeshLink(ai.NavOffMeshLink{
			Start:    pt2(-5, 5),
			End:      pt2(105, 5),
			Cost:     teleportCost,
			Area:     ai.NavAreaTeleport,
			Flags:    uint32(ai.NavFlagTeleport),
			ID:       77,
			UserData: "teleport",
		}) {
			t.Fatal("AddOffMeshLink failed")
		}
		return tm
	}

	// Cheap teleport behind start beats walking bridge.
	cheap := makeConnected(5)
	cheapRes := cheap.FindPathWithOptions(pt2(1, 5), pt2(109, 5), ai.PathOptions{})
	if !cheapRes.Found || len(cheapRes.Links) != 1 || cheapRes.Links[0].LinkID != 77 {
		t.Fatalf("expected cheap teleport to beat walking route, got %+v", cheapRes)
	}

	// Expensive teleport loses to walking bridge.
	expensive := makeConnected(150)
	expRes := expensive.FindPathWithOptions(pt2(1, 5), pt2(109, 5), ai.PathOptions{})
	if !expRes.Found || expRes.HasLinks() {
		t.Fatalf("expected walking route when teleport is more expensive, got %+v", expRes)
	}

	// 3. Same-tile, same-polygon cheap teleporter must not be skipped by direct shortcut.
	singleTile := ai.BuildTiledNavMesh([][]core.Point{rect2D(0, 0, 100, 20)}, 128)
	if !singleTile.AddOffMeshLink(ai.NavOffMeshLink{
		Start:    pt2(5, 10),
		End:      pt2(95, 10),
		Cost:     2,
		ID:       88,
		UserData: "warp",
	}) {
		t.Fatal("AddOffMeshLink on same polygon with distinct endpoints failed")
	}
	spRes := singleTile.FindPathWithOptions(pt2(2, 10), pt2(98, 10), ai.PathOptions{})
	if !spRes.Found || len(spRes.Links) != 1 || spRes.Links[0].LinkID != 88 {
		t.Fatalf("expected same-polygon cheap teleporter to be used, got %+v", spRes)
	}
}

func TestTiledNavMeshOffMeshRadiusSnapAndFilterNoWalkCollision(t *testing.T) {
	tm := ai.BuildTiledNavMesh([][]core.Point{
		rect2D(0, 0, 10, 10),
		rect2D(20, 0, 30, 10),
	}, 16)

	if tm.AddOffMeshLink(ai.NavOffMeshLink{Start: pt2(10.4, 5), End: pt2(19.6, 5)}) {
		t.Fatal("expected out-of-mesh endpoints with Radius=0 to fail")
	}
	if tm.AddOffMeshLink(ai.NavOffMeshLink{Start: pt2(10.4, 5), End: pt2(19.6, 5), Radius: -1}) {
		t.Fatal("expected negative Radius to fail")
	}
	if tm.AddOffMeshLink(ai.NavOffMeshLink{Start: pt2(10.4, 5), End: pt2(19.6, 5), Radius: float32(math.NaN())}) {
		t.Fatal("expected NaN Radius to fail")
	}
	if tm.AddOffMeshLink(ai.NavOffMeshLink{Start: pt2(10.4, 5), End: pt2(19.6, 5), Radius: 0.2}) {
		t.Fatal("expected Radius smaller than distance to edge to fail")
	}
	if !tm.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt2(10.4, 5),
		End:           pt2(19.6, 5),
		Radius:        0.75,
		Bidirectional: true,
		Area:          ai.NavAreaClimb,
		Flags:         uint32(ai.NavFlagClimb),
		ID:            42,
		UserData:      "ladder",
	}) {
		t.Fatal("expected endpoints within Radius=0.75 to snap and attach")
	}

	links := tm.OffMeshLinks()
	if len(links) != 1 || links[0].Start != pt2(10, 5) || links[0].End != pt2(20, 5) {
		t.Fatalf("OffMeshLinks = %+v, want snapped (10,5)->(20,5)", links)
	}

	// Exclude by Area or Flags.
	noClimbArea := ai.DefaultNavFilter()
	noClimbArea.ExcludeArea(ai.NavAreaClimb)
	if r := tm.FindPathWithFilterAndOptions(pt2(2, 5), pt2(28, 5), noClimbArea, ai.PathOptions{}); r.Found {
		t.Fatalf("expected ExcludeArea(NavAreaClimb) to block ladder, got %+v", r)
	}
	noClimbFlag := ai.DefaultNavFilter()
	noClimbFlag.ExcludeFlags |= ai.NavFlagClimb
	if r := tm.FindPathWithFilterAndOptions(pt2(2, 5), pt2(28, 5), noClimbFlag, ai.PathOptions{}); r.Found {
		t.Fatalf("expected ExcludeFlags(NavFlagClimb) to block ladder, got %+v", r)
	}

	// Walk-only filter must NOT traverse NavFlagClimb link (no bit-0 NavLinkOffMesh collision).
	walkOnly := ai.NavFilter{
		IncludeFlags: ai.NavFlagWalk,
		ExcludeFlags: ai.NavFlagDisabled,
	}
	if r := tm.FindPathWithFilterAndOptions(pt2(2, 5), pt2(28, 5), walkOnly, ai.PathOptions{}); r.Found {
		t.Fatalf("walk-only filter unexpectedly traversed climb link: %+v", r)
	}

	// Non-walking agent (IncludeFlags: NavFlagTeleport, ExcludeFlags: NavFlagWalk) must traverse
	// a NavFlagTeleport link even though NavLinkOffMesh shares bit 0 with NavFlagWalk.
	tpMesh := ai.BuildTiledNavMeshWithAreas([]ai.NavPolygonInput{
		{Vertices: rect2D(0, 0, 10, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagTeleport},
		{Vertices: rect2D(20, 0, 30, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagTeleport},
	}, 16)
	if !tpMesh.AddOffMeshLink(ai.NavOffMeshLink{
		Start: pt2(9, 5),
		End:   pt2(21, 5),
		Area:  ai.NavAreaTeleport,
		Flags: uint32(ai.NavFlagTeleport),
		ID:    200,
	}) {
		t.Fatal("AddOffMeshLink(teleport) failed")
	}
	tpOnly := ai.NavFilter{
		IncludeFlags: ai.NavFlagTeleport,
		ExcludeFlags: ai.NavFlagWalk,
	}
	tpRes := tpMesh.FindPathWithFilterAndOptions(pt2(2, 5), pt2(28, 5), tpOnly, ai.PathOptions{})
	if !tpRes.Found || len(tpRes.Links) != 1 || tpRes.Links[0].LinkID != 200 || tpRes.Links[0].Flags&ai.NavLinkOffMesh == 0 {
		t.Fatalf("teleport-only filter blocked by NavLinkOffMesh bit collision: %+v", tpRes)
	}

	// Untagged link (Flags == 0) defaults to NavFlagWalk.
	untagged := ai.BuildTiledNavMesh([][]core.Point{
		rect2D(0, 0, 10, 10),
		rect2D(20, 0, 30, 10),
	}, 16)
	if !untagged.AddOffMeshLink(ai.NavOffMeshLink{Start: pt2(9, 5), End: pt2(21, 5)}) {
		t.Fatal("AddOffMeshLink(untagged) failed")
	}
	if r := untagged.FindPathWithFilterAndOptions(pt2(2, 5), pt2(28, 5), walkOnly, ai.PathOptions{}); !r.Found || len(r.Links) != 1 {
		t.Fatalf("walk-only filter failed on untagged link: %+v", r)
	}

	// AreaCost scaling on link area chooses walking route when link area cost is high.
	choice := ai.BuildTiledNavMesh([][]core.Point{
		rect2D(0, 0, 40, 20),
	}, 20)
	if !choice.AddOffMeshLink(ai.NavOffMeshLink{
		Start: pt2(5, 10),
		End:   pt2(35, 10),
		Cost:  5,
		Area:  ai.NavAreaJump,
		Flags: uint32(ai.NavFlagJump),
		ID:    99,
	}) {
		t.Fatal("AddOffMeshLink failed")
	}
	cheapJump := ai.DefaultNavFilter()
	if r := choice.FindPathWithFilterAndOptions(pt2(2, 10), pt2(38, 10), cheapJump, ai.PathOptions{}); !r.Found || len(r.Links) != 1 {
		t.Fatalf("expected cheap jump link, got %+v", r)
	}
	expensiveJump := ai.DefaultNavFilter()
	expensiveJump.SetAreaCost(ai.NavAreaJump, 20.0) // 5 * 20 = 100 > walk 36
	if r := choice.FindPathWithFilterAndOptions(pt2(2, 10), pt2(38, 10), expensiveJump, ai.PathOptions{}); !r.Found || r.HasLinks() {
		t.Fatalf("expected walking route when NavAreaJump cost is scaled high, got %+v", r)
	}
}

func TestTiledNavMeshOffMeshSurvivesTileReplacementCarvingAndRemoval(t *testing.T) {
	// West island [0,30]x[0,30] (tile 0,0) and east island [40,70]x[0,30] (tile 1,0 and 2,0)
	// with tileSize=32.
	tm := ai.BuildTiledNavMesh([][]core.Point{
		rect2D(0, 0, 30, 30),
		rect2D(40, 0, 70, 30),
	}, 32)

	if !tm.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt2(28, 15),
		End:           pt2(42, 15),
		Radius:        0.5,
		Bidirectional: true,
		Area:          ai.NavAreaJump,
		Flags:         uint32(ai.NavFlagJump),
		ID:            55,
		UserData:      "bridge_jump",
	}) {
		t.Fatal("AddOffMeshLink failed")
	}

	start := pt2(4, 15)
	goal := pt2(66, 15)

	initial := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !initial.Found || len(initial.Links) != 1 || initial.Links[0].LinkID != 55 {
		t.Fatalf("initial path failed: %+v", initial)
	}
	if !tm.IsPathValid(initial) {
		t.Fatal("initial path should be valid")
	}

	// 1. Carve an obstacle on tile (0,0) away from the link takeoff (28,15):
	//    tile (0,0) rebuilds, path detours around the obstacle, and the link survives.
	detourObs, ok := tm.AddObstacle(ai.NavBoxObstacle(pt2(12, 8), pt2(20, 22)))
	if !ok {
		t.Fatal("AddObstacle(detour) failed")
	}
	if tm.IsPathValid(initial) {
		t.Fatal("initial path should be invalidated after obstacle on tile (0,0)")
	}
	aroundRes := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !aroundRes.Found || len(aroundRes.Links) != 1 || aroundRes.Links[0].LinkID != 55 {
		t.Fatalf("link did not survive obstacle carving elsewhere on tile: %+v", aroundRes)
	}
	if len(aroundRes.Path) <= 4 {
		t.Fatalf("expected detour around obstacle before jump link, got %v", aroundRes.Path)
	}
	tm.RemoveObstacle(detourObs)

	// 2. Carve an obstacle directly over the jump takeoff (28,15):
	//    link must cleanly detach while covered and re-attach when the obstacle is removed.
	coverObs, ok := tm.AddObstacle(ai.NavBoxObstacle(pt2(25, 12), pt2(31, 18)))
	if !ok {
		t.Fatal("AddObstacle(cover) failed")
	}
	if blocked := tm.FindPathWithOptions(start, goal, ai.PathOptions{}); blocked.Found {
		t.Fatalf("expected path blocked while takeoff is carved out, got %+v", blocked)
	}
	if blockedRev := tm.FindPathWithOptions(goal, start, ai.PathOptions{}); blockedRev.Found {
		t.Fatalf("expected reverse path blocked while takeoff is carved out, got %+v", blockedRev)
	}
	tm.RemoveObstacle(coverObs)
	afterRemove := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !afterRemove.Found || len(afterRemove.Links) != 1 || afterRemove.Links[0].LinkID != 55 {
		t.Fatalf("link failed to re-attach after removing covering obstacle: %+v", afterRemove)
	}

	// 3. ReplaceTile on tile (0,0) and RebuildTile on tile (1,0): link must re-attach with fresh salts.
	oldFromSalt := afterRemove.Links[0].FromPoly.Salt
	oldToSalt := afterRemove.Links[0].ToPoly.Salt
	newTile0 := ai.BakeNavTile(ai.NavTileCoord{X: 0, Y: 0}, 32, core.Point{}, []ai.NavPolygonInput{
		{Vertices: rect2D(0, 0, 30, 30)},
	})
	if !tm.ReplaceTile(ai.NavTileCoord{X: 0, Y: 0}, newTile0) {
		t.Fatal("ReplaceTile (0,0) failed")
	}
	if !tm.RebuildTile(ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatal("RebuildTile (1,0) failed")
	}
	afterReplace := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !afterReplace.Found || len(afterReplace.Links) != 1 || afterReplace.Links[0].LinkID != 55 {
		t.Fatalf("link did not survive ReplaceTile/RebuildTile: %+v", afterReplace)
	}
	if afterReplace.Links[0].FromPoly.Salt == oldFromSalt || afterReplace.Links[0].ToPoly.Salt == oldToSalt {
		t.Fatalf("expected refreshed poly salts after ReplaceTile/RebuildTile: old=(%d,%d) new=(%d,%d)",
			oldFromSalt, oldToSalt, afterReplace.Links[0].FromPoly.Salt, afterReplace.Links[0].ToPoly.Salt)
	}

	// 4. RemoveTile on (1,0) detaches link cleanly; re-adding tile (1,0) re-attaches it.
	tile1 := ai.BakeNavTile(ai.NavTileCoord{X: 1, Y: 0}, 32, core.Point{}, []ai.NavPolygonInput{
		{Vertices: rect2D(40, 0, 70, 30)},
	})
	if !tm.RemoveTile(ai.NavTileCoord{X: 1, Y: 0}) {
		t.Fatal("RemoveTile (1,0) failed")
	}
	if r := tm.FindPathWithOptions(start, goal, ai.PathOptions{}); r.Found {
		t.Fatalf("expected no path while tile (1,0) is removed, got %+v", r)
	}
	if !tm.AddTile(ai.NavTileCoord{X: 1, Y: 0}, tile1) {
		t.Fatal("re-AddTile (1,0) failed")
	}
	afterReadd := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !afterReadd.Found || len(afterReadd.Links) != 1 {
		t.Fatalf("link did not re-attach after re-AddTile (1,0): %+v", afterReadd)
	}

	// 5. Batch replacing both endpoint tiles simultaneously re-attaches cross-tile links cleanly.
	tm.Batch(func() {
		tm.ReplaceTile(ai.NavTileCoord{X: 0, Y: 0}, newTile0)
		tm.ReplaceTile(ai.NavTileCoord{X: 1, Y: 0}, tile1)
	})
	afterBatch := tm.FindPathWithOptions(start, goal, ai.PathOptions{})
	if !afterBatch.Found || len(afterBatch.Links) != 1 || afterBatch.Links[0].LinkID != 55 {
		t.Fatalf("link did not survive Batch tile replacement: %+v", afterBatch)
	}

	// 6. WatchPath and ClearOffMeshLinks invalidate active paths and bump revision.
	var invalidated []ai.NavTileChange
	tm.WatchPath(afterBatch.TileCoords(), func(ch ai.NavTileChange) {
		invalidated = append(invalidated, ch)
	})
	revBefore := tm.Revision()
	tm.ClearOffMeshLinks()
	if tm.Revision() <= revBefore || len(tm.OffMeshLinks()) != 0 {
		t.Fatalf("ClearOffMeshLinks failed: rev %d->%d links=%d", revBefore, tm.Revision(), len(tm.OffMeshLinks()))
	}
	if tm.IsPathValid(afterBatch) {
		t.Fatal("path should be invalid after ClearOffMeshLinks")
	}
	if len(invalidated) != 1 {
		t.Fatalf("expected 1 path invalidation callback on ClearOffMeshLinks, got %v", invalidated)
	}
	if r := tm.FindPathWithOptions(start, goal, ai.PathOptions{}); r.Found {
		t.Fatalf("expected disconnected islands after ClearOffMeshLinks, got %+v", r)
	}
}

func TestTiledNavMeshOffMeshMatchesUntiledNavMeshAndPartial(t *testing.T) {
	// Three platforms:
	//   A: [0, 50] x [0, 40] with pillar hole [18, 28] x [12, 28]
	//   B: [65, 115] x [0, 40]
	//   C: [140, 170] x [0, 40] (disconnected from A and B)
	polys := [][]core.Point{
		// Platform A around pillar [18,28]x[12,28]
		rect2D(0, 0, 50, 12),
		rect2D(0, 28, 50, 40),
		rect2D(0, 12, 18, 28),
		rect2D(28, 12, 50, 28),
		// Platform B
		rect2D(65, 0, 115, 40),
		// Platform C (unreachable)
		rect2D(140, 0, 170, 40),
	}

	untiled := ai.BuildNavMesh(polys)
	tiled := ai.BuildTiledNavMesh(polys, 20)

	links := []ai.NavOffMeshLink{
		{
			Start:         pt2(48, 20),
			End:           pt2(67, 20),
			Bidirectional: true,
			Area:          ai.NavAreaJump,
			Flags:         uint32(ai.NavFlagJump),
			ID:            1,
			UserData:      "jump",
		},
		{
			Start:         pt2(48, 36),
			End:           pt2(67, 36),
			Bidirectional: false,
			Cost:          3,
			Area:          ai.NavAreaTeleport,
			Flags:         uint32(ai.NavFlagTeleport),
			ID:            2,
			UserData:      "warp",
		},
	}
	for _, l := range links {
		if !untiled.AddOffMeshLink(l) || !tiled.AddOffMeshLink(l) {
			t.Fatalf("AddOffMeshLink(%d) failed", l.ID)
		}
	}

	cases := []struct {
		name  string
		start core.Point
		goal  core.Point
	}{
		{"around_pillar_to_jump", pt2(5, 20), pt2(110, 5)},
		{"prefer_cheap_warp", pt2(5, 35), pt2(110, 35)},
		{"reverse_uses_bidirectional_jump", pt2(110, 35), pt2(5, 20)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ur := untiled.FindPathWithOptions(tc.start, tc.goal, ai.PathOptions{})
			tr := tiled.FindPathWithOptions(tc.start, tc.goal, ai.PathOptions{})
			if ur.Found != tr.Found || len(ur.Links) != len(tr.Links) {
				t.Fatalf("mismatch: untiled=%+v tiled=%+v", ur, tr)
			}
			if len(ur.Path) != len(tr.Path) {
				t.Fatalf("path len mismatch: untiled=%v tiled=%v", ur.Path, tr.Path)
			}
			for i := range ur.Path {
				if !approxPt2D(ur.Path[i], tr.Path[i], 1e-2) {
					t.Fatalf("waypoint[%d] mismatch: untiled=%v tiled=%v", i, ur.Path[i], tr.Path[i])
				}
			}
			for i := range ur.Links {
				if ur.Links[i].LinkID != tr.Links[i].LinkID ||
					ur.Links[i].StartIndex != tr.Links[i].StartIndex ||
					ur.Links[i].EndIndex != tr.Links[i].EndIndex ||
					ur.Links[i].Start != tr.Links[i].Start ||
					ur.Links[i].End != tr.Links[i].End {
					t.Fatalf("link[%d] mismatch: untiled=%+v tiled=%+v", i, ur.Links[i], tr.Links[i])
				}
			}
		})
	}

	// Partial path toward unreachable platform C should still cross the jump from A to B
	// and end at B's eastern edge (x=115) with the traversed link reported.
	partial := tiled.FindPathWithOptions(pt2(5, 20), pt2(155, 20), ai.PathOptions{AllowPartial: true})
	if !partial.Found || !partial.Partial || !partial.HasLinks() {
		t.Fatalf("expected partial path crossing link onto platform B, got %+v", partial)
	}
	endPt := partial.Path[len(partial.Path)-1]
	if math.Abs(float64(endPt.X-115)) > 0.2 {
		t.Fatalf("expected partial path to reach east edge of platform B (x=115), got %v", endPt)
	}
}
