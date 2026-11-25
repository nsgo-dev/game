package ai_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/game/packages/game/ai"
	"github.com/nsgo-dev/game/packages/game/math2d"
	"github.com/nsgo-dev/game/packages/game/physics"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// buildReferenceCoverLevel constructs a deterministic 12x9 arena (tileSize = 16) with:
//   - Outer solid border walls
//   - A vertical full-height bunker wall near the left-center: col=4, rows 3..5
//   - A horizontal half-cover sandbag barricade near the center-top: cols 6..7, row=2
//   - A far-right pillar: col=9, row=6
//
// Threat sits on the right at (160, 72) (col=10, row=4.5).
// Agent sits on the left at (28, 72) (col=1.75, row=4.5).
// The best cover against the threat is the left face of the bunker at col=3, row=4 (center (56, 72)),
// which is full cover, directly facing (+1, 0) toward the threat, blocks LOS to the threat,
// and has the shortest walkable path from the agent.
func buildReferenceCoverLevel() (*tilemap.Tilemap, ai.Grid, []ai.CoverPoint, ai.LOSFunc) {
	const (
		cols     = 12
		rows     = 9
		tileSize = float32(16)
	)
	const halfFlag tilemap.TileFlags = 1 << 8

	m := tilemap.New(cols, rows, tileSize, tileSize)
	data := make([]uint32, cols*rows)
	setTile := func(col, row int, gid uint32) {
		data[row*cols+col] = gid
	}

	// Border walls (gid 1 = full wall).
	for c := range cols {
		setTile(c, 0, 1)
		setTile(c, rows-1, 1)
	}
	for r := range rows {
		setTile(0, r, 1)
		setTile(cols-1, r, 1)
	}
	// Bunker wall at col=4, rows 3..5 (gid 1 = full cover).
	for r := 3; r <= 5; r++ {
		setTile(4, r, 1)
	}
	// Half-cover sandbags at cols 6..7, row=2 (gid 2 = half cover).
	setTile(6, 2, 2)
	setTile(7, 2, 2)
	// Far pillar at col=9, row=6 (gid 1 = full cover).
	setTile(9, 6, 1)

	m.Layers = []tilemap.TileLayer{tilemap.NewTileLayer("walls", cols, rows, data)}
	m.SetFlags(1, tilemap.TileSolid)
	m.SetFlags(2, tilemap.TileSolid|halfFlag)

	grid := ai.GridFunc{
		Cols: cols,
		Rows: rows,
		Walk: func(col, row int) bool {
			return m.FlagsAt(col, row)&tilemap.TileSolid == 0
		},
	}

	covers := ai.GenerateCoverFromTilemap(m, ai.TileCoverOptions{
		HalfCoverMask: halfFlag,
		DefaultHeight: ai.CoverFull,
	})

	los := func(from, to math2d.Vec2) bool {
		_, hit := m.Raycast(from.Point(), to.Point(), tilemap.TileSolid)
		return !hit
	}

	return m, grid, covers, los
}

