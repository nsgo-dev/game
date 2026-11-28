# Agent Note: Procedural placement file and grid sampler

Status: implemented

## Problem

`pkg/procgen` generates tile layouts and `pkg/world3d` scatters foliage meshes, but nothing places prefabs (a chest, a spawner, a lamp post) by rules from a file a designer and a reviewer can read in a diff. Issue #1729 proposes a versioned `*.placement.yaml` pipeline of samplers, filters, transforms and spawns; this change lands the format and its first sampler only.

## Decision

`procgen.Placement` is a version 1 YAML format (`PlacementVersion`) with a `seed` and an ordered list of `steps`, each with a unique `id`, a `sampler`, an optional `spacing` and `count`, and a required `prefab`. `ParsePlacement` refuses unknown fields, trailing documents, unknown samplers and invalid values with `loaderr` errors that name the field; `EncodePlacement` writes canonical 2-space YAML, and the format has a conformance test and an entry on the File Formats pages (`procgen#placement`).

- **Only `grid` is accepted.** The parser refuses `poisson`, `surface`, `tiles`, `shape` and `path` until they are implemented. Accepting a name that `Points` then silently skips would let a file load and place nothing, which looks like a working file in review.
- **A grid step is a lattice, not random cells.** It places a point at every cell whose coordinates are multiples of `spacing`, and a positive `count` keeps that many of them with `random.Shuffle`. Drawing `count` random cells instead, as the first version of this change did, repeats cells and gives a step named grid no grid.
- **One stream per step.** Each step forks `random.New(seed).Fork(id)`, as the issue requires, so adding, removing or reordering steps does not move the other steps' points; a single shared stream would shift every later step when one step changes.
- **Integer cells.** `spacing` is an integer number of cells and points are lattice coordinates converted to float32, so no float arithmetic can differ between amd64 and arm64 (Go may fuse a multiply and add on arm64).
- **Order.** `Points` returns steps in file order and each step's points by Y then X, with IDs `<step>-<index>`, so the output is stable for tests and for writing into a scene later.

## Alternatives considered

- **Reserving the future sampler names and an `exclude` field now**: rejected for the reason above; fields with no behavior also freeze a schema before the filters that would use it are designed. They come back with their implementation, each adding to the version 1 schema or bumping it.
- **A separate `pkg/placement`**: rejected for now; the sampler is a few dozen lines next to the other seeded generators, and the issue's tilemap sampler will need `procgen.Grid`.

## Consequences

Games can load a placement file and get deterministic prefab points over a cell area. Not delivered from #1729: the other samplers, filters (`density`, `slope`, `height`, `attribute`, `distanceTo`, `subtract`), `transform`, weighted `spawn`, `game placement run` writing a generated group into a scene, editor preview, per-cell runtime generation and the foliage migration. The fixture's golden points are pinned by `TestPlacementGridGolden`, which CI runs on amd64 (Ubuntu) and arm64 (macOS).
