package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/project"
	"github.com/nsgo-dev/nsgo/core"
)

// gameYAML is Starburst's project file, which holds its settings: window, virtual resolution and input map.
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

// projectRegion returns the on-screen control region game.yaml binds to action, which the HUD draws.
func projectRegion(action string) core.Bounds {
	r, ok := gameProject.Region(action)
	if !ok {
		panic(fmt.Sprintf("starburst: game.yaml binds no region to action %q", action))
	}
	return r
}
