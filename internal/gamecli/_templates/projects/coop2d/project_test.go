package main

import (
	"github.com/nsgo-dev/game/pkg/project"
	"testing"
)

func TestProjectFileIsValid(t *testing.T) {
	if err := project.Validate(Project); err != nil {
		t.Fatal(err)
	}
}
