### Fixed

- The starter template's `go.mod` pins `packages/game` to a commit on `main`; the commit it pinned before was dropped when its branch was rebase merged, so a game made with `nsgo init -template` could not download the game layer.
