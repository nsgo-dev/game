### Added

- `game/tooling/jsonschema`, `cmd/gameschema`: JSON Schemas for the YAML formats, built from the Go types with descriptions from their doc comments; the scene schema is published at `https://game.nsgo.dev/schemas/scene.v1.json`, `sceneformat.Encode` names it in a `# yaml-language-server` comment so editors validate scene files, `datatable.Schema[T]` builds the schema of a versioned YAML data table, and CI fails when a committed schema drifts from its Go types (#501).
