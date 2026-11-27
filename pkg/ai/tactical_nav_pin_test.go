package ai_test

import (
	"math"
	"runtime"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai"
	"github.com/nsgo-dev/game/pkg/math2d"
	"github.com/nsgo-dev/nsgo/core"
)

// These tests pin the exact output of NavMeshGenerator and of PathDistanceTest's NavMesh branch, bit for bit, so that moving the navigation code into its own package (and the sampling and path-length primitives behind a new API) is provably behavior-preserving. The expectations were recorded from the implementation in pkg/ai/tactical.go before that move, on darwin/arm64 and linux/amd64.

// navPinMeshU is the U-shaped corridor from TestGeneratorsAndTests: left room, top bridge, right room.
func navPinMeshU() *ai.NavMesh {
	return ai.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 60, Y: 0}, {X: 60, Y: 100}, {X: 0, Y: 100}},
		{{X: 60, Y: 0}, {X: 180, Y: 0}, {X: 180, Y: 30}, {X: 60, Y: 30}},
		{{X: 180, Y: 0}, {X: 240, Y: 0}, {X: 240, Y: 100}, {X: 180, Y: 100}},
	})
}

// navPinMeshRooms is the two-room mesh from TestZeroAllocAfterWarmup.
func navPinMeshRooms() *ai.NavMesh {
	return ai.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}},
		{{X: 100, Y: 20}, {X: 200, Y: 20}, {X: 200, Y: 80}, {X: 100, Y: 80}},
	})
}

// navPinMeshSkew has fractional, non-axis-aligned vertices so centroids and containment tests exercise rounding.
func navPinMeshSkew() *ai.NavMesh {
	return ai.BuildNavMesh([][]core.Point{
		{{X: 0, Y: 0}, {X: 37.5, Y: -5}, {X: 52.25, Y: 41}, {X: 3, Y: 30}},
		{{X: 37.5, Y: -5}, {X: 90, Y: 10}, {X: 52.25, Y: 41}},
	})
}

type navPinGenCase struct {
	name string
	gen  ai.NavMeshGenerator
	ctx  ai.TacticalContext
	want []math2d.Vec2
}

type navPinPathCase struct {
	name string
	gen  ai.NavMeshGenerator
	ctx  ai.TacticalContext
	want []navPinCost
}

// navPinCost is one candidate's expected path cost. Cost was recorded on arm64. Path lengths go through math.Hypot, which is assembly on amd64 and 386 and pure Go elsewhere (where the compiler may fuse its multiply-add), so a few lengths differ by one ulp on amd64; AMD64 holds that value when it differs and is zero otherwise. CI runs only arm64 and amd64, so 386 is not pinned.
type navPinCost struct {
	Pos   math2d.Vec2
	Cost  float64
	AMD64 float64
}

func (c navPinCost) want() float64 {
	if runtime.GOARCH == "amd64" && c.AMD64 != 0 {
		return c.AMD64
	}
	return c.Cost
}

// navPinGenerate returns NavMeshGenerator's candidates in generation order (CustomTest sees them before the final sort).
func navPinGenerate(gen *ai.NavMeshGenerator, ctx ai.TacticalContext) []math2d.Vec2 {
	var got []math2d.Vec2
	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{gen},
		&ai.CustomTest{Eval: func(_ ai.TacticalContext, c ai.TacticalCandidate) (float64, bool) {
			if c.CoverIndex != -1 || c.CoverID != 0 || c.Height != 0 || c.Elevation != 0 || c.Facing != (math2d.Vec2{}) {
				panic("NavMeshGenerator emitted a candidate with non-default fields")
			}
			got = append(got, c.Pos)
			return 0, true
		}},
	)
	q.Run(ctx)
	return got
}

// navPinPathCosts returns each surviving candidate's NavMesh path cost in generation order. Fixed normalization over [0, 1024] with weight 1024 makes Score equal the raw cost exactly, since dividing and multiplying by a power of two is exact.
func navPinPathCosts(gen *ai.NavMeshGenerator, ctx ai.TacticalContext) []navPinCost {
	var got []navPinCost
	q := ai.NewTacticalQuery(
		[]ai.CandidateGenerator{gen},
		&ai.PathDistanceTest{NavMesh: gen.Mesh, NormMin: 0, NormMax: 1024, Weight: 1024},
		&ai.CustomTest{Eval: func(_ ai.TacticalContext, c ai.TacticalCandidate) (float64, bool) {
			got = append(got, navPinCost{Pos: c.Pos, Cost: c.Score})
			return 0, true
		}},
	)
	q.Run(ctx)
	return got
}

