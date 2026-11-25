### Added

- `game/sceneformat`: identity-based three-way scene merge (`Merge`, `MergeResult`) matching layers by `name`, objects by `id` anywhere in the tree (including across reparenting and layer moves), tilemaps and overrides by key, and connections by `(from, event, to, handler)`, with field-by-field merging, `Validate` enforcement on the merged result, and a versioned, deterministic YAML conflict report format (`ConflictVersion`, `ConflictDoc`, `ParseConflict`, `EncodeConflict`) (#1058).
- `game/cmd/game`: `go tool game merge` Git merge driver (`%O %A %B %P`, with `-out`, `-conflict-out`, and `-setup`), plus `.gitattributes` registration for `*.scene.yaml` and `*.scene.yml` in every project template and automatic local Git config setup in `game new` (#1058).
