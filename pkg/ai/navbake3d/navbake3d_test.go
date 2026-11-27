package navbake3d_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/nsgo-dev/game/pkg/ai/navbake3d"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

func v3(x, y, z float32) math3d.Vec3 { return math3d.Vec3{X: x, Y: y, Z: z} }

// findPath returns the path between a and b on nm, failing the test when there is none.
func findPath(t *testing.T, nm *world3d.NavMesh, a, b math3d.Vec3) []math3d.Vec3 {
	t.Helper()
	path, ok := nm.FindPath(a, b)
	if !ok {
		t.Fatalf("no path from %v to %v", a, b)
	}
	return path
}

// sampleSegments returns points every step along the path's segments, ends included.
func sampleSegments(path []math3d.Vec3, step float32) []math3d.Vec3 {
	out := []math3d.Vec3{path[0]}
	for i := 1; i < len(path); i++ {
		a, b := path[i-1], path[i]
		n := int(a.Dist(b)/step) + 1
		for k := 1; k <= n; k++ {
			out = append(out, a.Lerp(b, float32(k)/float32(n)))
		}
	}
	return out
}

func TestReferenceLevelClimbsStairs(t *testing.T) {
	nm := bakeReference(t).NavMesh()
	start := v3(1.5, 0, 2)
	top := v3(8.5, platformY, 2)
	path := findPath(t, nm, start, top)
	if end := path[len(path)-1]; end.Y < platformY-0.1 || end.Y > platformY+0.2 {
		t.Fatalf("path ends at height %v, want the platform's %v", end.Y, platformY)
	}
	// The mesh over the stairs is a ramp through the steps' edges: every point of the climb lies within a riser of the step under it, give or take a cell and the detail error, so the path never jumps more than a step at a time.
	tol := referenceSettings.CellHeight + navbake3d.DefaultDetailSampleMaxError
	var climbed bool
	for _, p := range sampleSegments(path, 0.05) {
		if !insideXZ(stairs, p.X, p.Z, -0.01) {
			continue
		}
		climbed = true
		step := int((p.X - stairs[0]) / stepRun)
		lo := stepRise * float32(step)
		hi := stepRise * float32(step+1)
		if p.Y < lo-tol || p.Y > hi+tol {
			t.Errorf("path point %v on step %d is at height %v, want between %v and %v", p, step, p.Y, lo, hi)
		}
	}
	if !climbed {
		t.Fatalf("path does not cross the stairs: %v", path)
	}
}

func TestReferenceLevelBridgeOverFloor(t *testing.T) {
	nm := bakeReference(t).NavMesh()
	// Across the bridge, the path stays on the deck.
	path := findPath(t, nm, v3(8.5, platformY, 2), v3(16.5, platformY, 2))
	for _, p := range sampleSegments(path, 0.1) {
		if p.Y < platformY-0.1 {
			t.Fatalf("path across the bridge drops to %v at %v", p.Y, p)
		}
	}
	// Under the bridge, the floor is walkable, its clearance being above the agent's height, and the path keeps to the floor.
	under := v3(12.5, 0, 2)
	i, ok := nm.FindPolygon(under)
	if !ok {
		t.Fatal("no polygon on the floor under the bridge")
	}
	if y := nm.Polys[i].HeightAt(under.X, under.Z); y > 0.2 {
		t.Fatalf("the point under the bridge snapped to height %v, want the floor", y)
	}
	path = findPath(t, nm, under, v3(12.5, 0, 4.5))
	for _, p := range path {
		if p.Y > 0.2 {
			t.Fatalf("path under the bridge climbs to %v", p)
		}
	}
	// The deck and the floor beneath it are not linked: from the floor under the bridge to its deck the path goes round by the stairs.
	path = findPath(t, nm, under, v3(12.5, platformY, 2))
	var climbed bool
	for _, p := range sampleSegments(path, 0.1) {
		if insideXZ(stairs, p.X, p.Z, -0.01) && p.Y > 0.2 {
			climbed = true
		}
	}
	if !climbed {
		t.Fatalf("path from under the bridge to its deck does not use the stairs: %v", path)
	}
}

