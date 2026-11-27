// Command editor is Field Medic's scene editor: the game layer's editor with the health component registered and the health plugin (package healthplugin) built in, which game.yaml turns on under editor.plugins. go tool game editor registers no plugins, so a game whose scenes are edited with its own plugins ships an editor command like this one, and go tool game editor runs it on the project.
//
// It takes the command line of package editorcmd, as go tool game editor does. Run it from examples/fieldmedic with go run ./cmd/editor, or name the project directory, its game.yaml or one of its scene files, then optionally an animation clip and the object to play it on: go run ./cmd/editor path/to/fieldmedic.
package main

import (
	"fmt"

	"github.com/nsgo-dev/game/examples/fieldmedic/health"
	"github.com/nsgo-dev/game/examples/fieldmedic/healthplugin"
	"github.com/nsgo-dev/game/pkg/editor"
	"github.com/nsgo-dev/game/pkg/editor/editorcmd"
)

func main() {
	editorcmd.Main(options())
}

// options are Field Medic's editor: its component types and plugin, its name in the window title, its window size, and a refusal to open a project that lists a plugin it lacks.
func options() editorcmd.Options {
	return editorcmd.Options{
		Components:     health.Components(),
		Plugins:        []editor.Plugin{healthplugin.Plugin()},
		AppName:        "Field Medic Editor",
		WindowWidth:    1280,
		WindowHeight:   800,
		RequirePlugins: true,
	}
}

// New returns a scene editor with Field Medic's components and plugins, on the project at path: its directory or its game.yaml. It opens the project's main scene, whose first layer is then the active layer the Admit tool places patients on, and fails when game.yaml lists a plugin this editor is built without.
func New(path string) (*editor.Editor, error) {
	ed, err := editorcmd.New(options())
	if err != nil {
		return nil, err
	}
	if err := editorcmd.Open(ed, []string{path}); err != nil {
		return nil, err
	}
	if missing := ed.MissingPlugins(); len(missing) > 0 {
		return nil, fmt.Errorf("%s lists editor plugins this editor is built without: %v", path, missing)
	}
	return ed, nil
}
