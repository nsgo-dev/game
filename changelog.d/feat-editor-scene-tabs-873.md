### Added

- `game/editor`: support multiple open scenes in tabs (`SceneTabsView`, `Tabs`, `ActiveTab`, `TabCount`, `SelectTab`, `SaveTab`, `CloseTab`, `RequestCloseTab`, `OpenInstancedScene`, `OpenBaseScene`) with separate per-tab documents, undo histories, selections, 2D/3D view transforms, and dirty markers, automatic rebuilding of open tabs whose instanced or base scenes are saved, and per-tab unsaved-changes confirmation on close while sharing the project file across tabs (#873).
