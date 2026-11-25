### Added

- `game/editor`: the 3D viewport draws the scene as the game does. A `gpu.View` fills the panel and draws the scene's `scenemodel` build (`Scene3D`), its primitives and glTF models shaded and textured with render3d, lit by `Editor.Lights3D` or by `DefaultLights3D` under a gradient sky, with the grid, axes, selection outline and gizmo on the canvas over it. Lit, Unlit and Wireframe shading (`SetShading3D`) are in the toolbar, `View > 3D Shading` and the command palette. Models, material files and images load through the `Resolver` or from the open project's files, and a scene that does not build says why in the viewport's corner (`Viewport3DError`). `OrbitCamera3D.SceneCamera` gives the `scene3d.Camera` the view draws through (#890).
- `game/scenemodel`: `Renderer.SetShading` switches between `ShadingLit` and `ShadingUnlit`, which draws each part at its textured base color plus its emissive color and ignores the lights (#890).

### Changed

- `game/editor`: a press in the 3D viewport selects a mesh or model by its triangles instead of a sprite-sized box, and the geometry an instanced scene adds selects its instance; `ObjectCorners3D` and `ObjectAABB3D` give such an object's box as its geometry's bounds. A gizmo drag, the animation clip's pose and a playing game move the built geometry in place without building it again. Where the target has no GPU the wireframe view remains (`Viewport3DOnGPU`) (#890).
