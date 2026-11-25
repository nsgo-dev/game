# Agent Note: Voxel sky and block light propagation

Status: implemented

## Problem

`packages/game/voxel` (introduced in [3D voxel chunks, greedy meshing, and YAML world persistence](./2026-11-24-voxel-chunks-and-meshing.md)) supports cross-chunk ambient occlusion, but ambient occlusion alone cannot light caves, overhangs, night scenes, or emissive blocks such as torches and glowstone. A sandbox voxel world needs per-voxel sky light and block light (`0..15` each) that update incrementally on single block edits, match a full recompute identically, cross chunk borders without seams, dirty only affected chunks, and flow into `render3d` vertex data.

## Decision

Extend `packages/game/voxel` with a dual-channel per-voxel light propagation engine and vertex-color mesh integration:

- **Block lighting properties (`BlockDef`, `BlockDoc`, `voxel.v1.json`)**: Add `Emissive uint8` (`0..15`) and `LightOpacity uint8` (`0..15`, for `Transparent` blocks) to `BlockDef` and `BlockDoc` with `omitempty` YAML serialization and schema bounds `0..15`. Opaque blocks (`Transparent == false`, including unregistered non-zero IDs) always block incoming light (`LightOpacity == 15`); transparent blocks with `LightOpacity == 0` let direct sky light (`15`) pass straight down unattenuated and attenuate propagated light by `1` per voxel step; transparent blocks with `LightOpacity > 0` (e.g. water `2`, leaves `1`) attenuate both direct sky columns and propagated light by `LightOpacity`. Existing YAML documents remain valid and byte-for-byte stable.
- **Packed nibble storage + sparse `airLight` cache**: Every `Chunk` stores a `size^3` `[]uint8` light slice (high nibble = sky light `0..15`, low nibble = block light `0..15`, default `0xF0` for unobstructed sky `15` and block `0`). Unallocated air chunks that carry non-default light (block light spilling past chunk edges or sky shadows passing through empty chunks between a roof and a floor) are stored in a separate internal `World.airLight` map that is invisible to `ChunkCount`, `DirtyChunks`, and `SaveYAML`, adopted automatically when a chunk is allocated, preserved across `PruneEmptyChunks`, and deleted in O(1) as soon as its `nonDefault` counter returns to `0`.
- **Well-defined vertical floor for sky light**: Because a sparse chunked world has no fixed bedrock plane, direct sky shadows propagate down to `minY = minChunkY * ChunkSize` (the bottom of the lowest non-empty chunk). Queries below `minY` clamp to `(x, minY, z)` for sky light (while block light propagates freely in all 3 axes within its finite radius `<= 15`). When the global `minChunkY` shifts, sky light recomputes across active chunks so incremental edits remain history-independent and equal to `World.RecomputeLight()`.
- **Incremental BFS flood fill and removal with net-change dirty tracking**: On `World.SetBlock`, edits that do not alter `LightOpacity` or `Emissive` return immediately. Otherwise, incremental removal BFS (`lightRemQ`) clears dependent voxels (including direct `-Y` sky-15 columns and recording cleared emissive blocks in `lightSeed`) and enqueues surviving boundary sources (`vLight >= oldVal`) into `lightAddQ` only when the cleared voxel can receive light (`LightOpacity < 15`). `World.origLight` snapshots the pre-edit light byte of touched voxels and marks a chunk (plus adjacent existing non-empty chunks for boundary voxels, covering both flat and smooth cross-chunk vertices) dirty only when a voxel's final light byte differs from its pre-edit byte.
- **Greedy meshing and `render3d.ColoredMesh` output**: `MeshOptions` adds `Lighting`, `SmoothLighting`, and `LightStyle` (defaulting to false so existing `MeshOptions{}` greedy reduction is unchanged). `Mesher` widens its greedy merge key to `uint64` packing `BlockID`, 4-corner AO, and 4-corner `(sky, block)` light so coplanar faces merge only when lighting matches, eliminating seams across chunk borders. `ChunkMesh` exposes raw per-vertex `SkyLight []uint8`, `BlockLight []uint8`, and linear RGBA `Colors []math3d.Vec4` (evaluated with quadratic linear-space `LightFactor(level)` and `CombineLightColor`), plus `ChunkMesh.ColoredMesh() render3d.ColoredMesh` ready for `render3d.UploadColoredMesh`, `render3d.NewUnlitPipeline`, and `render3d.NewMaterialPipeline` (`VertexColors: true`).

