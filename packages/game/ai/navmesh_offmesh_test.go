package ai_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/nsgo/core"
)

func TestNavMesh2DOffMeshOneWayDropLinkNeverReversed(t *testing.T) {
	// Ledge [0,10]x[0,10] and floor [20,30]x[0,10] separated by a gap [10,20].
	// All path waypoints along y=5 are strictly collinear, verifying that link endpoints
	// are preserved and never collapsed by collinear simplification.
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0: ledge
		rectPoly(20, 0, 30, 10), // 1: floor
	})

	ok := mesh.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt(9, 5),
		End:           pt(21, 5),
		Radius:        0.5,
		Bidirectional: false,
		Flags:         uint32(ai.NavFlagJump),
		ID:            101,
		Area:          ai.NavAreaJump,
		UserData:      "drop",
	})
	if !ok {
		t.Fatal("AddOffMeshLink returned false")
	}

	start := pt(2, 5)
	goal := pt(28, 5)

	res := mesh.FindPathWithOptions(start, goal, ai.PathOptions{})
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
	if link.FromPoly != 0 || link.ToPoly != 1 {
		t.Fatalf("link polys = %d->%d, want 0->1", link.FromPoly, link.ToPoly)
	}
	if link.Start != pt(9, 5) || link.End != pt(21, 5) {
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

	// One-way drop link must NEVER be used in reverse (floor -> ledge).
	rev := mesh.FindPathWithOptions(goal, start, ai.PathOptions{})
	if rev.Found {
		t.Fatalf("reverse path unexpectedly succeeded across one-way drop link: %+v", rev)
	}
	if _, ok := mesh.FindPath(goal, start); ok {
		t.Fatal("FindPath reverse unexpectedly succeeded")
	}

	// Incremental NavMeshSearch (1 expansion per step) and NavMeshQuery must match identically.
	search := mesh.StartSearch(start, goal, ai.PathOptions{})
	for !search.Status().Done() {
		search.Step(1)
	}
	incRes := search.Result()
	if !incRes.Found || len(incRes.Path) != len(res.Path) || len(incRes.Links) != 1 || incRes.Links[0] != link {
		t.Fatalf("incremental search mismatch: got %+v, want %+v", incRes, res)
	}

	q := ai.NewNavMeshQuery(mesh)
	var buf []core.Point
	qRes := q.FindPathWithFilterAndOptionsInto(buf, start, goal, ai.DefaultNavFilter(), ai.PathOptions{})
	if !qRes.Found || len(qRes.Path) != len(res.Path) || len(qRes.Links) != 1 || qRes.Links[0] != link {
		t.Fatalf("query result mismatch: got %+v, want %+v", qRes, res)
	}
	qRev := q.FindPathWithFilterAndOptionsInto(buf, goal, start, ai.DefaultNavFilter(), ai.PathOptions{})
	if qRev.Found {
		t.Fatalf("query reverse unexpectedly succeeded: %+v", qRev)
	}
}

