package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/packages/game/project"
)

// gameYAML is the game's project file: its name, id and version, its window, the virtual resolution it draws at, the input map that binds keys, gamepad buttons and the touch pad to its actions, and its export presets. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/packages/game/cmd/gameproject sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)
