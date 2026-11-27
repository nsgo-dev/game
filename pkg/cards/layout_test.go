package cards_test

import (
	"math"
	"testing"

	"github.com/nsgo-dev/game/pkg/cards"
	"github.com/nsgo-dev/game/pkg/math2d"
)

var cardSize = math2d.Vec2{X: 60, Y: 80}

func near32(a, b float32) bool { return math.Abs(float64(a-b)) <= 1e-4 }

func nearVec(a, b math2d.Vec2) bool { return near32(a.X, b.X) && near32(a.Y, b.Y) }

func TestFanCentersSpacesAndTilts(t *testing.T) {
	area := math2d.Rect{X: 0, Y: 300, W: 400, H: 100}
	f := cards.Fan{Spacing: 40, Arc: 30, Sag: 10}

	if got := f.Arrange(area, cardSize, 0, nil); len(got) != 0 {
		t.Fatalf("empty fan = %v, want no poses", got)
	}
	one := f.Arrange(area, cardSize, 1, nil)
	if len(one) != 1 || !nearVec(one[0].Pos, area.Center()) || one[0].Rot != 0 {
		t.Fatalf("single card = %+v, want upright at the center %v", one, area.Center())
	}

	three := f.Arrange(area, cardSize, 3, nil)
	if len(three) != 3 {
		t.Fatalf("got %d poses, want 3", len(three))
	}
	for i, wantX := range []float32{160, 200, 240} {
		if !near32(three[i].Pos.X, wantX) {
			t.Fatalf("card %d x = %g, want %g", i, three[i].Pos.X, wantX)
		}
	}
	// Symmetric: the ends tilt and drop alike in opposite directions, the middle stays upright.
	if three[1].Rot != 0 || !near32(three[0].Rot, -three[2].Rot) || three[2].Rot <= 0 {
		t.Fatalf("rotations = %g %g %g, want -r 0 r with r > 0", three[0].Rot, three[1].Rot, three[2].Rot)
	}
	if !near32(three[0].Pos.Y, three[2].Pos.Y) || three[0].Pos.Y <= three[1].Pos.Y {
		t.Fatalf("ys = %g %g %g, want ends below the middle", three[0].Pos.Y, three[1].Pos.Y, three[2].Pos.Y)
	}
}

func TestFanCompressesToFitAndReachesArcAtFullWidth(t *testing.T) {
	area := math2d.Rect{X: 0, Y: 0, W: 300, H: 100}
	f := cards.Fan{Spacing: 100, Arc: 40, Sag: 12}
	poses := f.Arrange(area, cardSize, 9, nil)
	// Half the room for centers is (300-60)/2 = 120, so the row spans 30..270.
	if !near32(poses[0].Pos.X, 30) || !near32(poses[8].Pos.X, 270) {
		t.Fatalf("ends at %g and %g, want 30 and 270", poses[0].Pos.X, poses[8].Pos.X)
	}
	wantRot := float32(20 * math.Pi / 180)
	if !near32(poses[8].Rot, wantRot) || !near32(poses[0].Rot, -wantRot) {
		t.Fatalf("end rotations %g %g, want ±%g", poses[0].Rot, poses[8].Rot, wantRot)
	}
	if !near32(poses[0].Pos.Y-poses[4].Pos.Y, 12) {
		t.Fatalf("end sag = %g, want 12", poses[0].Pos.Y-poses[4].Pos.Y)
	}
	// A slot narrower than a card keeps every card at its center.
	narrow := f.Arrange(math2d.Rect{W: 40, H: 100}, cardSize, 3, nil)
	for _, p := range narrow {
		if !near32(p.Pos.X, 20) || p.Rot != 0 {
			t.Fatalf("narrow pose %+v, want x 20 upright", p)
		}
	}
}

func TestFanInsertIndexFollowsX(t *testing.T) {
	area := math2d.Rect{X: 0, Y: 300, W: 400, H: 100}
	f := cards.Fan{Spacing: 40}
	// With a fourth card the places are at 140, 180, 220, 260.
	cases := []struct {
		x    float32
		want int
	}{{-500, 0}, {139, 0}, {170, 1}, {205, 2}, {255, 3}, {900, 3}}
	for _, c := range cases {
		if got := f.InsertIndex(area, cardSize, 3, math2d.Vec2{X: c.x, Y: 350}); got != c.want {
			t.Errorf("x=%g: index %d, want %d", c.x, got, c.want)
		}
	}
	if got := f.InsertIndex(area, cardSize, 0, math2d.Vec2{}); got != 0 {
		t.Fatalf("empty fan index = %d, want 0", got)
	}
	if got := f.InsertIndex(math2d.Rect{W: 10, H: 10}, cardSize, 2, math2d.Vec2{}); got != 2 {
		t.Fatalf("no-room fan index = %d, want the end", got)
	}
}

