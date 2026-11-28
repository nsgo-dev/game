package mcp3d_test

import (
	"errors"
	"testing"

	"github.com/nsgo-dev/game/pkg/mcp/mcp3d"
)

func TestFindNode(t *testing.T) {
	nodes := []mcp3d.Node{{Path: "world/ground", Name: "Ground"}, {Path: "world/player", Name: "Player"}}
	n, err := mcp3d.Find(nodes, "world/player")
	if err != nil || n.Name != "Player" {
		t.Fatalf("Find = %+v, %v; want Player", n, err)
	}
	if _, err := mcp3d.Find(nodes, "world/enemy"); !errors.Is(err, mcp3d.ErrNotFound) {
		t.Fatalf("missing node: err = %v, want ErrNotFound", err)
	}
}
