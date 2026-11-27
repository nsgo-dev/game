### Fixed

- `pkg/project`, `pkg/refs`: include installed library pack folders (`packs[].dir`, `project.FieldPackDir`) in `project.PathRefs` and `File.EachPath` so `game mv` and the editor rewrite `packs[].dir` in place when a pack folder or its parent directory moves, `game check` validates pack folders, and moving an individual tracked file or subfolder inside a pack without moving its folder is refused (#1607).
