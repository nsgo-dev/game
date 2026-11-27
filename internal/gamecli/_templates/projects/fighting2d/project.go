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

// touchLayoutYAML is the single-player on-screen touch layout: P1's floating stick at bottom left and L/H buttons at bottom right.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// versusTouchLayoutYAML is player 1's compacted versus touch layout on the left half of the screen; player 2 uses its mirrored copy on the right half.
//
//go:embed versus.touch.yaml
var versusTouchLayoutYAML []byte

// touchLayout and versusTouchLayout are the parsed touch layouts.
var (
	touchLayout       = mustParseTouchLayout("controls.touch.yaml", touchLayoutYAML)
	versusTouchLayout = mustParseTouchLayout("versus.touch.yaml", versusTouchLayoutYAML)
)

func mustParseTouchLayout(name string, data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("fighting2d: %s: %v", name, err))
	}
	return l
}
