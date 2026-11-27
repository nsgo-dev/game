package navbake3d_test

import (
	"math"
	"os"
	"testing"

	"github.com/nsgo-dev/game/pkg/gltf"
	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/pathfind3d/navbake3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
	"github.com/nsgo-dev/game/pkg/world3d"
)

// The reference level is 20 by 20 metres: a floor slab (a glTF box) for z in [0, 10], rolling terrain (a world3d heightmap) for z in [10, 20], and on the floor, as physics3d static boxes, a flight of stairs up to a platform with a bridge over the floor to a second platform, a flight of steps too tall to climb, a ceiling too low to walk under, and a wall.
const (
	stepRise  = 0.25 // the climbable stairs' riser, below the agent's climb
	stepRun   = 0.5
	stepCount = 9
	platformY = stepRise * stepCount // 2.25

	tallRise = 0.6 // the unclimbable steps' riser, above the agent's climb
)

// Footprints in XZ (min x, min z, max x, max z) of the level's parts.
var (
	stairs       = [4]float32{3, 1, 3 + stepRun*stepCount, 3} // x 3..7.5
	nearPlatform = [4]float32{7.5, 1, 9.5, 3}
	bridge       = [4]float32{9.5, 1, 15.5, 3}
	farPlatform  = [4]float32{15.5, 1, 17.5, 3}
	lowCeiling   = [4]float32{4, 5, 10, 7}
	wall         = [4]float32{12, 5, 13, 9}
	tallSteps    = [4]float32{15, 7.5, 17, 9.5}
	tallTop      = [4]float32{17, 7.5, 19.5, 9.5}
)

const (
	bridgeBottom = 2.0 // clearance under the bridge, above the agent's height
	ceilingLow   = 1.2 // clearance under the low ceiling, below the agent's height
	ceilingTop   = 1.5
)

// referenceAgent is a human-sized agent.
var referenceAgent = navbake3d.AgentType{Name: "humanoid", Radius: 0.4, Height: 1.8, MaxSlope: 45, MaxClimb: 0.4}

// referenceSettings bake the reference level at 20 cm cells, outlines kept within a cell of the walkable area.
var referenceSettings = navbake3d.Settings{CellSize: 0.2, CellHeight: 0.05, MaxSimplificationError: 0.2}

func box(w *physics3d.World, minX, minY, minZ, maxX, maxY, maxZ float32, layer physics3d.LayerMask) {
	b := physics3d.NewStaticBody(physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: minX, Y: minY, Z: minZ}, physics3d.Vec3{X: maxX, Y: maxY, Z: maxZ})), physics3d.Vec3{})
	b.Layer = layer
	w.AddBody(b)
}

// loadBox loads the unit box glTF fixture.
func loadBox(t testing.TB) *gltf.Document {
	t.Helper()
	doc, err := gltf.LoadFS(os.DirFS("testdata/level"), "box.gltf")
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// referenceLevel returns the reference level's geometry, with every source on layer 1 and a decoy box on layer 2 that a bake for layer 1 leaves out.
// hills are the terrain's heights along its far edge, 0.6 + 0.4·sin(c/2) for column c to two places, written out rather than computed so the level, and so the golden bake, is the same on every architecture.
var hills = [21]float32{0.60, 0.79, 0.94, 1.00, 0.96, 0.84, 0.66, 0.46, 0.30, 0.21, 0.22, 0.32, 0.49, 0.69, 0.86, 0.98, 1.00, 0.92, 0.76, 0.57, 0.38}

func referenceLevel(t testing.TB) *navbake3d.Geometry {
	t.Helper()
	g := &navbake3d.Geometry{}
	floor := math3d.TRS(math3d.Vec3{X: 10, Y: -0.1, Z: 5}, math3d.QuatIdentity(), math3d.Vec3{X: 20, Y: 0.2, Z: 10})
	g.AddGLTF(loadBox(t), floor, 1)

	const cols, rows = 21, 11
	h := &world3d.Heightmap{Cols: cols, Rows: rows, CellSize: 1, HeightScale: 1, Origin: math3d.Vec3{Z: 10}, Heights: make([]float32, cols*rows)}
	for r := range rows {
		for c := range cols {
			// Zero along the floor's edge, rising to gentle hills well below the agent's slope limit.
			h.Heights[r*cols+c] = float32(r) / float32(rows-1) * hills[c]
		}
	}
	g.AddHeightmap(h, 1)

	w := physics3d.NewWorld()
	for i := range stepCount {
		x0 := stairs[0] + float32(i)*stepRun
		box(w, x0, 0, stairs[1], x0+stepRun, stepRise*float32(i+1), stairs[3], 1)
	}
	box(w, nearPlatform[0], 0, nearPlatform[1], nearPlatform[2], platformY, nearPlatform[3], 1)
	box(w, bridge[0], bridgeBottom, bridge[1], bridge[2], platformY, bridge[3], 1)
	box(w, farPlatform[0], 0, farPlatform[1], farPlatform[2], platformY, farPlatform[3], 1)
	box(w, lowCeiling[0], ceilingLow, lowCeiling[1], lowCeiling[2], ceilingTop, lowCeiling[3], 1)
	box(w, wall[0], 0, wall[1], wall[2], 3, wall[3], 1)
	for i := range 3 {
		x0 := tallSteps[0] + float32(i)*(tallSteps[2]-tallSteps[0])/3
		box(w, x0, 0, tallSteps[1], x0+(tallSteps[2]-tallSteps[0])/3, tallRise*float32(i+1), tallSteps[3], 1)
	}
	box(w, tallTop[0], 0, tallTop[1], tallTop[2], tallRise*4, tallTop[3], 1)
	// A block on layer 2 in the middle of the open floor, which a layer 1 bake ignores.
	box(w, 1, 0, 6, 2.5, 3, 7.5, 2)
	g.AddPhysicsWorld(w, navbake3d.LayerAll)
	return g
}

// bakeReference bakes the reference level for referenceAgent on layer 1.
func bakeReference(t testing.TB) *navbake3d.Mesh {
	t.Helper()
	s := referenceSettings
	s.LayerMask = 1
	m, err := navbake3d.BakeMesh(referenceLevel(t), s, referenceAgent)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// insideXZ reports whether (x, z) lies in the footprint f grown by pad.
func insideXZ(f [4]float32, x, z, pad float32) bool {
	return x > f[0]-pad && x < f[2]+pad && z > f[1]-pad && z < f[3]+pad
}

// distXZ returns the XZ distance from (x, z) to the footprint f, 0 inside it.
func distXZ(f [4]float32, x, z float32) float32 {
	dx := max(f[0]-x, 0, x-f[2])
	dz := max(f[1]-z, 0, z-f[3])
	return float32(math.Sqrt(float64(dx*dx + dz*dz)))
}
