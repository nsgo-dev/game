# Agent Note: 3D colliders from glTF scenes, GridMaps, and heightmap images

Status: implemented

## Problem

3D levels in NSGo games are authored as glTF scenes (`packages/game/gltf`), modular `world3d.GridMap` layouts, or heightmap images, yet games previously had to unpack primitive vertices manually into `physics3d.NewTriMeshCollider`, skip collider-only glTF nodes by hand during rendering, add `GridMap` cell boxes one by one (creating dozens to thousands of redundant static bodies with internal cell seams), and convert `image.Image` heightmaps manually into `float32` height slices for `physics3d.NewHeightfieldCollider`.

## Decision

Split collider metadata parsing, physics construction, and GridMap bridging along clean, cycle-free package boundaries matching the repository's experimental-package architecture (aligned with [Direct voxel terrain collision for rigid bodies and characters](./2026-11-25-voxel-terrain-collision.md)):

- **Physics-independent collider annotations in `game/gltf` (`collider.go`)**:
  - `ParseNodeCollider(node)` resolves Godot-style node name suffixes (`-col`, `-colonly`, `-boxcol`, `-boxcolonly`, `-spherecol`, `-spherecolonly`, `-capsulecol`, `-capsulecolonly`, `-convcol`, `-convcolonly`, matched case-insensitively longest-first while stripping Blender's trailing `.001` duplicate numbering) and `node.Extras` (`extras.collider` as a shape string, boolean, or structured object with `shape`/`type`, `draw`/`only`/`visible`, `trigger`, `tag`, `center`, `halfExtents`/`size`, `radius`, `height`, `friction`, `restitution`, `layer`, `mask`).
  - `Node.IsDrawable()`, `Document.DrawableNodes()`, and `Document.SceneDrawableNodes(sceneIdx)` let any visual renderer skip `-colonly` and `draw: false` nodes without importing `physics3d`.
  - `Document.ColliderNodes()` and `Document.SceneColliderNodes(sceneIdx)` return deterministic DFS scene-ordered `NodeCollider` descriptors.
- **glTF collider builders, box merging, and heightmap images in `game/physics3d` (`gltf_colliders.go`, `box_merge.go`, `heightfield_image.go`)**:
  - `NewTriMeshFromGLTFPrimitive`, `NewTriMeshFromGLTFMesh`, and `NewTriMeshFromGLTFNode` transform indexed and non-indexed `ModeTriangles`, `ModeTriangleStrip`, and `ModeTriangleFan` primitives by the node's full `WorldMatrix`, automatically reversing triangle winding when the upper-left 3×3 determinant is negative so outward normals remain consistent.
  - `BuildGLTFColliders` and `World.AddGLTFColliders` construct static `TriMeshCollider` values and primitive `Body` instances (exact oriented boxes via `Body.Rot` when TRS-decomposable with trimesh/AABB fallback on shear, max-axis-scaled spheres, rotated capsules aligned to explicit Y or longest scaled mesh axis, and `TriMeshCollider` fallback for `-convcol`/`-convcolonly` until native convex hulls exist). `GLTFColliders.AddToWorld` / `RemoveFromWorld` manage level lifecycle cleanly.
  - `MergeBoxes(boxes, eps)` greedily merges adjacent aligned axis-aligned boxes along X, then Z, then Y using nested sort-and-cluster tolerance grouping (avoiding quantization bucket boundary misses), preserving exact point-set union and deterministic output ordering.
  - `NewHeightfieldFromImage(img, opts)` converts any Go `image.Image` (`*image.Gray`, `*image.Gray16`, `*image.NRGBA`, `*image.NRGBA64`, or arbitrary `image.Image`) into a `*HeightfieldCollider` with selectable `HeightmapChannel`, `CellSize` or `WorldWidth`/`WorldDepth`, `MinHeight`/`MaxHeight`, `Origin`, `FlipZ`, sub-image bounds support, and optional `(Cols, Rows)` nearest or bilinear resampling.
- **GridMap and Heightmap physics bridges in `game/world3d` (`gridmap_collider.go`)**:
  - `world3d` imports `physics3d` (matching `game/voxel/collider.go`) so `physics3d` stays free of `world3d`'s YAML/tilemap/quality dependencies and `GridMap` can access its internal chunks directly.
  - `GridMap.ColliderBoxes()`, `GridMap.MergedColliderBoxes()`, `GridMap.BuildStaticBodies(opts)`, and `GridMap.AddCollidersToWorld(world, opts)` extract world-space cell `ColliderBoxes` in deterministic `(Y, Z, X)` order and merge adjacent aligned cells via `physics3d.MergeBoxes` (or emit one body per box when `DisableMerge: true`).
  - `Heightmap.Collider()` bridges a `world3d.Heightmap` directly to a `*physics3d.HeightfieldCollider`.

## Alternatives considered

- **Having `physics3d` import `world3d` directly for `AddGridMapColliders`** — Would place all collider builders in a single package, but forces `physics3d` (and its dependents `voxel`, `vehicle`, and `ai/perception3d`) to transitively pull in `world3d` (`yaml.v3`, `tilemap`, `quality`), breaks the precedent established by `voxel.NewCollider`, and cannot directly inspect `GridMap`'s unexported chunk map without widening `GridMap`'s API.
- **Having `gltf` import `physics3d` to construct colliders directly on `gltf.Document`** — Would avoid adding a `gltf` import to `physics3d`, but would force every pure asset consumer (`render3d`, `anim3d`, `placeholder`, `tooling/assetbuild`) to pull in the entire 3D physics engine and `ai` package just to load a `.glb`. Keeping pure metadata/draw-filtering in `gltf` and physics construction in `physics3d` keeps lower-level asset packages lean.
- **Using coordinate quantization buckets for greedy 3D box merging** — Faster to write with hash maps, but two adjacent cell faces separated by 1 ULP near a bucket half-integer boundary could round to different buckets and silently fail to merge. Nested sort-and-cluster grouping along the two perpendicular axes guarantees every pair within `eps` lands in the same merge run while staying O(N log N) and deterministic.
- **Approximating `-convcol` / `-convcolonly` with an enclosing AABB or OBB instead of `TriMeshCollider`** — Simpler and creates a single primitive body, but turns sloped ramps, wedges, and bevels into solid rectangles that block characters. Using `TriMeshCollider` preserves exact sloped geometry until native convex hulls are added to `physics3d`.

## Consequences

- **Benefits**:
  - Level authors can tag Blender/glTF nodes with Godot-style `-col*` / `-*colonly` suffixes or `extras.collider` and get both automatic physics setup (`World.AddGLTFColliders`) and automatic visual exclusion (`doc.DrawableNodes()`).
  - `GridMap.AddCollidersToWorld` dramatically cuts static body count (e.g., merging a 6×1×6 floor + walls from 57 cell boxes down to a handful of static bodies) and removes internal cell seams while producing identical drop, raycast, and character-movement results.
  - `NewHeightfieldFromImage` supports 8-bit, 16-bit, and multi-channel RGBA heightmaps with exact sub-image and bilinear resampling support.
  - All package dependencies remain acyclic and respect `TestStabilityTable`.
- **Costs and known limits**:
  - `physics3d` now imports `game/gltf` and `game/math3d` (both experimental, pure-Go standard-library packages with no third-party dependencies).
  - `-convcol` and `-convcolonly` produce `TriMeshCollider` instances until `physics3d` gains a native convex hull primitive.
  - `HeightfieldCollider` uses a square `CellSize`; when `WorldWidth` and `WorldDepth` are both specified without explicit `Rows`, `Rows` is rounded to the nearest integer grid count matching `CellSize`.

## Verification

Run from `/Users/e-hu/worktrees/agy-game-816`:

- `(cd packages/game && env -u GOROOT go tool nsgo test ./gltf/...)` — verifies suffix parsing, `extras.collider` parsing/validation, `Node.IsDrawable`, `Document.DrawableNodes`, and `Document.ColliderNodes`.
- `(cd packages/game && env -u GOROOT go tool nsgo test ./physics3d/...)` — verifies glTF primitive/mesh/node trimesh conversion (indexed, non-indexed, strip, fan, mirrored winding), `-colonly` level walking with `CharacterController` and visual exclusion, box/sphere/capsule/convex/extras colliders with non-identity transforms, greedy 3D `MergeBoxes`, and `NewHeightfieldFromImage` across formats, channels, resampling, and dropped-body physics.
- `(cd packages/game && env -u GOROOT go tool nsgo test ./world3d/...)` — verifies `GridMap.AddCollidersToWorld` collider count reduction, determinism, and drop/raycast/character equivalence against `DisableMerge: true`, plus `Heightmap.Collider`.
- `(cd packages/game && env -u GOROOT go tool nsgo test . -- -run 'TestStabilityTable|TestAgentSkillValid')` — verifies package stability boundaries and skill contract.
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,web)` — verifies cross-platform vetting and compilation across all five targets.
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes` — verifies the changelog fragment and agent note tree/format gates.
