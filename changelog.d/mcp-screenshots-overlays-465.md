### Added

- `game/mcp`: `game_screenshot` tool captures the game view as PNG (returning base64 inline or writing confined inside `Config.ScreenshotDir`) with optional pixel-aligned overlays for sprite bounds (`bounds`), 2D physics collision shapes (`collision_shapes` / `physics`, covering `physics.World` boxes, circles, triggers, solid/one-way tiles and `physics.RigidWorld` bodies), world-space reference grid (`grid`), camera frame (`camera`) and bitmap labels (`labels`), aligned across `tilemap.Camera` pan/zoom/parallax and `screen.Viewport` scaling/letterboxing (#465).
