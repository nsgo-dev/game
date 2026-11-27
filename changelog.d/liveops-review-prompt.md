### Added

- `pkg/online/liveops`: added `ReviewPrompt` over `core.Storage` and `core.Scope.RequestReview` to gate in-app store rating requests on minimum sessions, days since first launch, positive moments, cross-version cooldown, once-per-version limits, negative-moment suppression, and fail-closed versioned JSON state (#1767).