func navPinSameVec2(a, b math2d.Vec2) bool {
	return math.Float32bits(a.X) == math.Float32bits(b.X) && math.Float32bits(a.Y) == math.Float32bits(b.Y)
}

func TestNavPinGeneratorCandidates(t *testing.T) {
	for _, tc := range navPinGenCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := navPinGenerate(&tc.gen, tc.ctx)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d candidates, want %d: %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if !navPinSameVec2(got[i], tc.want[i]) {
					t.Fatalf("candidate %d = %v (bits %08x %08x), want %v (bits %08x %08x)", i, got[i], math.Float32bits(got[i].X), math.Float32bits(got[i].Y), tc.want[i], math.Float32bits(tc.want[i].X), math.Float32bits(tc.want[i].Y))
				}
			}
		})
	}
}

func TestNavPinPathDistanceCosts(t *testing.T) {
	for _, tc := range navPinPathCases() {
		t.Run(tc.name, func(t *testing.T) {
			got := navPinPathCosts(&tc.gen, tc.ctx)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d costs, want %d: %v", len(got), len(tc.want), got)
			}
			for i := range got {
				if !navPinSameVec2(got[i].Pos, tc.want[i].Pos) {
					t.Fatalf("candidate %d = %v, want %v", i, got[i].Pos, tc.want[i].Pos)
				}
				if want := tc.want[i].want(); math.Float64bits(got[i].Cost) != math.Float64bits(want) {
					t.Fatalf("cost at %v = %v (bits %016x), want %v (bits %016x)", got[i].Pos, got[i].Cost, math.Float64bits(got[i].Cost), want, math.Float64bits(want))
				}
			}
		})
	}
}

