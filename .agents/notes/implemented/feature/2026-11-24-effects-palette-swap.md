# Agent Note: Palette Swap and Chain-Managed Effect Textures

Status: implemented

## Problem

`game/effects` fuses Kage fragment passes into a single `core.Shader` over the game frame (`image0`), and `Compile` previously refused all `imageNAt`/`imageNSize` references while `View` passed only `[v.Frame]`. Without a way for an effect pass to bind an auxiliary texture alongside the frame, a palette-swap pass cannot sample a palette texture on the GPU or match that behavior in headless CPU `Chain.Reference` tests, leaving issue #95 unimplementable without either breaking pass fusion or hard-coding colors into shader source.

## Decision

`game/effects` extends the effect contract with chain-managed auxiliary textures (`Texture(pos)` and `TextureSize()` in Kage, `Effect.Texture` on `Effect`, `Input.TextureAt`/`Input.TextureSize` in CPU references) and ships two ready palette effects plus a texture builder in `packages/game/effects/palette.go`:

- **Chain texture slot allocation (`chain.go`, `view.go`, `reference.go`)**: `Compile` inspects each effect's AST for unbound references to `Texture` or `TextureSize` and assigns sequential NSGo shader image slots `image1` through `image3` (`maxEffectImages = 3`, since `image0` is the frame and NSGo shaders support `image0`..`image3`). `Chain.HasTexture`, `Chain.Images`, `View.Textures`/`View.TexturesFunc`, and `Chain.ReferenceWithTextures` allow binding a default texture on `Effect.Texture` and overriding it per view or per reference render without recompiling the chain. Effects that do not call `Texture`/`TextureSize` compile to the exact same fused Kage source as before.
- **Color-key palette swap (`PaletteSwap`)**: reads a `W x H` palette texture where row `0` holds up to 256 source key colors (transparent cells are skipped) and rows `0..H-1` hold target palettes. Each non-transparent pixel unpremultiplies its straight RGB and matches against row `0` with per-channel tolerance `max(Tolerance, 0.002/alpha)`. Scaling the minimum tolerance by `1/alpha` accounts for 8-bit premultiplied rounding (`<= 0.5/255` in premultiplied channels) so translucent pixels still match their straight key color while distinct opaque 8-bit colors (`>= 1/255`) never collide. Larger `Tolerance` selects the nearest matching key by squared Euclidean RGB distance (`Tolerance = 1` quantizes every non-transparent pixel to its nearest key).
- **Luminance ramp palette (`PaletteRamp`)**: maps each non-transparent pixel's straight Rec. 709 luminance (`dot(rgb, vec3(0.2126, 0.7152, 0.0722))`) to `floor(l*(width-1)+0.5)` in `Row`, in O(1) texture reads for any palette width.
- **Uniform conventions and premultiplication**: both effects expose `Amount` (`0` to `1`, defaulting to `0` so omitted uniforms pass input through unchanged, preserving out-of-range channel values between chained passes) and `Row` (`0` to `height-1`, fractional values linearly blending adjacent rows so either `Amount` or `Row` animates smoothly on the frame clock). Replacement colors multiply by `min(c.a, 1.0)` directly in premultiplied space (`dst * ca`), keeping output premultiplied even when palette entries are translucent or transparent.
- **Texture builder (`PaletteImage`)**: builds a premultiplied `*image.RGBA` from rows of `core.Color`, padding shorter rows with transparent pixels.

## Alternatives considered

- **Pass palette colors as uniform arrays (`var Keys [N]vec4`, `var Values [N]vec4`) instead of a texture** — Uniform arrays avoid touching `chain.go`, `view.go`, and `reference.go` and require no extra texture bindings. Rejected because issue #95 explicitly requires mapping colors through a palette texture, artists author multi-row palette PNGs directly in tools like Aseprite, and uniform arrays impose fixed compile-time sizes and larger per-frame uniform payloads instead of binding an `ImageSource` once.
- **Allow effects to call `image1At`/`image1Size` directly** — Exposing raw image indices requires no `Texture`/`TextureSize` AST rewriting. Rejected because two textured effects in the same chain would collide on `image1` unless each effect knew its position in the chain; rewriting `Texture`/`TextureSize` to assigned `image1`..`image3` slots keeps effects composable in any order, matching how `Input`/`InputSize` abstracts `image0`.
- **Single-uniform `Row` without `Amount` on `PaletteSwap`** — Omitting `Amount` saves one uniform key when swapping a 2-row palette (`{"Row": 1}` instead of `{"Amount": 1, "Row": 1}`). Rejected because fading directly from the original frame (row `0`) to row `3` by animating `Row` passes through rows `1` and `2`, and omitting `Amount` breaks consistency with `PaletteRamp` (where row `0` is an active palette) and with every other built-in effect (`Bloom`, `VignetteEffect`, `FlashEffect`, `ShakeEffect`), where omitted uniforms leave the frame unchanged.

## Consequences

- **Benefits**:
  - Any game can swap pixel-art palettes (`PaletteSwap`), quantize to a palette (`Tolerance: 1`), or apply multi-row luminance ramps (`PaletteRamp`) in a single fused shader pass with zero extra intermediate render targets.
  - Up to 3 textured effects compose freely in one `Chain` alongside non-textured passes, and `View.TexturesFunc` swaps palette textures reactively without recompiling shaders.
  - Both effects produce identical premultiplied output on GPU and CPU `Chain.Reference`, verified by golden PNGs and fuzz tests across NaN, infinite, and out-of-range uniforms.
- **Costs and known limits**:
  - NSGo's shader bridge caps `MaxImages = 4` (`image0`..`image3`), limiting a single `Chain` to at most 3 textured effects besides the frame; revisit only if NSGo raises `shader.MaxImages` or `game/effects` introduces multi-pass offscreen ping-pong chains.
  - `PaletteSwap` caps its per-pixel linear scan of row `0` at 256 columns to prevent GPU fragment stalls on oversized textures; larger color-mapping tables (such as 3D color-grading LUTs) need a dedicated O(1) indexed LUT pass rather than a linear scan.
  - `Chain.Reference` requires palette textures to carry `Pixels` or encoded `Data` (PNG); `URI`-only asset sources return an error because headless tests have no platform asset loader.

## Verification

- `packages/game/effects/palette_test.go` verifies Kage compilation alone and chained (up to 3 textured effects, refusing a 4th and refusing `Texture`/`TextureSize` declarations), exact/fuzzy/nearest key matching, translucent alpha premultiplication, 256-step grayscale ramp accuracy, PNG `Data` decoding, `View.Textures`/`TexturesFunc` under `nsgotest`, golden PNGs (`testdata/golden/palette-swap.png` and `testdata/golden/palette-ramp.png`), and `FuzzPaletteReference`.
- Run from repository root:
  - `(cd packages/game && go tool nsgo check -target macos,windows,linux,android,web)`
  - `(cd packages/game && go tool nsgo test ./effects/... .)`
  - `go run ./tools/changelog check`
  - `bun run verify-notes`
