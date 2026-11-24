### Added

- `game generate component|system|scene|prefab <name>` writes a new component, system, scene or prefab into a game project in its conventions: the Go code and a test for each, the scene file of a scene or prefab in the current scene format, and its registration in the `Components`, `Systems` and `Prefabs` registries it declares once, and with `-autoload` or `-main` a scene's entry in `game.yaml`'s autoloads; what it writes builds and passes `game check`, and it refuses to overwrite a file or redeclare an identifier (#505).

### Changed

- The starter's `TestProjectFileIsValidAndCanonical` checks `nsgo.yaml` against `game.yaml` with `CheckNSGo` instead of `project.Check`, which failed on any autoload because the test saw only those two files, and the new `TestAutoloadsLoad` checks the scenes and resources `game.yaml` autoloads from the project directory, skipped under `nsgo test -target` where that directory cannot be read (#505).
