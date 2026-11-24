### Added

- `game/tooling/library`: an asset and plugin library: a versioned YAML index anyone can add packs to, pack manifests (`pack.yaml`) for asset, editor plugin and template packs, digests that pin every file, fetching from a directory or over https, and an installer that writes a pack's files into a project, records them with the autoload entries and editor plugins it adds in `game.yaml`, updates an installed pack in place and removes it again, restoring what it changed when a write fails (#493).
- `game/project`: a `packs` section in `game.yaml` recording the installed library packs, and `project.Check` reports a pack file that is missing (#493).
- `go tool game library` lists, installs, updates and removes packs and prints a pack's digest, and `go tool game editor -library` adds a library panel that installs packs into the open project (`Editor.Library`, `InstallPack`, `RemovePack`) (#493).
- `library/`: the shared library's index with a sample asset pack, `dungeon-tiles` (#493).
