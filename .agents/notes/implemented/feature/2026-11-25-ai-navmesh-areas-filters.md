# Agent Note: Area costs and query filters on 2D and 3D navigation meshes

Status: implemented

## Problem

Both `ai.NavMesh` (2D) and `world3d.NavMesh` (3D) treated every walkable polygon as equal during pathfinding and spatial queries. Games could not mark roads, grass, mud, water, or dynamic doors with different traversal costs or capability flags, could not filter queries per agent class (for example land vs swimming units), and could not open or close a doorway polygon at runtime without rebuilding the entire mesh.

## Decision

1. **Shared filter and tag types owned by `game/ai` and aliased by `game/world3d`**:
   - `game/ai` defines `NavArea` (`uint8`, with `NavAreaDefault = 0` and built-in constants `NavAreaRoad`, `NavAreaGrass`, `NavAreaMud`, `NavAreaWater`, `NavAreaDoor`, `NavAreaHazard`), `NavFlags` (`uint32` bitmask: `NavFlagWalk`, `NavFlagSwim`, `NavFlagDoor`, `NavFlagJump`, `NavFlagDisabled`, `NavFlagAll`), `NavFilter`, and `DefaultNavFilter()`.
   - Because `game/ai` is Stable and `game/world3d` is Experimental (`TestStabilityTable` forbids Stable packages from importing Experimental ones), `game/world3d` imports `game/ai` and re-exports type aliases (`type NavArea = ai.NavArea`, `type NavFlags = ai.NavFlags`, `type NavFilter = ai.NavFilter`), matching constants, and `DefaultNavFilter()`. Callers using both 2D and 3D meshes share a single `NavFilter` value with zero conversion.
2. **Backward-compatible defaults and O(1) runtime mutation**:
   - `BuildNavMesh` in both packages assigns `NavAreaDefault` and `NavFlagWalk` to every polygon. `BuildNavMeshWithAreas` accepts `[]NavPolygonInput` (with `yaml` field tags ready for future navmesh YAML formats) and defaults a zero `Flags` field to `NavFlagWalk`.
   - `DefaultNavFilter()` sets `IncludeFlags: NavFlagAll` and `ExcludeFlags: NavFlagDisabled` with unit cost (`1.0`) for every area. `FindPath` delegates to `FindPathWithFilter(..., DefaultNavFilter())`, so all existing tests behave identically while closing a door via `AddPolygonFlags(idx, NavFlagDisabled)` or `SetPolygonFlags(idx, 0)` immediately affects subsequent `FindPath` calls without rebuilding.
   - Unfiltered geometric queries (`FindPolygon`, `Contains`, `NearestPolygon`) continue inspecting every polygon in the mesh; `FindPolygonWithFilter`, `ContainsWithFilter`, and `NearestPolygonWithFilter` restrict results to polygons passing `filter.Allows(area, flags)`.
3. **Admissible area-weighted portal-graph A\***:
   - Both `ai.NavMesh.FindPathWithFilter` and `world3d.NavMesh.FindPathWithFilter` precompute per-polygon `allowed` and `polyCost` slices once per query (zero map lookups inside the search loop) and track `minCost`, the minimum positive finite area cost among allowed polygons in the mesh.
   - Each traversal segment inside polygon `p` costs `dist * polyCost[p]`, and the A\* heuristic is `minCost * dist(pt, goal)`. Scaling by `minCost` keeps the heuristic strictly admissible even when road areas use sub-unit multipliers (for example `0.5` vs grass `1.0`).
   - `world3d.NavMesh` upgrades its A\* corridor search from midpoint polygon-node search to the same deterministic portal-graph A\* (with exact 3D `bestPortalPoint3D`, virtual `goalNode` accounting for traversal inside the goal polygon, and `(f, h, id)` heap tie-breaking) while keeping its existing 3D funnel smoothing and stacked-level vertical separation unchanged.
   - When `start` and `goal` share an allowed polygon whose cost exceeds `minCost`, A\* seeds `goalNode` with the direct in-polygon cost instead of short-circuiting so a cheaper adjacent road corridor can still beat the direct slow segment.
