### Added

- `website`: list every project template (`action3d`, `fighting2d`, `fps3d`, `idle`, `platformer2d`, `puzzle2d`, `racing3d`, `roguelike2d`, `rts2d`, `shmup2d`, and `visualnovel`) on the showcase page across all 10 locales (#791, #797, #800, #801).

### Fixed

- `template/visualnovel`: set `OnDemand: true` on `game.Loop` and make `game_test.go` pin the test locale, post direct state mutations through `app.Post`, and step the status timer within `MaxSteps`, so `nsgo test -target macos -activate` advances past the title screen and passes without stalling (#801).
