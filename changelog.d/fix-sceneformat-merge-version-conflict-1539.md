### Fixed

- `pkg/sceneformat`: `Merge` now merges scene schema `version` monotonically as the maximum across `base`, `ours`, and `theirs` (never below `base`) instead of reporting a `scene.version` conflict when two branches independently upgrade a scene to different versions, validating the combined scene at the resulting version (#1539).
