# Agent Note: Off-mesh links for jumps, ladders, and teleports

Status: implemented

## Problem

`ai.NavMesh` and `world3d.NavMesh` previously carried only a minimal `AddOffMeshLink` helper added alongside [2D and 3D navmesh path-corridor agents](2026-11-25-ai-navmesh-agents.md): links lacked endpoint snap `Radius`, `Area` filtering/cost scaling, and `UserData`; path results (`NavPathResult`) did not expose which path segments crossed off-mesh links (and 2D collinear simplification could even collapse straight-line link entry/exit vertices); Euclidean A\* heuristics overestimated remaining cost whenever a cheap teleporter link existed (potentially missing a cheaper teleport placed behind the start or on the same polygon); and links could not be persisted in `navbake` / `navbake3d` YAML files or placed from a `sceneformat` scene.

## Decision

Extend off-mesh connections across the full navigation stack (`ai`, `world3d`, `navbake`, `navbake3d`, and `sceneformat`) while preserving `game/ai`'s Stable API compatibility and `TestStabilityTable` boundaries:

1. **Rich point-to-point off-mesh links (`NavOffMeshLink`) and link management**:
   - Add `Radius`, `Area`, and `UserData` to `ai.NavOffMeshLink` and `world3d.NavOffMeshLink`, plus convenience area/flag constants `NavAreaJump`, `NavAreaClimb`, `NavAreaTeleport`, `NavFlagClimb`, and `NavFlagTeleport` in `game/ai` (aliased in `game/world3d`).
   - When `Radius > 0`, `AddOffMeshLink` snaps endpoints outside a polygon onto the nearest polygon point within `Radius` (in 3D using `[Radius, max(Radius, VerticalTolerance), Radius]` extents so stacked floors remain vertically separated). Endpoints on the same polygon are allowed when distinct, supporting same-polygon teleporters.
   - Add `OffMeshLinks()` and `ClearOffMeshLinks()` on both `ai.NavMesh` and `world3d.NavMesh`, and propagate `Radius`, `Area`, and `UserData` to `NavLinkEvent` (using `max(entryRadius, Radius)` for `NavAgent` link entry).

2. **Shortcut-aware admissible A\* and `NavFilter` integration**:
   - Both `NavMeshSearch` and reusable `NavMeshQuery.runPortalAStar` (2D and 3D) filter off-mesh portals through `filter.Allows(link.Area, NavFlags(link.Flags))` and scale traversal cost by `filter.AreaCost(link.Area)`.
   - When the mesh has no off-mesh links or every link's effective cost is at least `dist(start, end) * minCost`, A\* uses the standard Euclidean heuristic with zero extra overhead.
   - When one or more shortcut links exist (`effectiveCost < dist(start, end) * minCost`), the search precomputes an exact lower bound `toGoal` per shortcut via bounded Bellman-Ford relaxation (up to 64 shortcut links, falling back to `h = 0` Dijkstra above 64) and evaluates `h(p) = min(dist(p, goal)*minCost, min_i(dist(p, s_i.start)*minCost + s_i.cost + s_i.toGoal))`. The same-polygon direct-path early exit checks `directCost <= h(start)` so cheaper teleporters are never bypassed.

3. **Explicit path link segment exposure (`NavPathResult.Links`)**:
   - Add `NavPathLink` (`StartIndex`, `EndIndex`, `FromPoly`, `ToPoly`, `Start`, `End`, `Radius`, `Area`, `Flags`, `LinkID`, `UserData`), `NavPathResult.Links`, `NavPathResult.HasLinks()`, and `NavPathResult.LinkAt(segmentIndex)`.
   - In 2D `finalizeCorridor` / `finalizeCorridorInto`, run collinear simplification strictly within walking segments between off-mesh links and never collapse link entry/exit vertices, guaranteeing `Path[link.StartIndex] == link.Start`, `Path[link.EndIndex] == link.End`, and `EndIndex == StartIndex + 1` even on collinear jump trajectories.
   - `NavMeshQuery` reuses scratch slices (`shortcuts`, `segPath`, `pathLinks`) so repeated queries remain zero-allocation after warm-up.

