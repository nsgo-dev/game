package navbake_test

import (
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/navbake"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// winding returns the winding number of polygon p around q.
func winding(p []core.Point, q [2]float64) int {
	w := 0
	for i := range p {
		a, b := vec(p[i]), vec(p[(i+1)%len(p)])
		if a[1] <= q[1] {
			if b[1] > q[1] && cross(a, b, q) > 0 {
				w++
			}
		} else if b[1] <= q[1] && cross(a, b, q) < 0 {
			w--
		}
	}
	return w
}

func inside(group [][]core.Point, q [2]float64) bool {
	w := 0
	for _, p := range group {
		w += winding(p, q)
	}
	return w != 0
}

// onEdge reports whether q lies within tol of an edge of src, where the bake's grid decides which side q is on: a zero-area spike of an obstacle has edges that block nothing, and a sliver thinner than the grid step vanishes when snapped.
func onEdge(src navbake.Source, q [2]float64, tol float64) bool {
	for _, e := range edges(src) {
		if pointSegDist(q, e.a, e.b) < tol {
			return true
		}
	}
	return false
}

// FuzzBake bakes random rooms of overlapping, self-touching and out-of-bounds obstacles and checks that every polygon is convex, lies in the walkable area and keeps the radius from every edge.
func FuzzBake(f *testing.F) {
	f.Add([]byte{40, 30, 3, 10, 10, 20, 10, 15, 20})
	f.Add([]byte{64, 64, 8, 0, 0, 32, 32, 0, 32, 32, 0, 16, 16, 48, 16, 48, 48, 16, 48})
	f.Add([]byte{20, 20, 1, 5, 5, 15, 5, 15, 15, 5, 15, 10, 10, 10, 10, 10, 10})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 || len(data) > 200 {
			return
		}
		w, h := float32(8+data[0]%57), float32(8+data[1]%57)
		radius := float32(data[2]%8) / 2
		opts := navbake.Options{}
		if data[2]&0x80 != 0 {
			opts.Precision = 0.25
		}
		src := navbake.Source{Outlines: [][]core.Point{rect(0, 0, w, h)}}
		rest := data[3:]
		for len(rest) >= 6 {
			n := 3 + int(rest[0]%4)
			if len(rest) < 1+2*n {
				break
			}
			var o []core.Point
			for k := range n {
				o = append(o, core.Point{X: core.Points(rest[1+2*k]%80) - 8, Y: core.Points(rest[2+2*k]%80) - 8})
			}
			src.Obstacles = append(src.Obstacles, o)
			rest = rest[1+2*n:]
		}
		m, err := navbake.Bake(src, navbake.Agent{Radius: radius}, opts)
		if err != nil {
			if strings.Contains(err.Error(), "internal") {
				t.Fatal(err)
			}
			return
		}
		if err := m.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := navbake.Parse(encode(t, m)); err != nil {
			t.Fatal(err)
		}
		// A coarser grid rounds the inflated edges by up to its step.
		tol := clearanceTol
		step := navbake.DefaultPrecision
		if opts.Precision != 0 {
			tol = float64(opts.Precision)
			step = float64(opts.Precision)
		}
		// Snapping crossings to the grid bends edges by under a step, so a point this close to an edge may land on either side.
		band := 4 * step
		for i, p := range m.Polygons {
			var c [2]float64
			for _, v := range p {
				c[0] += float64(v.X) / float64(len(p))
				c[1] += float64(v.Y) / float64(len(p))
			}
			if !onEdge(src, c, band) && (!inside(src.Outlines, c) || inside(src.Obstacles, c)) {
				t.Fatalf("polygon %d %v has its centroid outside the walkable area", i, p)
			}
			// The disc of the radius around the centroid lies in the walkable area; it is sampled, since edges of zero-area obstacles cancel and bound nothing.
			for _, rr := range []float64{float64(radius) / 2, float64(radius) - tol} {
				for k := range 32 {
					a := float64(k) * math.Pi / 16
					q := [2]float64{c[0] + rr*math.Cos(a), c[1] + rr*math.Sin(a)}
					if rr > 0 && !onEdge(src, q, band) && (!inside(src.Outlines, q) || inside(src.Obstacles, q)) {
						t.Fatalf("polygon %d centroid %v is within %v of the blocked point %v, want at least %v", i, c, rr, q, radius)
					}
				}
			}
		}
	})
}

// BenchmarkBakeTilemap bakes a 64 by 64 tile dungeon of scattered pillars and walls for an agent a quarter of a tile wide.
func BenchmarkBakeTilemap(b *testing.B) {
	tm := tilemap.New(64, 64, 16, 16)
	for row := range 64 {
		for col := range 64 {
			if row == 0 || col == 0 || row == 63 || col == 63 || (row*7+col*13)%11 == 0 || (row%8 == 4 && col%16 < 10) {
				tm.SetCellFlags(col, row, tilemap.TileSolid)
			}
		}
	}
	src, err := navbake.FromTilemap(tm, tilemap.TileSolid)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := navbake.Bake(src, navbake.Agent{Radius: 4}, navbake.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBakeRoom bakes the concave room with holes for three agent types.
func BenchmarkBakeRoom(b *testing.B) {
	src := concaveRoom()
	agents := []navbake.Agent{{Name: "small", Radius: 2}, {Name: "medium", Radius: 4}, {Name: "large", Radius: 8}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := navbake.BakeAll(src, agents, navbake.Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
