# Agent Note: Particle effect file contract

Status: implemented

## Problem

An explosion made of several emitters and runtime-tunable values previously required separate scene objects and Go-built configs. A shared file format was needed before playback, parameter binding and event delivery could be added.

## Decision

`pkg/particles` stores versioned effect files with ordered emitters, inline or file-backed configs, offsets, delays, durations, per-emitter minimum quality and typed float, int, color and vec2 parameters. Marshal and unmarshal are strict and deterministic, and `SetParam` validates values without partial mutation.

## Alternatives considered

Extending a single emitter file with nested emitters would make existing emitter files ambiguous and complicate tooling. Keeping effect definitions as Go structs would prevent editor previews and stable diffs.

## Consequences

Effects can be reviewed, cached and parameterized in text before a runtime player exists. Playback, binding emitter fields, particle events and culling remain follow-up behavior layers that can consume this stable schema.

## Verification

Existing particle effect tests cover byte-for-byte YAML round trips, strict invalid-file refusal, typed parameter mutation and persistence. Targeted tests and `go tool nsgo check` pass.
