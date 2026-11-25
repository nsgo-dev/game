### Changed

- The starter template's `go.mod` pins `packages/game` to `v0.0.0-20261125083919-59bc0354e207`, a newer commit on `main`, so a game made with `nsgo init -template` or `game new` builds against the game layer's current packages instead of the 2026-11-24 commit it pinned before.
