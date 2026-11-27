package main

import (
	_ "embed"

	"github.com/nsgo-dev/game/pkg/project"
)

// gameYAML is the game's project file, which holds its settings: its name, id and version, its window, its input map and its export presets. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/game project sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)

// arena is the parsed arena.yaml: the layout, the player, the weapons, the enemies, the pickups and the waves.
var arena = mustParseArena(arenaYAML)
