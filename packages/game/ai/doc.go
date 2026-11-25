// Package ai provides deterministic game AI building blocks: grid A* pathfinding (with line-of-sight smoothing, bounded expansion budgets, partial paths, incremental searches, and budgeted request queues), HPA* hierarchical pathfinding with incremental cluster updates, Jump Point Search (JPS) for uniform-cost grids, 2D navigation meshes with funnel smoothing, steering behaviors, behavior trees, utility AI scoring, goal-oriented action planning (GOAP), attack token pools, engagement slot assignment, squad coordination, spatial influence maps, tactical position queries with cover points, and perception with sight, hearing and memory.
//
// Everything runs cleanly on game.Loop's fixed steps with no background goroutines and no hidden global randomness.
package ai
