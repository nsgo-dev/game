# Agent Note: Sprite-sheet slice preview and frame pivots in import sidecars

Status: implemented

## Problem

Sprite sheets could only be sliced by editing `image.slice` in `*.import.yaml` by hand without visual feedback, and atlases built by `game assets` or `game pack` had no way to author or emit per-frame TexturePacker `pivot` coordinates for `sprite.LoadAtlas` (issue #880).

## Decision

Extend the `*.import.yaml` image schema, atlas packing pipeline, and the editor's image import panel from [editor-import-settings](2026-11-25-editor-import-settings.md) as one end-to-end contract:

- **Strict, deterministic `image.pivot` schema (`tooling/importsettings`)**:
  - `Image.Pivot` holds `Default *PivotPoint` (normalized `[x, y]` fractions of the untrimmed frame size, `[0, 0]` top-left to `[0.5, 1]` bottom-center) and `Cells []CellPivot` (0-based `{cell, pivot}` overrides in strictly ascending `cell` order).
  - `PivotPoint` uses custom `UnmarshalYAML` / `MarshalYAML` to require an exact 2-number sequence, reject non-numeric/null/non-finite entries, normalize `-0` to `0`, and always emit compact flow `[x, y]`.
  - `CellPivot.UnmarshalYAML` requires both `cell` and `pivot` keys explicitly and refuses unknown or duplicate keys. `Image.validate()` refuses empty `pivot: {}`, `pivot.cells` without `image.slice`, duplicate or unsorted cell indices, and cells `>= slice.count` when `count > 0`.
  - `Slice.Cells(bounds)` centralizes the exact pixel-rectangle grid math so `tooling/assetbuild`, `cmd/game pack`, and `editor` never drift on margin, spacing, or count calculation.
- **Atlas packing and `sprite.LoadAtlas` wiring (`tooling/pack`, `tooling/assetbuild`, `cmd/game`)**:
  - `pack.Input` and `pack.Frame` carry an optional `*pack.Pivot`; `encodeAtlasJSON` writes `"pivot": {"x": ..., "y": ...}` with `omitempty` so images without pivots keep identical JSON output, while pivoted frames load straight into `sprite.Frame.PivotX` / `PivotY` (properly scaled by `meta.scale` and untrimmed `sourceSize`).
  - Both `assetbuild.Builder.atlas` and `cmd/game pack` apply `image.slice` and `image.pivot` (checking per-cell overrides against the fitted cell count). `assetbuild.Builder.texture` refuses `pivot` on texture-only images unless an atlas also reads the image.
- **Interactive slice-grid & draggable pivot preview (`editor/asset_import_pivot.go`, `editor/asset_import_ui.go`)**:
  - Selecting an image in `FilesView` shows a cached checkerboard preview canvas (`DrawImageImportPreview`) displaying the image, active slice grid cells, and per-cell (or single-frame) pivot crosshairs distinguishing inherited defaults from per-cell overrides.
  - Dragging a marker (`BeginImagePivotDrag` / `UpdateImagePivotDrag` / `EndImagePivotDrag` / `CancelImagePivotDrag`) updates only live preview state during pointer motion and commits once on release through `EditAssetImportSettings` as one undoable `Editor.History` step.
  - Shrinking `slice.count` prunes out-of-range `pivot.cells`, and disabling `slice` clears `pivot.cells` while keeping `pivot.default` valid.

## Alternatives considered

- **Representing per-cell overrides as `map[string]PivotPoint` instead of `[]CellPivot`** — Slightly shorter YAML for single overrides (`"2": [0.25, 0.75]`). Rejected because `yaml.v3` quotes numeric string keys (`'2'`) and sorts them lexicographically (`"10"` before `"2"`), whereas `[]CellPivot` with ascending integer `Cell` produces clean unquoted numeric order, native draft-07 JSON Schema validation (`required`, `minimum: 0`, `additionalProperties: false`) via `jsonschema.Reflect`, and matches `tilemap.TilesetDoc.Tiles`.
- **Adding a separate docked Sprite Sheet Slicer panel instead of extending the Inspector image import view** — Could offer more horizontal width on narrow monitors. Rejected because [editor-import-settings](2026-11-25-editor-import-settings.md) already switches `InspectorView` to the asset import panel when an image is selected in `FilesView`, keeping slice preview, pivot controls, texture/atlas settings, credits, and Reimport in one cohesive inspector without competing for viewport dock height.
- **Writing the sidecar on every `OnMouseMove` during a pivot drag** — Simpler drag state. Rejected because it would spam disk writes and flood `Editor.History` with dozens of undo steps per drag; buffering `livePivot` during drag and committing once on release matches `BeginDrag2D` and `BeginRegionDrag`.

## Consequences

- **Benefits**:
  - Authors visually slice sprite sheets and drag default or per-cell pivots in the editor, get deterministic byte-for-byte round-tripping YAML sidecars with full Undo/Redo, and see rebuilt pivots immediately in `sprite.LoadAtlas` and the live editor viewport after clicking Reimport or Build Assets.
  - `Slice.Cells` guarantees the editor preview, `game assets`, and `game pack` agree pixel-for-pixel on every cell rectangle.
- **Costs and known limits**:
  - Dragging on the preview canvas clamps normalized coordinates to `[0, 1]` rounded to 4 decimal places so markers stay inside their cells; off-frame pivots outside `[0, 1]` remain editable via the numeric X/Y inputs or directly in YAML.
  - Image preview decoding supports PNG and JPEG via Go's standard library codecs; `.webp` files show metadata and settings without a pixel preview until a pure-Go WebP decoder is wired.

## Verification

- `(cd packages/game && env -u GOROOT go run ./cmd/gameschema -check)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./tooling/importsettings)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./tooling/pack)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./tooling/assetbuild)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./sprite)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/gameschema)`
- `(cd packages/game && env -u GOROOT go tool nsgo test ./cmd/game -- -run 'TestAssets|TestPack')`
- `(cd packages/game && TMPDIR=/tmp env -u GOROOT go tool nsgo test ./editor -- -run 'TestAssetImport|TestSpriteSheetSlice|TestImagePivot')`
- `(cd packages/game && env -u GOROOT go tool nsgo check -target macos,windows,linux,android,ios,web ./tooling/importsettings ./tooling/pack ./tooling/assetbuild ./editor)`
- `env -u GOROOT go run ./tools/changelog check && bun run verify-notes`