func TestReferenceLevelBestCoverDeterministic(t *testing.T) {
	_, grid, covers, los := buildReferenceCoverLevel()
	if len(covers) == 0 {
		t.Fatal("expected generated cover points in reference level")
	}

	// Verify both half and full cover were generated.
	var halfCount, fullCount int
	for _, cp := range covers {
		switch cp.Height {
		case ai.CoverHalf:
			halfCount++
		case ai.CoverFull:
			fullCount++
		}
	}
	if halfCount == 0 || fullCount == 0 {
		t.Fatalf("expected both half and full cover points, got half=%d full=%d", halfCount, fullCount)
	}

	// Build a threat influence layer centered at the threat's cell (10, 4).
	threat := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	threat.AddSource(ai.InfluenceSource{
		Cell:     ai.Cell{Col: 10, Row: 4},
		Strength: 10,
		Radius:   6,
		Falloff:  ai.FalloffLinear,
	})
	threat.Update(grid)

	makeQuery := func() *ai.TacticalQuery {
		return ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.CoverPointGenerator{Points: covers},
			},
			&ai.AngleTest{
				Mode:   ai.AngleCoverToTarget,
				Filter: ai.AtMost(math.Pi / 3),
				Weight: 2.0,
				Invert: true, // smaller angle to threat scores higher
			},
			&ai.CoverHeightTest{
				Prefer: ai.CoverFull,
				Weight: 1.0,
			},
			&ai.InfluenceTest{
				Layer:  threat,
				Weight: 1.5,
				Invert: true, // lower threat scores higher
			},
			&ai.LOSTest{
				LOS:       los,
				WantClear: false, // hidden from threat
				Purpose:   ai.TestFilterAndScore,
				Weight:    2.0,
			},
			&ai.PathDistanceTest{
				Grid:     grid,
				CellSize: 16,
				Weight:   2.5,
				Invert:   true, // shorter walkable path scores higher
			},
		)
	}

	ctx := ai.TacticalContext{
		QuerierID:  1,
		QuerierPos: math2d.V2(28, 72),  // col 1, row 4
		TargetPos:  math2d.V2(160, 72), // col 10, row 4
	}

	// Expected best cover: left face of bunker at col=3, row=4 -> center (56, 72), facing (+1, 0), full height.
	wantPos := math2d.V2(56, 72)
	wantFacing := math2d.V2(1, 0)

	q := makeQuery()
	firstRun := q.Run(ctx)
	if len(firstRun) == 0 {
		t.Fatal("expected at least one valid cover candidate")
	}
	best := firstRun[0]
	if !best.Pos.ApproxEqual(wantPos, 1e-3) {
		t.Fatalf("best cover pos = %v, want %v (all top3: %+v)", best.Pos, wantPos, firstRun[:min(3, len(firstRun))])
	}
	if !best.Facing.ApproxEqual(wantFacing, 1e-4) {
		t.Fatalf("best cover facing = %v, want %v", best.Facing, wantFacing)
	}
	if best.Height != ai.CoverFull {
		t.Fatalf("best cover height = %v, want CoverFull", best.Height)
	}
	if los(best.Pos, ctx.TargetPos) {
		t.Fatalf("best cover %v unexpectedly has clear LOS to threat %v", best.Pos, ctx.TargetPos)
	}

	// Repeated runs (both reusing q and creating fresh queries) must return identical results.
	for run := range 20 {
		got := q.Run(ctx)
		if !slices.Equal(got, firstRun) {
			t.Fatalf("run %d differed from first run:\ngot  %+v\nwant %+v", run, got, firstRun)
		}
		fresh := makeQuery().Run(ctx)
		if !slices.Equal(fresh, firstRun) {
			t.Fatalf("fresh query run %d differed from first run", run)
		}
	}
}

