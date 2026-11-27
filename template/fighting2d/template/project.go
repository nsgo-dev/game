package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is the game's project file, which holds its settings: its name, id and version, its window, and its export presets, with a virtual resolution, an input map, audio buses and autoloads to add as the game grows. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/gameproject sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)
