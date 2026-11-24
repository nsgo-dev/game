// Command editor is Field Medic's scene editor: the game layer's editor with the health component registered and the health plugin (package healthplugin) built in, which game.yaml turns on under editor.plugins. go tool game editor registers no plugins, so a game whose scenes are edited with its own plugins ships an editor command like this one.
//
// Run it from examples/fieldmedic with go run ./cmd/editor, or name the project directory or its game.yaml: go run ./cmd/editor path/to/fieldmedic.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/examples/fieldmedic/healthplugin"
	"github.com/nsgo-dev/game/packages/game/editor"
	"github.com/nsgo-dev/game/packages/game/sceneformat"
	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
)

func main() {
	dir := "."
	switch len(os.Args) {
	case 1:
	case 2:
		dir = os.Args[1]
	default:
		log.Fatal("usage: editor [project-dir | game.yaml]")
	}
	ed, err := New(dir)
	if err != nil {
		log.Fatal(err)
	}
	nsgo.Run(func(cx *core.Scope) core.Node {
		cx.SetTitle("Field Medic Editor")
		return editor.EditorView(cx, ed)
	}, nsgo.WindowSize(1280, 800))
}

// New returns a scene editor with Field Medic's components and plugins, on the project at path: its directory or its game.yaml. It opens the project's main scene, whose first layer is then the active layer the Admit tool places patients on, and fails when game.yaml lists a plugin this editor is built without.
func New(path string) (*editor.Editor, error) {
	// The ward draws its patients itself, so its scenes use no sprite atlases.
	ed, err := editor.New(nil, sceneformat.AtlasMap{})
	if err != nil {
		return nil, err
	}
	if err := ed.SetComponentRegistry(health.Components()); err != nil {
		return nil, err
	}
	if err := ed.RegisterPlugin(healthplugin.Plugin()); err != nil {
		return nil, err
	}
	if err := ed.OpenProject(path); err != nil {
		return nil, err
	}
	if missing := ed.MissingPlugins(); len(missing) > 0 {
		return nil, fmt.Errorf("%s lists editor plugins this editor is built without: %v", path, missing)
	}
	return ed, nil
}
