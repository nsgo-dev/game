// Package ai provides deterministic game AI building blocks: grid A* pathfinding, 2D navigation meshes with funnel smoothing, steering behaviors, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack token pools, engagement slot assignment, squad coordination, spatial influence maps, and EQS-style tactical position queries with cover points and reservations.
//
// Everything runs cleanly on game.Loop's fixed steps with no background goroutines and no hidden global randomness.
package ai
