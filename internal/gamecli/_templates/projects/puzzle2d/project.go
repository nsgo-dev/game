package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is the game's project file, which holds its settings: its name, id and version, its window, its input map, which binds the keyboard and game controllers to the game's actions, and its export presets. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/game project sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)