func TestReferenceLevelRefusesTallSteps(t *testing.T) {
	nm := bakeReference(t).NavMesh()
	top := v3(18.25, tallRise*4, 8.5)
	if _, ok := nm.FindPolygon(top); !ok {
		t.Fatal("the top of the tall steps is not walkable")
	}
	if path, ok := nm.FindPath(v3(12.5, 0, 4.5), top); ok {
		t.Fatalf("found a path up steps of %v, above the agent's climb: %v", tallRise, path)
	}
}

func TestReferenceLevelLowCeiling(t *testing.T) {
	m := bakeReference(t)
	nm := m.NavMesh()
	// No floor-level triangle reaches under the low ceiling.
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		for _, tri := range p.Triangles {
			v := m.TriangleVertices(p, tri)
			if v[0].Y > 0.5 || v[1].Y > 0.5 || v[2].Y > 0.5 {
				continue
			}
			c := v[0].Add(v[1]).Add(v[2]).Scale(1.0 / 3)
			if insideXZ(lowCeiling, c.X, c.Z, -0.01) {
				t.Fatalf("floor triangle %v lies under the low ceiling", v)
			}
		}
	}
	// A path past the ceiling goes round it.
	path := findPath(t, nm, v3(3, 0, 6), v3(11, 0, 6))
	for _, p := range sampleSegments(path, 0.05) {
		if p.Y < 0.5 && insideXZ(lowCeiling, p.X, p.Z, -0.01) {
			t.Fatalf("path passes under the low ceiling at %v", p)
		}
	}
}

func TestReferenceLevelKeepsRadiusFromWalls(t *testing.T) {
	m := bakeReference(t)
	// The walls and the erosion are measured in whole cells, and an outline is simplified within MaxSimplificationError of the cells, so the mesh keeps the radius to within a cell.
	tol := referenceSettings.CellSize
	for _, v := range m.Vertices {
		if v[1] > 0.5 {
			continue
		}
		for _, f := range [][4]float32{wall, lowCeiling, stairs, nearPlatform, farPlatform} {
			if f == stairs && v[1] > 0.1 {
				continue
			}
			if d := distXZ(f, v[0], v[2]); d < referenceAgent.Radius-tol {
				t.Errorf("floor vertex %v is %v from an obstacle, nearer than the agent's radius %v", v, d, referenceAgent.Radius)
			}
		}
	}
	// A path round the wall keeps its distance too.
	nm := m.NavMesh()
	path := findPath(t, nm, v3(11, 0, 8), v3(14, 0, 8))
	for _, p := range sampleSegments(path, 0.05) {
		if d := distXZ(wall, p.X, p.Z); d < referenceAgent.Radius-tol {
			t.Fatalf("path passes %v from the wall at %v", d, p)
		}
	}
}