func TestTwoAgentsNeverReserveSameCoverPointAndRelease(t *testing.T) {
	_, grid, covers, los := buildReferenceCoverLevel()

	res := ai.NewPointReservations(ai.ReservationConfig{
		DefaultTimeout: 2 * time.Second,
	})

	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{
			&ai.CoverPointGenerator{Points: covers},
		},
		&ai.AngleTest{
			Mode:   ai.AngleCoverToTarget,
			Filter: ai.AtMost(math.Pi / 3),
			Weight: 2.0,
			Invert: true,
		},
		&ai.LOSTest{
			LOS:       los,
			WantClear: false,
			Purpose:   ai.TestFilterOnly,
		},
		&ai.PathDistanceTest{
			Grid:     grid,
			CellSize: 16,
			Weight:   2.0,
			Invert:   true,
		},
	)

	// Two agents standing at the exact same spot query together against the same threat.
	ctxA := ai.TacticalContext{
		QuerierID:    101,
		QuerierPos:   math2d.V2(28, 72),
		TargetPos:    math2d.V2(160, 72),
		Reservations: res,
	}
	ctxB := ai.TacticalContext{
		QuerierID:    102,
		QuerierPos:   math2d.V2(28, 72),
		TargetPos:    math2d.V2(160, 72),
		Reservations: res,
	}

	spotA, okA := q.RunAndReserve(ctxA, 0)
	spotB, okB := q.RunAndReserve(ctxB, 0)
	if !okA || !okB {
		t.Fatalf("expected both agents to reserve cover, got okA=%v okB=%v", okA, okB)
	}
	if spotA.CoverID == spotB.CoverID {
		t.Fatalf("both agents reserved the same cover point ID %d", spotA.CoverID)
	}
	if res.Count() != 2 {
		t.Fatalf("reservation count = %d, want 2", res.Count())
	}
	if holder, ok := res.HolderOfCover(spotA.CoverID); !ok || holder != 101 {
		t.Fatalf("HolderOfCover(%d) = (%d, %v), want (101, true)", spotA.CoverID, holder, ok)
	}
	if holder, ok := res.HolderOfCover(spotB.CoverID); !ok || holder != 102 {
		t.Fatalf("HolderOfCover(%d) = (%d, %v), want (102, true)", spotB.CoverID, holder, ok)
	}

	// Agent B cannot steal Agent A's cover point directly, and its existing reservation stays intact.
	if res.Reserve(102, spotA, 0) {
		t.Fatal("agent 102 should not be able to reserve agent 101's cover point")
	}
	if curB, ok := res.Get(102); !ok || curB.CoverID != spotB.CoverID {
		t.Fatalf("failed reserve corrupted agent 102's reservation: %+v, %v", curB, ok)
	}

	// Agent A re-querying still sees its own cover point as available and retains it.
	spotA2, okA2 := q.RunAndReserve(ctxA, 0)
	if !okA2 || spotA2.CoverID != spotA.CoverID {
		t.Fatalf("agent 101 re-query got (%d, %v), want (%d, true)", spotA2.CoverID, okA2, spotA.CoverID)
	}

	// Explicit release on agent 101's death frees spotA immediately for agent 102.
	if !res.Release(101) {
		t.Fatal("expected Release(101) to return true")
	}
	if res.Release(101) {
		t.Fatal("second Release(101) should return false")
	}
	spotB2, okB2 := q.RunAndReserve(ctxB, 1*time.Second)
	if !okB2 || spotB2.CoverID != spotA.CoverID {
		t.Fatalf("after 101 died, agent 102 should claim best cover %d, got (%d, %v)", spotA.CoverID, spotB2.CoverID, okB2)
	}
	// Agent 102's previous cover spotB must have been released automatically on re-query.
	if _, held := res.HolderOfCover(spotB.CoverID); held {
		t.Fatalf("agent 102's previous cover %d should have been released on re-query", spotB.CoverID)
	}

	// Timeout expiration via deterministic Step(dt):
	res.Step(500 * time.Millisecond)
	if res.Count() != 1 {
		t.Fatalf("after 500ms expected 1 active reservation, got %d", res.Count())
	}
	res.Refresh(102, 1500*time.Millisecond)
	res.Step(1000 * time.Millisecond)
	if res.Count() != 1 {
		t.Fatalf("after Refresh(1.5s) and 1s step, expected 1 active reservation, got %d", res.Count())
	}
	res.Step(500 * time.Millisecond)
	if res.Count() != 0 {
		t.Fatalf("after remaining 500ms, expected 0 reservations, got %d", res.Count())
	}

	// MinSeparation prevents two agents from reserving non-cover positions too close together.
	posRes := ai.NewPointReservations(ai.ReservationConfig{MinSeparation: 20})
	if !posRes.ReservePos(1, math2d.V2(10, 10), 0) {
		t.Fatal("expected agent 1 to reserve (10, 10)")
	}
	if posRes.ReservePos(2, math2d.V2(20, 10), 0) {
		t.Fatal("agent 2 should not reserve within MinSeparation of agent 1")
	}
	if !posRes.ReservePos(2, math2d.V2(35, 10), 0) {
		t.Fatal("agent 2 should reserve beyond MinSeparation")
	}
}

