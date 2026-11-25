package editorcmd_test

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nsgo-dev/game/packages/game/editor/editorcmd"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
)

// A game's editor command builds its editor with the game's component types, and opens a project with them.
func ExampleOpen() {
	dir, err := os.MkdirTemp("", "editorcmd")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer os.RemoveAll(dir)
	files := map[string]string{
		"game.yaml":              "version: 1\nname: Demo\nid: dev.example.demo\nappVersion: 1.0.0\nwindow:\n  width: 320\n  height: 180\nautoload:\n  mainScene: room\n  scenes:\n    - name: room\n      path: scenes/room.scene.yaml\n",
		"scenes/room.scene.yaml": "version: 2\nname: Room\nlayers:\n  - name: main\n    objects:\n      - id: hero\n        components:\n          health: {hp: 3}\n",
	}
	for name, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Println(err)
			return
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			fmt.Println(err)
			return
		}
	}

	components := sceneformat.NewComponentRegistry()
	components.MustRegister(sceneformat.ComponentType{Name: "health", Fields: []sceneformat.Field{{Name: "hp", Kind: sceneformat.FieldInt, Default: 1}}})
	ed, err := editorcmd.New(editorcmd.Options{Components: components})
	if err != nil {
		fmt.Println(err)
		return
	}
	if err := editorcmd.Open(ed, []string{dir}); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(ed.Doc.Name, ed.ScenePath)
	hp, _ := ed.ComponentField("hero", "health", "hp")
	fmt.Println(hp)
	// Output:
	// Room scenes/room.scene.yaml
	// 3
}
