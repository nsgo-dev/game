// Package navbake bakes 2D navigation meshes for game/pathfind from level geometry: an outline with concave obstacles and holes, or a tilemap's solid cells, shrunk by an agent's radius and divided into the convex polygons pathfind.NavMesh searches.
//
// A bake snaps the geometry to a fine grid and works with exact integer predicates, so it never fails on touching or overlapping obstacles and gives the same polygons for the same input on every platform. Its result, a Mesh, saves to a versioned YAML file that loads back and saves to the same bytes, and Draw shows it as a debug overlay.
package navbake
