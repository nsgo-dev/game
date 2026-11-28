package main

import (
	_ "embed"
	"github.com/nsgo-dev/game/pkg/project"
)

//go:embed game.yaml
var gameYAML []byte

var Project = project.MustParse(gameYAML)
