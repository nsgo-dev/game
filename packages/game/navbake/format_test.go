package navbake_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/packages/game/internal/formattest"
	"github.com/nsgo-dev/game/packages/game/navbake"
	"github.com/nsgo-dev/nsgo/core"
)

func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*navbake.Mesh]{
		Decode: navbake.Parse,
		Encode: (*navbake.Mesh).Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "navbake",
	})
}

func TestParseRefuses(t *testing.T) {
	for _, c := range []struct {
		name, data, want string
	}{
		{"empty", "", "empty"},
		{"version", "version: 2\nradius: 1\npolygons: []\n", "version"},
		{"unknown field", "version: 1\nradius: 1\nsize: 3\npolygons: []\n", "size"},
		{"negative radius", "version: 1\nradius: -1\npolygons: []\n", "radius"},
		{"two vertices", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [1, 0]]\n", "at least 3"},
		{"three coordinates", "version: 1\nradius: 0\npolygons:\n  - [[0, 0, 0], [1, 0], [1, 1]]\n", "coordinates"},
		{"not convex", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [4, 0], [1, 1], [0, 4]]\n", "not convex"},
		{"clockwise", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [1, 0], [1, 1], [0, 1]]\n  - [[1, 0], [1, 1], [2, 1], [2, 0]]\n", "polygon 1: is wound clockwise"},
		{"clockwise star", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [6, 8], [10, 0], [-2, 5], [12, 5]]\n", "clockwise"},
		{"star", "version: 1\nradius: 0\npolygons:\n  - [[12, 5], [-2, 5], [10, 0], [6, 8], [0, 0]]\n", "not convex"},
		{"no area", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [1, 1], [2, 2]]\n", "no area"},
		{"not finite", "version: 1\nradius: 0\npolygons:\n  - [[0, 0], [.inf, 0], [1, 1]]\n", "not finite"},
		{"second document", "version: 1\nradius: 0\npolygons: []\n---\nversion: 1\n", "trailing"},
	} {
		_, err := navbake.Parse([]byte(c.data))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", c.name, err, c.want)
		}
	}
}

func TestEncodeKeepsFloat32(t *testing.T) {
	m := &navbake.Mesh{Version: navbake.FormatVersion, Agent: "a", Radius: 0.1, Polygons: []navbake.Polygon{
		{{X: 0.1, Y: -0.3}, {X: 1e7, Y: -0.3}, {X: 3.3333333, Y: 12345.678}},
	}}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	want := "version: 1\nagent: a\nradius: 0.1\npolygons:\n  - [[0.1, -0.3], [1e+07, -0.3], [3.3333333, 12345.678]]\n"
	if string(data) != want {
		t.Fatalf("encoded\n%s\nwant\n%s", data, want)
	}
	back, err := navbake.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range back.Polygons[0] {
		if v != m.Polygons[0][k] {
			t.Fatalf("vertex %d read back as %v, want %v", k, v, m.Polygons[0][k])
		}
	}
}

func TestMeshPortals(t *testing.T) {
	m := &navbake.Mesh{Version: 1, Polygons: []navbake.Polygon{
		poly(0, 0, 10, 0, 10, 10, 0, 10),
		poly(10, 0, 20, 0, 20, 5, 10, 5),
		poly(30, 0, 40, 0, 40, 10),
	}}
	got := m.Portals()
	if len(got) != 1 {
		t.Fatalf("portals %v, want one", got)
	}
	p := got[0]
	if p.From != 0 || p.To != 1 || p.A != (core.Point{X: 10, Y: 0}) || p.B != (core.Point{X: 10, Y: 5}) {
		t.Fatalf("portal %+v, want polygons 0-1 from (10, 0) to (10, 5)", p)
	}
	if (*navbake.Mesh)(nil).Portals() != nil {
		t.Fatal("nil mesh has portals")
	}
}

// TestMeshPortalsMatchNavMesh checks that the bucketed portal search finds exactly the neighbors that ai.BuildNavMesh's pairwise scan finds, on baked meshes and on a mesh with one polygon much larger than the rest.
func TestMeshPortalsMatchNavMesh(t *testing.T) {
	big := &navbake.Mesh{Version: 1, Polygons: []navbake.Polygon{poly(0, -1000, 1000, -1000, 1000, 0, 0, 0), poly(1000, -1000, 2000, -1000, 2000, 0, 1000, 0)}}
	for x := float32(0); x < 2000; x += 10 {
		big.Polygons = append(big.Polygons, poly(x, 0, x+10, 0, x+10, 10, x, 10))
	}
	meshes := []*navbake.Mesh{big, bake(t, concaveRoom(), 0), bake(t, concaveRoom(), 4)}
	for _, c := range []string{"dungeon.small.yaml", "dungeon.large.yaml", "dungeon_iso.small.yaml"} {
		data, err := os.ReadFile(filepath.Join("testdata", c))
		if err != nil {
			t.Fatal(err)
		}
		m, err := navbake.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		meshes = append(meshes, m)
	}
	for k, m := range meshes {
		want := map[[2]int]bool{}
		nav := m.NavMesh()
		for i := range m.Polygons {
			for _, j := range nav.Neighbors(i) {
				if i < j {
					want[[2]int{i, j}] = true
				}
			}
		}
		got := m.Portals()
		for n, p := range got {
			if n > 0 && (got[n-1].From > p.From || got[n-1].From == p.From && got[n-1].To >= p.To) {
				t.Fatalf("mesh %d: portals out of order at %d: %+v then %+v", k, n, got[n-1], p)
			}
			if !want[[2]int{p.From, p.To}] {
				t.Fatalf("mesh %d: portal %+v is not a NavMesh neighbor", k, p)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("mesh %d: %d portals, NavMesh has %d neighbor pairs", k, len(got), len(want))
		}
	}
}

func TestDraw(t *testing.T) {
	m := bake(t, concaveRoom(), 4)
	count := func(d *core.Drawing, kind core.DrawKind) int {
		n := 0
		for _, op := range d.Ops {
			if op.Kind == kind {
				n++
			}
		}
		return n
	}
	var d core.Drawing
	navbake.Draw(&d, m, core.RGBA(0x40a0ff, 0.25), core.RGB(0x2060c0), core.RGB(0xffc040), 1)
	if got := count(&d, core.DrawClosePath); got != 2*len(m.Polygons) {
		t.Errorf("%d closed paths, want two per polygon (%d)", got, 2*len(m.Polygons))
	}
	if got := count(&d, core.DrawFill); got != 1 {
		t.Errorf("%d fills, want 1", got)
	}
	if got := count(&d, core.DrawStroke); got != 2 {
		t.Errorf("%d strokes, want edges and portals", got)
	}
	if got, want := count(&d, core.DrawLineTo), countVertices(m)-len(m.Polygons)*2+len(m.Portals()); got != want {
		t.Errorf("%d lines, want %d", got, want)
	}

	d = core.Drawing{}
	navbake.Draw(&d, m, 0, core.RGB(0x2060c0), 0, 1)
	if count(&d, core.DrawFill) != 0 || count(&d, core.DrawStroke) != 1 {
		t.Error("transparent fill and portal colors still drew")
	}
	navbake.Draw(nil, m, 0, 0, 0, 1)
	d = core.Drawing{}
	navbake.Draw(&d, nil, core.RGB(0), core.RGB(0), core.RGB(0), 1)
	if len(d.Ops) != 0 {
		t.Error("nil mesh drew")
	}
}

func countVertices(m *navbake.Mesh) int {
	n := 0
	for _, p := range m.Polygons {
		n += 2 * len(p)
	}
	return n
}