func TestStackOffsetsAndDepth(t *testing.T) {
	area := math2d.Rect{X: 10, Y: 20, W: 60, H: 80}
	deck := cards.Stack{Offset: math2d.Vec2{X: 0, Y: -1}, Depth: 3}
	poses := deck.Arrange(area, cardSize, 5, nil)
	want := []float32{60, 59, 58, 58, 58}
	for i, p := range poses {
		if !near32(p.Pos.X, 40) || !near32(p.Pos.Y, want[i]) || p.Rot != 0 {
			t.Fatalf("card %d at %+v, want (40, %g)", i, p, want[i])
		}
	}
	pile := cards.Stack{Offset: math2d.Vec2{Y: 24}}
	if got := pile.Arrange(area, cardSize, 4, nil); !near32(got[3].Pos.Y, 60+72) {
		t.Fatalf("pile top at %g, want %g", got[3].Pos.Y, 60+72.0)
	}
	if got := deck.InsertIndex(area, cardSize, 7, math2d.Vec2{}); got != 7 {
		t.Fatalf("stack insert = %d, want 7 (on top)", got)
	}
}

func TestGridRowsColumnsAndInsertIndex(t *testing.T) {
	area := math2d.Rect{X: 0, Y: 0, W: 200, H: 200}
	g := cards.Grid{Gap: math2d.Vec2{X: 10, Y: 5}}
	// (200+10)/(60+10) = 3 columns.
	poses := g.Arrange(area, cardSize, 4, nil)
	want := []math2d.Vec2{{X: 30, Y: 40}, {X: 100, Y: 40}, {X: 170, Y: 40}, {X: 30, Y: 125}}
	for i := range want {
		if !nearVec(poses[i].Pos, want[i]) {
			t.Fatalf("cell %d at %v, want %v", i, poses[i].Pos, want[i])
		}
	}
	two := cards.Grid{Columns: 2}.Arrange(area, cardSize, 3, nil)
	if !nearVec(two[2].Pos, math2d.Vec2{X: 30, Y: 120}) {
		t.Fatalf("two-column third card at %v, want (30, 120)", two[2].Pos)
	}
	if got := g.InsertIndex(area, cardSize, 4, math2d.Vec2{X: 105, Y: 50}); got != 1 {
		t.Fatalf("insert at the second cell = %d, want 1", got)
	}
	if got := g.InsertIndex(area, cardSize, 4, math2d.Vec2{X: 190, Y: 190}); got != 4 {
		t.Fatalf("insert past the cards = %d, want 4", got)
	}
	if got := (cards.Grid{}).Arrange(math2d.Rect{W: 10, H: 10}, cardSize, 2, nil); !nearVec(got[1].Pos, math2d.Vec2{X: 30, Y: 120}) {
		t.Fatalf("narrow grid keeps one column, got %v", got[1].Pos)
	}
}

func TestGridInsertIndexIgnoresNonFinitePoints(t *testing.T) {
	area := math2d.Rect{X: 0, Y: 0, W: 200, H: 200}
	g := cards.Grid{Gap: math2d.Vec2{X: 10, Y: 5}}
	nan := float32(math.NaN())
	inf := float32(math.Inf(1))
	cases := []struct {
		p    math2d.Vec2
		want int
	}{
		{math2d.Vec2{X: nan, Y: nan}, 0},
		{math2d.Vec2{X: inf, Y: 0}, 2},
		{math2d.Vec2{X: -inf, Y: 0}, 0},
		{math2d.Vec2{X: 0, Y: inf}, 4},
		{math2d.Vec2{X: 1e30, Y: 1e30}, 4},
		{math2d.Vec2{X: -1e30, Y: -1e30}, 0},
	}
	for _, c := range cases {
		if got := g.InsertIndex(area, cardSize, 4, c.p); got != c.want {
			t.Errorf("InsertIndex at %v = %d, want %d", c.p, got, c.want)
		}
	}
	// A huge or infinite slot width caps the column count instead of overflowing it.
	for _, w := range []float32{inf, 1e30} {
		wide := math2d.Rect{W: w, H: 200}
		poses := g.Arrange(wide, cardSize, 3, nil)
		if poses[2].Pos.Y != poses[0].Pos.Y {
			t.Errorf("width %g wrapped a row: %v", w, poses)
		}
		if got := g.InsertIndex(wide, cardSize, 3, math2d.Vec2{X: 0, Y: 1e30}); got != 3 {
			t.Errorf("width %g: insert below = %d, want 3", w, got)
		}
	}
	// A huge explicit column count keeps every card on the first row.
	if got := (cards.Grid{Columns: math.MaxInt}).InsertIndex(area, cardSize, 4, math2d.Vec2{X: 0, Y: 150}); got != 4 {
		t.Errorf("huge column count: insert on the second row = %d, want 4", got)
	}
}

func TestPoseLerp(t *testing.T) {
	a := cards.Pose{Pos: math2d.Vec2{X: 0, Y: 10}, Rot: -1}
	b := cards.Pose{Pos: math2d.Vec2{X: 10, Y: 30}, Rot: 1}
	if got := a.Lerp(b, 0.5); !nearVec(got.Pos, math2d.Vec2{X: 5, Y: 20}) || got.Rot != 0 {
		t.Fatalf("midpoint = %+v", got)
	}
}

func BenchmarkFanArrange(b *testing.B) {
	area := math2d.Rect{X: 0, Y: 0, W: 800, H: 120}
	f := cards.Fan{Arc: 24, Sag: 14}
	dst := make([]cards.Pose, 0, 12)
	b.ReportAllocs()
	for b.Loop() {
		dst = f.Arrange(area, cardSize, 12, dst[:0])
	}
}
