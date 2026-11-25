### Added

- `game/editor`: the 2D viewport fills its panel and pans and zooms through the editor's own view (`View2D`), separate from the scene's camera and never saved: the wheel, a pinch or a touchpad scroll with Control or Command held zooms about the pointer, a touchpad scroll, a mouse wheel's sideways scroll, middle-drag, Space-drag or two-finger drag pans, F and Home frame the selection or everything, and Mod+0 returns to the scene camera's view, whose frame and bounds the canvas outlines and which Play always shows; hit testing, placing, moving, painting and plugin tools go through the view (`CanvasToScreen`, `ScreenToCanvas`, `ScreenToWorld`, `WorldToScreen`) (#864).

### Changed

- `game/editor`: `CanvasToWorld` and `WorldToCanvas` map through the 2D view rather than the scene's camera alone, and a screen-space layer's canvas point goes through `CanvasToScreen`; the 3D viewport fills the panel too and maps presses at `CanvasSize` (#864).
