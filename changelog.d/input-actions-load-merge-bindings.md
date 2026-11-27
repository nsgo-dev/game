### Fixed

- `pkg/input`: `Actions.Load` now merges saved bindings per name instead of replacing every map, keeping default bindings for actions and axes added after the player saved while preserving explicit unbindings saved as `none` or empty; `Actions.LoadReplace` keeps the full-snapshot replacement behavior (#1628).
