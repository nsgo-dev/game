package navbake_test

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/navbake"
	"github.com/nsgo-dev/game/pkg/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

var update = flag.Bool("update", false, "rewrite the golden meshes and reference paths under testdata")

// poly returns the polygon with vertices at the given x, y pairs.
func poly(xy ...float32) []core.Point {
	out := make([]core.Point, len(xy)/2)
	for i := range out {
		out[i] = core.Point{X: core.Points(xy[2*i]), Y: core.Points(xy[2*i+1])}
	}
	return out
}

func rect(x0, y0, x1, y1 float32) []core.Point { return poly(x0, y0, x1, y0, x1, y1, x0, y1) }

type segment struct{ a, b [2]float64 }

func vec(p core.Point) [2]float64 { return [2]float64{float64(p.X), float64(p.Y)} }

// edges returns every edge of the source's polygons.
func edges(src navbake.Source) []segment {
	var out []segment
	for _, group := range [][][]core.Point{src.Outlines, src.Obstacles} {
		for _, p := range group {
			for i := range p {
				out = append(out, segment{vec(p[i]), vec(p[(i+1)%len(p)])})
			}
		}
	}
	return out
}

func pointSegDist(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 0 {
		t = min(max(((p[0]-a[0])*dx+(p[1]-a[1])*dy)/l2, 0), 1)
	}
	return math.Hypot(p[0]-a[0]-t*dx, p[1]-a[1]-t*dy)
}

func cross(o, a, b [2]float64) float64 { return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0]) }

// segDist returns the distance between segments s and t.
func segDist(s, t segment) float64 {
	d1, d2 := cross(s.a, s.b, t.a), cross(s.a, s.b, t.b)
	d3, d4 := cross(t.a, t.b, s.a), cross(t.a, t.b, s.b)
	if d1*d2 < 0 && d3*d4 < 0 {
		return 0
	}
	return min(pointSegDist(s.a, t.a, t.b), pointSegDist(s.b, t.a, t.b), pointSegDist(t.a, s.a, s.b), pointSegDist(t.b, s.a, s.b))
}

// clearanceTol is how much closer than the radius a path may come: the float32 rounding of world coordinates.
const clearanceTol = 1e-3

// checkPathClearance fails t when a segment of path comes closer than r to an edge of src.
func checkPathClearance(t *testing.T, src navbake.Source, path []core.Point, r float64) {
	t.Helper()
	for i := 0; i+1 < len(path); i++ {
		s := segment{vec(path[i]), vec(path[i+1])}
		for _, e := range edges(src) {
			if d := segDist(s, e); d < r-clearanceTol {
				t.Fatalf("path %v: leg %d comes %.4f from edge %v, want at least %v", path, i, d, e, r)
			}
		}
	}
}

// checkMesh fails t when a polygon of m is not convex and counter-clockwise or a vertex comes closer than the radius to an edge of src.
func checkMesh(t *testing.T, src navbake.Source, m *navbake.Mesh) {
	t.Helper()
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, p := range m.Polygons {
		for k := range p {
			a, b, c := vec(p[k]), vec(p[(k+1)%len(p)]), vec(p[(k+2)%len(p)])
			if cross(a, b, c) <= 0 {
				t.Fatalf("polygon %d %v turns clockwise or straight at vertex %d", i, p, (k+1)%len(p))
			}
			for _, e := range edges(src) {
				if d := pointSegDist(a, e.a, e.b); d < float64(m.Radius)-clearanceTol {
					t.Fatalf("polygon %d vertex %v is %.4f from edge %v, want at least %v", i, a, d, e, m.Radius)
				}
			}
		}
	}
}

