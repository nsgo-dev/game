### Added

- `examples/lantern3d`: interactive 3D glTF 2.0 diorama scene (`Lantern 3D`) on NSGo's `gpu.View` with `game/gltf`, `render3d` meshes/textures/camera uniforms, `scene3d.OrbitController` mouse/touch/keyboard camera controls, a `render3d.LightRig` (moonlight key, orbiting wisp, shrine glow, ambient) shaded with render3d's metallic-roughness PBR and sRGB base-color textures, native `scene.HUD3D` overlay, `NoGPU` fallback, and cross-target headless tests on full and WebGL 2 / GLES 3.0 GPU profiles (#205).
