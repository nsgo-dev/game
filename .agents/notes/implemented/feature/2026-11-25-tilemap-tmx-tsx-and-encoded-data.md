# Agent Note: Tiled TMX/TSX and encoded tile data in game/tilemap

Status: implemented

## Problem

Tiled saves maps natively as XML (`.tmx`) with external XML tilesets (`.tsx`) and frequently encodes tile layers and infinite-map chunks as base64 with `zlib` or `gzip` compression. Previously `tilemap.LoadTiled` accepted only uncompressed JSON (`.tmj` / `.tsj`) and asked authors to re-export maps in Tiled, violating the repository rule to read other tools' exports in the form those tools write (#848). Because `game/tilemap` is a `Stable` package, adding TMX/TSX and compressed tile data must keep `LoadTiled` and `TilesetLoader` backwards-compatible, add no third-party dependencies, produce `*loaderr.Error` diagnostics with file, line, field, and fix hints, and resist decompression bombs under fuzzing.

## Decision

Extend `tilemap.LoadTiled` to detect XML vs JSON automatically and decode both into the same internal map representation (`tiledMap`, `tiledTileset`, `tiledLayer`, `tiledChunk`, `tiledObject`, `tiledProperty`) before running a single shared builder (`buildTiledMap`):

- **Format detection and cross-format external tilesets (`packages/game/tilemap/tmx.go`, `packages/game/tilemap/tiled.go`)**: `isXML` skips an optional UTF-8 BOM and ASCII whitespace and checks for `'<'`. Both `LoadTiled` (for maps) and `buildTiledMap` (for each external tileset loaded through `TilesetLoader`) dispatch on `isXML`, so `.tmx`->`.tsx`, `.tmj`->`.tsx`, `.tmx`->`.tsj`, and `.tmj`->`.tsj` all work uniformly without inspecting file extensions.
- **Backwards-compatible `TilesetLoader`**: Add `TilesetLoader.LoadTileset func(source string) ([]byte, error)` and fall back to `TilesetLoader.LoadJSON` whenever `LoadTileset` is nil, so existing callers passing `LoadJSON` automatically load both `.tsj` and `.tsx` tilesets without breaking the `Stable` API.
- **Streaming XML parser with exact source positions (`tmx.go`)**: Parse `.tmx` and `.tsx` with a streaming `xml.Decoder` loop (`xmlReader`) instead of `xml.Unmarshal` structs. This preserves interleaved draw order across sibling `<layer>`, `<imagelayer>`, `<objectgroup>`, and `<group>` elements (matching TMJ's single `layers` array), caps `<group>` nesting depth (`maxXMLDepth = 64`), and records exact 1-based `(line, col)` positions on every intermediate element in O(N) time via `xmlPos`. Errors from TMX/TSX pass `data == nil` to `loaderr.Locate` so precomputed XML lines are preserved, while JSON errors keep using `loaderr.FindField`.
- **Shared tile data decoder with bomb protection (`tmx.go`, `tiled.go`)**: Support JSON arrays, CSV text, legacy XML `<tile gid="..."/>`, and base64 little-endian `uint32` payloads (uncompressed, `compress/zlib`, and `compress/gzip`) on both finite layers and infinite-map chunks in both TMX and TMJ. Strip ASCII whitespace before `base64.StdEncoding.DecodeString` because Tiled indents base64 blocks with spaces. Enforce both an exact per-layer/chunk read cap (`io.LimitReader(r, limit+1)` checking `len > limit` and `r.Close()` checksums), a per-layer tile cap (`maxTiledLayerTiles = 16 << 20`), a per-map total decompressed byte budget (`maxMapDecompressedBytes = 64 << 20`), and chunk bounding-box overflow checks in `buildChunkedTileLayer`.
- **Refuse `zstd` cleanly**: Without an approved third-party decompressor dependency, `validateLayerEncoding` refuses `compression="zstd"` with a `*loaderr.Error` pointing at the `compression` field and line, explaining how to switch Tiled's "Tile Layer Format" to `zlib`, `gzip`, uncompressed base64, or CSV.

## Alternatives considered

- **Separate `LoadTMX` / `LoadTSX` entry points**: Rejected because callers would have to branch on file extension and maintain separate `TilesetLoader` callbacks for cross-format `.tmx`/`.tsj` and `.tmj`/`.tsx` references. Auto-detecting XML vs JSON inside `LoadTiled` matches `text.LoadBMFont` in this repository and lets every existing `LoadTiled` caller and `FuzzLoadTiled` gain TMX/TSX support automatically.
- **Struct-tag `xml.Unmarshal` plus a second line-scanning pass**: Rejected because Go's `encoding/xml` unmarshals `<layer>`, `<imagelayer>`, `<objectgroup>`, and `<group>` into separate struct slices, destroying interleaved draw order (`layerOrder`) when image layers and tile layers mix. A streaming `xml.Decoder` solves draw order, depth limiting, and exact per-element line tracking in one pass.
- **Adding a third-party `zstd` decompressor**: Rejected because `packages/game` requires explicit dependency approval and keeping `game/tilemap` on the Go standard library (`encoding/xml`, `encoding/base64`, `compress/zlib`, `compress/gzip`) avoids WebAssembly and mobile binary size growth while still giving users a clear fix hint.

## Consequences

- **Benefits**:
  - Maps saved in Tiled's native `.tmx`/`.tsx` format (CSV, XML tiles, base64, base64+zlib, base64+gzip, finite or infinite chunked) load to identical `*tilemap.Tilemap` values as `.tmj`/`.tsj`.
  - Zero new external dependencies and negligible overhead on the JSON `LoadTiled` fast path.
  - Structured `*loaderr.Error` diagnostics cover malformed XML, corrupted base64/zlib/gzip streams, `zstd`, decompression bombs, and cross-format external tileset errors with accurate filenames, lines, fields, and fix hints.
  - `FuzzLoadTiled`, `FuzzLoadTiledTMX`, and `FuzzLoadTiledCompressed` fuzz XML parsing and compressed tile payloads in CI (`.github/workflows/quality.yml`).
- **Costs**:
  - Maps saved with Tiled's `zstd` tile compression still require switching compression in Tiled until an approved pure-Go zstd decoder is needed.
  - Layers or normalized infinite chunk grids larger than `maxTiledLayerTiles` (16,777,216 tiles, 64 MiB) or maps decompressing more than 64 MiB in one `LoadTiled` call are rejected to bound memory use against crafted inputs.
