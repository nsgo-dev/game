package main

import (
	"embed"
	"errors"
	"fmt"

	"github.com/nsgo-dev/game/pkg/input"
	"github.com/nsgo-dev/game/pkg/project"
	"github.com/nsgo-dev/game/pkg/sceneformat"
)

// gameYAML is the game's project file, which holds its settings: its name, id and version, its window, its input map, the scene and data files it autoloads, and its export presets. nsgo.yaml repeats some of them; after editing game.yaml, run go run github.com/nsgo-dev/game/cmd/game project sync to update it.
//
//go:embed game.yaml
var gameYAML []byte

// gameFiles holds the files game.yaml autoloads, embedded so the game runs the same on every target without reading files at run time: the level scene and the actors file. Edit them and rebuild; the tests check that each is valid and canonical.
//
//go:embed scenes data
var gameFiles embed.FS

// Project is the game's parsed project file.
var Project = project.MustParse(gameYAML)

// touchLayoutYAML is the game's on-screen touch controls: floating move and camera sticks, and attack and jump buttons anchored inside the safe area.
//
//go:embed controls.touch.yaml
var touchLayoutYAML []byte

// touchLayout is the parsed controls.touch.yaml.
var touchLayout = mustParseTouchLayout(touchLayoutYAML)

func mustParseTouchLayout(data []byte) input.TouchLayout {
	l, err := input.ParseTouchLayout(data)
	if err != nil {
		panic(fmt.Sprintf("action3d: controls.touch.yaml: %v", err))
	}
	return l
}

// actorsResource is the name game.yaml's autoload gives the actors file.
const actorsResource = "actors"

// content is the game's loaded data: the actors file, the main scene built into a level, and the component types the scene uses.
type content struct {
	actors *Actors
	level  *level
	reg    *sceneformat.ComponentRegistry
}

// loadContent loads what game.yaml autoloads from gameFiles and builds the main scene.
func loadContent() (*content, error) {
	loaded, err := Project.LoadAutoloads(gameFiles)
	if err != nil {
		return nil, err
	}
	doc, ok := loaded.Scenes[loaded.MainScene]
	if !ok {
		return nil, fmt.Errorf("game.yaml: main scene %q is not autoloaded", loaded.MainScene)
	}
	raw, ok := loaded.Resources[actorsResource]
	if !ok {
		return nil, errors.New("game.yaml: the actors resource is not autoloaded")
	}
	a, err := parseActors(raw)
	if err != nil {
		return nil, err
	}
	reg := levelComponents()
	l, err := buildLevel(doc, reg)
	if err != nil {
		return nil, fmt.Errorf("scene %q: %w", loaded.MainScene, err)
	}
	return &content{actors: a, level: l, reg: reg}, nil
}
