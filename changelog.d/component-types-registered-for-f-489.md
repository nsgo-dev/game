### Added

- `game/sceneformat`: a component type registry (fields with kinds, defaults, ranges, steps and docs, declared without reflection); `ParseWith` refuses unregistered types, misspelled fields, wrong kinds and out-of-range values with the file, line and column, and `game/editor` checks edits against it and lists every registered field in the inspector (#489).
