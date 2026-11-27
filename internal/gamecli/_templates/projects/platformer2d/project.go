package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is the game's project file, which holds its settings: its name, id and version, its window, and its export presets, with a virtual resolution, an input map, audio buses and autoloads to add as the game grows. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/game project sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)

// touchLayoutYAML is the game's on-screen touch controls: a d-pad at the bottom left and a jump button at the bottom right.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// TouchLayout is the parsed controls.touch.yaml.
var TouchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("platformer2d: controls.touch.yaml: %v", err))
	}
	return l
}