4. **NavMesh YAML persistence and scene authoring**:
   - Add optional `Links []OffMeshLink` (`yaml:"links,omitempty"`) to `navbake.Mesh` (with `navbake.Source.Links` and flow `[x, y]` `navbake.Point`) and `navbake3d.Mesh` (updating `apps/website/public/schemas/navmesh.v1.json` via `gameschema`), keeping schema version `1` backward compatible. `Mesh.NavMesh()` in both packages registers all stored links automatically, and `navbake.Mesh.TiledNavMesh()` also registers them on `ai.TiledNavMesh` ([tiled off-mesh links](../bug-fix/2026-11-28-tiled-navmesh-offmesh-links.md)).
   - Add the built-in `"nav_link"` component in `packages/game/sceneformat/navlink.go` (`NavLinkComponent`, `RegisterNavLink`, `ExtractNavLinks2D`, `ExtractBuiltNavLinks2D`, `ExtractNavLinks3D`, `ExtractBuiltNavLinks3D`) expressing `start` and `end` offsets in the object's local space so parent transforms and instanced scenes carry both endpoints together.
   - `navbake3d.Geometry.AddOffMeshLink` and `Geometry.AddSceneFile` extract scene `"nav_link"` components into baked 3D navigation meshes so `game navmesh` persists them directly into `*.navmesh.yaml`.

## Alternatives considered

### Keeping Euclidean A\* heuristic unchanged when teleport links are present

Strongest argument: zero extra code or per-search setup when off-mesh links exist, and fast heuristic guidance in open arenas.

Rejected because a teleport link whose cost is lower than straight-line Euclidean distance breaks heuristic admissibility. When a cheap teleporter sits behind the start position or inside the same large polygon, Euclidean `f`-scores expand a longer walking route to the goal first and violate the requirement that a teleport is used whenever it is cheaper than any walking route. Precomputing lower bounds over only the shortcut links preserves full Euclidean speed on normal meshes while guaranteeing optimality with teleporters.

### Bumping `navbake` and `navbake3d` format versions to 2 for `links`

Strongest argument: cleanly separates files with off-mesh links from older files.

Rejected because `links` is purely optional (`omitempty`), existing version 1 files with no `links` key load and round-trip identically, and keeping version 1 avoids breaking existing baked navmesh files and tooling (`navmesh.v1.json`) for an additive field.

### Adding a dedicated top-level `navLinks` field to `sceneformat.SceneDoc` instead of a `"nav_link"` component

Strongest argument: keeps navigation links grouped at the root of a scene document rather than attached to objects.

Rejected because object components follow the established pattern of `"cover_point"` (`sceneformat/cover.go`) and built-in physics components (`sceneformat/physics.go`), allow ladders/jump pads/teleporters to be authored inside reusable instanced scene prefabs (`ObjectDoc.Scene`) with composed 2D/3D transforms, and require no scene schema version bump.

## Consequences

- **Benefit**: Both 2D (`game/ai`) and 3D (`game/world3d`) navigation meshes support Detour/Godot-style off-mesh links with radius snapping, one-way/two-way traversal, area/flag filtering, custom metadata, optimal teleporter routing, and explicit path-segment reporting.
- **Benefit**: Off-mesh links author once in a scene YAML (`nav_link`), bake deterministically into `navbake` / `navbake3d` YAML files, and restore automatically on `Mesh.NavMesh()`.
- **Benefit**: `game/ai` stays Stable with zero Experimental imports and zero allocations on warmed-up `NavMeshQuery` calls.
- **Cost**: Off-mesh link filtering, shortcut-heuristic relaxation, and link-preserving corridor finalization are mirrored across `game/ai` and `game/world3d` to preserve the Stable/Experimental package boundary. **Maintenance note**: keep the 2D and 3D implementations and test suites in sync.

## Verification

- `packages/game/ai/navmesh_offmesh_test.go`: one-way drop never reversed, collinear link preservation, teleport cost vs walking (including behind-start and same-polygon teleporters), radius snapping, bidirectional ladder reversal, `NavFilter` area/flag filtering, `NavAgent` `OnLink` metadata, `OffMeshLinks` / `ClearOffMeshLinks`, incremental `NavMeshSearch` and `NavMeshQuery` parity.
- `packages/game/world3d/navmesh_offmesh_test.go`: 3D one-way ledge drop never reversed, 3D teleport vs walking and filter area-cost scaling, 3D radius snapping preserving stacked-floor vertical separation, bidirectional vertical ladder, 3D `NavAgent` metadata, incremental and `NavMeshQuery` parity.
- `packages/game/sceneformat/navlink_test.go`: `"nav_link"` component schema validation, 2D and 3D hierarchy transform extraction, instanced prefab scene resolution, and end-to-end pathfinding on both `ai.NavMesh` and `world3d.NavMesh`.
- `packages/game/navbake/format_test.go`, `packages/game/navbake3d/format_test.go`, `packages/game/navbake3d/sources_test.go`: YAML validation, deterministic round-trip, `Mesh.NavMesh()` link loading, and `Geometry.AddSceneFile` end-to-end scene-to-YAML-to-pathfinding baking.
- Checked with `gofmt -l`, `go tool nsgo test`, `go tool nsgo check` across macOS/Windows/Linux/Android/Web/iOS, `go run ./cmd/gameschema -check`, `go run ./tools/changelog check`, and `bun run verify-notes`.