func bake(t testing.TB, src navbake.Source, r float32) *navbake.Mesh {
	t.Helper()
	m, err := navbake.Bake(src, navbake.Agent{Radius: r}, navbake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBakeOpenRoom(t *testing.T) {
	src := navbake.Source{Outlines: [][]core.Point{rect(0, 0, 100, 60)}}
	m := bake(t, src, 0)
	if len(m.Polygons) != 1 || !samePolygon(m.Polygons[0], rect(0, 0, 100, 60)) {
		t.Fatalf("radius 0: polygons %v, want the outline", m.Polygons)
	}
	m = bake(t, src, 5)
	if len(m.Polygons) != 1 || len(m.Polygons[0]) != 4 {
		t.Fatalf("radius 5: polygons %v, want one rectangle", m.Polygons)
	}
	for _, v := range m.Polygons[0] {
		x, y := float64(v.X), float64(v.Y)
		if math.Min(math.Abs(x-5), math.Abs(x-95)) > 0.01 || math.Min(math.Abs(y-5), math.Abs(y-55)) > 0.01 {
			t.Fatalf("radius 5: vertex %v, want the room inset by 5", v)
		}
	}
	checkMesh(t, src, m)
}

func samePolygon(p navbake.Polygon, want []core.Point) bool {
	if len(p) != len(want) {
		return false
	}
	for i := range p {
		if p[i] != want[i] {
			return false
		}
	}
	return true
}

// TestBakeReferencePath checks a path around a square pillar against the true shortest path for a disc-shaped agent: two tangents, two quarter arcs and the pillar's side, offset by the radius.
func TestBakeReferencePath(t *testing.T) {
	const r = 5.0
	src := navbake.Source{
		Outlines:  [][]core.Point{rect(0, 0, 100, 100)},
		Obstacles: [][]core.Point{rect(40, 40, 60, 60)},
	}
	m := bake(t, src, r)
	checkMesh(t, src, m)
	start, goal := core.Point{X: 10, Y: 50}, core.Point{X: 90, Y: 50}
	path, ok := m.NavMesh().FindPath(start, goal)
	if !ok {
		t.Fatal("no path around the pillar")
	}
	checkPathClearance(t, src, path, r)

	// Going under the pillar: from S tangent to the circle around corner (40, 40), along y = 35, and the mirror image on the right.
	d := math.Hypot(30, 10)
	tangent := math.Sqrt(d*d - r*r)
	tangentAngle := math.Atan2(10, -30) + math.Acos(r/d) - 2*math.Pi
	arc := r * (-math.Pi/2 - tangentAngle)
	want := 2*(tangent+arc) + 20
	got := pathLength(path)
	// The rounded corners are polygons that hold the circle, so the path is a little longer than the true one, never shorter.
	if got < want-clearanceTol || got > want+0.25 {
		t.Fatalf("path %v has length %.4f, want %.4f (+0.25)", path, got, want)
	}
}

func pathLength(path []core.Point) float64 {
	var l float64
	for i := 0; i+1 < len(path); i++ {
		l += math.Hypot(float64(path[i+1].X-path[i].X), float64(path[i+1].Y-path[i].Y))
	}
	return l
}

// concaveRoom is a U-shaped room with a triangular hole, an L-shaped (concave) pillar, two overlapping crates and a buttress that pokes through the outer wall.
func concaveRoom() navbake.Source {
	return navbake.Source{
		Outlines: [][]core.Point{poly(0, 0, 200, 0, 200, 150, 130, 150, 130, 60, 70, 60, 70, 150, 0, 150)},
		Obstacles: [][]core.Point{
			poly(20, 20, 50, 25, 30, 45),
			poly(150, 20, 180, 20, 180, 30, 160, 30, 160, 50, 150, 50),
			rect(20, 90, 40, 110),
			rect(30, 100, 50, 120),
			rect(190, 90, 220, 100),
		},
	}
}

func TestBakeConcaveRoomKeepsClearance(t *testing.T) {
	src := concaveRoom()
	const r = 4
	m := bake(t, src, r)
	checkMesh(t, src, m)
	golden(t, "concave.yaml", encode(t, m))

	nav := m.NavMesh()
	var pts []core.Point
	for y := float32(5); y < 150; y += 10 {
		for x := float32(5); x < 200; x += 10 {
			p := core.Point{X: core.Points(x), Y: core.Points(y)}
			if nav.Contains(p) {
				pts = append(pts, p)
			}
		}
	}
	if len(pts) < 100 {
		t.Fatalf("only %d sample points are on the mesh", len(pts))
	}
	found := 0
	for i := 0; i < len(pts); i += 3 {
		for j := 1; j < len(pts); j += 7 {
			path, ok := nav.FindPath(pts[i], pts[j])
			if !ok {
				continue
			}
			found++
			checkPathClearance(t, src, path, r)
		}
	}
	if found == 0 {
		t.Fatal("no paths found")
	}

	// Reference paths: across both arms of the U, around the L pillar and past the crates.
	var ref strings.Builder
	for _, q := range [][2]core.Point{
		{{X: 10, Y: 140}, {X: 190, Y: 140}},
		{{X: 190, Y: 10}, {X: 170, Y: 45}},
		{{X: 60, Y: 140}, {X: 10, Y: 10}},
		{{X: 100, Y: 10}, {X: 140, Y: 145}},
	} {
		path, ok := nav.FindPath(q[0], q[1])
		if !ok {
			t.Fatalf("no path from %v to %v", q[0], q[1])
		}
		checkPathClearance(t, src, path, r)
		fmt.Fprintln(&ref, path)
	}
	golden(t, "concave.paths.txt", []byte(ref.String()))
}

// TestBakeObstacleOrderIrrelevant checks that the mesh depends on the obstacles, not on the order they are listed in or how they are wound.
func TestBakeObstacleOrderIrrelevant(t *testing.T) {
	src := concaveRoom()
	want := encode(t, bake(t, src, 3))
	var shuffled navbake.Source
	shuffled.Outlines = src.Outlines
	for i := len(src.Obstacles) - 1; i >= 0; i-- {
		o := append([]core.Point(nil), src.Obstacles[i]...)
		for a, b := 0, len(o)-1; a < b; a, b = a+1, b-1 {
			o[a], o[b] = o[b], o[a]
		}
		shuffled.Obstacles = append(shuffled.Obstacles, o)
	}
	if got := encode(t, bake(t, shuffled, 3)); !bytes.Equal(got, want) {
		t.Fatalf("reordered obstacles bake differently:\n%s\nwant\n%s", got, want)
	}
}

func TestBakeDeterministicAndRoundTrip(t *testing.T) {
	src := concaveRoom()
	first := encode(t, bake(t, src, 4))
	second := encode(t, bake(t, src, 4))
	if !bytes.Equal(first, second) {
		t.Fatal("baking twice gives different bytes")
	}
	m, err := navbake.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	if again := encode(t, m); !bytes.Equal(again, first) {
		t.Fatalf("load then save changed the file:\n%s\nwant\n%s", again, first)
	}
}

func TestBakeAllMatchesBake(t *testing.T) {
	src := concaveRoom()
	agents := []navbake.Agent{{Name: "small", Radius: 2}, {Name: "medium", Radius: 0}, {Name: "large", Radius: 8}}
	all, err := navbake.BakeAll(src, agents, navbake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range agents {
		one, err := navbake.Bake(src, a, navbake.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got, want := encode(t, all[i]), encode(t, one); !bytes.Equal(got, want) {
			t.Fatalf("agent %q: BakeAll gives\n%s\nBake gives\n%s", a.Name, got, want)
		}
		if all[i].Agent != a.Name || all[i].Radius != a.Radius {
			t.Fatalf("mesh %d is for %q radius %v, want %q radius %v", i, all[i].Agent, all[i].Radius, a.Name, a.Radius)
		}
		checkMesh(t, src, all[i])
	}
	// A larger agent fits through less.
	if area(all[2]) >= area(all[0]) {
		t.Fatalf("large agent's area %v is not below the small one's %v", area(all[2]), area(all[0]))
	}
}

func area(m *navbake.Mesh) float64 {
	var a float64
	for _, p := range m.Polygons {
		for k := range p {
			b := p[(k+1)%len(p)]
			a += float64(p[k].X)*float64(b.Y) - float64(b.X)*float64(p[k].Y)
		}
	}
	return a / 2
}

// TestBakePinchedCorners checks two blocks that touch only at a corner: no agent with a radius squeezes between them.
func TestBakePinchedCorners(t *testing.T) {
	src := navbake.Source{
		Outlines:  [][]core.Point{rect(0, 0, 100, 100)},
		Obstacles: [][]core.Point{rect(30, 30, 50, 50), rect(50, 50, 70, 70)},
	}
	m := bake(t, src, 3)
	checkMesh(t, src, m)
	path, ok := m.NavMesh().FindPath(core.Point{X: 60, Y: 35}, core.Point{X: 40, Y: 65})
	if !ok {
		t.Fatal("no path around the blocks")
	}
	checkPathClearance(t, src, path, 3)
}

func TestBakeWalkablePocket(t *testing.T) {
	// A ring wound counter-clockwise around a clockwise hole: the hole's inside is walkable again.
	src := navbake.Source{
		Outlines: [][]core.Point{rect(0, 0, 100, 100)},
		Obstacles: [][]core.Point{
			rect(20, 20, 80, 80),
			poly(40, 40, 40, 60, 60, 60, 60, 40),
		},
	}
	nav := bake(t, src, 0).NavMesh()
	for _, c := range []struct {
		p    core.Point
		want bool
	}{
		{core.Point{X: 10, Y: 10}, true},
		{core.Point{X: 30, Y: 30}, false},
		{core.Point{X: 50, Y: 50}, true},
	} {
		if got := nav.Contains(c.p); got != c.want {
			t.Errorf("Contains(%v) = %v, want %v", c.p, got, c.want)
		}
	}
	if _, ok := nav.FindPath(core.Point{X: 10, Y: 10}, core.Point{X: 50, Y: 50}); ok {
		t.Error("found a path into the walled pocket")
	}
}

func TestBakeErrors(t *testing.T) {
	room := [][]core.Point{rect(0, 0, 10, 10)}
	nan := core.Points(math.NaN())
	for _, c := range []struct {
		name   string
		src    navbake.Source
		agents []navbake.Agent
		opts   navbake.Options
		want   string
	}{
		{"no outline", navbake.Source{}, []navbake.Agent{{Name: "a"}}, navbake.Options{}, "no outline"},
		{"negative radius", navbake.Source{Outlines: room}, []navbake.Agent{{Name: "a", Radius: -1}}, navbake.Options{}, "radius"},
		{"radius too large", navbake.Source{Outlines: room}, []navbake.Agent{{Name: "a", Radius: 1e30}}, navbake.Options{}, "too large"},
		{"negative precision", navbake.Source{Outlines: room}, []navbake.Agent{{Name: "a"}}, navbake.Options{Precision: -1}, "precision"},
		{"not finite", navbake.Source{Outlines: [][]core.Point{{{X: nan}, {X: 1}, {Y: 1}}}}, []navbake.Agent{{Name: "a"}}, navbake.Options{}, "not finite"},
		{"too far", navbake.Source{Outlines: [][]core.Point{rect(0, 0, 1e9, 1e9)}}, []navbake.Agent{{Name: "a"}}, navbake.Options{}, "too far"},
		{"past 2^24 steps", navbake.Source{Outlines: [][]core.Point{rect(0, 0, 65536.01, 10)}}, []navbake.Agent{{Name: "a"}}, navbake.Options{}, "2^24"},
		{"grown past 2^24 steps", navbake.Source{Outlines: [][]core.Point{rect(65500, 0, 65530, 30)}}, []navbake.Agent{{Name: "a", Radius: 8}}, navbake.Options{}, "grown by the agent radius"},
		{"unnamed agent", navbake.Source{Outlines: room}, []navbake.Agent{{Radius: 1}}, navbake.Options{}, "no name"},
		{"duplicate agent", navbake.Source{Outlines: room}, []navbake.Agent{{Name: "a"}, {Name: "a"}}, navbake.Options{}, "twice"},
	} {
		_, err := navbake.BakeAll(c.src, c.agents, c.opts)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", c.name, err, c.want)
		}
	}
}

// TestBakeFarFromOrigin bakes a room near the edge of the 2^24-step range and checks that the mesh still keeps its clearance and encodes, which float32 coordinates past that range would break.
func TestBakeFarFromOrigin(t *testing.T) {
	for _, off := range []float32{0, 30000, 65000, -65400} {
		at := func(p []core.Point) []core.Point {
			out := make([]core.Point, len(p))
			for i, v := range p {
				out[i] = core.Point{X: v.X + core.Points(off), Y: v.Y + core.Points(off)}
			}
			return out
		}
		src := navbake.Source{
			Outlines:  [][]core.Point{at(rect(0, 0, 100, 100))},
			Obstacles: [][]core.Point{at(rect(40, 40, 60, 60)), at(poly(70, 10, 90, 10, 80, 30))},
		}
		m := bake(t, src, 3)
		if len(m.Polygons) == 0 {
			t.Fatalf("offset %v: no polygons", off)
		}
		checkMesh(t, src, m)
		if _, err := navbake.Parse(encode(t, m)); err != nil {
			t.Fatalf("offset %v: %v", off, err)
		}
	}
}

func TestBakeNothingWalkable(t *testing.T) {
	src := navbake.Source{Outlines: [][]core.Point{rect(0, 0, 10, 10)}}
	m := bake(t, src, 6)
	if len(m.Polygons) != 0 {
		t.Fatalf("an agent wider than the room got polygons %v", m.Polygons)
	}
	if _, err := navbake.Parse(encode(t, m)); err != nil {
		t.Fatal(err)
	}
}

func loadTiled(t testing.TB, name string) *tilemap.Tilemap {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	m, err := tilemap.LoadTiled(data, tilemap.TilesetLoader{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBakeTiledMapsMatchGolden(t *testing.T) {
	for _, c := range []struct {
		tmj    string
		agents []navbake.Agent
	}{
		{"dungeon.tmj", []navbake.Agent{{Name: "small", Radius: 3}, {Name: "large", Radius: 6}}},
		{"dungeon_iso.tmj", []navbake.Agent{{Name: "small", Radius: 3}}},
	} {
		m := loadTiled(t, c.tmj)
		src, err := navbake.FromTilemap(m, tilemap.TileSolid)
		if err != nil {
			t.Fatal(err)
		}
		meshes, err := navbake.BakeAll(src, c.agents, navbake.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for i, mesh := range meshes {
			checkMesh(t, src, mesh)
			golden(t, strings.TrimSuffix(c.tmj, ".tmj")+"."+c.agents[i].Name+".yaml", encode(t, mesh))
		}
	}
}

// TestFromTilemapCells checks, on every orientation, that a mesh baked with no radius covers the centre of every open cell and no solid one.
func TestFromTilemapCells(t *testing.T) {
	layout := []string{
		".....",
		".#.#.",
		"..#..",
		".#...",
	}
	for _, c := range []struct {
		name string
		set  func(m *tilemap.Tilemap)
	}{
		{"orthogonal", func(m *tilemap.Tilemap) {}},
		{"isometric", func(m *tilemap.Tilemap) { m.Orientation = tilemap.OrientationIsometric }},
		{"staggered", func(m *tilemap.Tilemap) { m.Orientation = tilemap.OrientationStaggered }},
		{"staggered x even", func(m *tilemap.Tilemap) {
			m.Orientation, m.StaggerAxis, m.StaggerIndex = tilemap.OrientationStaggered, tilemap.StaggerX, tilemap.StaggerEven
		}},
		{"hexagonal", func(m *tilemap.Tilemap) { m.Orientation, m.HexSideLength = tilemap.OrientationHexagonal, 8 }},
		{"hexagonal x", func(m *tilemap.Tilemap) {
			m.Orientation, m.StaggerAxis = tilemap.OrientationHexagonal, tilemap.StaggerX
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			tm := tilemap.New(len(layout[0]), len(layout), 32, 16)
			c.set(tm)
			for row, line := range layout {
				for col, ch := range line {
					if ch == '#' {
						tm.SetCellFlags(col, row, tilemap.TileSolid)
					}
				}
			}
			src, err := navbake.FromTilemap(tm, tilemap.TileSolid)
			if err != nil {
				t.Fatal(err)
			}
			nav := bake(t, src, 0).NavMesh()
			for row, line := range layout {
				for col, ch := range line {
					center := tm.TileToWorld(col, row)
					if got, want := nav.Contains(center), ch == '.'; got != want {
						t.Errorf("cell (%d, %d) at %v: Contains = %v, want %v", col, row, center, got, want)
					}
				}
			}
			// Staggered and hexagonal outlines are made of every cell, so only the obstacles bound the clearance.
			checkMesh(t, navbake.Source{Obstacles: src.Obstacles}, bake(t, src, 2))
		})
	}
}

func TestFromTilemapErrors(t *testing.T) {
	if _, err := navbake.FromTilemap(nil, tilemap.TileSolid); err == nil {
		t.Error("nil map: no error")
	}
	if _, err := navbake.FromTilemap(tilemap.New(0, 3, 16, 16), tilemap.TileSolid); err == nil {
		t.Error("empty map: no error")
	}
	if _, err := navbake.FromTilemap(tilemap.New(3, 3, 16, 16), 0); err == nil {
		t.Error("empty mask: no error")
	}
}

func encode(t testing.TB, m *navbake.Mesh) []byte {
	t.Helper()
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// golden compares data with testdata/name, or rewrites the file with -update.
func golden(t *testing.T, name string, data []byte) {
	t.Helper()
	file := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("%s differs from the golden file:\n%s\nwant\n%s", name, data, want)
	}
}
