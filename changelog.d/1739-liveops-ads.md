### Added

- `pkg/online/liveops`: `Ads` wraps a host `AdHost` for mini-game rewarded and interstitial ads, reporting `AdRewarded` only for a video watched to the end and at most once per show, mapping `ErrAdNoFill` to `AdNoFill`, refusing overlapping shows with `ErrAdBusy`, enforcing an interstitial per-session cap, minimum interval and `Disabled` predicate, and calling `Pause` and `ResetInput` hooks around each show (#1739).
