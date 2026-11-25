### Added

- `game/world3d`: add version 1 YAML formats (`*.gridmap.yaml` and `*.foliage.yaml`) with strict parsing, deterministic canonical encoding, box-run compression, runtime world builders (`LoadGridMapFS`, `LoadTerrainFS`, `LoadFoliageFS`, `TerrainDoc.BuildHeightmap`/`BuildLOD`, `GridMapDoc.Build`, `GridMap.ToDoc`, `FoliageDoc.ScatterAll`), optional `chunkSize` on `TerrainDoc`, published JSON Schemas (`gridmap.v1.json`, `foliage.v1.json`, updated `terrain.v1.json`), and `game check` (`tooling/assetcheck`) validation (#859).
