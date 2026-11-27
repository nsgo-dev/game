package main

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is Field Medic's project file: its window, its main scene and the editor plugins its scenes are edited with.
//
//go:embed game.yaml
var gameYAML []byte

// sceneFiles holds the scene files game.yaml autoloads.
//
//go:embed scenes/*.scene.yaml
var sceneFiles embed.FS

// gameProject is the parsed game.yaml.
var gameProject = project.MustParse(gameYAML)

// loadMainWard loads the ward from the main scene game.yaml names.
func loadMainWard() (*Ward, error) {
	a := gameProject.Autoload
	if a == nil || a.MainScene == "" {
		return nil, fmt.Errorf("fieldmedic: game.yaml names no main scene")
	}
	for _, s := range a.Scenes {
		if s.Name != a.MainScene {
			continue
		}
		data, err := fs.ReadFile(sceneFiles, s.Path)
		if err != nil {
			return nil, err
		}
		return LoadWard(data, s.Path)
	}
	return nil, fmt.Errorf("fieldmedic: game.yaml autoloads no scene %q", a.MainScene)
}
