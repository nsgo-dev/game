package physics

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
)

func chainPoints(xy ...float32) []core.Point {
	out := make([]core.Point, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		out = append(out, core.Point{X: core.Points(xy[i]), Y: core.Points(xy[i+1])})
	}
	return out
}

// A body resting on a chain vertex gets one manifold for it, not one from each segment that shares the vertex; the position correction applies every manifold, so a duplicate would push the body out twice.
func TestChainSharedVertexGivesOneManifold(t *testing.T) {
	cases := []struct {
		name  string
		chain []core.Point
		loop  bool
		ball  Circle
	}{
		{"convex peak", chainPoints(0, 100, 100, 50, 200, 100), false, Circle{X: 100, Y: 43, R: 8}},
		{"flat seam", chainPoints(0, 100, 100, 100, 200, 100), false, Circle{X: 100, Y: 93, R: 8}},
		{"loop closing vertex", chainPoints(0, 0, 200, 0, 200, 200, 0, 200), true, Circle{X: 0, Y: -7, R: 8}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ground := NewStaticRigidBody(ChainShape(tc.chain, tc.loop))
			ball := NewRigidCircle(tc.ball.X, tc.ball.Y, tc.ball.R, 1)
			for _, pair := range [][2]*RigidBody{{ground, ball}, {ball, ground}} {
				got := collideRigidPair(pair[0], pair[1], nil)
				if len(got) != 1 || got[0].count != 1 {
					t.Fatalf("got %d manifolds %+v, want one with one point", len(got), got)
				}
				if d := got[0].points[0].depth; d < 0.99 || d > 1.01 {
					t.Fatalf("depth = %v, want 1", d)
				}
			}
		})
	}
}

// A box spanning a flat seam keeps a contact under each of its corners: only points two segments report alike are dropped.
func TestChainFlatSeamKeepsDistinctPoints(t *testing.T) {
	ground := NewStaticRigidBody(ChainShape(chainPoints(0, 100, 100, 100, 200, 100), false))
	box := NewRigidBox(100, 91, 20, 20, 1)
	got := collideRigidPair(ground, box, nil)
	var xs []float64
	for _, m := range got {
		for _, p := range m.points[:m.count] {
			for _, x := range xs {
				if x == p.px {
					t.Fatalf("point x=%v reported twice in %+v", x, got)
				}
			}
			xs = append(xs, p.px)
		}
	}
	corners := 0
	for _, x := range xs {
		if x == 90 || x == 110 {
			corners++
		}
	}
	if corners != 2 {
		t.Fatalf("contact points at x %v, want both corners 90 and 110", xs)
	}
}
