package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
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

// touchLayoutYAML is the game's on-screen touch controls: floating move and look sticks, and fire, jump, reload, swap and weapon slot buttons anchored inside the safe area.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// touchLayout is the parsed controls.touch.yaml.
var touchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("fps3d: controls.touch.yaml: %v", err))
	}
	return l
}