4. **No speculative standalone navmesh YAML schema**:
   - Neither `game/ai` nor `game/world3d` currently defines a versioned `*.navmesh.yaml` schema on `main` (navmesh generation/baking formats are tracked separately in `#822` and `#823`). Rather than inventing a conflicting YAML schema here, `NavPolygonInput` carries `yaml:"vertices"`, `yaml:"area,omitempty"`, and `yaml:"flags,omitempty"` tags and `world3d.NavPoly` exposes `Area` and `Flags` directly so upcoming navmesh YAML formats serialize them cleanly.

## Alternatives considered

1. **Duplicate `NavArea`, `NavFlags`, and `NavFilter` as distinct types in `world3d`**:
   - Strongest argument: avoids adding an import edge from `game/world3d` to `game/ai` and keeps `world3d` self-contained.
   - Rejected because `game/world3d` is Experimental while `game/ai` is Stable, so importing `game/ai` obeys the stability DAG, eliminates duplicated filter semantics (`Allows`, `AreaCost`, `Clone`), and lets 3D games pass an `ai.NavFilter` straight into `world3d.NavMesh`.
2. **Store `AreaCosts` as a fixed `[64]float64` array on `NavFilter` (Recast/Detour style)**:
   - Strongest argument: avoids map lookup overhead and allows direct value copying without a `Clone` helper.
   - Rejected because a `[64]float64` value is 512 bytes copied on every by-value query call, caps area IDs at 63 instead of full `uint8` (0..255), and makes zero-value initialization awkward (`0` vs default `1.0`). Precomputing `polyCost []float64` once per `FindPathWithFilter` call removes map lookups from the A\* hot loop while keeping `NavFilter` compact and ergonomic for struct literals (`AreaCosts: map[ai.NavArea]float64{ai.NavAreaRoad: 0.5}`).
3. **Keep `world3d`'s polygon-node A\* with portal midpoints and fixed unit heuristic**:
   - Strongest argument: smaller diff in `world3d/navmesh.go`.
   - Rejected because an unscaled Euclidean heuristic is inadmissible whenever a road has cost `< 1.0`, polygon-node A\* collapses multiple entry portals into `goalPoly` without accounting for the final leg's area cost inside `goalPoly`, and midpoint sampling misestimates wide polygons bordering roads.
4. **Add a brand-new `*.navmesh.yaml` document schema in this change**:
   - Strongest argument: would let callers serialize a standalone navmesh file immediately in `#824`.
   - Rejected because navmesh baking and file formats are being introduced in `#822` and `#823`; defining a separate format here risks schema and symbol conflicts. **Upgrade trigger**: when `#822` or `#823` lands the `*.navmesh.yaml` schema, wire `NavPolygonInput.Area` and `NavPolygonInput.Flags` into its polygon records and conformance tests.

## Consequences

- **Benefits**:
  - Games can model roads, mud, hazards, water, and dynamic doors identically across 2D and 3D navmeshes with deterministic, optimal area-weighted paths.
  - Door and gate toggling is O(1) with zero mesh rebuilds or re-linking.
  - All existing 2D and 3D navmesh tests pass unchanged under `DefaultNavFilter()`.
- **Costs**:
  - `FindPathWithFilter` allocates small per-query `allowed` and `polyCost` slices of length `len(polys)` (and flat portal-index slices in `world3d`) on each call; high-frequency callers can use the zero-allocation `NavMeshQuery` / `FindPathInto` APIs introduced in [2D and 3D navigation mesh spatial queries and zero-allocation path reuse](2026-11-25-ai-navmesh-queries.md) without changing `NavFilter`.
  - Funnel string-pulling remains geometric within the A\*-selected polygon corridor (standard Recast/Detour behavior) rather than computing Snell's-law refraction points inside a single portal.
- **Verification**:
  - `packages/game/ai/navmesh_test.go` and `packages/game/world3d/navmesh_test.go` verify road-over-grass reference paths, water exclusion vs swimming agents, runtime door flag toggling without rebuilding, runtime area changes, filtered spatial queries, multi-level 3D ramp gating, invalid cost rejection, nil/bounds safety, and 50-run determinism.
  - `go tool nsgo test ./ai/...`, `go tool nsgo test ./world3d/...`, `go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid'`, `go tool nsgo check -target macos,windows,linux,android,web`, `go run ./tools/changelog check`, and `bun run verify-notes`.
