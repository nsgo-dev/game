### Changed

- Starburst: replace the four separate movement `touchRegion` rectangles, invisible fire zone, and 1pt outline hints with on-screen twin-stick `input.TouchControls` loaded from `controls.touch.yaml` (floating left move stick, floating right aim-and-fire stick, and a round `NOVA` button above the aim stick), and declare all six build targets including `ios` in `game.yaml` and `nsgo.yaml`.

### Fixed

- Starburst: keep mouse world aim strictly mouse-driven (`OnMouseMove` and `OnMouseDown`) and clear it on any touch so touch devices no longer aim at a stale pressed point and return to nearest-enemy auto-aim when the aim stick is released.