func TestPerStepBudgetStrictlyBounded(t *testing.T) {
	_, grid, covers, rawLOS := buildReferenceCoverLevel()

	var raycastCalls, pathCalls int
	countedLOS := func(from, to math2d.Vec2) bool {
		raycastCalls++
		return rawLOS(from, to)
	}
	astar := ai.NewAStar()
	countedPath := func(from, to math2d.Vec2) (float64, bool) {
		pathCalls++
		sc := ai.Cell{Col: int(from.X / 16), Row: int(from.Y / 16)}
		gc := ai.Cell{Col: int(to.X / 16), Row: int(to.Y / 16)}
		_, cost, ok := astar.Find(grid, sc, gc)
		return cost * 16, ok
	}

	const (
		maxRaycasts    = 5
		maxPathQueries = 3
	)
	budget := ai.NewQueryBudget(maxRaycasts, maxPathQueries)

	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{
			&ai.CoverPointGenerator{Points: covers},
		},
		&ai.AngleTest{
			Mode:   ai.AngleCoverToTarget,
			Weight: 2.0,
			Invert: true,
		},
		&ai.DistanceTest{
			To:     ai.OriginQuerier,
			Weight: 1.5,
			Invert: true,
		},
		&ai.LOSTest{
			LOS:       countedLOS,
			WantClear: false,
			Purpose:   ai.TestFilterAndScore,
			Weight:    1.0,
		},
		&ai.PathDistanceTest{
			PathCost: countedPath,
			Purpose:  ai.TestFilterAndScore,
			Weight:   1.0,
			Invert:   true,
		},
	)

	// Run 3 agents in the same step sharing one QueryBudget.
	for agentID := uint64(1); agentID <= 3; agentID++ {
		q.Run(ai.TacticalContext{
			QuerierID:  agentID,
			QuerierPos: math2d.V2(28, 72),
			TargetPos:  math2d.V2(160, 72),
			Budget:     budget,
		})
	}

	if raycastCalls > maxRaycasts {
		t.Fatalf("raycast calls = %d, exceeded step budget %d", raycastCalls, maxRaycasts)
	}
	if budget.RaycastsUsed() != raycastCalls {
		t.Fatalf("budget.RaycastsUsed() = %d, actual calls = %d", budget.RaycastsUsed(), raycastCalls)
	}
	if pathCalls > maxPathQueries {
		t.Fatalf("path query calls = %d, exceeded step budget %d", pathCalls, maxPathQueries)
	}
	if budget.PathQueriesUsed() != pathCalls {
		t.Fatalf("budget.PathQueriesUsed() = %d, actual calls = %d", budget.PathQueriesUsed(), pathCalls)
	}
	if budget.RaycastsRemaining() != 0 {
		t.Fatalf("expected raycast budget exhausted across 3 agents, remaining = %d", budget.RaycastsRemaining())
	}
	if !q.LastStats().BudgetExhausted {
		t.Fatal("expected BudgetExhausted=true once step budget ran out")
	}

	// Next step: Reset replenishes the budget and allows queries again.
	budget.Reset()
	raycastCalls, pathCalls = 0, 0
	if budget.RaycastsRemaining() != maxRaycasts || budget.PathQueriesRemaining() != maxPathQueries {
		t.Fatalf("budget not reset: raycasts=%d paths=%d", budget.RaycastsRemaining(), budget.PathQueriesRemaining())
	}

	best, ok := q.Best(ai.TacticalContext{
		QuerierID:  1,
		QuerierPos: math2d.V2(28, 72),
		TargetPos:  math2d.V2(160, 72),
		Budget:     budget,
	})
	if !ok {
		t.Fatal("expected valid candidate after budget reset")
	}
	if raycastCalls > maxRaycasts || pathCalls > maxPathQueries {
		t.Fatalf("post-reset calls (%d, %d) exceeded budget (%d, %d)", raycastCalls, pathCalls, maxRaycasts, maxPathQueries)
	}
	// Because preliminary Angle + Distance scores ran before LOS, the top candidates (including the bunker left face)
	// were evaluated first within the 5-raycast budget!
	if best.Facing.X < 0.9 {
		t.Fatalf("expected preliminary scoring to prioritize threat-facing cover under tight budget, got %+v", best)
	}
}

