### Fixed

- `game/scenemodel`: `Renderer.PrepareScene` now shades unshadowed point and spot lights through `render3d.ClusteredLightSystem` (up to 512 visible local lights on storage-buffer targets, and 16 camera-prioritized visible local lights on WebGL 2 / OpenGL ES 3.0 fallback targets) instead of silently truncating past 16 point or 8 spot lights in file order; `Built.LightRig` and `Built.LightRigNear` pick the lights nearest the camera when truncating; and `game check` warns with file, line and dropped count when a scene exceeds the 16-light fallback cap (#1500).