func TestNavMesh2DOffMeshTeleportCostVsWalking(t *testing.T) {
	// 1. Disconnected islands: teleport connects them.
	disconnected := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),
		rectPoly(100, 0, 110, 10),
	})
	if !disconnected.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt(8, 5),
		End:           pt(102, 5),
		Bidirectional: true,
		Cost:          5,
		Area:          ai.NavAreaTeleport,
		Flags:         uint32(ai.NavFlagTeleport),
		ID:            1,
		UserData:      "teleport",
	}) {
		t.Fatal("AddOffMeshLink failed on disconnected islands")
	}
	res := disconnected.FindPathWithOptions(pt(2, 5), pt(108, 5), ai.PathOptions{})
	if !res.Found || len(res.Links) != 1 || res.Links[0].UserData != "teleport" {
		t.Fatalf("expected teleport across disconnected islands, got %+v", res)
	}

	// 2. Two islands connected by a walking route (total walk ~108) AND a teleporter placed
	// *behind* start in poly 0 (at x=-5, while start is at x=1 and goal is at x=109).
	// A naive Euclidean A* heuristic would miss this teleporter; our shortcut-aware admissible
	// heuristic must find it when cheap and reject it when expensive.
	makeConnected := func(teleportCost float32) *ai.NavMesh {
		m := ai.BuildNavMesh([][]core.Point{
			rectPoly(-10, 0, 0, 10),   // 0: alcove behind start with teleporter pad
			rectPoly(0, 0, 10, 10),    // 1: start island
			rectPoly(10, 0, 100, 10),  // 2: long walking bridge
			rectPoly(100, 0, 110, 10), // 3: goal island
		})
		if !m.AddOffMeshLink(ai.NavOffMeshLink{
			Start:    pt(-5, 5),
			End:      pt(105, 5),
			Cost:     teleportCost,
			Area:     ai.NavAreaTeleport,
			Flags:    uint32(ai.NavFlagTeleport),
			ID:       77,
			UserData: "teleport",
		}) {
			t.Fatal("AddOffMeshLink failed")
		}
		return m
	}

	// Cheap teleport: walk 6 to (-5,5) + teleport 5 + walk 4 to (109,5) = 15 < walk 108.
	cheap := makeConnected(5)
	cheapRes := cheap.FindPathWithOptions(pt(1, 5), pt(109, 5), ai.PathOptions{})
	if !cheapRes.Found || len(cheapRes.Links) != 1 || cheapRes.Links[0].LinkID != 77 {
		t.Fatalf("expected cheap teleport to beat walking route, got %+v", cheapRes)
	}
	qCheap := ai.NewNavMeshQuery(cheap).FindPathWithFilterAndOptionsInto(nil, pt(1, 5), pt(109, 5), ai.DefaultNavFilter(), ai.PathOptions{})
	if !qCheap.Found || len(qCheap.Links) != 1 || qCheap.Links[0].LinkID != 77 {
		t.Fatalf("query expected cheap teleport, got %+v", qCheap)
	}

	// Expensive teleport: walk 6 + teleport 150 + walk 4 = 160 > walk 108 -> must walk instead!
	expensive := makeConnected(150)
	expRes := expensive.FindPathWithOptions(pt(1, 5), pt(109, 5), ai.PathOptions{})
	if !expRes.Found || expRes.HasLinks() {
		t.Fatalf("expected walking route when teleport is more expensive, got %+v", expRes)
	}
	qExp := ai.NewNavMeshQuery(expensive).FindPathWithFilterAndOptionsInto(nil, pt(1, 5), pt(109, 5), ai.DefaultNavFilter(), ai.PathOptions{})
	if !qExp.Found || qExp.HasLinks() {
		t.Fatalf("query expected walking route when teleport is more expensive, got %+v", qExp)
	}

	// 3. Same-polygon cheap teleporter must not be skipped by same-poly direct shortcut!
	singlePoly := ai.BuildNavMesh([][]core.Point{rectPoly(0, 0, 100, 20)})
	if !singlePoly.AddOffMeshLink(ai.NavOffMeshLink{
		Start:    pt(5, 10),
		End:      pt(95, 10),
		Cost:     2,
		ID:       88,
		UserData: "warp",
	}) {
		t.Fatal("AddOffMeshLink on same polygon with distinct endpoints failed")
	}
	spRes := singlePoly.FindPathWithOptions(pt(2, 10), pt(98, 10), ai.PathOptions{})
	if !spRes.Found || len(spRes.Links) != 1 || spRes.Links[0].LinkID != 88 {
		t.Fatalf("expected same-polygon cheap teleporter to be used, got %+v", spRes)
	}
}

