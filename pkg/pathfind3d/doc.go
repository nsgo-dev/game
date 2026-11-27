// Package pathfind3d provides deterministic 3D navigation for NSGo 3D games: navigation meshes of walkable 3D polygons with slope filtering, polygon areas/flags/filters, off-mesh links, a BVH spatial index, reusable zero-allocation queries (NavMeshQuery) and incremental searches, path-corridor navigation agents and agent groups, and tiled navigation meshes with dynamic obstacles that stream in and out with world3d's chunks (NavTileStreamer).
//
// It shares NavArea, NavFlags and NavFilter with package pathfind, its 2D counterpart, and package navbake3d bakes its meshes from level geometry. All positions follow package math3d's right-handed Y-up conventions (+X right, +Y up, -Z forward).
package pathfind3d
