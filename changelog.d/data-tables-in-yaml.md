### Changed

- `game/tooling/datatable`: YAML data tables are a versioned `version`/`rows` document that refuses unknown and duplicate fields, `MarshalYAML`/`SaveYAML` write them deterministically for a lossless round trip, and YAML tables hot-reload like CSV and JSON (#478).
