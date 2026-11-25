### Added

- `game/editor`: object-by-object Scene Merge Conflict screen (`MergeConflictView`, `OpenMergeConflict`, `OpenMergeConflictSession`, `ResolveMergeConflict`, `TakeAllAgentMergeSuggestions`, `ApplyMergeCustomYAML`, `FinishMerge`, `AbortMerge`) with `MINE`/`THEIRS`/`RESULT` live 2D previews, `CONFLICTS` and `MERGED BY ID` lists, agent suggestions with YAML and reasons, hand-edited YAML validation, `game check` + short playtest verification, Git merge commits, and merge abort (#1059).
- `game/editor/editormcp`: `merge_status`, `merge_resolve`, `merge_finish`, and `merge_abort` MCP tools for inspecting, suggesting, resolving, finishing, and aborting scene merge conflicts (#1059).
