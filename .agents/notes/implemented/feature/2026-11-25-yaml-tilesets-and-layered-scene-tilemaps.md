# Agent Note: YAML tilesets and layered tilemaps in scene files

Status: implemented

## Problem

A scene's own tilemap was one inline grid of atlas frame numbers that `sceneformat.Build` turned into a `sprite.Batch` (#847). A level made in the NSGo editor therefore had no collision flags, tile properties, animated or flipped tiles, layers or culling, and a scene that named a Tiled `.tmj` needed a `TilemapResolver` every game wrote itself, which could only hand back that same flat grid. The runtime `tilemap.Tilemap` already did all of this for Tiled and LDtk maps, but nothing in the repository's own text formats reached it.

## Decision

### The tileset format lives in package tilemap

`packages/game/tilemap/tileset_doc.go` defines `TilesetDoc`, the version 1 `*.tileset.yaml` format (`TilesetFormatVersion`, schema `tileset.v1.json`), with `ParseTileset`, `EncodeTileset`, `ValidateTileset` and `LoadTilesetFS`. A tileset is either a grid image (`image`, `columns`, `tileCount`, `spacing`, `margin`) or atlas frames (`atlas`, `frames`), never both. `tiles` lists only the tiles that carry more than a picture, in strictly ascending `id` order, which makes the canonical form unique and catches duplicates. Paths (`image`) are slash-separated from the project root, like every other path the repository's own YAML writes; Tiled and LDtk paths stay relative to their file.

