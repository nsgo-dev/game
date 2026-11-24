package refs_test

import (
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/packages/game/refs"
)

// PlanMove rewrites every reference to the file it moves, in place, and records the move in the project index.
func ExamplePlanMove() {
	fsys := fstest.MapFS{
		"game.yaml": {Data: []byte("version: 1\nname: Cave\nid: dev.example.cave\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n")},
		"scenes/cave.scene.yaml": {Data: []byte(`version: 2
uid: cave000000001
layers:
  - name: walls
    objects:
      - id: wall
        material: materials/stone.material.yaml # rough stone
`)},
		"materials/stone.material.yaml": {Data: []byte("type: material\nversion: 1\nuid: mat0000000001\n")},
	}
	plan, err := refs.PlanMove(fsys, "materials/stone.material.yaml", "materials/rock.material.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, r := range plan.Updated {
		fmt.Printf("%s:%d: %s %s\n", r.File, r.Line, r.Field, r.Value)
	}
	for _, w := range plan.Writes {
		fmt.Printf("--- %s\n%s", w.Path, w.Data)
	}
	// Output:
	// scenes/cave.scene.yaml:7: material materials/stone.material.yaml
	// --- game.index.yaml
	// # The stable IDs of this project's files, kept by game index, game mv and the editor. Keep it in version control.
	// version: 1
	// files:
	//   - path: materials/rock.material.yaml
	//     uid: mat0000000001
	//   - path: scenes/cave.scene.yaml
	//     uid: cave000000001
	// --- scenes/cave.scene.yaml
	// version: 2
	// uid: cave000000001
	// layers:
	//   - name: walls
	//     objects:
	//       - id: wall
	//         material: materials/rock.material.yaml # rough stone
}

// After a file moves without the tools, Check reports each reference that broke, and where the index says the file went.
func ExampleProject_Check() {
	fsys := fstest.MapFS{
		"game.yaml":               {Data: []byte("version: 1\nname: Cave\nid: dev.example.cave\nappVersion: 0.1.0\nwindow: {width: 640, height: 360}\n")},
		"scenes/a.scene.yaml":     {Data: []byte("version: 2\nlayers:\n  - name: l\n    objects:\n      - id: e\n        scene: scenes/enemy.scene.yaml\n")},
		"actors/enemy.scene.yaml": {Data: []byte("version: 2\nuid: enemy00000001\n")},
		refs.IndexFileName:        {Data: []byte("version: 1\nfiles:\n  - path: scenes/enemy.scene.yaml\n    uid: enemy00000001\n")},
	}
	p, err := refs.Scan(fsys)
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, problem := range p.Check() {
		fmt.Println(problem)
	}
	// Output:
	// scenes/a.scene.yaml:6: scene "scenes/enemy.scene.yaml": not found; it moved to "actors/enemy.scene.yaml"; run game index to update the reference
}
