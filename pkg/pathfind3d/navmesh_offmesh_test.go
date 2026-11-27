package pathfind3d_test

import (
	"math"
	"testing"
	"time"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind"
	"github.com/nsgo-dev/game/pkg/pathfind3d"
)

func TestNavMesh3DOffMeshOneWayLedgeDropNeverReversed(t *testing.T) {
	// Upper ledge at y=4 covering [0,10]x[0,10], lower floor at y=0 covering [12,24]x[0,10].
	nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 10, 10, 4),  // 0: ledge
		quadXZ(12, 0, 24, 10, 0), // 1: floor
	}, math3d.DegToRad(45))

	if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start:         math3d.Vec3{X: 9, Y: 4, Z: 5},
		End:           math3d.Vec3{X: 13, Y: 0, Z: 5},
		Radius:        0.5,
		Bidirectional: false,
		Flags:         uint32(pathfind3d.NavFlagJump),
		ID:            7,
		Area:          pathfind3d.NavAreaJump,
		UserData:      "drop",
	}) {
		t.Fatal("AddOffMeshLink returned false")
	}

	start := math3d.Vec3{X: 2, Y: 4, Z: 5}
	goal := math3d.Vec3{X: 22, Y: 0, Z: 5}

	res := nm.FindPathWithOptions(start, goal, pathfind.PathOptions{})
	if !res.Found || res.Partial {
		t.Fatalf("expected complete path down ledge, got %+v", res)
	}
	if !res.HasLinks() || len(res.Links) != 1 {
		t.Fatalf("expected 1 link segment, got %+v", res.Links)
	}
	link := res.Links[0]
	if link.StartIndex != 1 || link.EndIndex != 2 {
		t.Fatalf("link indices = [%d, %d], want [1, 2] (path=%v)", link.StartIndex, link.EndIndex, res.Path)
	}
	if link.FromPoly != 0 || link.ToPoly != 1 {
		t.Fatalf("link polys = %d->%d, want 0->1", link.FromPoly, link.ToPoly)
	}
	if !link.Start.ApproxEqual(math3d.Vec3{X: 9, Y: 4, Z: 5}, 1e-4) ||
		!link.End.ApproxEqual(math3d.Vec3{X: 13, Y: 0, Z: 5}, 1e-4) {
		t.Fatalf("link endpoints = %v->%v", link.Start, link.End)
	}
	if !res.Path[link.StartIndex].ApproxEqual(link.Start, 1e-4) ||
		!res.Path[link.EndIndex].ApproxEqual(link.End, 1e-4) {
		t.Fatalf("path[%d]=%v path[%d]=%v mismatch link %v->%v",
			link.StartIndex, res.Path[link.StartIndex], link.EndIndex, res.Path[link.EndIndex], link.Start, link.End)
	}
	if link.Radius != 0.5 || link.Area != pathfind3d.NavAreaJump || link.LinkID != 7 || link.UserData != "drop" {
		t.Fatalf("link metadata mismatch: %+v", link)
	}
	if _, ok := res.LinkAt(0); ok {
		t.Fatal("segment 0 should not be a link")
	}
	if got, ok := res.LinkAt(1); !ok || got.LinkID != 7 {
		t.Fatalf("LinkAt(1) = (%+v, %v), want link 7", got, ok)
	}
	if _, ok := res.LinkAt(2); ok {
		t.Fatal("segment 2 should not be a link")
	}

	// Reverse (floor -> ledge) must NEVER use the one-way drop link.
	if rev := nm.FindPathWithOptions(goal, start, pathfind.PathOptions{}); rev.Found {
		t.Fatalf("reverse path unexpectedly succeeded over one-way drop link: %+v", rev)
	}

	// Incremental search and NavMeshQuery must match identically.
	search := nm.StartSearch(start, goal, pathfind.PathOptions{})
	for !search.Status().Done() {
		search.Step(1)
	}
	incRes := search.Result()
	if !incRes.Found || len(incRes.Links) != 1 || incRes.Links[0] != link {
		t.Fatalf("incremental search mismatch: got %+v, want %+v", incRes, res)
	}

	q := pathfind3d.NewNavMeshQuery(nm)
	qRes := q.FindPathWithFilterAndOptionsInto(nil, start, goal, pathfind3d.DefaultNavFilter(), pathfind.PathOptions{})
	if !qRes.Found || len(qRes.Links) != 1 || qRes.Links[0] != link {
		t.Fatalf("query result mismatch: got %+v, want %+v", qRes, res)
	}
	if qRev := q.FindPathWithFilterAndOptionsInto(nil, goal, start, pathfind3d.DefaultNavFilter(), pathfind.PathOptions{}); qRev.Found {
		t.Fatalf("query reverse unexpectedly succeeded: %+v", qRev)
	}
}

