### Fixed

- Lazily allocate `tilemap.Tilemap`'s edit ring buffer only on per-cell edits, deduplicate Tiled XML/JSON layer decoders, and pack `world3d.BuildNavMesh` polygon vertices into a single backing slice to restore `LoadTiled`, `NavMeshFromBake`, and WebAssembly size budgets.
