// Package ai provides deterministic game AI building blocks: grid A* pathfinding (with line-of-sight smoothing, bounded expansion budgets, partial paths, incremental searches, and budgeted request queues), 2D navigation meshes with funnel smoothing, steering behaviors, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack token pools, engagement slot assignment, squad coordination, spatial influence maps, and tactical position queries with cover points.
//
// Everything runs cleanly on game.Loop's fixed steps with no background goroutines and no hidden global randomness.
package ai
