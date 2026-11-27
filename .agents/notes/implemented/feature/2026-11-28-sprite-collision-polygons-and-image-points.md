# Agent Note: Per-frame sprite collision polygons and named image points

Status: implemented

## Problem

Sprite frames in `pkg/sprite` carried only atlas rectangles, trim offsets, untrimmed sizes, and pivots, forcing 2D games to hand-size `collider2d` boxes and hard-code pixel offsets for muzzles, hands, and feet that broke whenever art changed or flipped (#1758).

## Decision

Extend the `*.import.yaml` image sidecar, atlas packing pipeline (`pkg/tooling/pack`, `pkg/tooling/assetbuild`, `internal/gamecli`), `pkg/sprite`, `pkg/sceneformat`, and `pkg/editor` as one end-to-end contract that does all pixel tracing offline at asset build time so runtime binaries never trace images:

- **Strict, deterministic sidecar fields (`pkg/importsettings/collision.go`)**:
  - `Image.Collision` adds `mode` (`alpha`, `box`, `ellipse`, `polygon`, `none`), `tolerance` (`0..255` alpha cutoff where pixels with `alpha > tolerance` are solid), `maxVertices` (`3..8`, default `8` matching `physics.MaxPolygonVertices`), `points` (explicit vertices for `polygon` mode), and ascending per-cell `cells` overrides (requiring `image.slice`).
  - `Image.Points` (`map[string]Point2D`) stores named `[x, y]` frame-pixel points applied to unsliced images and as defaults across sliced sheets, with `Image.CellPoints` (`[]CellPoints`, ascending integer `cell`) providing per-cell overrides merged on top of defaults.
  - `Point2D`, `CellCollision`, and `CellPoints` use custom YAML marshalers/unmarshalers to enforce exact 2-number sequences, require mandatory keys, refuse unknown/duplicate keys, normalize `-0` to `0`, and round-trip deterministically against `import.v1.json`.
- **Offline alpha tracing and convex decomposition (`pkg/tooling/pack/collision.go`, `pkg/tooling/pack/pack.go`)**:
  - `pack.TraceCollision` extracts exact pixel-boundary outer contours (marching squares on the `(w+1)x(h+1)` vertex grid with sharpest-right-turn saddle resolution so diagonal pixels separate into simple loops), simplifies staircases with starting-point-invariant closed Ramer-Douglas-Peucker (`epsilon = 0.75px`), splits concave outlines via ear-clipping triangulation + Hertel-Mehlhorn convex merging, and caps convex parts to `maxVertices` via Visvalingam-Whyatt minimum-area decimation.
  - Output polygons are strictly convex, wound clockwise in Y-down space (positive signed area), canonicalized by starting vertex `(Y, X)` and sorted deterministically, and encoded under `frames.<name>.shapes` and `frames.<name>.points` with `omitempty` (preserving byte-identical JSON for atlases without shapes or points).
  - `assetbuild.Builder.atlas` and `game pack` bake shapes and points per frame; `assetbuild.Builder.texture` refuses `collision`, `points`, and `cellPoints` on texture-only images.
- **Runtime loading and world transforms (`pkg/sprite/atlas.go`, `pkg/sprite/sprite.go`)**:
  - `sprite.Frame` adds `Shapes [][]math2d.Vec2` and `Points map[string]math2d.Vec2` (in untrimmed frame points, divided by `meta.scale`).
  - `LoadAtlas` reads packed `shapes` and `points` as well as Aseprite `meta.slices` entries with `pivot` (`bounds + pivot` across each key's frame span), processing slices before `meta.frameTags` so `Atlas.Animations` copies carry the slice points.
  - `Sprite.PointWorld(name)` transforms a named point through `Sprite.Transform()`; `Sprite.ShapesWorld()` transforms `Frame.Shapes` and reverses vertex order whenever the affine determinant is negative so mirrored sprites keep clockwise Y-down winding.
- **Dynamic sprite colliders (`pkg/sceneformat/physics.go`, `pkg/sceneformat/physics_sprite.go`)**:
  - `collider2d` and `trigger2d` accept `shape: "sprite"`, populating `World2D` / `Rigid2D` / `Triggers2D` from the object's live `bo.Sprite.ShapesWorld()`.
  - Multi-polygon concave frames spawn synchronized companion parts (`extraBodies`, `extraRigids`, `extraTriggers`) with deduplicated trigger enter/stay/exit events; dynamic `RigidBody` instances use a convex hull while `World2D` keeps every exact convex part.
  - `PhysicsScene.SyncSpriteColliders()` runs at the start of `PhysicsScene.Step` (and can be called directly) using an O(1) transform/frame snapshot check so frame swaps, flips, scale, and rotation update colliders immediately with zero steady-state allocations when unchanged.
- **Inspector overlay and interactive point dragging (`pkg/editor/asset_import.go`, `asset_import_pivot.go`, `asset_import_ui.go`)**:
  - `DrawImageImportPreview` overlays cached `pack.TraceCollision` polygons and named point diamond markers with labels directly on the image import preview canvas, reusing the same tracing code as `game assets`.
  - Dragging a named point marker buffers live coordinates during pointer motion and commits once on release via `SetImageImportPoint` / `SetImageImportCellPoint` as one undoable `Editor.History` step.

## Alternatives considered

- **Tracing sprite alpha at runtime inside `sprite.LoadAtlas` or `sceneformat.BuildPhysics`** — Avoids adding `shapes` to atlas JSON. Rejected because URI-backed atlases do not expose decoded pixels at load time, tracing every frame at startup wastes CPU and links image-tracing code into runtime binaries, and baking into atlas JSON keeps runtime loading O(vertices) and deterministic.
- **Nesting per-cell point overrides inside `image.points` as a reserved `cells` key** — Avoids a separate `image.cellPoints` field. Rejected because reserving a key inside `points` prevents naming a point `"cells"` and breaks clean draft-07 `additionalProperties` map typing in `import.v1.json`, whereas `image.points` as `map[string]Point2D` matches the natural `{muzzle: [14, 6], feet: [8, 16]}` syntax and `image.cellPoints` mirrors `pivot.cells` and `collision.cells` with ascending integer indices.
- **Using midpoint marching squares instead of pixel-boundary edge tracing** — Produces diagonal chamfers directly on staircases. Rejected because midpoint marching squares shrinks a 1x1 pixel to a half-area diamond and clips 0.5px off every corner of axis-aligned boxes and L-shapes, whereas exact pixel-boundary tracing preserves exact rectangle and L-shape bounds while closed RDP at `epsilon = 0.75px` collapses 1-pixel diagonal staircases cleanly.

## Consequences

- **Benefits**:
  - Artists and developers author collision shapes and attachment points once beside the sprite art, preview and drag them visually in the editor, and get exact per-frame physics and flip/rotation-aware world coordinates with zero runtime image tracing.
  - Existing atlases without `shapes` or `points` emit identical JSON, and Aseprite users can reuse native slice pivots without a sidecar.
- **Costs and known limits**:
  - Adding slice and map fields (`Shapes`, `Points`) to `sprite.Frame` makes `sprite.Frame` non-comparable with `==` in Go; callers comparing frames must compare fields or use `reflect.DeepEqual`.
  - Dynamic 2D rigid bodies (`body2d.kind: dynamic`) with concave multi-part sprite frames approximate the rigid body's single `physics.RigidBody` with a convex hull (up to 8 vertices) while `World2D` queries, kinematic collisions, static rigid bodies, and triggers use every exact convex part.

## Verification

- `go run ./tools/gameschema -check`
- `go tool nsgo test ./pkg/importsettings`
- `go tool nsgo test ./pkg/tooling/pack`
- `go tool nsgo test ./pkg/tooling/assetbuild`
- `go tool nsgo test ./pkg/sprite`
- `go tool nsgo test ./pkg/animclip`
- `go tool nsgo test ./pkg/sceneformat`
- `go tool nsgo test ./pkg/editor -- -run 'TestAssetImport|TestSpriteSheetSlice|TestImagePivot|TestImageImportCollision'`
- `go tool nsgo test ./internal/gamecli -- -run 'TestPack|TestAssets'`
- `go tool nsgo test ./tools/gameschema -- -run 'TestImportSchema'`
- `go tool nsgo check -target host -jobs 2`
- `go run ./tools/changelog check && bun run verify-notes`
