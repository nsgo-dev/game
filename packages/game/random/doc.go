// Package random provides deterministic, cross-platform pseudo-random streams, procedural noise generators, and helpers for verifying simulation determinism.
//
// Each Stream implements Xoshiro256** seeded through SplitMix64 with fixed, specification-stable arithmetic, so a given seed produces the exact same sequence of bits, integers and floats on every Go version, operating system and architecture.
//
// Noise provides deterministic 2D and 3D value and simplex noise, multi-octave fractals (FBM, ridged, billow, domain warping), and zero-allocation 2D grid-fill helpers using pure integer hashing and explicit IEEE-754 float64 arithmetic for bit-identical procedural generation across arm64, x86_64 and WebAssembly.
//
// A game derives independent child streams by name with Fork or keeps them together in Streams, snapshots and restores generator state for saves and replays with State and SetState, and tests simulation repeatability with CompareRuns, CheckSteps and AssertDeterministic.
package random
