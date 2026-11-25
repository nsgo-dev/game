### Added

- `game/sceneformat`: extend built-in `body3d` (`trigger` kind, `layer`/`mask`, `physicsMaterial` path) and `collider3d`/`trigger3d` (`cylinder`, model-derived `convex` and static `trimesh`, terrain `heightfield`, local `rotX`/`rotY`/`rotZ`), with `BuildPhysics` creating matching `physics3d` bodies, triangle meshes (`TriMeshes3D`), and heightfields (`Heightfields3D`) (#902).
- `game/editor`: add `Gizmo3DCollider` size handles (`ColliderHandles3D`, shortcut `c`) through the 3D gizmo API with single-step undo, draw wireframes for all 3D collider shapes (including cylinder, convex hull, triangle mesh, and heightfield), and add the **Fit to Model Bounds** button (`FitCollider3DToBounds`) in the Inspector (#902).
