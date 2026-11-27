# Agent Note: Preserve off-mesh links on ai.TiledNavMesh and navbake.Mesh.TiledNavMesh

Status: implemented

## Problem

`navbake.Mesh` stores off-mesh links (jumps, drops, ladders, teleports) under `links:` and `Mesh.NavMesh` registers every link via `ai.NavMesh.AddOffMeshLink`, but `Mesh.TiledNavMesh(tileSize)` copied only `m.Polygons` and silently discarded `m.Links` (`nsgo-dev/game#1587`). `ai.TiledNavMesh` itself lacked an off-mesh link API, so levels needing both dynamic obstacle carving (or per-tile rebuilding) and off-mesh traversal had to give up one of them, and switching from `Mesh.NavMesh` to `Mesh.TiledNavMesh` caused agents to lose jump/ladder/teleport connections without any error.

## Decision

Add full tile-aware off-mesh link support to `ai.TiledNavMesh` (`pkg/ai/tiled_navmesh.go`) and register `m.Links` in `navbake.Mesh.TiledNavMesh` (`pkg/ai/navbake/format.go`), keeping parity with `ai.NavMesh` ([off-mesh links](../feature/2026-11-25-ai-offmesh-links.md)) and preserving O(touched tiles) dynamic obstacle rebuilds ([tiled navigation meshes](../feature/2026-11-25-ai-tiled-navmesh-dynamic-obstacles.md)):

1. **Tile-indexed off-mesh link registry on `ai.TiledNavMesh`**:
   - Expose `AddOffMeshLink(NavOffMeshLink) bool`, `OffMeshLinks() []NavOffMeshLink`, and `ClearOffMeshLinks()` with the same endpoint validation, `Radius` snapping, `NavLinkOffMesh` tagging, separate `filterFlags` (`offMeshFilterFlags`, preventing bit-0 `NavFlagWalk` collision), cost fallback, and same-polygon teleporter semantics as `ai.NavMesh`.
   - Store each link's original `rawStart`/`rawEnd` alongside its currently attached `fromCoord`/`fromSlot`/`fromPoly` and `toCoord`/`toSlot`/`toPoly`, and index its 1-based ID in `tileOffMeshLinks[coord]` across the candidate tile AABBs covering `[endpoint ± (Radius + 2*portalEps)]` (falling back to `globalOffMeshLinks` only when a very large snap radius spans more than `256` tiles).
   - Whenever tiles are installed, replaced, rebuilt, removed, carved by dynamic obstacles, or flushed via `Batch`, detach only links touching the affected tile coordinates before mutating polygons/slots and re-attach them from `rawStart`/`rawEnd` after the rebuild completes. Unrelated tiles pay zero link work (`len(offMeshLinks) == 0` fast path or empty map lookup), and a link whose endpoint is temporarily carved away or unloaded detaches cleanly without dangling cross-tile portals and re-attaches automatically once the polygon returns.
   - `AddOffMeshLink` and `ClearOffMeshLinks` bump `tm.revision`, bump `tileVersions` on touched tiles, and notify listeners/path watchers so `IsPathValid` and `WatchPath` invalidate affected paths.

2. **Shortcut-aware sparse A\* and segmented corridor finalization**:
   - Extend `tiledPortal2D` with off-mesh fields (`endPos`, `cost`, `flags`, `offMeshIdx`, `linkID`, `offMesh`). `Raycast`, corner bridging, and cardinal-border `removePortalsTo` explicitly skip `offMesh` portals.
   - `FindPathWithFilterAndOptions` computes shortcut lower bounds (`buildTiledNavShortcuts2D` + shared `heuristic2D`) so cheap teleporters placed behind the start or inside the same polygon remain optimal, resets funnel apex state cleanly at each link exit (`endPos`, destination polygon area cost), and breaks A\* heap ties deterministically with `tieLink`.
   - Add `TiledNavPathLink` (using `NavPolyRef` for `FromPoly`/`ToPoly`), `TiledNavPathResult.Links`, `HasLinks()`, and `LinkAt(segmentIndex)`. `finalizeTiledCorridor2D` splits string-pulling and `tightenTiledPath2D` at every off-mesh portal so walking sub-segments still eliminate tile-border kinks while collinear link entry/exit vertices are never collapsed.

