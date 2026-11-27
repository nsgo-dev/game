### Fixed

- `game/lights2d`: `Scene.Draw` now renders directional spotlights with smoothstep angular cone falloff matching `AttenuationAt` and `EvaluateAt` instead of a hard pie-slice edge (#1584).