func TestNavMesh2DOffMeshRadiusSnapBidirectionalFilterAndAgent(t *testing.T) {
	mesh := ai.BuildNavMesh([][]core.Point{
		rectPoly(0, 0, 10, 10),  // 0
		rectPoly(20, 0, 30, 10), // 1
	})

	// Endpoints slightly outside polygon edges: fail with Radius 0 or negative/NaN, succeed with Radius 1.0.
	if mesh.AddOffMeshLink(ai.NavOffMeshLink{Start: pt(10.4, 5), End: pt(19.6, 5)}) {
		t.Fatal("expected out-of-mesh endpoints with Radius=0 to fail")
	}
	if mesh.AddOffMeshLink(ai.NavOffMeshLink{Start: pt(10.4, 5), End: pt(19.6, 5), Radius: -1}) {
		t.Fatal("expected negative Radius to fail")
	}
	if mesh.AddOffMeshLink(ai.NavOffMeshLink{Start: pt(10.4, 5), End: pt(19.6, 5), Radius: float32(math.NaN())}) {
		t.Fatal("expected NaN Radius to fail")
	}
	if mesh.AddOffMeshLink(ai.NavOffMeshLink{Start: pt(10.4, 5), End: pt(19.6, 5), Radius: 0.2}) {
		t.Fatal("expected Radius smaller than distance to edge to fail")
	}
	if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
		Start:         pt(10.4, 5),
		End:           pt(19.6, 5),
		Radius:        0.75,
		Bidirectional: true,
		Area:          ai.NavAreaClimb,
		Flags:         uint32(ai.NavFlagClimb),
		ID:            42,
		UserData:      "ladder",
	}) {
		t.Fatal("expected endpoints within Radius=0.75 to snap and attach")
	}

	links := mesh.OffMeshLinks()
	if len(links) != 1 || links[0].Start != pt(10, 5) || links[0].End != pt(20, 5) {
		t.Fatalf("OffMeshLinks = %+v, want snapped (10,5)->(20,5)", links)
	}

	// Bidirectional: both forward and reverse traverse the ladder with properly oriented Start/End.
	fwd := mesh.FindPathWithOptions(pt(2, 5), pt(28, 5), ai.PathOptions{})
	if !fwd.Found || len(fwd.Links) != 1 || fwd.Links[0].Start != pt(10, 5) || fwd.Links[0].End != pt(20, 5) {
		t.Fatalf("forward bidirectional ladder mismatch: %+v", fwd)
	}
	rev := mesh.FindPathWithOptions(pt(28, 5), pt(2, 5), ai.PathOptions{})
	if !rev.Found || len(rev.Links) != 1 || rev.Links[0].Start != pt(20, 5) || rev.Links[0].End != pt(10, 5) ||
		rev.Links[0].FromPoly != 1 || rev.Links[0].ToPoly != 0 {
		t.Fatalf("reverse bidirectional ladder mismatch: %+v", rev)
	}

	// NavFilter excludes by Area or by Flags.
	noClimbArea := ai.DefaultNavFilter()
	noClimbArea.ExcludeArea(ai.NavAreaClimb)
	if r := mesh.FindPathWithFilterAndOptions(pt(2, 5), pt(28, 5), noClimbArea, ai.PathOptions{}); r.Found {
		t.Fatalf("expected ExcludeArea(NavAreaClimb) to block ladder, got %+v", r)
	}
	noClimbFlag := ai.DefaultNavFilter()
	noClimbFlag.ExcludeFlags |= ai.NavFlagClimb
	if r := mesh.FindPathWithFilterAndOptions(pt(2, 5), pt(28, 5), noClimbFlag, ai.PathOptions{}); r.Found {
		t.Fatalf("expected ExcludeFlags(NavFlagClimb) to block ladder, got %+v", r)
	}

	// NavAgent delivers Radius, Area, and UserData in OnLink event.
	var sawEv ai.NavLinkEvent
	var sawLink bool
	agent := ai.NewNavAgent(mesh, pt(2, 5), ai.NavAgentConfig{
		MaxSpeed:         20,
		MaxAcceleration:  100,
		StoppingDistance: 0.3,
		OnLink: func(a *ai.NavAgent, ev ai.NavLinkEvent, dt time.Duration) bool {
			sawEv = ev
			sawLink = true
			a.SetPosition(ev.End)
			return true
		},
	})
	agent.SetTarget(pt(28, 5))
	const dt = time.Second / 60
	for range 300 {
		agent.Step(dt)
		if agent.State() == ai.NavAgentArrived {
			break
		}
	}
	if agent.State() != ai.NavAgentArrived || !sawLink {
		t.Fatalf("agent failed to traverse ladder: state=%v sawLink=%v", agent.State(), sawLink)
	}
	if sawEv.Radius != 0.75 || sawEv.Area != ai.NavAreaClimb || sawEv.UserData != "ladder" || sawEv.LinkID != 42 {
		t.Fatalf("NavLinkEvent metadata mismatch: %+v", sawEv)
	}

	// ClearOffMeshLinks removes all links and bumps revision.
	revBefore := mesh.Revision()
	mesh.ClearOffMeshLinks()
	if mesh.Revision() <= revBefore || len(mesh.OffMeshLinks()) != 0 {
		t.Fatalf("ClearOffMeshLinks failed: rev %d->%d, links=%d", revBefore, mesh.Revision(), len(mesh.OffMeshLinks()))
	}
	if r := mesh.FindPathWithOptions(pt(2, 5), pt(28, 5), ai.PathOptions{}); r.Found {
		t.Fatalf("expected disconnected islands after ClearOffMeshLinks, got %+v", r)
	}
}

