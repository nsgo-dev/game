# Agent Note: Preserve zero parallax on tilemap layers across drawing and Tiled encoding

Status: implemented

## Problem

`pkg/tilemap` treated a layer parallax factor of `0` as "unset / default `1`" in both rendering (`effectiveLayerOffset` skipped `px == 0`) and Tiled JSON encoding (`encodeTiledTileLayer`, `encodeTiledImageLayer`, and `encodeTiledObjectLayer` omitted `0`), because Go's zero value on `TileLayer.ParallaxX`/`ParallaxY`, `ImageLayer.ParallaxX`/`ParallaxY`, and `ObjectLayer.ParallaxX`/`ParallaxY` doubled as the default for programmatically built layers (`nsgo-dev/game#1582`). As a result, a Tiled layer with `parallaxx="0"` or `parallaxy="0"` (or a group-multiplied factor equal to `0`) scrolled with the world camera like `1` instead of staying fixed to the screen, and saving the map through `EncodeTiled` dropped the `0` factor altogether.

## Decision

Separate explicit parallax factors (including `0`) from unset Go struct-literal zero values using a `HasParallax bool` flag and constructors that default to `(1, 1)`, mirroring the existing `TintColor`/`Tinted` pattern on the same three layer structs (see [collection tilesets, tile objects, and layer tints](../feature/2026-11-25-tilemap-collection-objects-templates.md)):

- **Explicit parallax flag (`HasParallax`)**: `TileLayer`, `ImageLayer`, and `ObjectLayer` carry `HasParallax bool`. `effectiveParallax(px, py, hasParallax)` returns `(px, py)` verbatim when `hasParallax` is true, and replaces any zero axis with `1` when `hasParallax` is false so existing zero-value struct literals keep scrolling at `(1, 1)` without breaking backward compatibility in the `Stable` `game/tilemap` package.
- **Constructors and loaders**: `NewTileLayer`, `NewChunkedTileLayer`, `NewImageLayer`, `NewObjectLayer`, `LoadTiled` (dense tile, chunked tile, image, object, and tile-collision layers), and `LoadLDtk` initialize `HasParallax: true` with default parallax `(1, 1)`. Callers using any constructor or loader can assign `layer.ParallaxX = 0` directly without toggling `HasParallax`. `LoadTiled` normalizes IEEE `-0.0` (for example from `-1 * 0` across nested groups) to `+0.0`.
- **Rendering and Tiled encoding**: `effectiveLayerOffset` applies `ox += float32(cx * (1 - px))` whenever effective `px != 1` (so `0` adds `+cx`, cancelling camera translation across tile, chunked, cached, projected, Y-sorted, image, and object layers). `encodeTiledParallax` writes `parallaxx`/`parallaxy` whenever effective parallax differs from `1`, including explicit `0`, and omits `1` (Tiled's default).

## Alternatives considered

- **Change `ParallaxX`/`ParallaxY` to `*float32`**: mirrors `sceneformat.LayerDoc` (see [sceneformat parallax layers](../feature/2026-11-25-sceneformat-parallax-layers.md)) and makes unset vs `0` representable without a boolean field, but `game/tilemap` is a `Stable` package and changing exported field types breaks every caller reading or writing `float32`.
- **Default constructors to `1` without `HasParallax` and always treat `0` as screen-fixed**: avoids adding a field to the three layer structs, but `ImageLayer` and `ObjectLayer` previously had no constructors and existing callers (including package tests) build them as struct literals leaving `ParallaxX`/`ParallaxY` at `0`; treating every `0` as fixed would silently break those layers on moved cameras and write `"parallaxx": 0` in `EncodeTiled`.
- **Unexported `parallaxSet` with `SetParallax(x, y)` methods like `scene.Layer`**: prevents setting `HasParallax` in struct literals, which is awkward because `Tilemap.Layers`, `ImageLayers`, and `Objects` are value slices rather than pointer slices, and diverges from `Tinted bool` already exported on the same three structs.

## Consequences

- **Benefits**: Tiled `.tmx` and `.tmj` layers with parallax `0` (direct or produced by group multiplication) stay fixed to the screen across every draw path and round-trip deterministically through `EncodeTiled` and `LoadTiled`. Existing struct-literal callers remain backward-compatible, and callers using `NewTileLayer`, `NewChunkedTileLayer`, `NewImageLayer`, or `NewObjectLayer` get `(1, 1)` defaults where setting `0` works directly.
- **Costs**: Hand-built struct literals that want `0` without calling a constructor must set `HasParallax: true` (and explicitly set the other axis to `1` if only one axis should be fixed), matching the existing `Tinted: true` requirement for transparent black `#00000000`.
