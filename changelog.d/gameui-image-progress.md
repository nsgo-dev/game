### Added

- `pkg/gameui`: `ImageProgress`, a read-only progress meter built from under/progress/over `NineSlice` image layers, with linear fills (`left_to_right`, `right_to_left`, `top_to_bottom`, `bottom_to_top`, `center_h`, `center_v`) and radial pie fills (`clockwise`, `counter_clockwise` with `StartAngle` and `Span`) that keep nine-slice caps at any value; the `image_progress` screen node maps `under`/`progress`/`over`/`fill`/`startAngle`/`span`/`min`/`max`/`value` onto it, and `game check`, `refs`, and `game mv` follow the new image fields (#1427).
