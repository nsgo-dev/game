### Fixed

- `pkg/online/server`: `Matchmaker`'s default rule now removes candidate tickets only when an anchor fills a room, so tickets tentatively checked for an unfilled room remain available to form later rooms in the same `Match` call (#1600).