## Alternatives considered

- **Storing light only inside allocated block chunks (`World.chunks`) without `airLight`** — Simpler memory model, but a torch placed on the edge of a chunk next to an unallocated chunk drops abruptly to `0` across the boundary (creating a visible seam on adjacent stone faces), and a floating roof chunk above an unallocated air gap fails to cast a shadow onto chunks below. Keeping `airLight` separate from `World.chunks` preserves seamless lighting without polluting `ChunkCount`, `DirtyChunks`, or YAML saves.
- **Computing light lazily on mesh build instead of incrementally on `SetBlock`** — Avoids per-edit BFS during bulk world generation, but breaks immediate `World.SkyLight`/`World.BlockLight` queries and complicates minimal cross-chunk dirty-chunk tracking before remeshing. Keeping edits incremental with reusable scratch queues and skipping `addQ` pushes from opaque voxels keeps single edits fast while `FromDoc` batches a single `RecomputeLight()` on YAML load.
- **Persisting computed sky/block light grids in YAML saves** — Speeds up load time slightly, but bloats author-facing YAML files with derived data that can drift out of sync with hand-edited block runs. Persisting only `emissive` and `lightOpacity` in `BlockDoc` and recomputing light in `FromDoc` keeps YAML files compact, diffable, and always consistent.
- **Linear `level / 15` brightness in `LightFactor` instead of quadratic `(level / 15)^2`** — Simpler formula, but `render3d.ColoredMesh.Colors` is in linear RGB space and gamma-encoded to sRGB on output, making linear `1/15` appear ~29% bright on screen. Quadratic linear brightness gives a perceptually even falloff from `0` (pitch dark) to `1` (full light) after sRGB encoding.

## Consequences

- **Benefits**:
  - Incremental torch placement/removal, wall blocking/unblocking, and sky roof edits match `World.RecomputeLight()` voxel-for-voxel across positive and negative coordinates and across chunk borders.
  - Cross-chunk smooth lighting and greedy meshing produce zero lighting seams on shared chunk boundaries while dirtying only genuinely affected chunks.
  - `ChunkMesh.ColoredMesh()` plugs directly into `render3d`'s existing `UploadColoredMesh` and vertex-color PBR/unlit pipelines while keeping `ChunkMesh.Mesh` and `ChunkMesh.AO` backward compatible.
- **Costs and known limits**:
  - Each allocated `Chunk` holds an extra `size^3` bytes for packed light (4 KB for a 16×16×16 chunk, a 50% increase over the 8 KB `[]BlockID` array).
  - Bulk procedural generation via individual `SetBlock` calls runs incremental light checks per voxel (though bottom-up builds hit the opaque early-exit fast path); very large procedural terrain generators can also build a `WorldDoc` and load via `FromDoc` for a single whole-world light pass.
  - Colored block light (RGB per-voxel light channels rather than a single 4-bit block-light intensity tinted by `LightStyle.BlockColor`) is not included and would require widening per-voxel light storage beyond one byte.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-860`:

- `(cd packages/game && env -u GOROOT go tool nsgo test ./voxel/...)` — runs unit tests for `Emissive`/`LightOpacity` validation and YAML round-trip, torch placement/removal and overlapping light equivalence to full recompute, multi-chunk sky columns and water attenuation, 150-step randomized differential testing against full recompute across chunk boundaries, cross-chunk seam-free meshing and minimal dirty tracking, and `render3d.ColoredMesh` vertex layout validation.
- `(cd packages/game && env -u GOROOT go tool nsgo test ./render3d/...)` and `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/gameschema/...)` — confirms `render3d` tests and `voxel.v1.json` schema conformance pass.
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid|TestFormatPackagesRunConformance')` — confirms repository-wide contract tests pass.
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)` — confirms cross-platform compilation and vetting.
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes` — confirms changelog fragment and agent note tree/format checks pass.
