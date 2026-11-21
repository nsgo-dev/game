// Package entities provides a small, zero-reflection entity-component-system (ECS) for NSGo games: generational entities, typed sparse-set components, allocation-free queries, topologically ordered systems, prefabs and JSON world snapshots integrated with game/save.
//
// # Architectural Decision (#136)
//
// Three architectures were weighed for representing game objects in packages/game:
//
//  1. A small, zero-reflection, generational-index ECS with typed sparse-set storage (chosen).
//  2. An object-oriented scene tree of heap-allocated nodes with virtual update methods.
//  3. A reflection-driven archetype ECS with dynamic tables and query planners.
//
// This package chooses (1) for five reasons:
//
//   - Predictable, allocation-free hot loops: entities are 64-bit generational handles (32-bit slot index + 32-bit generation) recycled through a free list, and each component type lives in a contiguous typed sparse set. Spawning, destroying, accessing and querying components perform zero heap allocations in steady state and avoid GC pointer chasing.
//   - Zero reflection: Go generics identify component stores via zero-sized type tags and keep all component access, queries and prefab builders statically typed with no reflect imports.
//   - Safe handle lifetimes: stale Entity handles are automatically invalidated when a slot is destroyed and recycled, eliminating dangling references without weak pointers or runtime bookkeeping.
//   - Explicit, deterministic execution: systems declare Before/After ordering constraints resolved once via Kahn's topological sort with registration-order tie-breaking, making frame order inspectable (for debug overlays) and reproducible across runs and platforms.
//   - Clean serialization boundaries: only component types explicitly registered with RegisterCodec are written into deterministic WorldSnapshot JSON values and persisted through game/save, keeping transient runtime state (such as sprite handles or audio voices) out of save files while preserving entity IDs and cross-entity references.
//
// All methods on World and Scheduler are designed to be called on the engine's goroutine, matching game.Loop and NSGo's core.Scope.
package entities
