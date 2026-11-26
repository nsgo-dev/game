### Added

- `game/project`, `game/playtest`: add versioned `brief.yaml` game brief format (`project.Brief`, `ParseBrief`, `EncodeBrief`, JSON Schema `brief.v1.json`) with strict line-numbered validation, gap detection, and structured playtest targets readable by `playtest.TargetsFromBrief` and `playtest.ApplyBriefTargets` (#1026).
- `game/editor`, `game/editor/editormcp`: add Settings › Game brief (`brief.yaml`) and Settings › Project rules (`AGENTS.md`) pages with undoable editing, gap badges, reverted-review rule suggestions, reviewable agent edits (`brief_edit`, `rules_edit`), MCP context exposure (`project_context`, `editor://brief`, `editor://rules`, `editor://context`), the "Agent on this page" card, and Settings search agent dispatch (#1026).