func TestGeneratorsAndTests(t *testing.T) {
	t.Run("RingGenerator flanking query", func(t *testing.T) {
		q := ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.RingGenerator{
					// Default Center is OriginTarget.
					InnerRadius:   50,
					OuterRadius:   100,
					Rings:         2,
					PointsPerRing: 8,
					FaceCenter:    true,
				},
			},
			&ai.AngleTest{
				Mode:   ai.AngleFlank,
				Filter: ai.AtLeast(math.Pi * 0.75), // behind the target (looking +X)
				Weight: 2.0,
			},
			&ai.DistanceTest{
				To:     ai.OriginTarget,
				Filter: ai.AtMost(60), // inner ring
				Weight: 1.0,
				Invert: true,
			},
		)

		res := q.Run(ai.TacticalContext{
			QuerierPos: math2d.V2(0, 0),
			TargetPos:  math2d.V2(100, 0),
			TargetDir:  math2d.V2(1, 0), // target faces +X, so behind is -X -> (50, 0)
		})
		if len(res) == 0 {
			t.Fatal("expected flanking candidates behind target")
		}
		if !res[0].Pos.ApproxEqual(math2d.V2(50, 0), 1e-3) {
			t.Fatalf("best rear flank = %v, want (50, 0)", res[0].Pos)
		}
		if !res[0].Facing.ApproxEqual(math2d.V2(1, 0), 1e-4) {
			t.Fatalf("FaceCenter facing = %v, want (1, 0)", res[0].Facing)
		}
	})

	t.Run("GridGenerator with InfluenceLayer", func(t *testing.T) {
		grid := ai.NewBoolGrid(6, 6)
		grid.SetWalkable(2, 2, false)
		grid.SetWalkable(3, 2, false)

		danger := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 10)
		danger.AddSource(ai.InfluenceSource{
			Cell:     ai.Cell{Col: 0, Row: 0},
			Strength: 10,
			Radius:   5,
			Falloff:  ai.FalloffLinear,
		})
		danger.Update(grid)

		q := ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.GridGenerator{
					Grid:     grid,
					CellSize: 10,
				},
			},
			&ai.InfluenceTest{
				Layer:             danger,
				RejectOutOfBounds: true,
				Weight:            3.0,
				Invert:            true, // safest (lowest danger) wins
			},
			&ai.DistanceTest{
				To:     ai.OriginTarget,
				Weight: 1.0, // farthest from threat wins ties
			},
		)

		best, ok := q.Best(ai.TacticalContext{
			QuerierPos: math2d.V2(15, 15),
			TargetPos:  math2d.V2(5, 5),
		})
		if !ok {
			t.Fatal("expected valid grid candidate")
		}
		// Cell (5, 5) has center (55, 55), farthest from (0, 0) with 0 danger.
		if !best.Pos.ApproxEqual(math2d.V2(55, 55), 1e-3) {
			t.Fatalf("safest grid cell = %v, want (55, 55)", best.Pos)
		}
	})

	t.Run("NavMeshGenerator and NavMesh PathDistanceTest", func(t *testing.T) {
		// U-shaped corridor: left room -> top bridge -> right room.
		// Point (210, 80) is geometrically close to (30, 80) across the gap,
		// but has a long NavMesh path around the top bridge, whereas (30, 20) is short!
		mesh := ai.BuildNavMesh([][]core.Point{
			{{X: 0, Y: 0}, {X: 60, Y: 0}, {X: 60, Y: 100}, {X: 0, Y: 100}},
			{{X: 60, Y: 0}, {X: 180, Y: 0}, {X: 180, Y: 30}, {X: 60, Y: 30}},
			{{X: 180, Y: 0}, {X: 240, Y: 0}, {X: 240, Y: 100}, {X: 180, Y: 100}},
		})

		q := ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.NavMeshGenerator{
					Mesh:    mesh,
					Spacing: 20,
				},
			},
			&ai.PathDistanceTest{
				NavMesh: mesh,
				Filter:  ai.Between(10, 120),
				Weight:  2.0,
				Invert:  true, // shortest navmesh path
			},
		)

		results := q.Run(ai.TacticalContext{
			QuerierPos: math2d.V2(30, 80),
		})
		if len(results) == 0 {
			t.Fatal("expected reachable navmesh candidates")
		}
		// Every surviving candidate must lie inside the mesh and have path length <= 120
		// (so none from the far right room at x >= 180 can survive!).
		for _, c := range results {
			if !mesh.Contains(c.Pos.Point()) {
				t.Fatalf("navmesh candidate %v outside mesh", c.Pos)
			}
			if c.Pos.X >= 180 {
				t.Fatalf("far-room candidate %v should have been filtered by PathDistanceTest <= 120", c.Pos)
			}
		}

		// Centroid mode (Spacing == 0) emits one candidate per polygon.
		centroidQ := ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.NavMeshGenerator{Mesh: mesh},
			},
		)
		centroids := centroidQ.Run(ai.TacticalContext{})
		if len(centroids) != 3 {
			t.Fatalf("centroid mode emitted %d candidates, want 3", len(centroids))
		}
	})

	t.Run("CoverPointGenerator tag and height filtering + CustomTest", func(t *testing.T) {
		pts := []ai.CoverPoint{
			{ID: 1, Pos: math2d.V2(10, 0), Facing: math2d.V2(1, 0), Height: ai.CoverHalf, Tags: []string{"window"}},
			{ID: 2, Pos: math2d.V2(20, 0), Facing: math2d.V2(1, 0), Height: ai.CoverFull, Tags: []string{"flank", "window"}},
			{ID: 3, Pos: math2d.V2(30, 0), Facing: math2d.V2(1, 0), Height: ai.CoverFull, Tags: []string{"flank"}},
		}
		q := ai.NewTacticalQuery(
			[]ai.CandidateGenerator{
				&ai.CoverPointGenerator{
					Points:      pts,
					MinHeight:   ai.CoverFull,
					RequiredTag: "window",
				},
			},
			&ai.CustomTest{
				Eval: func(_ ai.TacticalContext, c ai.TacticalCandidate) (float64, bool) {
					return float64(c.Pos.X), true
				},
				Weight: 1.0,
			},
		)
		best, ok := q.Best(ai.TacticalContext{})
		if !ok || best.CoverID != 2 {
			t.Fatalf("got (%+v, %v), want CoverID 2", best, ok)
		}
	})
}

