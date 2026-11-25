### Added

- `game/pixelfx/melee`: ported all 12 pixelfx melee effects (`claw-swipe`, `dagger-flurry`, `double-slash`, `hammer-smash`, `heavy-cleave`, `scythe-reap`, `spear-sweep`, `spin-slash`, `sword-slash`, `thrust`, `uppercut`, `whip-crack`) to Go, with their size options (`radius`, `reach`, `length`) kept between 1 and 416 px, each with its act for the stock character, the category's swing, slash, crack, rock and star helpers, parity tests against pixelfx's own frames, and `game pixelfx` CLI registration (#764).
