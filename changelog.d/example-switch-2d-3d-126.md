### Added

- `examples/lantern3d`: switch between the 3D glTF Lantern Shrine (`gpu.View`) and a top-down 2D Courtyard Map (`scene.Scene` + `sprite.Batch`) through `scene.Manager.View` with `scene.Fade`, push a transparent 2D pause overlay without rebuilding 3D GPU pipelines, and share one `*input.Actions` and `*sound.Mixer` (continuous ambient music plus switch chime SFX) across transitions (#126).
