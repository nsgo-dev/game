// Package flow holds gameplay flow primitives driven by a game.Loop's fixed steps: game-clock timers, finite and hierarchical state machines, step sequences, a typed event bus and a branching dialogue runtime.
//
// Nothing in flow reads the wall clock, spawns goroutines or keeps package-level state, so every piece pauses with the loop, stays deterministic under nsgotest and isolates cleanly per scene or world.
package flow
