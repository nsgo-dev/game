package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is Starburst's project file, which holds its settings: window, virtual resolution, input map and export presets.
//
//go:embed game.yaml
var gameYAML []byte

// gameProject is the parsed game.yaml.
var gameProject = project.MustParse(gameYAML)

// gameViewport maps the window onto the virtual resolution game.yaml sets.
var gameViewport, _ = gameProject.Viewport()

// VirtualWidth and VirtualHeight define Starburst's crisp 16:9 pixel-art canvas, as game.yaml sets it.
var (
	VirtualWidth  = gameViewport.Width
	VirtualHeight = gameViewport.Height
)

// touchLayoutYAML is Starburst's on-screen touch controls: floating twin sticks for movement and aim/fire, and a round Nova button above the aim stick.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// touchLayout is the parsed controls.touch.yaml.
var touchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("starburst: controls.touch.yaml: %v", err))
	}
	return l
}