func TestNavMesh3DOffMeshTeleportCostVsWalking(t *testing.T) {
	// 1. Disconnected 3D islands connected by a teleport link.
	disc := pathfind3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 10, 10, 0),
		quadXZ(100, 0, 110, 10, 5),
	}, math3d.DegToRad(45))
	if !disc.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start:    math3d.Vec3{X: 8, Y: 0, Z: 5},
		End:      math3d.Vec3{X: 102, Y: 5, Z: 5},
		Cost:     4,
		Area:     pathfind3d.NavAreaTeleport,
		Flags:    uint32(pathfind3d.NavFlagTeleport),
		ID:       12,
		UserData: "teleport",
	}) {
		t.Fatal("AddOffMeshLink failed")
	}
	discRes := disc.FindPathWithOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 108, Y: 5, Z: 5}, pathfind.PathOptions{})
	if !discRes.Found || len(discRes.Links) != 1 || discRes.Links[0].UserData != "teleport" {
		t.Fatalf("expected teleport across disconnected 3D islands, got %+v", discRes)
	}

	// 2. Connected 3D islands with a walking bridge AND a teleporter placed behind start.
	makeConnected := func(teleportCost float32) *pathfind3d.NavMesh {
		nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
			quadXZ(-10, 0, 0, 10, 0),   // 0: alcove behind start
			quadXZ(0, 0, 10, 10, 0),    // 1: start island
			quadXZ(10, 0, 100, 10, 0),  // 2: long bridge
			quadXZ(100, 0, 110, 10, 0), // 3: goal island
		}, math3d.DegToRad(45))
		if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
			Start:    math3d.Vec3{X: -5, Y: 0, Z: 5},
			End:      math3d.Vec3{X: 105, Y: 0, Z: 5},
			Cost:     teleportCost,
			Area:     pathfind3d.NavAreaTeleport,
			Flags:    uint32(pathfind3d.NavFlagTeleport),
			ID:       55,
			UserData: "teleport",
		}) {
			t.Fatal("AddOffMeshLink failed")
		}
		return nm
	}

	// Cheaper teleport (6 + 5 + 4 = 15 < 108): must use teleport.
	cheap := makeConnected(5)
	cheapRes := cheap.FindPathWithOptions(math3d.Vec3{X: 1, Y: 0, Z: 5}, math3d.Vec3{X: 109, Y: 0, Z: 5}, pathfind.PathOptions{})
	if !cheapRes.Found || len(cheapRes.Links) != 1 || cheapRes.Links[0].LinkID != 55 {
		t.Fatalf("expected cheap 3D teleport to beat walking route, got %+v", cheapRes)
	}
	qCheap := pathfind3d.NewNavMeshQuery(cheap).FindPathWithFilterAndOptionsInto(
		nil, math3d.Vec3{X: 1, Y: 0, Z: 5}, math3d.Vec3{X: 109, Y: 0, Z: 5}, pathfind3d.DefaultNavFilter(), pathfind.PathOptions{},
	)
	if !qCheap.Found || len(qCheap.Links) != 1 || qCheap.Links[0].LinkID != 55 {
		t.Fatalf("query expected cheap 3D teleport, got %+v", qCheap)
	}

	// More expensive teleport (6 + 200 + 4 = 210 > 108): must walk instead!
	expensive := makeConnected(200)
	expRes := expensive.FindPathWithOptions(math3d.Vec3{X: 1, Y: 0, Z: 5}, math3d.Vec3{X: 109, Y: 0, Z: 5}, pathfind.PathOptions{})
	if !expRes.Found || expRes.HasLinks() {
		t.Fatalf("expected 3D walking route when teleport is more expensive, got %+v", expRes)
	}

	// Area cost scaling via NavFilter can flip preference at runtime.
	costlyTeleport := pathfind3d.DefaultNavFilter()
	costlyTeleport.SetAreaCost(pathfind3d.NavAreaTeleport, 40) // 5 * 40 = 200 > 108
	scaledRes := cheap.FindPathWithFilterAndOptions(
		math3d.Vec3{X: 1, Y: 0, Z: 5}, math3d.Vec3{X: 109, Y: 0, Z: 5}, costlyTeleport, pathfind.PathOptions{},
	)
	if !scaledRes.Found || scaledRes.HasLinks() {
		t.Fatalf("expected area-cost-scaled teleport to fall back to walking, got %+v", scaledRes)
	}
}

