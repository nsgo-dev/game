### Fixed

- `game check`: delegates to a project's own editor command (`editorcmd -check`) when present so scene component fields and connection events, handlers, and arguments are checked against the project's registry, prints a note with `game generate editor` instructions when checking scenes with connections without an editor command, and adds `assetcheck.CheckWith` to `action3d`'s `project_test.go` (#1513).
