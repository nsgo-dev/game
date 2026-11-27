// Package ai provides deterministic game AI decision-making and movement building blocks: steering behaviors, velocity-based local avoidance (ORCA) in 2D and 3D XZ, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack token pools, engagement slot assignment, squad coordination, spatial influence maps, tactical position queries with cover points, and perception with sight, hearing and memory. Grids, A*, graphs, waypoints and 2D navigation meshes live in game/pathfind.
//
// Everything runs cleanly on game.Loop's fixed steps with no background goroutines and no hidden global randomness.
package ai
