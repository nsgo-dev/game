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

func TestProviderRaycastContract(t *testing.T) {
	p := fake3D{nodes: []mcp3d.Node{{Path: "world/ground"}, {Path: "world/player"}}}
	if len(p.Nodes()) != 2 || p.Camera().Projection != "perspective" {
		t.Fatalf("provider = %+v / %+v", p.Nodes(), p.Camera())
	}
	hit, ok := p.Raycast(mcp3d.Vec3{}, mcp3d.Vec3{Z: 1})
	if !ok || hit.NodePath != "world/player" {
		t.Fatalf("raycast = %+v, %v", hit, ok)
	}
}

type fake3D struct{ nodes []mcp3d.Node }

func (f fake3D) Nodes() []mcp3d.Node { return f.nodes }
func (fake3D) Camera() mcp3d.Camera {
	return mcp3d.Camera{Projection: "perspective", Forward: mcp3d.Vec3{Z: 1}}
}
func (f fake3D) Raycast(_, _ mcp3d.Vec3) (mcp3d.Hit, bool) {
	return mcp3d.Hit{NodePath: f.nodes[1].Path, Distance: 1}, true
}
