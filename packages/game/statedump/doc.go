// Package statedump runs a game headless for a fixed number of steps and writes its entities, components and scene state as deterministic YAML, so an agent or a test can diff one run against another.
//
// A Dump holds the run's seed, rate and step count, the scene stack (Scene), named game-wide values outside the entity world (Values), and every live entity with its hierarchy parent and its codec-registered components, as entities.World.Snapshot records them.
//
// Encode writes a Dump as version 1 YAML: fixed key order, entities by ascending slot index, components by ascending codec name, values by ascending byte order of their names, and component and value data turned from their JSON encoding into YAML mappings and sequences that keep the JSON field order and the exact number text. Two runs with the same seed and inputs on the same CPU architecture therefore write byte-identical files (a game's own float math can differ between arm64 and amd64, where the compiler may fuse a multiply and an add), and a change shows as a few changed lines in a line diff. Parse reads such a file back, refusing unknown fields, other versions, trailing documents and data JSON cannot hold, and Encode(Parse(b)) returns b for every b that Encode wrote.
//
// Run plays a replay.Recording against a simulation without a window and captures the Dump at the end; Capture takes one at any point; Diff prints a unified line diff of two dumps for a test failure or an agent's report.
package statedump
