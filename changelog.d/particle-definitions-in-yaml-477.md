### Added

- `game/particles`: emitter definitions are YAML files with a version field, refused unknown keys and deterministic output; `MarshalConfigYAML`/`UnmarshalConfigYAML` join the JSON codec, and `LoadConfigFile`, `LoadConfigFS`, `SaveConfigFile` and `HotConfig` reloads read and write YAML (#477).