func TestNavMesh3DOffMeshRadiusSnapVerticalLadderFilterAndAgent(t *testing.T) {
	// Vertically stacked floors: lower floor at y=0, upper balcony directly above at y=5.
	nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
		quadXZ(0, 0, 10, 10, 0), // 0: ground floor
		quadXZ(0, 0, 10, 10, 5), // 1: upper balcony
	}, math3d.DegToRad(45))

	// Invalid radii rejected.
	if nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start:  math3d.Vec3{X: 10.4, Y: 0, Z: 5},
		End:    math3d.Vec3{X: 10.4, Y: 5, Z: 5},
		Radius: -0.5,
	}) {
		t.Fatal("negative radius should fail")
	}
	if nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start:  math3d.Vec3{X: 10.4, Y: 0, Z: 5},
		End:    math3d.Vec3{X: 10.4, Y: 5, Z: 5},
		Radius: float32(math.NaN()),
	}) {
		t.Fatal("NaN radius should fail")
	}

	// Vertical ladder slightly off the edge at x=10.4 snaps to ground (10,0,5) and upper (10,5,5)
	// cleanly keeping stacked levels separated.
	if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
		Start:         math3d.Vec3{X: 10.4, Y: 0, Z: 5},
		End:           math3d.Vec3{X: 10.4, Y: 5, Z: 5},
		Radius:        0.75,
		Bidirectional: true,
		Area:          pathfind3d.NavAreaClimb,
		Flags:         uint32(pathfind3d.NavFlagClimb),
		ID:            33,
		UserData:      "ladder",
	}) {
		t.Fatal("expected vertical ladder with Radius=0.75 to snap to both stacked levels")
	}

	stored := nm.OffMeshLinks()
	if len(stored) != 1 ||
		!stored[0].Start.ApproxEqual(math3d.Vec3{X: 10, Y: 0, Z: 5}, 1e-4) ||
		!stored[0].End.ApproxEqual(math3d.Vec3{X: 10, Y: 5, Z: 5}, 1e-4) {
		t.Fatalf("OffMeshLinks = %+v", stored)
	}

	// Climb up: ground -> balcony.
	up := nm.FindPathWithOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 2, Y: 5, Z: 5}, pathfind.PathOptions{})
	if !up.Found || len(up.Links) != 1 || up.Links[0].FromPoly != 0 || up.Links[0].ToPoly != 1 {
		t.Fatalf("climb up mismatch: %+v", up)
	}
	// Climb down: balcony -> ground.
	down := nm.FindPathWithOptions(math3d.Vec3{X: 2, Y: 5, Z: 5}, math3d.Vec3{X: 2, Y: 0, Z: 5}, pathfind.PathOptions{})
	if !down.Found || len(down.Links) != 1 || down.Links[0].FromPoly != 1 || down.Links[0].ToPoly != 0 {
		t.Fatalf("climb down mismatch: %+v", down)
	}

	// Filter excludes ladder by area or flag.
	noClimb := pathfind3d.DefaultNavFilter()
	noClimb.ExcludeArea(pathfind3d.NavAreaClimb)
	if r := nm.FindPathWithFilterAndOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 2, Y: 5, Z: 5}, noClimb, pathfind.PathOptions{}); r.Found {
		t.Fatalf("expected ExcludeArea to block ladder, got %+v", r)
	}

	// 3D NavAgent receives Radius, Area, UserData in OnLink.
	var sawEv pathfind3d.NavLinkEvent
	var sawLink bool
	agent := pathfind3d.NewNavAgent(nm, math3d.Vec3{X: 2, Y: 0, Z: 5}, pathfind3d.NavAgentConfig{
		MaxSpeed:         6,
		MaxAcceleration:  30,
		StoppingDistance: 0.3,
		OnLink: func(a *pathfind3d.NavAgent, ev pathfind3d.NavLinkEvent, dt time.Duration) bool {
			sawEv = ev
			sawLink = true
			a.SetPosition(ev.End)
			return true
		},
	})
	agent.SetTarget(math3d.Vec3{X: 2, Y: 5, Z: 5})
	const dt = time.Second / 60
	for range 400 {
		agent.Step(dt)
		if agent.State() == pathfind3d.NavAgentArrived {
			break
		}
	}
	if agent.State() != pathfind3d.NavAgentArrived || !sawLink {
		t.Fatalf("3D agent failed ladder traversal: state=%v sawLink=%v", agent.State(), sawLink)
	}
	if sawEv.Radius != 0.75 || sawEv.Area != pathfind3d.NavAreaClimb || sawEv.UserData != "ladder" || sawEv.LinkID != 33 {
		t.Fatalf("3D NavLinkEvent metadata mismatch: %+v", sawEv)
	}

	// ClearOffMeshLinks removes links and bumps revision.
	revBefore := nm.Revision()
	nm.ClearOffMeshLinks()
	if nm.Revision() <= revBefore || len(nm.OffMeshLinks()) != 0 {
		t.Fatalf("ClearOffMeshLinks failed: rev %d->%d, links=%d", revBefore, nm.Revision(), len(nm.OffMeshLinks()))
	}
	if r := nm.FindPathWithOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 2, Y: 5, Z: 5}, pathfind.PathOptions{}); r.Found {
		t.Fatalf("expected unreachable after ClearOffMeshLinks, got %+v", r)
	}
}