func navPinGenCases() []navPinGenCase {
	return []navPinGenCase{
		{name: "U centroids", gen: ai.NavMeshGenerator{Mesh: navPinMeshU()}, want: []math2d.Vec2{
			{X: 30, Y: 50},
			{X: 120, Y: 15},
			{X: 210, Y: 50},
		}},
		{name: "U centroids MaxRadius", gen: ai.NavMeshGenerator{Mesh: navPinMeshU(), MaxRadius: 70}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(30, 80)}, want: []math2d.Vec2{
			{X: 30, Y: 50},
		}},
		{name: "U spacing 20", gen: ai.NavMeshGenerator{Mesh: navPinMeshU(), Spacing: 20}, want: []math2d.Vec2{
			{X: 0, Y: 0},
			{X: 20, Y: 0},
			{X: 40, Y: 0},
			{X: 60, Y: 0},
			{X: 80, Y: 0},
			{X: 100, Y: 0},
			{X: 120, Y: 0},
			{X: 140, Y: 0},
			{X: 160, Y: 0},
			{X: 180, Y: 0},
			{X: 200, Y: 0},
			{X: 220, Y: 0},
			{X: 240, Y: 0},
			{X: 0, Y: 20},
			{X: 20, Y: 20},
			{X: 40, Y: 20},
			{X: 60, Y: 20},
			{X: 80, Y: 20},
			{X: 100, Y: 20},
			{X: 120, Y: 20},
			{X: 140, Y: 20},
			{X: 160, Y: 20},
			{X: 180, Y: 20},
			{X: 200, Y: 20},
			{X: 220, Y: 20},
			{X: 240, Y: 20},
			{X: 0, Y: 40},
			{X: 20, Y: 40},
			{X: 40, Y: 40},
			{X: 60, Y: 40},
			{X: 180, Y: 40},
			{X: 200, Y: 40},
			{X: 220, Y: 40},
			{X: 240, Y: 40},
			{X: 0, Y: 60},
			{X: 20, Y: 60},
			{X: 40, Y: 60},
			{X: 60, Y: 60},
			{X: 180, Y: 60},
			{X: 200, Y: 60},
			{X: 220, Y: 60},
			{X: 240, Y: 60},
			{X: 0, Y: 80},
			{X: 20, Y: 80},
			{X: 40, Y: 80},
			{X: 60, Y: 80},
			{X: 180, Y: 80},
			{X: 200, Y: 80},
			{X: 220, Y: 80},
			{X: 240, Y: 80},
			{X: 0, Y: 100},
			{X: 20, Y: 100},
			{X: 40, Y: 100},
			{X: 60, Y: 100},
			{X: 180, Y: 100},
			{X: 200, Y: 100},
			{X: 220, Y: 100},
			{X: 240, Y: 100},
		}},
		{name: "U spacing 20 MaxRadius custom center", gen: ai.NavMeshGenerator{Mesh: navPinMeshU(), Spacing: 20, MaxRadius: 50, Center: ai.OriginCustom, CustomCenter: math2d.V2(120, 15)}, want: []math2d.Vec2{
			{X: 80, Y: 0},
			{X: 100, Y: 0},
			{X: 120, Y: 0},
			{X: 140, Y: 0},
			{X: 160, Y: 0},
			{X: 80, Y: 20},
			{X: 100, Y: 20},
			{X: 120, Y: 20},
			{X: 140, Y: 20},
			{X: 160, Y: 20},
		}},
		{name: "rooms spacing 25", gen: ai.NavMeshGenerator{Mesh: navPinMeshRooms(), Spacing: 25}, want: []math2d.Vec2{
			{X: 0, Y: 0},
			{X: 25, Y: 0},
			{X: 50, Y: 0},
			{X: 75, Y: 0},
			{X: 100, Y: 0},
			{X: 0, Y: 25},
			{X: 25, Y: 25},
			{X: 50, Y: 25},
			{X: 75, Y: 25},
			{X: 100, Y: 25},
			{X: 125, Y: 25},
			{X: 150, Y: 25},
			{X: 175, Y: 25},
			{X: 200, Y: 25},
			{X: 0, Y: 50},
			{X: 25, Y: 50},
			{X: 50, Y: 50},
			{X: 75, Y: 50},
			{X: 100, Y: 50},
			{X: 125, Y: 50},
			{X: 150, Y: 50},
			{X: 175, Y: 50},
			{X: 200, Y: 50},
			{X: 0, Y: 75},
			{X: 25, Y: 75},
			{X: 50, Y: 75},
			{X: 75, Y: 75},
			{X: 100, Y: 75},
			{X: 125, Y: 75},
			{X: 150, Y: 75},
			{X: 175, Y: 75},
			{X: 200, Y: 75},
			{X: 0, Y: 100},
			{X: 25, Y: 100},
			{X: 50, Y: 100},
			{X: 75, Y: 100},
			{X: 100, Y: 100},
		}},
		{name: "rooms spacing 25 MaxRadius target center", gen: ai.NavMeshGenerator{Mesh: navPinMeshRooms(), Spacing: 25, MaxRadius: 60, Center: ai.OriginTarget}, ctx: ai.TacticalContext{TargetPos: math2d.V2(110, 50)}, want: []math2d.Vec2{
			{X: 100, Y: 0},
			{X: 75, Y: 25},
			{X: 100, Y: 25},
			{X: 125, Y: 25},
			{X: 150, Y: 25},
			{X: 50, Y: 50},
			{X: 75, Y: 50},
			{X: 100, Y: 50},
			{X: 125, Y: 50},
			{X: 150, Y: 50},
			{X: 75, Y: 75},
			{X: 100, Y: 75},
			{X: 125, Y: 75},
			{X: 150, Y: 75},
			{X: 100, Y: 100},
		}},
		{name: "skew centroids", gen: ai.NavMeshGenerator{Mesh: navPinMeshSkew()}, want: []math2d.Vec2{
			{X: 23.1875, Y: 16.5},
			{X: 59.916668, Y: 15.333333},
		}},
		{name: "skew centroids MaxRadius", gen: ai.NavMeshGenerator{Mesh: navPinMeshSkew(), MaxRadius: 25}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(20, 12)}, want: []math2d.Vec2{
			{X: 23.1875, Y: 16.5},
		}},
		{name: "skew spacing 6.1", gen: ai.NavMeshGenerator{Mesh: navPinMeshSkew(), Spacing: 6.1}, want: []math2d.Vec2{
			{X: 0, Y: 0},
			{X: 6.1, Y: 0},
			{X: 12.2, Y: 0},
			{X: 18.3, Y: 0},
			{X: 24.4, Y: 0},
			{X: 30.5, Y: 0},
			{X: 36.6, Y: 0},
			{X: 42.7, Y: 0},
			{X: 48.8, Y: 0},
			{X: 54.899998, Y: 0},
			{X: 6.1, Y: 6.1},
			{X: 12.2, Y: 6.1},
			{X: 18.3, Y: 6.1},
			{X: 24.4, Y: 6.1},
			{X: 30.5, Y: 6.1},
			{X: 36.6, Y: 6.1},
			{X: 42.7, Y: 6.1},
			{X: 48.8, Y: 6.1},
			{X: 54.899998, Y: 6.1},
			{X: 61, Y: 6.1},
			{X: 67.1, Y: 6.1},
			{X: 73.2, Y: 6.1},
			{X: 6.1, Y: 12.2},
			{X: 12.2, Y: 12.2},
			{X: 18.3, Y: 12.2},
			{X: 24.4, Y: 12.2},
			{X: 30.5, Y: 12.2},
			{X: 36.6, Y: 12.2},
			{X: 42.7, Y: 12.2},
			{X: 48.8, Y: 12.2},
			{X: 54.899998, Y: 12.2},
			{X: 61, Y: 12.2},
			{X: 67.1, Y: 12.2},
			{X: 73.2, Y: 12.2},
			{X: 79.299995, Y: 12.2},
			{X: 85.4, Y: 12.2},
			{X: 6.1, Y: 18.3},
			{X: 12.2, Y: 18.3},
			{X: 18.3, Y: 18.3},
			{X: 24.4, Y: 18.3},
			{X: 30.5, Y: 18.3},
			{X: 36.6, Y: 18.3},
			{X: 42.7, Y: 18.3},
			{X: 48.8, Y: 18.3},
			{X: 54.899998, Y: 18.3},
			{X: 61, Y: 18.3},
			{X: 67.1, Y: 18.3},
			{X: 73.2, Y: 18.3},
			{X: 79.299995, Y: 18.3},
			{X: 6.1, Y: 24.4},
			{X: 12.2, Y: 24.4},
			{X: 18.3, Y: 24.4},
			{X: 24.4, Y: 24.4},
			{X: 30.5, Y: 24.4},
			{X: 36.6, Y: 24.4},
			{X: 42.7, Y: 24.4},
			{X: 48.8, Y: 24.4},
			{X: 54.899998, Y: 24.4},
			{X: 61, Y: 24.4},
			{X: 67.1, Y: 24.4},
			{X: 6.1, Y: 30.5},
			{X: 12.2, Y: 30.5},
			{X: 18.3, Y: 30.5},
			{X: 24.4, Y: 30.5},
			{X: 30.5, Y: 30.5},
			{X: 36.6, Y: 30.5},
			{X: 42.7, Y: 30.5},
			{X: 48.8, Y: 30.5},
			{X: 54.899998, Y: 30.5},
			{X: 61, Y: 30.5},
			{X: 36.6, Y: 36.6},
			{X: 42.7, Y: 36.6},
			{X: 48.8, Y: 36.6},
			{X: 54.899998, Y: 36.6},
		}},
		{name: "skew spacing 7.5 MaxRadius", gen: ai.NavMeshGenerator{Mesh: navPinMeshSkew(), Spacing: 7.5, MaxRadius: 25.3}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(40, 15)}, want: []math2d.Vec2{
			{X: 22.5, Y: 0},
			{X: 30, Y: 0},
			{X: 37.5, Y: 0},
			{X: 45, Y: 0},
			{X: 52.5, Y: 0},
			{X: 22.5, Y: 7.5},
			{X: 30, Y: 7.5},
			{X: 37.5, Y: 7.5},
			{X: 45, Y: 7.5},
			{X: 52.5, Y: 7.5},
			{X: 60, Y: 7.5},
			{X: 15, Y: 15},
			{X: 22.5, Y: 15},
			{X: 30, Y: 15},
			{X: 37.5, Y: 15},
			{X: 45, Y: 15},
			{X: 52.5, Y: 15},
			{X: 60, Y: 15},
			{X: 22.5, Y: 22.5},
			{X: 30, Y: 22.5},
			{X: 37.5, Y: 22.5},
			{X: 45, Y: 22.5},
			{X: 52.5, Y: 22.5},
			{X: 60, Y: 22.5},
			{X: 22.5, Y: 30},
			{X: 30, Y: 30},
			{X: 37.5, Y: 30},
			{X: 45, Y: 30},
			{X: 52.5, Y: 30},
			{X: 60, Y: 30},
			{X: 37.5, Y: 37.5},
			{X: 45, Y: 37.5},
		}},
	}
}

