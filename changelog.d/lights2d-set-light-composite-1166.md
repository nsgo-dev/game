### Added

- `game/lights2d`: add `Scene.SetLightComposite` and `Scene.EffectiveLightComposite` so light glows can explicitly use `core.CompositeSourceOver` while keeping `core.CompositeLighter` as the zero-value default, and preserve explicit `CompositeSourceOver` across `sceneformat.BuiltScene.SyncLights2D` (#1166).