3. **Baked mesh bridge (`navbake.Mesh.TiledNavMesh`)**:
   - Share `OffMeshLink.navOffMeshLink()` between `Mesh.NavMesh` and `Mesh.TiledNavMesh` so both register every stored link identically after building their polygons.

## Alternatives considered

### Baking off-mesh portals statically into `NavTile` during `BakeNavTile`

Strongest argument: keeps all tile geometry and connectivity self-contained inside `NavTile` snapshots.

Rejected because off-mesh links commonly span two different tiles (`Start` in tile A, `End` in tile B), and obstacle carving or replacing one tile must re-resolve only the affected endpoint against the live neighbor tile without re-baking the neighbor's `NavTile`. Storing links in a mesh-level registry keyed by candidate endpoint tiles handles intra-tile, cross-tile, border-snapping, carving, and tile streaming uniformly.

### Returning an error or panicking in `Mesh.TiledNavMesh` when `m.Links` is non-empty

Strongest argument: smallest change to stop silent drops immediately.

Rejected because changing `Mesh.TiledNavMesh`'s signature breaks callers, panicking blocks games from combining dynamic obstacles with baked off-mesh links, and full support on `ai.TiledNavMesh` is clean and self-contained.

### Reusing `NavPathLink` (`FromPoly int`, `ToPoly int`) on `TiledNavPathResult`

Strongest argument: avoids adding `TiledNavPathLink`.

Rejected because `TiledNavMesh` identifies polygons by `NavPolyRef` (`Tile`, `Poly`, `Salt`) across `Corridor`, `TiledNavRaycastHit`, and spatial queries; flat `int` polygon indices are meaningless on a tiled mesh.

## Consequences

- **Benefits**: `navbake.Mesh.TiledNavMesh` preserves every baked off-mesh link and matches `Mesh.NavMesh` pathfinding across jumps, drops, ladders, and teleports while retaining dynamic obstacle carving and per-tile rebuilding. Links survive tile replacement, obstacle carving/restoration, and tile unload/reload automatically without leaking stale portals or slowing unrelated tile rebuilds.
- **Costs**: `ai.TiledNavMesh` maintains a per-tile link index (`tileOffMeshLinks`) and mirrors `ai.NavMesh`'s off-mesh shortcut heuristic and corridor segmentation. `world3d.TiledNavMesh` / `navbake3d.Mesh.TiledNavMesh` and `NavAgent` tiled-mesh support remain tracked separately (`nsgo-dev/game#1717`).

## Verification

- `pkg/ai/tiled_navmesh_offmesh_test.go`: one-way cross-tile drop never reversed and collinear endpoint preservation, `Raycast` ignoring off-mesh links, teleport cost vs walking (disconnected, behind-start, same-polygon), `Radius` snapping, `NavFilter` area/flag filtering without `NavLinkOffMesh`/`NavFlagWalk` bit collision, survival across obstacle carving (elsewhere and directly over endpoint + removal), `ReplaceTile`, `RebuildTile`, `RemoveTile` + re-`AddTile`, `Batch`, `IsPathValid`/`WatchPath`/`ClearOffMeshLinks`, untiled `NavMesh` parity, and `AllowPartial` paths crossing links.
- `pkg/ai/navbake/tiled_test.go`: `Bake` -> YAML `Encode`/`Parse` round-trip -> `Mesh.TiledNavMesh` vs `Mesh.NavMesh` parity across one-way cross-tile jumps, bidirectional cross-tile ladders, intra-tile teleports, `NavFilter` exclusions, dynamic obstacle carving, and nil-mesh safety.
- Verified with `gofmt -l pkg/ai/`, `go tool nsgo test ./pkg/ai/...`, `go tool nsgo check -target macos_arm64,linux_x64,windows_x64 -jobs 2 ./pkg/ai/...`, `(cd tools && go run ./changelog check)`, and `bun run verify-notes`.
