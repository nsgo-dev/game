# gametest

`game/gametest` rasterizes recorded `core.DrawOp` slices without a window or GPU, compares rendered frames against golden PNG fixtures, generates visual diff images, and extracts drawn image rectangles for structural assertions.

```go
import "github.com/nsgo-dev/game/pkg/gametest"
```

## Software rendering and golden checks

`RenderOps(width, height, ops, opts...)` replays recorded `core.DrawOp` operations—transforms, alpha, fills, strokes, rounded rects, arcs, ellipses, dashed lines, polygon paths, bitmap text, clears, images and `DrawAtlas`—into a deterministic premultiplied `*image.RGBA`.

`AssertGolden(t, name, got)` compares `got` against `testdata/golden/<name>.png` (and `AssertOpsGolden` combines `RenderOps` with `AssertGolden`). `AssertGoldenAt(t, goldenPath, got)` compares against any path, and `CompareGolden(got, goldenPath, update)` returns the diff image and mismatch count directly.

Before pixel-by-pixel comparison, `CompareGolden` round-trips `got` through PNG encoding and decoding so comparison runs in the non-premultiplied color space stored on disk. Premultiplied `*image.RGBA` frames with transparent backgrounds or partial alpha therefore match a freshly written golden with zero mismatches.

### Updating goldens

Pass `-update-golden` or set `UPDATE_GOLDEN=1` (`true` / `yes`) to write or overwrite golden PNGs instead of failing on differences:

```sh
go tool nsgo test ./pkg/gametest -update-golden
UPDATE_GOLDEN=1 go tool nsgo test ./pkg/gametest
```

## Where failure images go

When `AssertGolden` or `AssertGoldenAt` detects mismatched pixels, it writes two PNGs that outlive the test for inspection—the actual frame (`<name>.got.png`) and the visual diff (`<name>.diff.png`, unchanged pixels dimmed in grayscale and differing pixels highlighted in solid magenta `#ff00ff`)—and prints both paths in the failure message:

- **Default**: next to the golden file, at `<dir>/<name>.got.png` and `<dir>/<name>.diff.png` (for `AssertGolden(t, "scene", img)`, `testdata/golden/scene.got.png` and `testdata/golden/scene.diff.png`). Both patterns (`*.got.png` and `*.diff.png`) are ignored by Git.
- **Custom or CI directory**: set `GOLDEN_DIFF_DIR` to write failure images under that directory instead, so CI can upload one artifact folder. When the golden path lies under `testdata/golden/`, its relative subdirectory is preserved beneath `GOLDEN_DIFF_DIR` to avoid name collisions.

On a later passing or `-update-golden` run, `AssertGoldenAt` removes any stale `<name>.got.png` and `<name>.diff.png` at those paths.

## Comparing image draws

When a test checks where sprites land rather than exact pixels, `ImageDraws(ops)` replays transforms, alpha, composite modes and clips to return each drawn image rectangle regardless of whether it was recorded as individual `DrawImageFrom` ops or packed into a `DrawAtlas`. `AssertSameImages(t, got, want)` asserts that two op recordings produce the same sequence of image draws within floating-point tolerance (`1e-3`).
# Photosensitivity checks

Use the flash limiter tests as a regression guard when recording accessibility scenarios. A recorded run should exercise repeated full-screen flashes with the player's Reduce Flashes setting enabled.
