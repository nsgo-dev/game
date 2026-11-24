# Agent Note: CRT curvature and scanlines effect

Status: implemented

## Problem

`game/effects` fuses post-processing passes into one Kage fragment shader with no offscreen texture between passes, and tests each built-in effect headless through a CPU `Reference`. Adding CRT curvature and scanlines (#94) must work cleanly inside that single-draw fused chain: it must not multiply upstream multi-tap passes such as `Bloom`, it must produce visible scanlines at the 2x+ virtual-canvas scaling that pixel-art games use as well as at 1:1 resolution, it must handle non-finite uniforms identically on GPU and CPU, and its curved bezel must stay black even when chained after effects that clamp or fill outside the frame.

## Decision

`effects.CRT()` returns a single-pass Kage `Effect` (`Name: "crt"`) with a matching CPU `Reference` and three scalar uniforms:

- `Curvature`: orthogonal barrel distortion `p' = (p.x*(1 + k*p.y^2), p.y*(1 + k*p.x^2))` on centered `[-1, 1]` coordinates, clamped to `[0, 4]` with `NaN` treated as `0`. The four edge midpoints stay pinned to the frame border while the corners pull inward. Coordinates that warp outside `[0, size)` return opaque black `vec4(0, 0, 0, 1)` directly without calling `Input`.
- `Scanlines`: darkening strength clamped to `[0, 1]` (`NaN` as `0`), modulating RGB while preserving premultiplied alpha via `factor = 1 - s * (0.5 - 0.5 * sin(2*pi * uv.y * lines))` evaluated on the curved `uv.y` so scanlines bow with the glass.
- `Lines`: scanline count across the frame height, clamped to `(0, 16384]`, defaulting to `InputSize().y` when `<= 0` or `NaN`.

When both `Curvature` and `Scanlines` are `<= 0` (or `NaN`), or when `InputSize()` is non-positive, `CRT` skips the warp and scanline math and returns `Input(pos)` unchanged so intermediate out-of-range values pass through a chain untouched. `Input(samplePos)` appears once in the Kage source.

### Invariants

- Every pixel of an opaque frame stays opaque (`alpha = 255`), including the curved bezel and darkened scanline troughs, for all finite and non-finite uniform values.
- Translucent pixels inside the screen keep their alpha and remain premultiplied (`0 <= rgb <= a`).
- `CRT` reads its upstream `Effect` at most once per output pixel, and zero times on bezel pixels outside the curved glass.

## Alternatives considered

### Why not radial barrel distortion `p * (1 + k * dot(p, p))`?

Radial distortion is isotropic and matches a spherical lens with one dot product. However, it shrinks the usable screen even at the four edge midpoints (`(±1, 0)` and `(0, ±1)` already pull inward by `1 + k`), leaving black bands on all four sides unless compensated by a zoom factor that alters center scale. Orthogonal `p.x*(1 + k*p.y^2), p.y*(1 + k*p.x^2)` keeps the center scale intact and pins the edge midpoints to the border, matching the classic curved-tube silhouette.

### Why not cosine scanlines `0.5 - 0.5 * cos(2*pi * uv.y * lines)` or omitting `Lines`?

Cosine puts the bright peak at the exact center (`r + 0.5`) of each frame row and dark troughs on row boundaries (`r` and `r + 1`), which aligns symmetrically with pixel centers. At 2x view scaling with one scanline per frame row (`lines = size.y`), however, pixel centers sample `uv.y * lines` at `r + 0.25` and `r + 0.75`, where cosine evaluates to `0` at both points and makes scanlines invisible. Sine evaluates to `+1` and `-1` at `r + 0.25` and `r + 0.75`, giving full peak-to-trough contrast at 2x and smooth ramps at 3x and 4x. Exposing `Lines` also avoids Nyquist aliasing for 1:1 frames, where `size.y` cycles cannot resolve in `size.y` pixels.

### Why not include chromatic aberration or built-in vignette in `CRT`?

Bundling RGB subpixel fringe and corner darkening into one effect would let a single `CRT()` pass approximate a complete retro monitor preset. Chromatic aberration requires three offset `Input` taps (red, green, blue), which in a fused chain such as `MustCompile(Bloom(), CRT())` would run `Bloom`'s 17 taps 51 times per pixel. Keeping `CRT` single-tap preserves chain performance; games that want darkened corners already compose `CRT()` with `VignetteEffect()`.

### Why not rely on `image0At` transparency for the curved bezel?

Returning `Input(samplePos)` directly without a bounds check is simpler and lets a caller's underlying view show through the corners when `CRT` is the only pass. When chained after `ShakeEffect` (which clamps coordinates to `[0.5, size-0.5]`) or `FlashEffect` (which blends toward an opaque color), out-of-range coordinates return clamped or opaque pixels instead of transparent bezel. Explicit bounds checking against `[0, size)` returning opaque black `vec4(0, 0, 0, 1)` keeps the bezel black regardless of upstream passes and prevents light window backgrounds from leaking through corners.

## Consequences

- **Benefit:** `CRT()` works standalone (curvature only, scanlines only, or both) and chains cleanly after `ShakeEffect`, `Bloom`, and `VignetteEffect` without multiplying upstream taps (1 upstream call in the fused shader).
- **Benefit:** Both 2x+ pixel-art frames (with default `Lines: 0`) and 1:1 frames (with explicit `Lines`) render crisp scanlines that match between GPU shaders and CPU golden reference tests.
- **Cost:** Games wanting RGB phosphor fringe or soft bezel feathering must add a separate pass or compose with `VignetteEffect`; the curved bezel edge is a hard pixel boundary rather than anti-aliased. Revisit adding optional bezel softening if high-DPI non-pixel-art games report visible stair-stepping along curved edges.

## Testing

- `packages/game/effects/crt_test.go` verifies golden reference output (`testdata/golden/crt.png`), corner bezel and edge-midpoint geometry, 2x default and 1x custom `Lines` scanlines, zero/negative/`NaN` passthrough in isolation and in a chain, single-tap fused chain source, animated uniform mounting via `nsgotest`, and fuzzing across finite and non-finite uniforms (`FuzzCRTReference`).
- Verified with `(cd packages/game && go tool nsgo test ./effects/...)` and `(cd packages/game && go tool nsgo check -target macos,windows,linux,android,web)`.
