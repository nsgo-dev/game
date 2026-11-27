package project_test

import (
	"fmt"
	"testing/fstest"

	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// A Resolver over a project's files builds its scenes with the project's own atlases, found by name nearest the scene as `game check` finds them.
func ExampleResolver() {
	fsys := fstest.MapFS{
		"sprites/actors.json": {Data: []byte(`{"frames": {"hero": {"frame": {"x": 0, "y": 0, "w": 16, "h": 16}}}, "meta": {"image": "actors.png"}}`)},
		"sprites/actors.png":  {Data: []byte("the packed image")},
		"scenes/room.scene.yaml": {Data: []byte(`version: 2
layers:
  - name: actors
    objects:
      - id: hero
        sprite: {atlas: actors, frame: hero}
`)},
	}
	r := project.NewResolver(fsys).ForScene("scenes/room.scene.yaml")
	fmt.Println(r.AtlasNames())

	doc, err := r.Scene("scenes/room.scene.yaml")
	if err != nil {
		fmt.Println(err)
		return
	}
	built, err := sceneformat.Build(doc, r)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%s\n", built.Object("hero").Atlas.Image.Data)
	// Output:
	// [actors]
	// the packed image
}
