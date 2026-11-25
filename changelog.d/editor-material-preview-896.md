### Added

- `game/editor`: inspector Material creation/editing actions and a docked Material Editor panel (`MaterialEditorPanelView`) editing all `resource.Material` fields with live `render3d` sphere/cube and 2D sprite previews, live `resource.Library.Reload` viewport updates, Undo/Redo, and canonical `resource.Encode` saving (#896).
- `game/project`: `Resolver.Library` exposes the project's shared `*resource.Library` so tools and the scene editor can live-reload materials in place via `resource.Library.Reload` (#896).
