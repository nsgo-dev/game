# Lantern 3D

**Lantern 3D** is an interactive 2D ↔ 3D game example built with NSGo and the Stage 2 3D game layer (`gltf`, `math3d`, `scene3d`, `render3d`, `scene.Manager.View`, `scene.HUD3D`, `input.Actions`, `sound.Mixer`, `game.Loop`).

It pairs two managed scenes through `scene.Manager.View`:

- **3D Lantern Shrine (`gpu.View`)**: loads an embedded binary glTF 2.0 diorama (`assets/lantern_shrine.glb`) containing a stepped stone courtyard, a tiered lantern shrine with a glowing crystal core, four corner lantern bollards, a cedar crate, an azure crystal cluster, an orbiting light wisp, and two embedded PNG textures. Its lights are a `render3d.LightRig` (a directional moonlight key, an orbiting warm point light, the shrine's glow and ambient) uploaded through `render3d.GPULightBuffer`, and each glTF material's base color, metallic, roughness and emissive factors go through `render3d.PBRDrawBuffer` to render3d's metallic-roughness PBR shading, with the base-color textures sampled as sRGB.
- **2D Courtyard Map (`scene.Scene` / `core.Canvas`)**: a top-down 2D overworld view of the same twilight courtyard with a `sprite.Batch` (traveler, corner lanterns, cedar crate, azure crystal cluster) and a glowing central shrine portal. Walking onto the portal and pressing **`E`** (or pressing **`Tab` / `M`** or clicking the HUD switch button anywhere) fades smoothly into the 3D Lantern Shrine and back via `scene.Fade`.
- **Transparent 2D Pause Overlay (`Manager.Push` / `Manager.Pop`)**: pressing **`P`** or **`Escape`** pushes a translucent 2D pause overlay on top of either scene without tearing down or rebuilding the 3D `gpu.View` pipelines underneath.
- **Shared input & shared audio**: a single `*input.Actions` (`Manager.Actions`) drives both 3D camera orbit and 2D traveler movement (resetting held keys cleanly on switch), while a single `*sound.Mixer` on the Manager's parent scope keeps the synthesized twilight ambient music playing uninterrupted across every 2D ↔ 3D transition and plays a temple chime SFX on each switch.

## Controls

- **`Tab` / `M`** (or **2D Map / 3D Shrine** button): fade between the 3D Lantern Shrine and the 2D Courtyard Map.
- **`P` / `Escape`** (or **Pause / Resume** button): push or pop the transparent 2D pause overlay.
- **In 3D Shrine**:
  - **Drag** (mouse left button or 1-finger touch): orbit the camera around the shrine.
  - **Scroll** the mouse wheel, or **pinch** with two fingers or on a trackpad: zoom in and out.
  - **Arrow keys** or **WASD**: orbit camera; **`=` / `-`**: zoom.
  - **`Space`** (or **Orbit** button): toggle gentle auto-orbit.
  - **`L`** (or **Lights** button): toggle the orbiting point light and crystal motion.
  - **`R`** (or **Reset** button): reset the camera to its default vantage point.
- **In 2D Courtyard Map**:
  - **Arrow keys** or **WASD**: walk the traveler across the courtyard.
  - **`E` / `Enter`** (at the glowing central shrine portal): enter the 3D Lantern Shrine.
  - **`R`** (or **Reset** button): return the traveler to the courtyard entrance.
- **`H`**: show or hide the native HUD overlay.
- **Audio** button: mute or unmute the shared audio mixer across both scenes.

## Run, test and cross-check

From `examples/lantern3d/`:

```sh
# Run on the desktop
go tool nsgo dev

# Run headless GPU, 2D <-> 3D switching, shared input/audio, pause overlay, camera, lighting, HUD, and device-loss tests
go tool nsgo test ./...

# Cross-compile check across desktop, mobile and web targets
go tool nsgo check -target macos,windows,linux,android,web
```

## Assets

All geometry and embedded PNG textures in [`assets/lantern_shrine.glb`](assets/lantern_shrine.glb) are original CC0-1.0 works generated deterministically by [`tools/genassets/main.go`](tools/genassets/main.go). The 2D courtyard sprite sheet and ambient/chime WAV audio clips are synthesized deterministically in memory at runtime. See [`CREDITS.md`](CREDITS.md).