func TestOffMeshLinkFlagFilteringNoWalkCollision(t *testing.T) {
	walkOnly := ai.NavFilter{
		IncludeFlags: ai.NavFlagWalk,
		ExcludeFlags: ai.NavFlagDisabled,
	}

	for _, tc := range []struct {
		name string
		area ai.NavArea
		flag ai.NavFlags
	}{
		{"jump", ai.NavAreaJump, ai.NavFlagJump},
		{"climb", ai.NavAreaClimb, ai.NavFlagClimb},
		{"teleport", ai.NavAreaTeleport, ai.NavFlagTeleport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mesh := ai.BuildNavMesh([][]core.Point{
				rectPoly(0, 0, 10, 10),
				rectPoly(20, 0, 30, 10),
			})
			if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
				Start: pt(9, 5),
				End:   pt(21, 5),
				Area:  tc.area,
				Flags: uint32(tc.flag),
				ID:    100,
			}) {
				t.Fatalf("AddOffMeshLink(%s) failed", tc.name)
			}

			// Walk-only agents must NOT traverse explicit jump/climb/teleport links.
			if r := mesh.FindPathWithFilterAndOptions(pt(2, 5), pt(28, 5), walkOnly, ai.PathOptions{}); r.Found {
				t.Fatalf("walk-only filter traversed %s link: %+v", tc.name, r)
			}
			q := ai.NewNavMeshQuery(mesh)
			if r := q.FindPathWithFilterAndOptionsInto(nil, pt(2, 5), pt(28, 5), walkOnly, ai.PathOptions{}); r.Found {
				t.Fatalf("walk-only NavMeshQuery traversed %s link: %+v", tc.name, r)
			}

			// Default filter traverses the link and preserves NavLinkOffMesh on NavPathLink.Flags.
			def := mesh.FindPathWithOptions(pt(2, 5), pt(28, 5), ai.PathOptions{})
			if !def.Found || len(def.Links) != 1 || def.Links[0].Flags&ai.NavLinkOffMesh == 0 {
				t.Fatalf("default filter failed %s link: %+v", tc.name, def)
			}
		})
	}

	// Non-walking agent (IncludeFlags: NavFlagTeleport, ExcludeFlags: NavFlagWalk) must still
	// traverse a NavFlagTeleport link even though NavLinkOffMesh shares bit 0 with NavFlagWalk.
	t.Run("exclude_walk_allows_teleport", func(t *testing.T) {
		mesh := ai.BuildNavMeshWithAreas([]ai.NavPolygonInput{
			{Vertices: rectPoly(0, 0, 10, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagTeleport},
			{Vertices: rectPoly(20, 0, 30, 10), Area: ai.NavAreaDefault, Flags: ai.NavFlagTeleport},
		})
		if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
			Start: pt(9, 5),
			End:   pt(21, 5),
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
		res := mesh.FindPathWithFilterAndOptions(pt(2, 5), pt(28, 5), tpOnly, ai.PathOptions{})
		if !res.Found || len(res.Links) != 1 || res.Links[0].LinkID != 200 || res.Links[0].Flags&ai.NavLinkOffMesh == 0 {
			t.Fatalf("teleport-only filter blocked by NavLinkOffMesh bit collision: %+v", res)
		}
		q := ai.NewNavMeshQuery(mesh)
		qres := q.FindPathWithFilterAndOptionsInto(nil, pt(2, 5), pt(28, 5), tpOnly, ai.PathOptions{})
		if !qres.Found || len(qres.Links) != 1 || qres.Links[0].LinkID != 200 {
			t.Fatalf("teleport-only NavMeshQuery failed: %+v", qres)
		}
	})

	// Untagged off-mesh links (Flags == 0) default to NavFlagWalk so DefaultNavFilter and walk-only allow them.
	t.Run("untagged_defaults_to_walk", func(t *testing.T) {
		mesh := ai.BuildNavMesh([][]core.Point{
			rectPoly(0, 0, 10, 10),
			rectPoly(20, 0, 30, 10),
		})
		if !mesh.AddOffMeshLink(ai.NavOffMeshLink{
			Start: pt(9, 5),
			End:   pt(21, 5),
		}) {
			t.Fatal("AddOffMeshLink(untagged) failed")
		}
		if r := mesh.FindPathWithFilterAndOptions(pt(2, 5), pt(28, 5), walkOnly, ai.PathOptions{}); !r.Found || len(r.Links) != 1 {
			t.Fatalf("walk-only filter failed on untagged link: %+v", r)
		}
	})
}
