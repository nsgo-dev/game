### Added

- `pkg/sceneformat`: added `ComponentRegistry.Merge` (all-or-nothing merge with collision diagnostics naming both sides), `ComponentTypeSchema`, `ComponentRegistry.JSONSchema`, and `SceneSchema` for deterministic draft-07 scene component schemas (#1271).
- `pkg/jsonschema`: added `Schema.Default` to emit the JSON Schema `default` keyword (#1271).
- `pkg/editor/editorcmd` and `cmd/game`: added `editorcmd -schema` and `game schema [-out file] [project-dir | game.yaml]` to emit a project's deterministic scene JSON Schema without opening a window (#1271).

### Fixed

- `pkg/sceneformat` and `tools/gameschema`: `ComponentTypeSchema` now types `FieldPoint`, `FieldRect`, `FieldCircle`, and `FieldPolygon` fields and emits `default` values in generated scene schemas instead of leaving shape fields untyped (#1271).
