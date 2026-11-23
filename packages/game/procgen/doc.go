// Package procgen generates seeded, deterministic 2D levels on a tile grid: BSP rooms joined by corridors, cellular-automata caves and wave function collapse over a YAML rule set.
//
// Every generator draws all of its randomness from a random.Stream, so the same seed and configuration produce the same Grid, cell for cell, on every operating system and architecture. Each generator also guarantees that its walkable cells form one 4-connected region, which Regions and Connected check; a game can run the same check on a level it edits afterwards.
//
// A Grid holds one uint32 value per cell. BSP and Cave write Wall and Floor; WFC writes indexes into its Rules' tiles. Paint copies a Grid into a tilemap.Tilemap layer through a value-to-GID function.
package procgen
