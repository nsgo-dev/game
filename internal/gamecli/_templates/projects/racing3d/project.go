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

// touchLayoutYAML is the game's on-screen touch controls: a floating steering stick at the bottom left and gas, brake and respawn buttons at the bottom right, anchored inside the safe area.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// touchLayout is the parsed controls.touch.yaml.
var touchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("racing3d: controls.touch.yaml: %v", err))
	}
	return l
}