func TestCoverGenerationFromPhysics2DAndGrid(t *testing.T) {
	// Two touching boxes side-by-side: interior shared edge must NOT emit cover points inside the neighbor.
	boxes := []physics.Box{
		{X: 0, Y: 0, W: 64, H: 32},
		{X: 64, Y: 0, W: 64, H: 32},
	}
	pts := ai.GenerateCoverFromBoxes(boxes, ai.ColliderCoverOptions{
		Spacing:  32,
		Standoff: 8,
	})
	if len(pts) == 0 {
		t.Fatal("expected cover points from boxes")
	}
	for _, cp := range pts {
		for i, b := range boxes {
			if b.Contains(cp.Pos.X, cp.Pos.Y) {
				t.Fatalf("cover point %+v landed inside box %d (%+v)", cp, i, b)
			}
		}
		if math.Abs(float64(cp.Facing.Len()-1)) > 1e-4 {
			t.Fatalf("non-unit facing %v on cover %d", cp.Facing, cp.ID)
		}
	}

	// Physics World with box + circular pillar + solid tile grid.
	world := &physics.World{
		Tiles: physics.NewGrid(6, 6, 16, 16),
	}
	world.Tiles.(*physics.Grid).Set(5, 5, physics.TileSolid)
	crate := physics.NewBoxBody(32, 32, 32, 16)
	crate.Tag = "crate"
	pillar := physics.NewCircleBody(120, 40, 12)
	pillar.Tag = "pillar"
	world.Add(crate, pillar)

	wPts := ai.GenerateCoverFromWorld(world, ai.ColliderCoverOptions{
		Spacing:  16,
		Standoff: 6,
		HeightForBody: func(b *physics.Body) ai.CoverHeight {
			if b.Tag == "crate" {
				return ai.CoverHalf
			}
			return ai.CoverFull
		},
	})
	var sawCrateHalf, sawPillarFull, sawTile bool
	for _, cp := range wPts {
		if cp.HasTag("crate") && cp.Height == ai.CoverHalf {
			sawCrateHalf = true
		}
		if cp.HasTag("pillar") && cp.Height == ai.CoverFull {
			sawPillarFull = true
		}
		if len(cp.Tags) == 0 {
			sawTile = true
		}
	}
	if !sawCrateHalf || !sawPillarFull || !sawTile {
		t.Fatalf("missing expected cover sources: crate=%v pillar=%v tile=%v (total=%d)", sawCrateHalf, sawPillarFull, sawTile, len(wPts))
	}

	// Direct GenerateCoverFromGrid.
	g := ai.NewBoolGrid(5, 5)
	g.SetWalkable(2, 2, false)
	gPts := ai.GenerateCoverFromGrid(g, math2d.V2(10, 20), 16, 16, ai.GridCoverOptions{DefaultHeight: ai.CoverHalf})
	if len(gPts) != 4 {
		t.Fatalf("single isolated blocked cell should yield 4 cardinal cover points, got %d", len(gPts))
	}
	for _, cp := range gPts {
		if cp.Height != ai.CoverHalf {
			t.Fatalf("expected CoverHalf, got %v", cp.Height)
		}
	}
}

