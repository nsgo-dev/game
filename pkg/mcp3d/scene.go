// Package mcp3d defines optional records and callbacks for runtime 3D MCP
// inspection without importing the renderer into pkg/mcp.
package mcp3d

import "errors"

var ErrNotFound = errors.New("mcp3d: node not found")

type Vec3 struct{ X, Y, Z float32 }
type Node struct {
	Path, Kind, Name     string
	Position, Scale      Vec3
	BoundsMin, BoundsMax Vec3
	Model, Material      string
}
type Camera struct {
	Position, Forward Vec3
	Projection        string
	FOV               float32
}
type Hit struct {
	NodePath      string
	Point, Normal Vec3
	Distance      float32
}
type Provider interface {
	Nodes() []Node
	Camera() Camera
	Raycast(origin, direction Vec3) (Hit, bool)
}

func Find(nodes []Node, path string) (Node, error) {
	for _, n := range nodes {
		if n.Path == path {
			return n, nil
		}
	}
	return Node{}, ErrNotFound
}
