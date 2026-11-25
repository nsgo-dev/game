package navbake_test

import (
	"fmt"

	"github.com/nsgo-dev/game/packages/game/navbake"
	"github.com/nsgo-dev/game/packages/game/tilemap"
	"github.com/nsgo-dev/nsgo/core"
)

// Bake shrinks a room with a pillar by the agent's radius, and the mesh's path keeps that far from the pillar.
func ExampleBake() {
	src := navbake.Source{
		Outlines:  [][]core.Point{{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 60}, {X: 0, Y: 60}}},
		Obstacles: [][]core.Point{{{X: 40, Y: 20}, {X: 60, Y: 20}, {X: 60, Y: 40}, {X: 40, Y: 40}}},
	}
	mesh, err := navbake.Bake(src, navbake.Agent{Name: "guard", Radius: 4}, navbake.Options{})
	if err != nil {
		panic(err)
	}
	path, ok := mesh.NavMesh().FindPath(core.Point{X: 10, Y: 30}, core.Point{X: 90, Y: 30})
	fmt.Println(len(mesh.Polygons), ok)
	for _, p := range path {
		fmt.Printf("(%.2f, %.2f)\n", p.X, p.Y)
	}
	// Output:
	// 16 true
	// (10.00, 30.00)
	// (37.73, 16.60)
	// (39.20, 15.99)
	// (60.80, 15.99)
	// (62.27, 16.60)
	// (90.00, 30.00)
}

// FromTilemap turns a tilemap's solid cells into obstacles, and BakeAll bakes one mesh per agent type.
func ExampleFromTilemap() {
	tm := tilemap.New(6, 4, 16, 16)
	for col := 1; col < 5; col++ {
		tm.SetCellFlags(col, 2, tilemap.TileSolid)
	}
	src, err := navbake.FromTilemap(tm, tilemap.TileSolid)
	if err != nil {
		panic(err)
	}
	meshes, err := navbake.BakeAll(src, []navbake.Agent{{Name: "rat", Radius: 2}, {Name: "ogre", Radius: 12}}, navbake.Options{})
	if err != nil {
		panic(err)
	}
	for _, m := range meshes {
		_, ok := m.NavMesh().FindPath(core.Point{X: 48, Y: 8}, core.Point{X: 48, Y: 56})
		fmt.Println(m.Agent, len(m.Polygons), ok)
	}
	// Output:
	// rat 16 true
	// ogre 3 false
}

// A baked mesh saves to YAML, one polygon per line.
func ExampleMesh_Encode() {
	src := navbake.Source{Outlines: [][]core.Point{{{X: 0, Y: 0}, {X: 40, Y: 0}, {X: 40, Y: 20}, {X: 20, Y: 20}, {X: 20, Y: 40}, {X: 0, Y: 40}}}}
	mesh, err := navbake.Bake(src, navbake.Agent{Name: "small", Radius: 2}, navbake.Options{})
	if err != nil {
		panic(err)
	}
	data, err := mesh.Encode()
	if err != nil {
		panic(err)
	}
	fmt.Print(string(data))
	// Output:
	// version: 1
	// agent: small
	// radius: 2
	// polygons:
	//   - [[2.0078125, 2.0078125], [37.992188, 2.0078125], [37.992188, 17.992188], [19.601562, 17.992188]]
	//   - [[2.0078125, 2.0078125], [19.601562, 17.992188], [18.863281, 18.296875]]
	//   - [[2.0078125, 2.0078125], [18.863281, 18.296875], [18.296875, 18.863281]]
	//   - [[2.0078125, 2.0078125], [18.296875, 18.863281], [17.992188, 19.601562]]
	//   - [[2.0078125, 2.0078125], [17.992188, 19.601562], [17.992188, 37.992188], [2.0078125, 37.992188]]
}
