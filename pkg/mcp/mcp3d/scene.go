// Package mcp3d defines optional serializable records and a provider contract for inspecting a running 3D scene over MCP: nodes, the active camera and raycast hits. It imports no renderer, scene3d or physics3d package, so pkg/mcp stays free of 3D code and the game keeps ownership of rendering and physics. Registering MCP tools on top of it is not part of this package yet.
package mcp3d

import "errors"

// ErrNotFound reports that no node has the requested path.
var ErrNotFound = errors.New("mcp3d: node not found")

// Vec3 is a position, direction or scale in world space.
type Vec3 struct{ X, Y, Z float32 }

// Node is one node of the 3D scene graph. Path is unique within the scene; BoundsMin and BoundsMax are the node's world-space axis-aligned bounds, and Model and Material name the assets it draws with, if any.
type Node struct {
	Path, Kind, Name     string
	Position, Scale      Vec3
	BoundsMin, BoundsMax Vec3
	Model, Material      string
}

// Camera is the active camera's pose and projection. Projection is "perspective" or "orthographic"; FOV is the vertical field of view in degrees for a perspective camera.
type Camera struct {
	Position, Forward Vec3
	Projection        string
	FOV               float32
}

// Hit is the nearest node a ray hits, with the world-space hit point and surface normal and the distance along the ray.
type Hit struct {
	NodePath      string
	Point, Normal Vec3
	Distance      float32
}

// Provider is implemented by the game over its own scene, camera and physics. Raycast reports false when the ray hits nothing.
type Provider interface {
	Nodes() []Node
	Camera() Camera
	Raycast(origin, direction Vec3) (Hit, bool)
}

// Find returns the node whose path equals path, or ErrNotFound.
func Find(nodes []Node, path string) (Node, error) {
	for _, n := range nodes {
		if n.Path == path {
			return n, nil
		}
	}
	return Node{}, ErrNotFound
}
