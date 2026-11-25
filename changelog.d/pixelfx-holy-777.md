### Added

- `game/pixelfx/holy`: ported all 10 pixelfx holy effects (`blessing`, `divine-shield`, `feather-burst`, `halo`, `heal`, `heal-aoe`, `holy-pillar`, `resurrection`, `smite`, `sunburst`) and the category's shared helpers (silhouette recolors, floor ovals, feathers of light, plus signs) to Go, with parity tests against pixelfx and `game pixelfx` CLI registration. `divine-shield`'s `radius`, `heal-aoe`'s `reach`, `sunburst`'s `rays` and `holy-pillar`'s `shafts` are held to a finite range, so a negative, infinite or NaN value renders instead of hanging (#777).
