### Added

- `game generate component|system|scene|prefab <name>` writes a new component, system, scene or prefab into a game project in its conventions: the Go code and a test for each, the scene file of a scene or prefab in the current scene format, and its registration in the `Components`, `Systems` and `Prefabs` registries it declares once, and with `-autoload` or `-main` a scene's entry in `game.yaml`'s autoloads; what it writes builds and passes `game check`, and it refuses to overwrite a file or redeclare an identifier (#505).

### Changed

- The starter's `TestProjectFileIsValidAndCanonical` checks the project directory itself, so the scenes and resources `game.yaml` autoloads must exist and load, where it used to see only `game.yaml` and `nsgo.yaml` and failed on any autoload (#505).