func TestZeroAllocAfterWarmup(t *testing.T) {
	_, grid, covers, los := buildReferenceCoverLevel()
	threat := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	threat.AddSource(ai.InfluenceSource{
		Cell:     ai.Cell{Col: 10, Row: 4},
		Strength: 10,
		Radius:   5,
	})
	threat.Update(grid)

	res := ai.NewPointReservations(ai.ReservationConfig{DefaultTimeout: 2 * time.Second})
	budget := ai.NewQueryBudget(16, 16)

	qCover := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{
			&ai.CoverPointGenerator{Points: covers},
		},
		&ai.AngleTest{
			Mode:   ai.AngleCoverToTarget,
			Filter: ai.AtMost(math.Pi / 2),
			Weight: 2.0,
			Invert: true,
		},
		&ai.InfluenceTest{
			Layer:  threat,
			Weight: 1.0,
			Invert: true,
		},
		&ai.LOSTest{
			LOS:       los,
			WantClear: false,
			Purpose:   ai.TestFilterAndScore,
			Weight:    1.5,
		},
		&ai.PathDistanceTest{
			Grid:     grid,
			CellSize: 16,
			Weight:   2.0,
			Invert:   true,
		},
	)

	ctx := ai.TacticalContext{
		QuerierID:    1,
		QuerierPos:   math2d.V2(28, 72),
		TargetPos:    math2d.V2(160, 72),
		Reservations: res,
		Budget:       budget,
	}

	// Warm-up internal buffers and caller dst slice.
	var dst []ai.TacticalCandidate
	for range 3 {
		budget.Reset()
		dst = qCover.RunInto(dst, ctx)
		budget.Reset()
		_, _ = qCover.RunAndReserve(ctx, 2*time.Second)
		res.Step(16 * time.Millisecond)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		budget.Reset()
		_, ok := qCover.Best(ctx)
		if !ok {
			t.Fatal("expected best cover")
		}
	}); allocs != 0 {
		t.Fatalf("TacticalQuery.Best allocated %.2f times/op, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		budget.Reset()
		dst = qCover.RunInto(dst, ctx)
	}); allocs != 0 {
		t.Fatalf("TacticalQuery.RunInto allocated %.2f times/op, want 0", allocs)
	}

	if allocs := testing.AllocsPerRun(50, func() {
		budget.Reset()
		_, ok := qCover.RunAndReserve(ctx, 2*time.Second)
		if !ok {
			t.Fatal("expected RunAndReserve success")
		}
		res.Step(16 * time.Millisecond)
	}); allocs != 0 {
		t.Fatalf("TacticalQuery.RunAndReserve + Reservations.Step allocated %.2f times/op, want 0", allocs)
	}

	// NavMesh generator + NavMesh PathDistanceTest must also be zero-alloc after warm-up.
	mesh := ai.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
		{{X: 100, Y: 20}, {X: 200, Y: 20}, {X: 200, Y: 80}, {X: 100, Y: 80}},
	})
	qNav := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{
			&ai.NavMeshGenerator{Mesh: mesh, Spacing: 25},
		},
		&ai.PathDistanceTest{
			NavMesh: mesh,
			Weight:  1.0,
			Invert:  true,
		},
	)
	navCtx := ai.TacticalContext{QuerierPos: math2d.V2(20, 50)}
	for range 3 {
		_, _ = qNav.Best(navCtx)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_, ok := qNav.Best(navCtx)
		if !ok {
			t.Fatal("expected navmesh best")
		}
	}); allocs != 0 {
		t.Fatalf("NavMesh TacticalQuery.Best allocated %.2f times/op, want 0", allocs)
	}
}