func TestNavMesh3DOffMeshFlagFilteringNoWalkCollision(t *testing.T) {
	walkOnly := pathfind3d.NavFilter{
		IncludeFlags: pathfind3d.NavFlagWalk,
		ExcludeFlags: pathfind3d.NavFlagDisabled,
	}

	for _, tc := range []struct {
		name string
		area pathfind3d.NavArea
		flag pathfind3d.NavFlags
	}{
		{"jump", pathfind3d.NavAreaJump, pathfind3d.NavFlagJump},
		{"climb", pathfind3d.NavAreaClimb, pathfind3d.NavFlagClimb},
		{"teleport", pathfind3d.NavAreaTeleport, pathfind3d.NavFlagTeleport},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
				quadXZ(0, 0, 10, 10, 0),
				quadXZ(20, 0, 30, 10, 0),
			}, math.Pi/4)
			if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
				Start: math3d.Vec3{X: 9, Y: 0, Z: 5},
				End:   math3d.Vec3{X: 21, Y: 0, Z: 5},
				Area:  tc.area,
				Flags: uint32(tc.flag),
				ID:    100,
			}) {
				t.Fatalf("AddOffMeshLink(%s) failed", tc.name)
			}

			// Walk-only agents must NOT traverse explicit jump/climb/teleport links.
			if r := nm.FindPathWithFilterAndOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, walkOnly, pathfind.PathOptions{}); r.Found {
				t.Fatalf("walk-only filter traversed 3D %s link: %+v", tc.name, r)
			}
			q := pathfind3d.NewNavMeshQuery(nm)
			if r := q.FindPathWithFilterAndOptionsInto(nil, math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, walkOnly, pathfind.PathOptions{}); r.Found {
				t.Fatalf("walk-only 3D NavMeshQuery traversed %s link: %+v", tc.name, r)
			}

			// Default filter traverses the link and preserves NavLinkOffMesh on NavPathLink.Flags.
			def := nm.FindPathWithOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, pathfind.PathOptions{})
			if !def.Found || len(def.Links) != 1 || def.Links[0].Flags&pathfind3d.NavLinkOffMesh == 0 {
				t.Fatalf("default filter failed 3D %s link: %+v", tc.name, def)
			}
		})
	}

	// Non-walking agent (IncludeFlags: NavFlagTeleport, ExcludeFlags: NavFlagWalk) must still
	// traverse a NavFlagTeleport link even though NavLinkOffMesh shares bit 0 with NavFlagWalk.
	t.Run("exclude_walk_allows_teleport", func(t *testing.T) {
		nm := pathfind3d.BuildNavMeshWithAreas([]pathfind3d.NavPolygonInput{
			{Vertices: quadXZ(0, 0, 10, 10, 0), Area: pathfind3d.NavAreaDefault, Flags: pathfind3d.NavFlagTeleport},
			{Vertices: quadXZ(20, 0, 30, 10, 0), Area: pathfind3d.NavAreaDefault, Flags: pathfind3d.NavFlagTeleport},
		}, math.Pi/4)
		if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
			Start: math3d.Vec3{X: 9, Y: 0, Z: 5},
			End:   math3d.Vec3{X: 21, Y: 0, Z: 5},
			Area:  pathfind3d.NavAreaTeleport,
			Flags: uint32(pathfind3d.NavFlagTeleport),
			ID:    200,
		}) {
			t.Fatal("AddOffMeshLink(teleport) failed")
		}
		tpOnly := pathfind3d.NavFilter{
			IncludeFlags: pathfind3d.NavFlagTeleport,
			ExcludeFlags: pathfind3d.NavFlagWalk,
		}
		res := nm.FindPathWithFilterAndOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, tpOnly, pathfind.PathOptions{})
		if !res.Found || len(res.Links) != 1 || res.Links[0].LinkID != 200 || res.Links[0].Flags&pathfind3d.NavLinkOffMesh == 0 {
			t.Fatalf("3D teleport-only filter blocked by NavLinkOffMesh bit collision: %+v", res)
		}
		q := pathfind3d.NewNavMeshQuery(nm)
		qres := q.FindPathWithFilterAndOptionsInto(nil, math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, tpOnly, pathfind.PathOptions{})
		if !qres.Found || len(qres.Links) != 1 || qres.Links[0].LinkID != 200 {
			t.Fatalf("3D teleport-only NavMeshQuery failed: %+v", qres)
		}
	})

	// Untagged off-mesh links (Flags == 0) default to NavFlagWalk so DefaultNavFilter and walk-only allow them.
	t.Run("untagged_defaults_to_walk", func(t *testing.T) {
		nm := pathfind3d.BuildNavMesh([][]math3d.Vec3{
			quadXZ(0, 0, 10, 10, 0),
			quadXZ(20, 0, 30, 10, 0),
		}, math.Pi/4)
		if !nm.AddOffMeshLink(pathfind3d.NavOffMeshLink{
			Start: math3d.Vec3{X: 9, Y: 0, Z: 5},
			End:   math3d.Vec3{X: 21, Y: 0, Z: 5},
		}) {
			t.Fatal("AddOffMeshLink(untagged) failed")
		}
		if r := nm.FindPathWithFilterAndOptions(math3d.Vec3{X: 2, Y: 0, Z: 5}, math3d.Vec3{X: 28, Y: 0, Z: 5}, walkOnly, pathfind.PathOptions{}); !r.Found || len(r.Links) != 1 {
			t.Fatalf("3D walk-only filter failed on untagged link: %+v", r)
		}
	})
}
