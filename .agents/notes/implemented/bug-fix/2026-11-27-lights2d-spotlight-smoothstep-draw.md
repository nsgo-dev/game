# Agent Note: Smoothstep Angular Falloff for Spotlights in lights2d.Scene.Draw

Status: implemented

## Problem

`lights2d.Light.AttenuationAt`, `Scene.EvaluateAt`, and `NormalSprite` all apply smoothstep angular falloff `1 - u*u*(3 - 2*u)` across a spotlight's half-cone (`ConeSpread` in `(0, 2*math.Pi)`), matching the documented contract in `docs/en/lights2d.md`. `Scene.Draw`, however, previously drew a spotlight as a single radial-gradient arc wedge spanning `[ConeAngle - half, ConeAngle + half]`, leaving the rendered glow at full radial brightness all the way to the cone boundary and creating a visible hard pie-slice edge that contradicted CPU illumination queries (#1584).

## Decision

`Scene.Draw` in `pkg/lights2d/lights.go` renders each spotlight as `spotlightBands = 16` concentric nested radial-gradient wedges centered on `ConeAngle`, sharing `smoothstepCone` with `AttenuationAt`:

- **Equal-step inverse smoothstep widths**: wedge `i` (`0..15`) spans `ConeAngle ± (ConeSpread/2) * u_i`, where `u_i = invSmoothstepCone((i + 0.5) / 16)` uses the exact cubic inverse `0.5 - sin(asin(2*w - 1) / 3)` precomputed at init time. Every point inside the cone sits within `1 / (2*16) = 3.125%` of `smoothstepCone(u)` without per-frame trigonometry.
- **Telescoping per-mode gradients**: concentric nested wedges eliminate radial anti-aliasing seams, and each composite mode supported by [Explicit SourceOver Light Composite on lights2d.Scene](../feature/2026-11-25-lights2d-set-light-composite.md) uses a telescoping per-wedge gradient so any prefix of `k` wedges reproduces a single pass at angular weight `k/16`:
  - `CompositeLighter`: `spotlightDeltaGradient` cumulatively quantizes stop alphas to 8-bit steps (`q(wNext) - q(wPrev)`), avoiding systematic multi-pass rounding drift, dim-tail loss, and premature clamping when `Intensity > 1`.
  - `CompositeScreen`: `spotlightScreenGradient` computes per-channel premultiplied steps `L_c*(T_next - T_prev)/(1 - L_c*T_prev)` so the screen product telescopes to `1 - (1 - dst_c)*(1 - L_c*T_k)` on each RGB channel.
  - `CompositeSourceOver` (and other non-additive modes): `spotlightOverGradient` computes `a = (T_next - T_prev)/(1 - T_prev)` so repeated alpha-over compositing telescopes to `dst*(1 - T_k) + L*T_k` with no multi-pass center darkening.
- **No `SetAlpha` override**: all angular weighting lives in gradient stop alpha rather than `d.SetAlpha`, preserving any global alpha the caller set on `d`.
- **Omnidirectional lights unchanged**: lights with `ConeSpread <= 0` or `ConeSpread >= 2*math.Pi` continue to draw a single full-circle `lightGradient`.

## Alternatives considered

### Multiplying a `CanvasConic` gradient by `CanvasRadial`

NSGo's `core.Drawing` has no transparency-layer (`SaveLayer`) operation, and `Scene.Draw(d *core.Drawing, bounds core.Bounds)` has no `*core.Scope` to allocate a `core.OffscreenImage`. Compositing a conic mask onto the main canvas with `CompositeDestinationIn` is unbounded and would clear the underlying world and ambient layer.

### Disjoint side-by-side angular pie slices

Drawing non-overlapping angular slices avoids multi-pass compounding for non-additive composite modes, and each slice uses a single scaled radial gradient. Rejected because adjacent vector rays share anti-aliased edges that produce visible dark hairline cracks and moiré fans near the spotlight apex under `CompositeSourceOver` and `CompositeScreen`, whereas concentric nested wedges have zero internal radial seams.

### Pre-rendered `*image.RGBA` spotlight sprite cached per light

Rendering an exact 2D smoothstep texture into `*image.RGBA` and drawing it with `DrawImageIn` would match `EvaluateAt` continuously in a single draw op. Rejected because `Light` is a plain value struct with no cache lifecycle or `Invalidate` method, animating `Intensity` or `ConeSpread` would trigger per-frame CPU rasterization and GPU uploads across platforms, and vector arcs keep an exact crisp apex at every DPI and radius.

## Consequences

- **Benefits**: Spotlight beams render with smooth feathered edges matching `EvaluateAt` within ~3–5% across inner, transition, outer, and out-of-cone angles under `CompositeLighter`, `CompositeScreen`, and `CompositeSourceOver`, while keeping shadow visibility-polygon clipping, point lights, and caller `globalAlpha` intact.
- **Costs and known limits**: Each visible spotlight emits up to 16 radial-gradient arc fills instead of 1 (zero-alpha wedges are skipped), and the smoothstep curve is a 16-band staircase approximation rather than an analytical per-pixel shader.
- **Verification**: `TestSceneDrawSpotlightSmoothstepMatchesEvaluateAt`, `TestSceneDrawSpotlightReproIssue1584AndSymmetry`, `TestSceneDrawSpotlightCompositeModesAndOmnidirectionalEdgeCases`, and `TestSceneDrawAmbientAndLightsOps` in `pkg/lights2d/lights2d_test.go` verify both platform-independent recorded op evaluation and native windowless `nsgotest.Render` pixel readback against `EvaluateAt`.
