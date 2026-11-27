# Agent Note: Round-trip golden comparisons in PNG space and persist failure diffs

Status: implemented

## Problem

`pkg/gametest` had two defects that broke golden testing for translucent frames and made mismatch failures impossible to inspect:

1. `CompareGolden` wrote golden fixtures with `image/png` (which stores non-premultiplied 8-bit channels) and later diffed the live premultiplied `*image.RGBA` against the decoded `*image.NRGBA` using exact 16-bit `color.Color.RGBA()` equality. Any pixel with `0 < A < 255` could round differently when un-premultiplied and re-premultiplied (for example `color.RGBA{R: 1, A: 200}`), so a frame with partial alpha failed immediately after `-update-golden`.
2. `AssertGoldenAt` wrote `<name>.got.png` and `<name>.diff.png` into `t.TempDir()` right before calling `t.Fatalf`, so Go's `testing` cleanup deleted the directory before the developer or CI could inspect the reported paths.

## Decision

- **PNG-space round-trip before diffing (`pkg/gametest/golden.go`)**: `CompareGolden` passes non-empty `got` images through an in-memory PNG encode (`png.NoCompression`) and decode (`roundTripPNG`) before calling `DiffImage(gotPNG, want)`. Both sides therefore share the exact pixel representation that `WritePNG` and `ReadPNG` produce across every `image.Image` color model (`*image.RGBA`, `*image.NRGBA`, 16-bit images, grayscale, paletted). Empty `got` images skip encoding so `DiffImage` reports the dimension mismatch instead of a PNG size error.
- **Persistent failure artifacts (`pkg/gametest/golden.go`, `.gitignore`)**: `AssertGoldenAt` writes `<name>.got.png` and `<name>.diff.png` beside `goldenPath` by default (both patterns ignored in `.gitignore`), or under `GOLDEN_DIFF_DIR` when that environment variable is set (preserving relative subdirectories under `DefaultGoldenDir` to avoid collisions across nested golden folders). Write errors are appended to the fatal message instead of silently ignored. On a passing or updating run (`mismatched == 0`), stale `<name>.got.png` and `<name>.diff.png` files at those paths are removed.

## Alternatives considered

- **Convert both sides to 8-bit `color.NRGBA` inside `DiffImage`**: avoids an in-memory PNG round-trip, but loses precision for 16-bit images (`*image.RGBA64`/`*image.NRGBA64`) and duplicates `image/png`'s color-model conversion rules instead of guaranteeing exact parity with `WritePNG`/`ReadPNG`. Keeping `DiffImage` exact and round-tripping in `CompareGolden` with `png.NoCompression` avoids DEFLATE overhead while guaranteeing that any image written by `CompareGolden(..., update=true)` matches `CompareGolden(..., update=false)`.
- **Write failure artifacts to an `os.MkdirTemp` directory left uncleaned**: keeps the repository working tree untouched, but scatters orphaned temp directories on disk and gives CI no stable path to upload. Writing beside the golden (gitignored) with a `GOLDEN_DIFF_DIR` override makes local side-by-side inspection immediate and gives CI a single upload directory.

## Consequences

- **Benefits**: premultiplied translucent frames match freshly updated goldens with zero mismatches while single-step color and alpha regressions are still caught; failure artifacts outlive failing tests and sub-tests locally and in CI; stale diffs are automatically cleaned up once the test passes.
- **Costs and known limits**: `CompareGolden` allocates one uncompressed PNG buffer and decoded image per non-updating check; callers that invoke `DiffImage` directly on a premultiplied `*image.RGBA` and a PNG-decoded `*image.NRGBA` still compare exact 16-bit `RGBA()` values.
