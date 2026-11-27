package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is Cave Run's project file, which holds its settings: window, virtual resolution, input map, audio buses, autoloaded level and export presets.
//
//go:embed game.yaml
var gameYAML []byte

// gameProject is the parsed game.yaml.
var gameProject = project.MustParse(gameYAML)

// gameViewport maps the window onto the virtual resolution game.yaml sets.
var gameViewport, _ = gameProject.Viewport()

// virtualWidth and virtualHeight are the virtual canvas size in game points.
var (
	virtualWidth  = gameViewport.Width
	virtualHeight = gameViewport.Height
)

// touchLayoutYAML is Cave Run's on-screen touch controls: a d-pad at the bottom left and a large jump button at the bottom right.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// touchLayout is the parsed controls.touch.yaml.
var touchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("caverun: controls.touch.yaml: %v", err))
	}
	return l
}