func navPinPathCases() []navPinPathCase {
	return []navPinPathCase{
		{name: "U spacing 20 from left room", gen: ai.NavMeshGenerator{Mesh: navPinMeshU(), Spacing: 20}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(30, 80)}, want: []navPinCost{
			{Pos: math2d.Vec2{X: 0, Y: 0}, Cost: 85.44003745317531},
			{Pos: math2d.Vec2{X: 20, Y: 0}, Cost: 80.62257748298549},
			{Pos: math2d.Vec2{X: 40, Y: 0}, Cost: 80.62257748298549},
			{Pos: math2d.Vec2{X: 60, Y: 0}, Cost: 85.44003745317531},
			{Pos: math2d.Vec2{X: 80, Y: 0}, Cost: 94.36503170309288},
			{Pos: math2d.Vec2{X: 100, Y: 0}, Cost: 108.30951894845299},
			{Pos: math2d.Vec2{X: 120, Y: 0}, Cost: 125.39155827344669},
			{Pos: math2d.Vec2{X: 140, Y: 0}, Cost: 143.7495564016283},
			{Pos: math2d.Vec2{X: 160, Y: 0}, Cost: 162.7125840375585},
			{Pos: math2d.Vec2{X: 180, Y: 0}, Cost: 182.00268771698282},
			{Pos: math2d.Vec2{X: 200, Y: 0}, Cost: 201.4877295812165},
			{Pos: math2d.Vec2{X: 220, Y: 0}, Cost: 221.09772490945005},
			{Pos: math2d.Vec2{X: 240, Y: 0}, Cost: 240.79239485739956},
			{Pos: math2d.Vec2{X: 0, Y: 20}, Cost: 67.0820393249937},
			{Pos: math2d.Vec2{X: 20, Y: 20}, Cost: 60.82762530298219},
			{Pos: math2d.Vec2{X: 40, Y: 20}, Cost: 60.82762530298219},
			{Pos: math2d.Vec2{X: 60, Y: 20}, Cost: 67.0820393249937},
			{Pos: math2d.Vec2{X: 80, Y: 20}, Cost: 80.6701987234509},
			{Pos: math2d.Vec2{X: 100, Y: 20}, Cost: 99.5405752046296},
			{Pos: math2d.Vec2{X: 120, Y: 20}, Cost: 119.13714425143519},
			{Pos: math2d.Vec2{X: 140, Y: 20}, Cost: 138.93209643143848},
			{Pos: math2d.Vec2{X: 160, Y: 20}, Cost: 158.8082751596619},
			{Pos: math2d.Vec2{X: 180, Y: 20}, Cost: 178.72546473637593},
			{Pos: math2d.Vec2{X: 200, Y: 20}, Cost: 198.666207424635},
			{Pos: math2d.Vec2{X: 220, Y: 20}, Cost: 218.62171436726697},
			{Pos: math2d.Vec2{X: 240, Y: 20}, Cost: 238.58708272165245},
			{Pos: math2d.Vec2{X: 0, Y: 40}, Cost: 50},
			{Pos: math2d.Vec2{X: 20, Y: 40}, Cost: 41.23105625617661},
			{Pos: math2d.Vec2{X: 40, Y: 40}, Cost: 41.23105625617661},
			{Pos: math2d.Vec2{X: 60, Y: 40}, Cost: 50},
			{Pos: math2d.Vec2{X: 180, Y: 40}, Cost: 188.309518948453},
			{Pos: math2d.Vec2{X: 200, Y: 40}, Cost: 200.67019872345088},
			{Pos: math2d.Vec2{X: 220, Y: 40}, Cost: 219.5405752046296},
			{Pos: math2d.Vec2{X: 240, Y: 40}, Cost: 239.1371442514352},
			{Pos: math2d.Vec2{X: 0, Y: 60}, Cost: 36.05551275463989},
			{Pos: math2d.Vec2{X: 20, Y: 60}, Cost: 22.360679774997898},
			{Pos: math2d.Vec2{X: 40, Y: 60}, Cost: 22.360679774997898},
			{Pos: math2d.Vec2{X: 60, Y: 60}, Cost: 36.05551275463989},
			{Pos: math2d.Vec2{X: 180, Y: 60}, Cost: 208.309518948453},
			{Pos: math2d.Vec2{X: 200, Y: 60}, Cost: 214.36503170309288},
			{Pos: math2d.Vec2{X: 220, Y: 60}, Cost: 228.309518948453},
			{Pos: math2d.Vec2{X: 240, Y: 60}, Cost: 245.3915582734467},
			{Pos: math2d.Vec2{X: 0, Y: 80}, Cost: 30},
			{Pos: math2d.Vec2{X: 20, Y: 80}, Cost: 10},
			{Pos: math2d.Vec2{X: 40, Y: 80}, Cost: 10},
			{Pos: math2d.Vec2{X: 60, Y: 80}, Cost: 30},
			{Pos: math2d.Vec2{X: 180, Y: 80}, Cost: 228.309518948453},
			{Pos: math2d.Vec2{X: 200, Y: 80}, Cost: 232.16116701979803},
			{Pos: math2d.Vec2{X: 220, Y: 80}, Cost: 242.34076132278147},
			{Pos: math2d.Vec2{X: 240, Y: 80}, Cost: 256.4120157075195, AMD64: 256.4120157075196},
			{Pos: math2d.Vec2{X: 0, Y: 100}, Cost: 36.05551275463989},
			{Pos: math2d.Vec2{X: 20, Y: 100}, Cost: 22.360679774997898},
			{Pos: math2d.Vec2{X: 40, Y: 100}, Cost: 22.360679774997898},
			{Pos: math2d.Vec2{X: 60, Y: 100}, Cost: 36.05551275463989},
			{Pos: math2d.Vec2{X: 180, Y: 100}, Cost: 248.309518948453},
			{Pos: math2d.Vec2{X: 200, Y: 100}, Cost: 251.11061784125818},
			{Pos: math2d.Vec2{X: 220, Y: 100}, Cost: 258.9320964314385},
			{Pos: math2d.Vec2{X: 240, Y: 100}, Cost: 270.50496352138185},
		}},
		{name: "U centroids from right room", gen: ai.NavMeshGenerator{Mesh: navPinMeshU()}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(230, 90)}, want: []navPinCost{
			{Pos: math2d.Vec2{X: 30, Y: 50}, Cost: 234.15800951370642, AMD64: 234.15800951370645},
			{Pos: math2d.Vec2{X: 120, Y: 15}, Cost: 139.94908114333145},
			{Pos: math2d.Vec2{X: 210, Y: 50}, Cost: 44.721359549995796},
		}},
		{name: "rooms spacing 25", gen: ai.NavMeshGenerator{Mesh: navPinMeshRooms(), Spacing: 25}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(20, 50)}, want: []navPinCost{
			{Pos: math2d.Vec2{X: 0, Y: 0}, Cost: 53.85164807134504, AMD64: 53.851648071345046},
			{Pos: math2d.Vec2{X: 25, Y: 0}, Cost: 50.24937810560445},
			{Pos: math2d.Vec2{X: 50, Y: 0}, Cost: 58.309518948453},
			{Pos: math2d.Vec2{X: 75, Y: 0}, Cost: 74.33034373659252},
			{Pos: math2d.Vec2{X: 100, Y: 0}, Cost: 94.33981132056603},
			{Pos: math2d.Vec2{X: 0, Y: 25}, Cost: 32.01562118716424},
			{Pos: math2d.Vec2{X: 25, Y: 25}, Cost: 25.495097567963924},
			{Pos: math2d.Vec2{X: 50, Y: 25}, Cost: 39.05124837953327, AMD64: 39.05124837953328},
			{Pos: math2d.Vec2{X: 75, Y: 25}, Cost: 60.415229867972855},
			{Pos: math2d.Vec2{X: 100, Y: 25}, Cost: 83.81527307120106},
			{Pos: math2d.Vec2{X: 125, Y: 25}, Cost: 107.93516572461452},
			{Pos: math2d.Vec2{X: 150, Y: 25}, Cost: 132.38202294873727},
			{Pos: math2d.Vec2{X: 175, Y: 25}, Cost: 157.0031846810758},
			{Pos: math2d.Vec2{X: 200, Y: 25}, Cost: 181.72781845386248},
			{Pos: math2d.Vec2{X: 0, Y: 50}, Cost: 20},
			{Pos: math2d.Vec2{X: 25, Y: 50}, Cost: 5},
			{Pos: math2d.Vec2{X: 50, Y: 50}, Cost: 30},
			{Pos: math2d.Vec2{X: 75, Y: 50}, Cost: 55},
			{Pos: math2d.Vec2{X: 100, Y: 50}, Cost: 80},
			{Pos: math2d.Vec2{X: 125, Y: 50}, Cost: 105},
			{Pos: math2d.Vec2{X: 150, Y: 50}, Cost: 130},
			{Pos: math2d.Vec2{X: 175, Y: 50}, Cost: 155},
			{Pos: math2d.Vec2{X: 200, Y: 50}, Cost: 180},
			{Pos: math2d.Vec2{X: 0, Y: 75}, Cost: 32.01562118716424},
			{Pos: math2d.Vec2{X: 25, Y: 75}, Cost: 25.495097567963924},
			{Pos: math2d.Vec2{X: 50, Y: 75}, Cost: 39.05124837953327, AMD64: 39.05124837953328},
			{Pos: math2d.Vec2{X: 75, Y: 75}, Cost: 60.415229867972855},
			{Pos: math2d.Vec2{X: 100, Y: 75}, Cost: 83.81527307120106},
			{Pos: math2d.Vec2{X: 125, Y: 75}, Cost: 107.93516572461452},
			{Pos: math2d.Vec2{X: 150, Y: 75}, Cost: 132.38202294873727},
			{Pos: math2d.Vec2{X: 175, Y: 75}, Cost: 157.0031846810758},
			{Pos: math2d.Vec2{X: 200, Y: 75}, Cost: 181.72781845386248},
			{Pos: math2d.Vec2{X: 0, Y: 100}, Cost: 53.85164807134504, AMD64: 53.851648071345046},
			{Pos: math2d.Vec2{X: 25, Y: 100}, Cost: 50.24937810560445},
			{Pos: math2d.Vec2{X: 50, Y: 100}, Cost: 58.309518948453},
			{Pos: math2d.Vec2{X: 75, Y: 100}, Cost: 74.33034373659252},
			{Pos: math2d.Vec2{X: 100, Y: 100}, Cost: 94.33981132056603},
		}},
		{name: "skew spacing 7.5", gen: ai.NavMeshGenerator{Mesh: navPinMeshSkew(), Spacing: 7.5}, ctx: ai.TacticalContext{QuerierPos: math2d.V2(8, 20)}, want: []navPinCost{
			{Pos: math2d.Vec2{X: 0, Y: 0}, Cost: 21.540659228538015, AMD64: 21.54065922853802},
			{Pos: math2d.Vec2{X: 7.5, Y: 0}, Cost: 20.00624902374256},
			{Pos: math2d.Vec2{X: 15, Y: 0}, Cost: 21.18962010041709},
			{Pos: math2d.Vec2{X: 22.5, Y: 0}, Cost: 24.70323865407125},
			{Pos: math2d.Vec2{X: 30, Y: 0}, Cost: 29.73213749463701},
			{Pos: math2d.Vec2{X: 37.5, Y: 0}, Cost: 35.64056677439347},
			{Pos: math2d.Vec2{X: 45, Y: 0}, Cost: 42.05948168962618},
			{Pos: math2d.Vec2{X: 52.5, Y: 0}, Cost: 48.787805853512204},
			{Pos: math2d.Vec2{X: 7.5, Y: 7.5}, Cost: 12.509996003196806},
			{Pos: math2d.Vec2{X: 15, Y: 7.5}, Cost: 14.326548781894404},
			{Pos: math2d.Vec2{X: 22.5, Y: 7.5}, Cost: 19.144189719076646},
			{Pos: math2d.Vec2{X: 30, Y: 7.5}, Cost: 25.30316185775999},
			{Pos: math2d.Vec2{X: 37.5, Y: 7.5}, Cost: 32.03903868720158},
			{Pos: math2d.Vec2{X: 45, Y: 7.5}, Cost: 39.054449170357024},
			{Pos: math2d.Vec2{X: 52.5, Y: 7.5}, Cost: 46.22228899567827},
			{Pos: math2d.Vec2{X: 60, Y: 7.5}, Cost: 53.48130514488217},
			{Pos: math2d.Vec2{X: 67.5, Y: 7.5}, Cost: 60.7988486733096},
			{Pos: math2d.Vec2{X: 75, Y: 7.5}, Cost: 68.15607089614248},
			{Pos: math2d.Vec2{X: 7.5, Y: 15}, Cost: 5.024937810560445},
			{Pos: math2d.Vec2{X: 15, Y: 15}, Cost: 8.602325267042627},
			{Pos: math2d.Vec2{X: 22.5, Y: 15}, Cost: 15.337861650177969},
			{Pos: math2d.Vec2{X: 30, Y: 15}, Cost: 22.561028345356952},
			{Pos: math2d.Vec2{X: 37.5, Y: 15}, Cost: 29.920728600754366},
			{Pos: math2d.Vec2{X: 45, Y: 15}, Cost: 37.33630940518894},
			{Pos: math2d.Vec2{X: 52.5, Y: 15}, Cost: 44.78001786511479},
			{Pos: math2d.Vec2{X: 60, Y: 15}, Cost: 52.2398315464359},
			{Pos: math2d.Vec2{X: 67.5, Y: 15}, Cost: 59.70971445250764},
			{Pos: math2d.Vec2{X: 75, Y: 15}, Cost: 67.18630812896329},
			{Pos: math2d.Vec2{X: 82.5, Y: 15}, Cost: 74.66759672039807},
			{Pos: math2d.Vec2{X: 7.5, Y: 22.5}, Cost: 2.5495097567963927},
			{Pos: math2d.Vec2{X: 15, Y: 22.5}, Cost: 7.433034373659253},
			{Pos: math2d.Vec2{X: 22.5, Y: 22.5}, Cost: 14.713938969562163},
			{Pos: math2d.Vec2{X: 30, Y: 22.5}, Cost: 22.14158982548453},
			{Pos: math2d.Vec2{X: 37.5, Y: 22.5}, Cost: 29.605742686174924},
			{Pos: math2d.Vec2{X: 45, Y: 22.5}, Cost: 37.08436328157732},
			{Pos: math2d.Vec2{X: 52.5, Y: 22.5}, Cost: 44.57016939613311},
			{Pos: math2d.Vec2{X: 60, Y: 22.5}, Cost: 52.06006146750117},
			{Pos: math2d.Vec2{X: 67.5, Y: 22.5}, Cost: 59.552497848536966},
			{Pos: math2d.Vec2{X: 7.5, Y: 30}, Cost: 10.012492197250394},
			{Pos: math2d.Vec2{X: 15, Y: 30}, Cost: 12.206555615733702},
			{Pos: math2d.Vec2{X: 22.5, Y: 30}, Cost: 17.613914953808536},
			{Pos: math2d.Vec2{X: 30, Y: 30}, Cost: 24.16609194718914},
			{Pos: math2d.Vec2{X: 37.5, Y: 30}, Cost: 31.148836254345042},
			{Pos: math2d.Vec2{X: 45, Y: 30}, Cost: 38.3275357934736},
			{Pos: math2d.Vec2{X: 52.5, Y: 30}, Cost: 45.609757727924844},
			{Pos: math2d.Vec2{X: 60, Y: 30}, Cost: 52.952809179494906},
			{Pos: math2d.Vec2{X: 37.5, Y: 37.5}, Cost: 34.30014577228499},
			{Pos: math2d.Vec2{X: 45, Y: 37.5}, Cost: 40.929817981515626},
			{Pos: math2d.Vec2{X: 52.5, Y: 37.5}, Cost: 47.81736086402093},
		}},
	}
}
