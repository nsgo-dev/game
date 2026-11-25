package main

import (
	_ "embed"
	"fmt"

	"github.com/nsgo-dev/game/packages/game/platformer"
	"github.com/nsgo-dev/game/packages/game/rooms"
)

// The game's data files, embedded so the game runs the same on every target without reading files at run time. Edit them and rebuild; the tests check that each one is valid and canonical.
var (
	// levelYAML is the level: its tiles, tile size and lives, in the format Level describes.
	//
	//go:embed data/level.yaml
	levelYAML []byte
	// roomsYAML splits the level into rooms, each with its own camera bounds and the transition the camera plays on entering it, in package rooms' format.
	//
	//go:embed data/rooms.yaml
	roomsYAML []byte
	// playerYAML tunes the player's controller: run speed, jump, gravity, coyote time and jump buffering, in package platformer's format.
	//
	//go:embed data/player.yaml
	playerYAML []byte
)

// gameData is the parsed contents of the data files.
type gameData struct {
	level  *Level
	rooms  *rooms.Map
	player platformer.Config
}

// loadData parses the embedded data files.
func loadData() (gameData, error) {
	lvl, err := parseLevel(levelYAML)
	if err != nil {
		return gameData{}, fmt.Errorf("data/level.yaml: %w", err)
	}
	rm, err := rooms.Parse(roomsYAML)
	if err != nil {
		return gameData{}, fmt.Errorf("data/rooms.yaml: %w", err)
	}
	cfg, err := platformer.ParseConfig(playerYAML)
	if err != nil {
		return gameData{}, fmt.Errorf("data/player.yaml: %w", err)
	}
	return gameData{level: lvl, rooms: rm, player: cfg}, nil
}

// mustLoadData is loadData for data embedded in the binary, which the tests keep valid.
func mustLoadData() gameData {
	d, err := loadData()
	if err != nil {
		panic(err)
	}
	return d
}
