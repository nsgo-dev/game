// Package procgen generates seeded, deterministic 2D levels on a tile grid: BSP rooms joined by corridors, cellular-automata caves and wave function collapse over a YAML rule set.
//
// Every generator draws all of its randomness from a random.Stream, so the same seed and configuration produce the same Grid, cell for cell, on every operating system and architecture. BSP and Cave always leave their Floor cells as one 4-connected region, and so does WFC unless WFCConfig.AllowDisconnected is set or its rules have no walkable tile; Regions and Connected check this, and a game can run the same check on a level it edits afterwards.
//
// A Grid holds one uint32 value per cell. BSP and Cave write Wall and Floor; WFC writes indexes into its Rules' tiles. Paint copies a Grid into a tilemap.Tilemap layer through a value-to-GID function.
package procgen
