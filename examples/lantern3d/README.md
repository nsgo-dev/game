# Lantern 3D

**Lantern 3D** is an interactive 3D glTF scene example built with NSGo and the Stage 2 3D game layer (`gltf`, `math3d`, `scene3d`, `render3d`, `scene.HUD3D`, `game.Loop`).

It loads an embedded binary glTF 2.0 diorama (`assets/lantern_shrine.glb`) containing a stepped stone courtyard, a tiered lantern shrine with a glowing crystal core, four corner lantern bollards, a cedar crate, an azure crystal cluster, an orbiting light wisp, and two embedded PNG textures. The scene is rendered on NSGo's portable `gpu.View` with a depth buffer, a directional twilight key light, an orbiting warm point light, an inner shrine glow, hemisphere ambient lighting, and a native `scene.HUD3D` overlay.

## Controls

- **Drag** (mouse left button or 1-finger touch): orbit the camera around the shrine.
- **Scroll** (mouse wheel or 2-finger pinch): zoom in and out.
- **Arrow keys** or **WASD**: orbit camera; **`=` / `-`**: zoom.
- **`Space`** (or **Orbit** button): toggle gentle auto-orbit.
- **`L`** (or **Lights** button): toggle the orbiting point light and crystal motion.
- **`R`** (or **Reset** button): reset the camera to its default vantage point.
- **`H`**: show or hide the native HUD overlay.

## Run, test and cross-check

From `examples/lantern3d/`:

```sh
# Run on the desktop
go tool nsgo dev

# Run headless GPU, camera, lighting, HUD, and device-loss tests
go tool nsgo test ./...

# Cross-compile check across desktop, mobile and web targets
go tool nsgo check -target macos,windows,linux,android,web
```

## Assets

All geometry and embedded PNG textures in [`assets/lantern_shrine.glb`](assets/lantern_shrine.glb) are original CC0-1.0 works generated deterministically by [`tools/genassets/main.go`](tools/genassets/main.go). See [`CREDITS.md`](CREDITS.md).
