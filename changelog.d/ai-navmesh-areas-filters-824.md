### Added

- `game/ai`, `game/world3d`: add per-polygon navigation areas (`NavArea`), runtime traversal flags (`NavFlags`), build-time tagging (`BuildNavMeshWithAreas`, `NavPolygonInput`), O(1) runtime area/flag mutation without rebuilding (`PolygonArea`, `SetPolygonArea`, `PolygonFlags`, `SetPolygonFlags`, `AddPolygonFlags`, `ClearPolygonFlags`), and shared query filters (`NavFilter`, `DefaultNavFilter`, `FindPathWithFilter`, `FindPolygonWithFilter`, `ContainsWithFilter`, `NearestPolygonWithFilter`) with admissible area-weighted portal A* across both 2D and 3D navigation meshes (#824).
