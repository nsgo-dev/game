### Added

- `game/text`: signed-distance-field fonts (`SDFFont`) loaded from msdf-atlas-gen JSON or BMFont with a `distanceField` record, resolved at the drawn size so their edges stay sharp at any scale, with outlines, and drawn in 3D world space as camera-facing billboards or fixed-orientation text (`DrawWorld`, `LayoutWorld`) (#348).
