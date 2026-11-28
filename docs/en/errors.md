# Errors an agent can act on

Every loader and validator in this module returns the same error type when a file cannot be used: `*loaderr.Error` from `github.com/nsgo-dev/game/pkg/loaderr`. It names the file, the line and column, the field, what was wrong, what was expected and how to fix it, so a person, or an agent reading a test or build log, can open the file at that line and correct it without reading the loader's source.

```text
levels/cave.scene.yaml:14:7: layers[1].space: layer "hud" has invalid space "iso"; expected "world" or "screen"; fix: set space to world or screen, or remove it for world
```

The message reads `file:line:column: field: problem; expected E; fix: F`, leaving out the parts that are not known. Editors and terminals turn the `file:line:column` prefix into a link.

## The fields

| Field | Holds |
| --- | --- |
| `File` | The file's path or name. A loader that reads a path fills it in; one that is handed bytes leaves it to its caller (see below). |
| `Line`, `Column` | 1-based position of the problem, 0 when it is not at one place, such as a file that cannot be read. |
| `Field` | Where in the document, as a path: `layers[2].objects[0].id`, or `tilemaps["a.b"].ref` for a key that holds a dot. Empty for the document as a whole. |
| `Problem` | What is wrong. |
| `Expected` | What the loader expected there, when the problem does not already say it. |
| `Fix` | What to change so the file loads. Every error a loader returns has one. |
| `Err` | The underlying error: `errors.Is` and `errors.As` see through an `Error`, so sentinels such as `save.ErrCorrupt` and `modding.ErrInvalidManifest` still match. |

`loaderr.As(err)` returns the `*Error` in a chain.

## Naming the file

A loader that reads a path, such as `particles.LoadConfigFile`, `gltf.LoadFS`, `replay.LoadFile`, `datatable.LoadFile` or `editor.OpenFile`, names that path in its errors. A loader handed bytes, such as `sceneformat.Parse` or `tilemap.LoadTiled`, knows the line but not the file, and writes `<input>` in its place; the code that read the file names it:

```go
data, err := fs.ReadFile(assets, "levels/cave.scene.yaml")
if err != nil {
	return err
}
doc, err := sceneformat.Parse(data)
if err != nil {
	return loaderr.WithFile(err, "levels/cave.scene.yaml")
}
```

`WithFile` names the file only on an error that is itself a `*loaderr.Error`, as every loader's entry point returns. An error that wraps one, such as `fmt.Errorf("level 3: %w", err)`, is returned unchanged, so its context and sentinels still match; call `WithFile` before wrapping.

Values kept in `core.Storage`, such as input bindings, debug tunables and save slots, name their storage key as the file. A save slot in a `FileBackend` names its file, `<dir>/<slot>.json` or `<dir>/<slot>.json.bak` for the backup copy.

## What each loader reports