`Tilemap.AddTilesetDoc(doc, firstGID, TilesetAssets)` builds it: properties become `Properties` typed as Tiled types them (`bool`, `int`, `float`, `string`), `solid`/`oneWay`/`flags` go through `SetFlags` (the same path Tiled's `solid` property takes, so tileshape, `FlagsAt`, `Raycast` and fog see them), shapes become `Tileset.Shapes` exactly as Tiled's collision editor's do, and animations become `Tileset.Animations`. Terrains become `Tileset.Terrains` (`Terrain`, an `AutotileSet` in GIDs plus `Contains`), so `ApplyAutotile` and `SetAutotile` take them directly. A tile's terrain neighbours are full direction words (`north` ... `northwest`), not `n`/`e`, because YAML 1.1 readers take `n` and `y` for booleans and yaml.v3 quotes them.

Property values are scalars only. `EncodeTileset` writes a whole-number float as `2.0` (`wholeFloat`), so `scale: 2.0` reads back as a float, which a round trip through `float64` would otherwise turn into the int `2`.

### Layered grids in scene version 2

`sceneformat.TilemapDoc` gains `tilesetFile`, `tileLayers` and, for LDtk sources, `level`. Scene version 2 is not released yet (its changelog fragment is still in `changelog.d`), so these join version 2 instead of opening a version 3; a version 1 scene refuses them, and `cmd/gameschema`'s `sceneV2Fields` leaves them out of `scene.v1.json`.

A tile layer's cells are `rows`, one string per row of space-separated cells: `.` for empty, otherwise the tileset's local tile ID followed by any of `h`, `v`, `d` (Tiled's flip bits). One row per line keeps a painted cell a one-line diff, and the IDs are the same numbers the tileset file's `tiles[].id` uses. `Parse` accepts aligned rows and any letter order; `Encode` writes the canonical form through `canonicalTilemaps`, without changing the document. In memory a cell is a tile value, the GID the built map gives it: local ID plus 1 with `tilemap`'s flip bits (`TileLayerDoc.Cell`, `SetCell`, `Cells`, `ParseTileRow`, `FormatTileRow`). A grid names one tileset, which always starts at GID 1.

### Build

A layered grid and an external source both become a `tilemap.Tilemap` in the new `BuiltLayer.Map`; a single grid of atlas frames still becomes the `BuiltLayer.Tilemap` sprite batch, unchanged. Build reads tilesets and maps through the optional `FileResolver` and images through the optional `ImageResolver` (else `core.ImageSource{URI: path}`, as `LoadTiled`'s default is); `FSAssets` implements `FileResolver` and `SceneResolver` over an `fs.FS`, which is the built-in resolver that lets a scene use `.tmj`, `.tmx` and `.ldtk` sources with no game code. A resolver that implements `TilemapResolver` keeps loading sources itself first, so existing games behave as before, except for a source that names an LDtk `level`: `Tilemap(source)` has no way to receive the level, so Build reads that source through the `FileResolver` rather than let the resolver silently build another level.

`BuiltScene.Draw` culls a map to the scene's view: a `tilemap.Camera` of the virtual viewport placed where `drawWithCamera` places the scene camera (clamped to its bounds), or the bare viewport for screen-space layers and scenes without a camera. Without a virtual size nothing is culled. `BuiltScene.DrawAllTiles` turns culling off; the editor sets it on its edit scene and on an open animation clip's preview scene, both drawn through its 2D view that pans past the camera's frame, and leaves it off in Play mode, where `playScene.Update(dt)` now also advances animated tiles.

### Editor, check and references

The editor paints layered grids with the same commands: `ActiveTileLayer` picks the tile layer (first by default), `ActiveTileFlip` the flips, and a palette tile ID is the local ID plus 1 so that 0 keeps meaning "empty" as it does for `PaintTile` on single grids; the palette's labels and the status bar show `PaletteTile.Number`, the local ID the rows write, so the UI and the file name a tile the same way. `game editor` points the editor's resolver at the open project's files (`FSAssets` over the directory of `game.yaml`, keeping the demo atlases), so layered grids and Tiled and LDtk sources build in the shipped editor too. A command stores tile values as `int64` so one `tileChange` holds either kind, and applies a batch by parsing and rewriting each touched row once. The palette of a layered grid reads the tileset through the resolver's `FileResolver`; [the tile palette note](2026-11-25-editor-tile-palette.md) still describes single grids.

`tooling/assetcheck` checks `*.tileset.yaml` files (parse, image exists and is large enough, atlas frames exist), a layered grid's `tilesetFile` and its cells against the tileset's tile count, `.tmx` sources (loaded with `tilemap.LoadTiled`, their external `.tsx`/`.tsj` tilesets and images checked), `.ldtk` sources, and an LDtk `level`. A YAML tileset's blob terrain refuses a corner neighbour without both edges beside it, since `ComputeMask8` never produces that mask, and an animation frame must name its `tile`, so a left-out field never becomes tile 0 and a save never adds it; the tileset schema allows a duration only as a string, as `go.yaml.in/yaml/v3` decodes `time.Duration`. `refs` records `tilesetFile` (`FieldTilesetFile`) and a tileset's `image`, and reads `*.tileset.yaml` files as `KindTileset`, so `game mv` rewrites them.

## Alternatives considered

- **Keep `data: [...]` and encode flips as Tiled GIDs with high bits** — no new syntax, and the editor's existing int grid code would carry over. Rejected because `2147483651` is unreadable and undiffable, which breaks the repository's "every file a person writes is text a person can read" rule; the chunk format (`tilemap#chunk`) can afford it only because a program writes it.
- **A parallel sparse `flips` list beside integer data** — keeps `data` numeric. Rejected because a cell's tile and its flips would live in two places that must be kept in step by hand, and one flat list still makes every edit a change to one very long line.
- **Several tilesets per grid with Tiled-style `firstgid`** — closer to Tiled. Rejected for now: the IDs in rows would stop matching the tileset file's IDs, adding tiles to one tileset would renumber the next, and the issue asks for one tileset reference. A second tileset can be added later as a list without changing existing files.
- **Build single grids into a `tilemap.Tilemap` too** — one code path. Rejected because `BuiltLayer.Tilemap` is public, its tests pin a single `DrawAtlas` call, and nothing a single grid can express needs the map.
- **Scene format version 3 for the new fields** — the strict reading of "new fields need a version". Rejected because version 2 has not shipped; a third schema and version gate would only make files carry a number nobody has seen released.
- **A new package for the tileset document** — mirrors `sceneformat`. Rejected because the document is the tilemap package's own data (it builds `Tileset`, `TileShape`, `TileFlags` and `AutotileSet`), and a separate package would need those exported only to be copied across.

## Consequences

- **Benefits**: a scene built with `FSAssets` gets the full tilemap runtime (flags, properties, shapes, animation, terrains, culling, `Raycast`, `tileshape.New`) from text files alone, and a Tiled or LDtk map named in a scene needs no game code. Loading and saving a tileset or scene changes no byte (conformance tests on `tilemap/testdata/*.tileset.yaml` and `sceneformat/testdata/canonical/layered_tilemap.scene.yaml`).
- **Costs and known limits**: one tileset per layered grid; a grid's cells are capped at `1 << 24` over all its layers and `MaxTileLayers` layers. Layers have no offset, parallax or y-sort fields yet. The editor paints but does not autotile with terrains, and its palette lists at most 16384 tiles. Culling uses the virtual viewport, so a game that draws a scene into a canvas larger than its virtual size must set `DrawAllTiles` or a virtual size that matches. Revisit the one-tileset rule if a real level needs tiles of two images in one layer.

## Verification

- `go tool nsgo test ./tilemap` and `./sceneformat` from `packages/game`: `TestTilesetFormatConformance`, `FuzzParseTileset`, `TestLayeredTilemapDrawsVisibleTilesAndAnimates`, `TestTiledSourceBuildsWithoutAResolver`, `TestLDtkSourceBuildsTheNamedLevel`.
- `go tool nsgo test ./editor`: `TestPaintLayeredTilemap`, `TestTilePaletteShowsALayeredGridsTileset`, `TestClipPreviewDrawsEveryTileOfALayeredGrid`; `go tool nsgo test ./cmd/game`: `TestEditorReadsTheOpenProjectsFiles`; `./sceneformat`: `TestTilemapResolverLeavesLDtkLevelsToBuild`; `./tooling/assetcheck`: `TestTMXTilemapSources`.
- `go tool nsgo test ./tooling/assetcheck ./refs`, and `go run ./cmd/gameschema -check`.
