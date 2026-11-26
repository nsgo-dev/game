# Agent Note: Drop the software GPU workarounds for nsgo-dev/nsgo#2344

Status: implemented

## Problem

Before v0.3.4, NSGo's software GPU renderer, which runs every headless `nsgotest` and `gpu.NewSoftwareRenderer` test, did not clip triangles in clip space: it dropped a triangle with a vertex behind the camera, and a fullscreen triangle at depth 1.0 interpolated to depths just above 1.0 at some pixels, which then failed `CompareLessEqual` against a depth buffer cleared to 1.0 (nsgo-dev/nsgo#2344). The game layer shaped its shaders, a template and its tests around this. NSGo v0.3.4 clips in homogeneous clip space, interpolates depth relative to the first vertex so a constant-depth triangle stays exact, clamps fragment depth to the viewport's depth range and fills shared edges once with a top-left rule, so the workarounds now only cost clarity.

## Decision

The game layer requires NSGo v0.3.4 (#1126) and drops each workaround:

- **Skybox depth**: `SkyboxWGSL` and `SkyboxToneMapWGSL` (`packages/game/render3d/skybox.go`, `ibl_hdr.go`) and the scene renderer's skybox (`packages/game/scenemodel/renderer.go`) emit depth `1.0`, the far plane, instead of `1 - 2^-24`. With `CompareLessEqual` and depth writes off, the sky passes only where the depth buffer still holds its clear value, so it paints over no geometry nearer than the far plane. This supersedes the depth choice in [skybox and IBL](../feature/2026-11-24-render3d-skybox-and-ibl.md).

## Alternatives considered

- **Keep `1 - 2^-24`**: it works on every backend and changes no pixels. It is rejected because its only reason was the software renderer's rounding, it still paints the sky over geometry within the far plane's last depth step, and every reader has to learn why the constant is not the textbook `1.0`.

## Consequences

- **Benefits**: the skybox uses the textbook far-plane depth.
- **Costs and known limits**: the game layer and every template require NSGo v0.3.4; on an older NSGo, headless tests show clear-colour dots in the sky.

## Verification

`go tool nsgo test ./render3d` from `packages/game`: `TestSkyboxKeepsGeometryNearFarPlane` checks that geometry 5 units inside the far plane survives the sky, and the IBL scene tests (`assertNoClearPixels`) check that no clear-colour pixel shows through the sky.
