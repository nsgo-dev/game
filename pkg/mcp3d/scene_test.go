package mcp3d_test

import (
	"github.com/nsgo-dev/game/pkg/mcp3d"
	"testing"
)

func TestFindNode(t *testing.T) {
	n, err := mcp3d.Find([]mcp3d.Node{{Path: "world/player", Name: "Player"}}, "world/player")
	if err != nil || n.Name != "Player" {
		t.Fatalf("%+v %v", n, err)
	}
}
