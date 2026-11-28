# Agent Note: Procedural placement determinism

Status: implemented

## Problem

Procedural placement needed a reviewable YAML contract and stable output so generated prefab points could be regenerated after edits on every supported architecture.

## Decision

`procgen.Placement` uses strict version-1 YAML with ordered grid steps, a global seed, and per-step forked random streams. `Points` orders cells canonically and keeps sampled subsets sorted by Y then X. Parsing rejects unknown fields and malformed steps.

## Alternatives considered

Sharing one mutable random stream across steps would make unrelated edits move existing placements. Unseeded sampling would make diffs and bug reports irreproducible.

## Consequences

Placement files round-trip and produce deterministic golden point sets. Surface, tile, path, Poisson, filters, editor previews, and runtime spawning remain follow-up extensions.