func BenchmarkTacticalQueryCover(b *testing.B) {
	_, grid, covers, los := buildReferenceCoverLevel()
	threat := ai.NewInfluenceLayerForGrid(grid, math2d.Zero(), 16)
	threat.AddSource(ai.InfluenceSource{Cell: ai.Cell{Col: 10, Row: 4}, Strength: 10, Radius: 5})
	threat.Update(grid)
	budget := ai.NewQueryBudget(16, 8)
	res := ai.NewPointReservations(ai.ReservationConfig{DefaultTimeout: 2 * time.Second})

	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{&ai.CoverPointGenerator{Points: covers}},
		&ai.AngleTest{Mode: ai.AngleCoverToTarget, Filter: ai.AtMost(math.Pi / 2), Weight: 2.0, Invert: true},
		&ai.InfluenceTest{Layer: threat, Weight: 1.0, Invert: true},
		&ai.LOSTest{LOS: los, WantClear: false, Purpose: ai.TestFilterAndScore, Weight: 1.5},
		&ai.PathDistanceTest{Grid: grid, CellSize: 16, Weight: 2.0, Invert: true},
	)
	ctx := ai.TacticalContext{
		QuerierID:    1,
		QuerierPos:   math2d.V2(28, 72),
		TargetPos:    math2d.V2(160, 72),
		Reservations: res,
		Budget:       budget,
	}

	// Warm-up.
	_, _ = q.Best(ctx)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		budget.Reset()
		_, _ = q.Best(ctx)
	}
}
