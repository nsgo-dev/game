// Package procgen generates seeded, deterministic 2D levels on a tile grid: BSP rooms joined by corridors, cellular-automata caves, wave function collapse over a YAML rule set, and hand-made room templates placed on a dungeon layout graph.
//
// Every generator draws all of its randomness from a random.Stream, so the same seed and configuration produce the same Grid, cell for cell, on every operating system and architecture. BSP, Cave and GenerateRooms always leave their walkable cells as one 4-connected region, and so does WFC unless WFCConfig.AllowDisconnected is set or its rules have no walkable tile; Regions and Connected check this, and a game can run the same check on a level it edits afterwards.
//
// A Grid holds one uint32 value per cell. BSP, Cave and GenerateRooms write Wall and Floor (room templates may also use custom tile values); WFC writes indexes into its Rules' tiles. Paint copies a Grid into a tilemap.Tilemap layer through a value-to-GID function, and GridFromTilemap reads a tilemap layer back into a Grid.
package procgen
