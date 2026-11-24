# Agent Note: 3D voxel chunks, greedy meshing, and YAML world persistence

Status: implemented

## Problem

NSGo's 3D stack (`math3d`, `render3d`, `scene3d`, `world3d`) has heightmap terrain and modular tile `GridMap`s, but no dense chunked voxel representation for editable sandbox worlds. Naive per-voxel face emission generates excessive triangle counts across large terrains (1,536 quads for a single solid 16×16×16 chunk) and full-world remeshes stall frames when a player edits a block. Editable voxel worlds also need a plain-text, reviewable, deterministic save format that obeys the repository's YAML contract.

## Decision

Add a dedicated `packages/game/voxel` package (listed as `Experimental` in `docs/en/stability.md` and `docs/zh/stability.md`) with the following architecture and contracts:

- **Dedicated package `game/voxel` instead of expanding `game/world3d`**: `voxel` depends on `render3d` (emitting `render3d.Mesh` directly), `math3d`, `loaderr`, and `go.yaml.in/yaml/v3`, keeping `world3d` free of GPU/render3d dependencies while matching peer genre packages (`bullets`, `rooms`, `vehicle`).
- **Cubic power-of-two chunks (`ChunkSize` in `4..64`, default `16`)**: `World` stores chunks in a sparse `map[ChunkCoord]*Chunk` with flat `(y*N + z)*N + x` `[]BlockID` (`uint16`) storage. Power-of-two dimensions allow branch-free arithmetic right-shift (`>> shift`) and bitmask (`& mask`) coordinate conversion that works identically for negative coordinates.
- **Minimal boundary-aware dirty tracking**: `World.SetBlock` marks only the edited voxel's chunk dirty. Adjacent existing chunks are marked dirty only when the edited voxel sits on a shared boundary and the edit alters neighbor face visibility or cross-chunk ambient occlusion (opacity change or transparent-block transition). Replacing one opaque block with another opaque block never dirties neighboring chunks.
- **Padded-neighborhood greedy mesher (`Mesher`)**: `Mesher` retains reusable `(N+2)^3` padded block/opacity buffers, an `N×N` slice mask, and per-material index buckets. Before sweeping the six face directions, `fillPadded` copies the chunk and its existing face (and, when AO is enabled, edge/corner) neighbors once, eliminating all map lookups from the inner meshing loops. Coplanar visible faces merge along `(u, v)` when both `BlockID` and packed 4-corner AO values match, and quad diagonals flip when `a0 + a2 < a1 + a3` while preserving counter-clockwise winding (`(v1 - v0) × (v2 - v0) · normal > 0`).
- **Per-material `SubMesh` output**: `ChunkMesh.Mesh` is a valid `render3d.Mesh` ready for `render3d.UploadMesh`, with `ChunkMesh.SubMeshes` partitioning `Indices` by ascending `BlockDef.MaterialID`.
- **Cached-chunk 3D DDA raycast (`World.Raycast` / `World.RaycastWith`)**: Amanatides & Woo traversal in `float64` voxel coordinates caches the active `*Chunk` pointer across steps inside a chunk and returns hit block, struck face/normal, and adjacent placement coordinate.
- **Versioned RLE YAML format (`WorldDoc`, `voxel.v1.json`)**: `SaveYAML` / `Encode` writes deterministic 2-space YAML prefixed with `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/voxel.v1.json`, palette entries sorted by `ID`, non-empty chunks sorted by `(X, Y, Z)`, and `(y, z, x)` run-length encoded tokens (`"id*count"` or `"id"`). `Parse` refuses unknown fields (`KnownFields(true)`) and trailing documents and reports errors via `*loaderr.Error`. `cmd/gameschema` publishes `apps/website/public/schemas/voxel.v1.json`.

## Alternatives considered

- **Adding voxel chunks inside `packages/game/world3d`** — Would keep all 3D world structures in one package, but `world3d` intentionally avoids importing `render3d` (its `TerrainMesh` duplicates vertex slices). Giving `voxel` its own package lets `ChunkMesh` embed `render3d.Mesh` directly without pulling `render3d` and `nsgo/gpu` into `world3d`.
- **Arbitrary non-power-of-two rectangular chunk dimensions (e.g., 16×256×16 columns)** — Column chunks avoid vertical chunk boundaries for sky-to-bedrock worlds, but editing a single block forces remeshing a 256-tall column (16× more work per edit) and non-power-of-two sizes replace bit-shifts with integer Euclidean division in hot coordinate conversions. Cubic 16×16×16 chunks keep single-edit remesh times around 35 µs.
- **Binary chunk saves or base64-encoded binary blobs inside YAML** — Smaller on disk for noisy random chunks, but violates the repository rule in `AGENTS.md` that every author-facing and program-written format is readable, diffable plain text. `(y, z, x)` RLE tokens (`"1*2048"`, `"2*256"`, `"0*1792"`) keep typical layered terrain chunks both human-readable and compact.
- **Querying `World.Block` directly during meshing instead of a padded `(N+2)^3` scratch buffer** — Simpler code with no scratch state, but performs tens of thousands of hash-map lookups per chunk (especially with AO enabled), degrading 256×256 meshing throughput.

## Consequences

- **Benefits**:
  - Greedy meshing reduces a solid 16×16×16 chunk from 1,536 quads to 6 quads (256× reduction).
  - Editing an interior block in a 256×256 terrain remeshes only 1 chunk in ~36 µs (`BenchmarkRemeshSingleEdit256x256`), and remeshing all 256 chunks from scratch takes ~7.9 ms (`BenchmarkMeshFull256x256Area`) — well within a 16.6 ms (60 fps) desktop frame budget.
  - YAML saves are deterministic across insertion orders, round-trip byte-for-byte identically, and validate against the published `voxel.v1.json` schema.
- **Costs and known limits**:
  - `ChunkMesh.AO` is returned as a separate `[]uint8` slice rather than packed into `render3d.Mesh` because `render3d.MeshVertexLayout` has fixed position/normal/UV attributes (`32` bytes). Games that render AO either upload `AO` in a secondary vertex buffer slot or pack it in a custom voxel shader. If `render3d.Mesh` later adds vertex colors, `ChunkMesh` can optionally populate them.
  - RLE YAML files expand linearly on high-entropy random noise chunks (one token per differing adjacent voxel). If large procedural worlds require streaming region files, a multi-file per-region YAML layout can be layered on top of `ChunkDoc`.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-535`:

- `(cd packages/game && go tool nsgo test ./voxel/... ./cmd/gameschema/... .)` — verifies registry, negative coordinates, minimal dirty-chunk tracking, CCW winding, greedy quad reduction, cross-chunk/transparent culling, cross-chunk AO, DDA raycast, YAML round-trip determinism/validation, `TestStabilityTable`, and `voxel.v1.json` schema conformance.
- `(cd packages/game && go tool nsgo test -bench=. ./voxel/...)` — runs `BenchmarkRemeshSingleEdit256x256` (~35.7 µs/op), `BenchmarkMeshFull256x256Area` (~7.86 ms/op), and `BenchmarkRaycast256x256` (~187 ns/op).
- `(cd packages/game && go run ./cmd/gameschema -check)` — confirms `apps/website/public/schemas/voxel.v1.json` matches `voxel.WorldDoc`.
- `(cd packages/game && go tool nsgo check -target macos,windows,linux,android,web)` — confirms cross-platform compilation and vetting.
- `go run ./tools/changelog check` and `bun run verify-notes`.