| Package | Loader | Lines |
| --- | --- | --- |
| `sceneformat` | `Parse`, `ParseWith`, `ParseConflict` | Decode errors and every schema rule, by field; the `*FieldError` `Validate` returns stays in the chain, and `ParseWith` names `ParseOptions.File` |
| `project` | `Parse`, `ParseBrief`, `LoadFile` | Project and brief schema rules, by field |
| `resource` | `Parse`, `ParseAs`, `Library.Load`, `Library.Reload`, `Library.ReloadFile` | Header, material, curve, gradient, animation, theme, bus layout and physics material rules, by field; `Library` names the resource path |
| `importsettings` | `Parse`, `ParseAssetSource` | Sidecar sections, pivots, slices, credits and asset provenance fields, by field |
| `tooling/refs` | `ParseIndex` | Project index paths, ordering, UIDs and SHA-256 digests, by field |
| `particles` | `UnmarshalConfigYAML`, `UnmarshalConfigJSON`, `LoadConfigFile`, `LoadConfigFS`, `HotConfig` | Decode errors, durations, colors and enumerations, by field or at the value's line |
| `skeletal` | `Load` | Bones, slots, skins and keyframes, by field |
| `anim3d` | `LoadEventsSidecar`, `LoadEventsFromGLTFExtras` | Decode errors and sidecar schema rules, by field |
| `animclip` | `ParseStateMachine` | State machine states, transitions and parameters, by field; `*StateMachineError` stays in the chain |
| `sprite` | `LoadAtlas` | Frames, frame tags and nine-slices, by field |
| `tilemap` | `LoadTiled`, `LoadLDtk`, `LoadLDtkAll` | Map, tilesets (an external tileset names its own file), layers, objects and properties, by field |
| `l10n` | `Catalog.LoadJSON`, `ParseAssetManifest`, `AssetCatalog.LoadAssetManifestYAML` | Keys, asset variants and groups, by field |
| `input` | `Actions.Load`, `Haptics.Load` | Stored bindings, by field, under the storage key |
| `modding` | `ParseManifest`, `ModManifest.Validate`, `Manager.Discover`, `Manager.DiscoverFS`, `Manager.Reload` | Each manifest field, a game version the mod does not support and a duplicate id, naming `<mod>/mod.yaml` |
| `gameplugin/manifest` | `Parse` | Plugin manifest fields and capabilities, by field |
| `gameui` | `ParseScreen` | Screen layout tree, device classes and widgets, by field |
| `replay` | `Decode`, `LoadFile` | Header and steps, by field, such as `steps[3].press[0]` |
| `statedump` | `Parse`, `ReadFile` | Header, scene state, values, entities and JSON-incompatible YAML nodes, by field or node line |
| `undo` | `DecodeLog` | Version, counts, required keys and moves, by field |
| `save` | `Store.Load`, `Store.LoadChecked`, `SettingsStore.Load` | Envelope, data and checks, naming the copy's file or storage key (a custom `Backend`'s copies by slot, `slot (backup)` for the backup) |
| `entities` | `LoadWorld`, `World.Restore` | Entity indices, parents and components, by field |
| `assets` | `ParsePackManifest`, `Manager.LoadAtlas`, `Group` atlases | Decode errors; atlases name their URI |
| `world3d`, `voxel`, `render3d` | `LoadMeshLibraryFromGLTF`, `world3d.Parse*`, `voxel.Parse`, `ParseLightProbeGrid`, `LoadLightProbeGridFS` | Version, bounds, counts, meshes, chunks and probe coefficients, by field; `render3d.ErrInvalidProbe` stays in the chain |
| `ai`, `pathfind/navbake`, `pathfind3d/navbake3d` | `ParseGOAP`, `ParseUtility`, `navbake.Parse`, `navbake3d.Parse` | Facts, actions, goals, curves, polygons and off-mesh links, by field |
| `quest`, `rooms`, `loot`, `inventory`, `ability`, `stats`, `cards`, `worldmap`, `cutscene`, `vn`, `bullets`, `framedata`, `fighting`, `platformer`, `vehicle`, `rts`, `rhythm`, `idle`, `procgen`, `quality` | Each package's YAML `Parse*` and `Load*` entry points | Decode errors, custom durations/ranges and every validation rule, by field; sentinels such as `quest.ErrInvalidBook`, `rooms.ErrInvalidMap` and `stats.Err*` stay in the chain |
| `online/accounts`, `online/accounts/authtoken`, `online/achievements`, `online/cloudsave`, `online/leaderboard`, `online/leaderboard/selfhost`, `online/liveops`, `online/netplay/wire` | Each package's YAML `Parse*` and `Load*` entry points | Service configs, key sets, catalogs, reminders and wire schemas, by field |
| `tooling/playtest` | `ParseConfig`, `LoadConfigFile`, `ParseReport`, `LoadReportFile` | Scope, runs, bot config, metrics, goals and report sections, by field |
| `editor` | `OpenFile`, `ParseEditorSettings`, `ParsePermissions`, `ParseAutomations`, `ParseLayout`, `ParseRecentProjects`, `ParseCheckpointManifest`, `ParseAgentThread`, `ParseAgentThreadYAML`, `parseViewState` | Editor configs, layouts, checkpoints, threads and view state, by field |
| `gltf` | `Load`, `LoadFS` | The top-level item at fault, such as `nodes[3]` |
| `text` | `LoadBMFont`, `LoadBMFontImage` | The record at fault, in text and XML fonts |
| `dialogue` | `ParseYarn` | The script line at fault |
| `datatable` | `LoadCSV`, `LoadJSON`, `LoadYAML`, `LoadFile` | The cell at fault; `CellError` stays in the chain |
| `tooling/devstate`, `tooling/hotreload`, `tooling/library`, `tooling/assetbuild` | `Store.LoadSnapshot`, `ParseMaterial`, `ParseIndex`, `ParseManifest`, `parseCache` | Decode errors and manifest, index and material rules, by field |
| `tooling/gamedebug` | `Console.LoadTunables` | The tunable at fault, under the storage key |
| `tooling/bench`, `tooling/sizecheck` | `bench.Decode`, `bench.LoadFile`, `sizecheck.LoadBudget`, `sizecheck.LoadMobileBudget` | Decode errors, limits and benchmark metrics, by field |

A file that cannot be read has no line: its error names the file, keeps the read error in `Err` and still gives a fix.

## Writing a loader

1. Validate the decoded document with `loaderr.Errorf(field, fix, format, args...)`, building `field` with `loaderr.Path("layers", i, "objects", j)` or extending one with `loaderr.Join(at, "alpha")`. A validator does not know lines, so it names the field instead.
2. In the loader's entry point, pass every error through `loaderr.Locate(err, file, data, fix)` once (or `loaderr.LocateJSON(err, file, data, fix)` for JSON-only loaders that do not need YAML support). It finds the field's line and column in the YAML or JSON text, turns decode errors from `encoding/json` and YAML (syntax, unknown fields, wrong types, duplicate keys, an early end) into an `Error` with their line and a fix, names the file, and gives the loader's `fix` to an error that has none.
3. For a line-based format that is neither YAML nor JSON, such as BMFont text or Yarn, set `Line` directly.
4. Write a fix as an instruction: what to write, where, or which tool to export from again. "Set space to world or screen" helps; "invalid value" does not.
5. Walk maps in sorted key order, so the same problem is reported on every run when a file has several.
6. Test with `internal/loaderrtest`: `Expect(t, err, file, line)` checks that the error names the file and line and has a fix, and `ExpectField(t, err, file, data, field)` also checks the field. For YAML formats, `internal/formattest.Run` verifies that unknown fields and missing, zero or future versions return a `*loaderr.Error` with the matching line and a fix, and `internal/repocheck` enforces that every YAML-decoding package runs both `formattest.Run` and `loaderrtest`.

A custom `UnmarshalYAML` knows the node it was handed: return a `*loaderr.Error` with the node's `Line` and `Column`, as `particles` does for durations and colors, and `Locate` keeps them. A custom `UnmarshalJSON` cannot say where it failed: the `encoding/json` that `nsgo build` links returns its error without the field's path, so keep the value it refuses and report it from the validator with the field instead.
