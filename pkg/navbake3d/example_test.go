package navbake3d_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/math3d"
	"github.com/nsgo-dev/game/pkg/navbake3d"
	"github.com/nsgo-dev/game/pkg/physics3d"
)

// A 10 by 10 metre floor with a pillar in the middle bakes into a mesh whose paths go round the pillar, keeping the agent's radius from it.
func Example() {
	w := physics3d.NewWorld()
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{Y: -0.2}, physics3d.Vec3{X: 10, Z: 10})), physics3d.Vec3{}))
	w.AddBody(physics3d.NewStaticBody(physics3d.BoxShape(physics3d.BoxFromMinMax(physics3d.Vec3{X: 4, Z: 4}, physics3d.Vec3{X: 6, Y: 3, Z: 6})), physics3d.Vec3{}))

	var g navbake3d.Geometry
	g.AddPhysicsWorld(w, navbake3d.LayerAll)
	f, err := navbake3d.Bake(&g, navbake3d.Settings{CellSize: 0.1, CellHeight: 0.05}, []navbake3d.AgentType{{Name: "humanoid", Radius: 0.3, Height: 1.8}})
	if err != nil {
		fmt.Println(err)
		return
	}
	mesh, _ := f.Mesh("humanoid")
	nav := mesh.NavMesh()

	path, ok := nav.FindPath(math3d.Vec3{X: 2, Z: 5}, math3d.Vec3{X: 8, Z: 5})
	fmt.Println("path found:", ok)
	clear := true
	for _, p := range path {
		if p.X > 3.75 && p.X < 6.25 && p.Z > 3.75 && p.Z < 6.25 {
			clear = false
		}
	}
	fmt.Println("keeps clear of the pillar:", clear)
	_, inside := nav.FindPolygon(math3d.Vec3{X: 5, Z: 5})
	fmt.Println("walkable inside the pillar:", inside)
	fmt.Println("agent types:", len(f.Meshes), f.Meshes[0].Agent.Name)
	// Output:
	// path found: true
	// keeps clear of the pillar: true
	// walkable inside the pillar: false
	// agent types: 1 humanoid
}

// Parse reads a navigation mesh file, and Mesh.NavMesh turns an agent type's mesh into a world3d.NavMesh to find paths on.
func ExampleParse() {
	data := []byte(`version: 1
settings:
  cellSize: 0.3
  cellHeight: 0.2
  minRegionArea: 1
  maxEdgeLength: 12
  maxSimplificationError: 0.4
  maxVertsPerPoly: 6
  detailSampleDistance: 1.8
  detailSampleMaxError: 0.2
  layerMask: 4294967295
meshes:
  - agent:
      name: humanoid
      radius: 0.6
      height: 2
      maxSlope: 45
      maxClimb: 0.9
    vertices:
      - [0, 0, 0]
      - [0, 0, 4]
      - [4, 1, 4]
      - [4, 1, 0]
    polygons:
      - vertices: [0, 1, 2, 3]
        triangles:
          - [0, 1, 2]
          - [0, 2, 3]
`)
	f, err := navbake3d.Parse(data)
	if err != nil {
		fmt.Println(err)
		return
	}
	mesh, _ := f.Mesh("humanoid")
	nav := mesh.NavMesh()
	path, ok := nav.FindPath(math3d.Vec3{X: 0.5, Z: 2}, math3d.Vec3{X: 3.5, Y: 1, Z: 2})
	fmt.Println(ok, len(nav.Polys))
	fmt.Printf("climbs to %.3f\n", path[len(path)-1].Y)
	// Output:
	// true 2
	// climbs to 0.875
}
