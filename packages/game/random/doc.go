// Package random provides deterministic, cross-platform pseudo-random streams and helpers for verifying simulation determinism.
//
// Each Stream implements Xoshiro256** seeded through SplitMix64 with fixed, specification-stable arithmetic, so a given seed produces the exact same sequence of bits, integers and floats on every Go version, operating system and architecture.
//
// A game derives independent child streams by name with Fork or keeps them together in Streams, snapshots and restores generator state for saves and replays with State and SetState, and tests simulation repeatability with CompareRuns, CheckSteps and AssertDeterministic.
package random
