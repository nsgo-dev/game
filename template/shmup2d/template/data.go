package main

import (
	"embed"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/bullets"
	"github.com/nsgo-dev/game/packages/game/particles"
	"github.com/nsgo-dev/game/packages/game/sprite"
)

// dataFS holds the game's data files, each YAML that a designer edits without touching Go: the stage (stage.yaml), the bullet patterns (patterns.yaml) and the explosion particles (explosion.yaml).
//
//go:embed data/*.yaml
var dataFS embed.FS

// gameData is everything the game loads once at startup to start a run.
type gameData struct {
	stage     *stageFile
	patterns  *bullets.File
	explosion particles.Config
	atlas     *sprite.Atlas
}

// loadData reads and checks the data files and paints the atlas.
func loadData() (*gameData, error) {
	stageYAML, err := dataFS.ReadFile("data/stage.yaml")
	if err != nil {
		return nil, err
	}
	stage, err := parseStage(stageYAML)
	if err != nil {
		return nil, fmt.Errorf("data/stage.yaml: %w", err)
	}
	patternsYAML, err := dataFS.ReadFile("data/patterns.yaml")
	if err != nil {
		return nil, err
	}
	patterns, err := bullets.Parse(patternsYAML)
	if err != nil {
		return nil, fmt.Errorf("data/patterns.yaml: %w", err)
	}
	explosion, err := particles.LoadConfigFS(dataFS, "data/explosion.yaml")
	if err != nil {
		return nil, fmt.Errorf("data/explosion.yaml: %w", err)
	}
	return &gameData{stage: stage, patterns: patterns, explosion: explosion, atlas: makeAtlas()}, nil
}

// newRun starts a run of the stage on a playfield of width by height points.
func (d *gameData) newRun(width, height float32) (*world, error) {
	return newWorld(d.stage, d.patterns, d.atlas, d.explosion, width, height)
}
