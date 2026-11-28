# Agent Note: Procedural placement pipeline

Status: implemented

## Problem

The procedural package generated tile layouts but had no text-first way to describe deterministic prefab placement points across a scene.

## Decision

`procgen.Placement` parses strict version 1 YAML steps with named samplers and a seed. The initial `grid` sampler uses `random.Stream` and returns a stable sorted point set with prefab IDs.

## Alternatives considered

Hand-placing props was rejected because terrain edits require repeated manual work. Using architecture-dependent math/rand was rejected because generated scenes must reproduce in reviews and replays.

## Consequences

Games and editor tooling have a deterministic placement contract that can grow with surface, path and exclusion samplers. Scene writing and advanced samplers remain follow-up work.