func TestReferenceLevelLayerMask(t *testing.T) {
	m := bakeReference(t)
	// The layer 2 block stands on open floor; a layer 1 bake walks through where it stands.
	nm := m.NavMesh()
	if _, ok := nm.FindPolygon(v3(1.75, 0, 6.75)); !ok {
		t.Fatal("the layer 2 block was baked into a layer 1 mesh")
	}
	s := referenceSettings
	m2, err := navbake3d.BakeMesh(referenceLevel(t), s, referenceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := m2.NavMesh().FindPolygon(v3(1.75, 0, 6.75)); ok && m2.NavMesh().Polys[i].HeightAt(1.75, 6.75) < 0.5 {
		t.Fatal("an all-layer bake walks through the layer 2 block")
	}
}

func TestReferenceLevelTerrain(t *testing.T) {
	nm := bakeReference(t).NavMesh()
	path := findPath(t, nm, v3(5, 0, 8), v3(10, 0, 18))
	end := path[len(path)-1]
	// The terrain rises to between 0.2 and 1 at the far edge; the path ends on it within the detail error.
	want := float32(18-10) / 10 * float32(0.6+0.4*sinf(10*0.5))
	if d := end.Y - want; d < -0.25 || d > 0.25 {
		t.Fatalf("path ends at height %v on terrain of height %v", end.Y, want)
	}
}

func TestBakeIsDeterministicAndRoundTrips(t *testing.T) {
	bake := func() []byte {
		f, err := navbake3d.Bake(referenceLevel(t), referenceSettings, []navbake3d.AgentType{referenceAgent, {Name: "small", Radius: 0.2, Height: 1, MaxClimb: 0.3}})
		if err != nil {
			t.Fatal(err)
		}
		data, err := navbake3d.Encode(f)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first, second := bake(), bake()
	if !bytes.Equal(first, second) {
		t.Fatal("baking the same level twice wrote different YAML")
	}
	f, err := navbake3d.Parse(first)
	if err != nil {
		t.Fatal(err)
	}
	again, err := navbake3d.Encode(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("load then save changed the baked YAML")
	}
	if len(f.Meshes) != 2 || f.Meshes[0].Agent.Name != "humanoid" || f.Meshes[1].Agent.Name != "small" {
		t.Fatalf("meshes for %v, want humanoid then small", f.Meshes)
	}
	// The file records every setting, the defaults included.
	if f.Settings.MaxVertsPerPoly != navbake3d.DefaultMaxVertsPerPoly || f.Settings.LayerMask != navbake3d.LayerAll || f.Meshes[1].Agent.MaxSlope != navbake3d.DefaultAgentMaxSlope {
		t.Fatalf("settings %+v and agent %+v leave defaults unrecorded", f.Settings, f.Meshes[1].Agent)
	}
	// The smaller agent fits under the low ceiling.
	small, _ := f.Mesh("small")
	path, ok := small.NavMesh().FindPath(v3(3, 0, 6), v3(11, 0, 6))
	if !ok {
		t.Fatal("no path for the small agent")
	}
	var under bool
	for _, p := range sampleSegments(path, 0.05) {
		if p.Y < 0.5 && insideXZ(lowCeiling, p.X, p.Z, -0.5) {
			under = true
		}
	}
	if !under {
		t.Fatal("the small agent, under the ceiling's clearance, went round it")
	}
	if _, ok := f.Mesh("giant"); ok {
		t.Fatal("found a mesh for an agent type that was not baked")
	}
}

func TestBakeMeshIsConvexAndCovered(t *testing.T) {
	m := bakeReference(t)
	if len(m.Polygons) == 0 {
		t.Fatal("no polygons")
	}
	for pi := range m.Polygons {
		p := &m.Polygons[pi]
		n := len(p.Vertices)
		if n < 3 || n > navbake3d.DefaultMaxVertsPerPoly {
			t.Fatalf("polygon %d has %d vertices", pi, n)
		}
		// Convex in XZ: every corner turns the same way, counter-clockwise seen from above.
		var polyArea float64
		for i := range n {
			a := m.Vertices[p.Vertices[i]].Vec3()
			b := m.Vertices[p.Vertices[(i+1)%n]].Vec3()
			polyArea += orient(m.Vertices[p.Vertices[0]].Vec3(), a, b)
		}
		sign := float64(1)
		if polyArea < 0 {
			sign = -1
		}
		for i := range n {
			a := m.Vertices[p.Vertices[i]].Vec3()
			b := m.Vertices[p.Vertices[(i+1)%n]].Vec3()
			c := m.Vertices[p.Vertices[(i+2)%n]].Vec3()
			if orient(a, b, c)*sign < -1e-6 {
				t.Fatalf("polygon %d is not convex at vertex %d", pi, (i+1)%n)
			}
		}
		// The detail triangles cover the polygon exactly and wind counter-clockwise seen from above.
		var triArea float64
		for _, tri := range p.Triangles {
			v := m.TriangleVertices(p, tri)
			a := orient(v[0], v[1], v[2])
			if a >= 0 {
				t.Fatalf("polygon %d triangle %v winds clockwise or is degenerate", pi, tri)
			}
			if nrm := v[1].Sub(v[0]).Cross(v[2].Sub(v[0])); nrm.Y <= 0 {
				t.Fatalf("polygon %d triangle %v faces down", pi, tri)
			}
			triArea += a
		}
		if d := math.Abs(triArea) - math.Abs(polyArea); math.Abs(d) > 1e-3*(1+math.Abs(polyArea)) {
			t.Fatalf("polygon %d: triangles cover area %v of %v", pi, triArea, polyArea)
		}
	}
}

func sinf(x float64) float64 { return math.Sin(x) }

func orient(a, b, c math3d.Vec3) float64 {
	return float64(b.X-a.X)*float64(c.Z-a.Z) - float64(b.Z-a.Z)*float64(c.X-a.X)
}

func TestBakeErrors(t *testing.T) {
	g := referenceLevel(t)
	// A floor tile and a triangle 1e19 m away, a level whose width in cells overflows an int.
	huge := &navbake3d.Geometry{}
	huge.AddTriangle(v3(0, 0, 0), v3(0, 0, 1), v3(1, 0, 1), 1)
	huge.AddTriangle(v3(1e19, 0, 0), v3(1e19, 0, 1), v3(2e19, 0, 1), 1)
	cases := []struct {
		name   string
		g      *navbake3d.Geometry
		s      navbake3d.Settings
		agents []navbake3d.AgentType
		want   string
	}{
		{"no agents", g, navbake3d.Settings{}, nil, "no agent types"},
		{"duplicate agent", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a"}, {Name: "a"}}, "repeats agent name"},
		{"unnamed agent", g, navbake3d.Settings{}, []navbake3d.AgentType{{}}, "name"},
		{"vertical slope", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a", MaxSlope: 90}}, "below 90"},
		{"negative radius", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a", Radius: -1}}, "radius"},
		{"negative cell", g, navbake3d.Settings{CellSize: -1}, []navbake3d.AgentType{{Name: "a"}}, "cellSize"},
		{"too many verts", g, navbake3d.Settings{MaxVertsPerPoly: 13}, []navbake3d.AgentType{{Name: "a"}}, "maxVertsPerPoly"},
		{"too many cells", g, navbake3d.Settings{CellSize: 0.001}, []navbake3d.AgentType{{Name: "a"}}, "larger cellSize"},
		{"huge level", huge, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a"}}, "larger cellSize"},
		{"tiny detail spacing", g, navbake3d.Settings{DetailSampleDistance: 0.001}, []navbake3d.AgentType{{Name: "a"}}, "detailSampleDistance"},
		{"huge height", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a", Height: 1e10}}, "agent height"},
		{"huge climb", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a", MaxClimb: 1e10}}, "agent maxClimb"},
		{"huge radius", g, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a", Radius: 1e10}}, "agent radius"},
		{"empty", &navbake3d.Geometry{}, navbake3d.Settings{}, []navbake3d.AgentType{{Name: "a"}}, "no level geometry"},
		{"masked out", g, navbake3d.Settings{LayerMask: 1 << 7}, []navbake3d.AgentType{{Name: "a"}}, "no level geometry"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := navbake3d.Bake(c.g, c.s, c.agents)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Bake error %v, want one mentioning %q", err, c.want)
			}
		})
	}
	if _, err := navbake3d.BakeMesh(nil, navbake3d.Settings{}, navbake3d.AgentType{Name: "a"}); !errors.Is(err, navbake3d.ErrNoGeometry) {
		t.Fatalf("BakeMesh(nil) error %v, want ErrNoGeometry", err)
	}
	// A minimum island area larger than the level drops every island rather than overflowing.
	if m, err := navbake3d.BakeMesh(g, navbake3d.Settings{MinRegionArea: 1e30}, referenceAgent); err != nil {
		t.Fatal(err)
	} else if len(m.Polygons) != 0 {
		t.Fatalf("BakeMesh with a huge minRegionArea kept %d polygons, want none", len(m.Polygons))
	}
	// Cells larger than the default detail spacing raise it to one cell instead of refusing.
	if f, err := navbake3d.Bake(g, navbake3d.Settings{CellSize: 2}, []navbake3d.AgentType{{Name: "a"}}); err != nil {
		t.Fatal(err)
	} else if f.Settings.DetailSampleDistance != 2 {
		t.Fatalf("Bake with 2 m cells used detailSampleDistance %v, want 2", f.Settings.DetailSampleDistance)
	}
}
