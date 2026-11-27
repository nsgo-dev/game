# Agent Note: Cave Run playfield scaling on landscape phones

Status: implemented

## Problem

Cave Run configures a `320×180` virtual canvas with `mode: fit` and `integerScale: true` in `examples/caverun/game.yaml`. Although `hud-layer` already overlays the full-size `game-canvas` via `Position: core.Absolute`, integer scaling rounds `min(w/320, h/180)` down to a whole integer:

- On a `844×390` landscape iPhone viewport, integer scaling caps at `2×` (`640×360`), leaving vertical letterbox bars instead of filling the view height.
- On a slightly shorter landscape phone viewport (such as `750×340` in a mobile web browser or DevTools emulation, where height falls under `360` points), integer scaling drops all the way to `1×` (`320×180`), leaving the playfield covering less than a quarter of the screen.
- Without `d.SetImageSmoothing(false)` in `drawCanvas`, scaled pixel-art tiles and sprites also blurred and showed slight atlas edge bleeding on fractional and 2x canvases.

## Decision

- `caveRun.effectiveViewport(size)` in `examples/caverun/game.go` derives the active `screen.Viewport` from the configured `g.viewport` (loaded from `game.yaml` and adjustable via `SettingsMenu`): when `IntegerScale` is enabled and either the device is touch-first (`env.Idiom() == core.IdiomPhone || env.Pointer() == core.PointerCoarse`) or whole-integer scaling would drop below `2×` (`min(w/320, h/180) < 2`), it relaxes `IntegerScale` to `false` so `ModeFit` fills the full view height (`2.167×` at `844×390`, `1.889×` at `750×340`). Desktop windows at `2×` or larger (`960×540`, `1000×600`, `1280×720`) keep exact whole-integer scaling.
- `drawCanvas` and `actions.MapPoint` both use `effectiveViewport`, and `drawCanvas` calls `d.SetImageSmoothing(false)` inside `vp.Draw`'s callback (which `vp.Draw` wraps in `Save`/`Restore`), keeping tiles, sprites, particles, and bitmap text sampled with `core.ImageSamplingNearest` while leaving the on-screen touch controls drawn afterwards smooth.
- `examples/caverun/controls.touch.yaml` sizes the d-pad to `104×104` at offset `{x: 4, y: 8}` and the jump button to `96×96` at offset `{x: 20, y: 16}` (updated in [touch controls](../feature/2026-11-27-input-touch-controls.md)) so the wider full-height playfield at `844×390` and `750×340` with landscape iPhone safe areas (`Left: 47, Right: 47, Bottom: 21`) still leaves the explorer's entire `16×16` spawn sprite uncovered.

## Alternatives considered

- **Turn off `integerScale` globally in `examples/caverun/game.yaml`** — simplest change, no per-device branch. Rejected because desktop windows resized to non-integer multiples (such as `1000×600`) benefit from exact whole-integer pixel scaling, and `game.yaml` continues to demonstrate the engine's `integerScale: true` option alongside `SettingsMenu`.
- **Crop vertical rows of the virtual canvas (`ModeFill` or a shorter virtual height) on phones** — fills height while keeping a `2×` integer scale at `340` points. Rejected because cropping vertical rows cuts off the top ceiling or bottom lava floor of the `20×11.25`-tile camera framing, hiding hazards and platforms on upper bridges.
- **Only relax `IntegerScale` when `scale < 2`, leaving `844×390` at `2×`** — preserves exact `2×` pixels at `844×390`. Rejected because `360` points still leaves vertical letterbox bars inside a `390`-point phone screen, whereas nearest-neighbor fractional `ModeFit` fills the full `390`-point height (`2.167×`, 82% view coverage) with crisp pixel edges.

## Consequences

- **Benefits**: Landscape phone screens (`844×390`, `750×340`, and smaller browser viewports) fill 100% of the view height instead of collapsing to `1×`; pixel art renders with nearest-neighbor sampling and no atlas seam bleed; desktop integer scaling and `SettingsMenu` display overrides stay intact; the d-pad and jump button remain clear of the explorer at spawn.
- **Costs and known limits**: Fractional scaling on phones (`2.167×` at `844×390`, `1.889×` at `750×340`) produces slight sub-pixel texel width variation (2 vs 3 device pixels per virtual pixel), which is barely perceptible on 2×/3× Retina phone displays. On an exact 16:9 phone with zero pillarbox (such as iPhone SE `667×375`), the bottom-left d-pad still sits near the leftmost terrain tiles because the explorer spawns two tiles from the left wall.
- **Verification**: `examples/caverun/input_test.go` verifies `game-canvas` fills `844×390` beneath `hud-layer`, asserts `Scale >= 2`, full view height, `> 75%` coverage, `DrawScale >= 2`, and `core.ImageSamplingNearest` at `844×390`, checks `750×340` fills full height without dropping to `1×`, confirms desktop `1000×600` keeps integer `3×`, and checks that neither touch control overlaps the explorer's `16×16` spawn sprite at `844×390` or `750×340` with landscape iPhone safe areas.
