package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/packages/game/project"
)

// gameYAML is SlideGem's project file, which holds its settings: window, virtual resolution, audio bus and export presets. The board's 36 cell bindings follow its layout, so they stay in code (bindCellActions).
//
//go:embed game.yaml
var gameYAML []byte

// gameProject is the parsed game.yaml.
var gameProject = project.MustParse(gameYAML)

// gameViewport maps the window onto the virtual resolution game.yaml sets.
var gameViewport, _ = gameProject.Viewport()

// VirtualWidth and VirtualHeight define SlideGem's portrait-friendly virtual canvas, as game.yaml sets it.
var (
	VirtualWidth  = gameViewport.Width
	VirtualHeight = gameViewport.Height
)
