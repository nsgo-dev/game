package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is the game's project file: its name, id and version, its window, the virtual resolution it draws at, the input map that binds keys and gamepad buttons to its actions, and its export presets. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/game project sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)

// touchLayoutYAML is the game's on-screen touch controls: a floating stick on moveX and moveY at the bottom left and A and B buttons at the bottom right.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// TouchLayout is the parsed controls.touch.yaml.
var TouchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("rpg2d: controls.touch.yaml: %v", err))
	}
	return l
}
