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
| `sceneformat` | `Parse`, `ParseWith` | Decode errors and every schema rule, by field; the `*FieldError` `Validate` returns stays in the chain, and `ParseWith` names `ParseOptions.File` |
| `particles` | `UnmarshalConfigYAML`, `UnmarshalConfigJSON`, `LoadConfigFile`, `LoadConfigFS`, `HotConfig` | Decode errors, durations, colors and enumerations, by field or at the value's line |
| `skeletal` | `Load` | Bones, slots, skins and keyframes, by field |
| `anim3d` | `LoadEventsSidecar`, `LoadEventsFromGLTFExtras` | Decode errors |
| `sprite` | `LoadAtlas` | Frames, frame tags and nine-slices, by field |
| `tilemap` | `LoadTiled`, `LoadLDtk`, `LoadLDtkAll` | Map, tilesets (an external tileset names its own file), layers, objects and properties, by field |
| `l10n` | `Catalog.LoadJSON`, `ParseAssetManifest`, `AssetCatalog.LoadAssetManifestYAML` | Keys, asset variants and groups, by field |
| `input` | `Actions.Load`, `Haptics.Load` | Stored bindings, by field, under the storage key |
| `modding` | `ParseManifest`, `ModManifest.Validate`, `Manager.Discover`, `Manager.DiscoverFS`, `Manager.Reload` | Each manifest field, a game version the mod does not support and a duplicate id, naming `<mod>/mod.yaml` |
| `replay` | `Decode`, `LoadFile` | Header and steps, by field, such as `steps[3].press[0]` |
| `save` | `Store.Load`, `Store.LoadChecked`, `SettingsStore.Load` | Envelope, data and checks, naming the copy's file or storage key (a custom `Backend`'s copies by slot, `slot (backup)` for the backup) |
| `entities` | `LoadWorld`, `World.Restore` | Entity indices, parents and components, by field |
| `assets` | `ParsePackManifest`, `Manager.LoadAtlas`, `Group` atlases | Decode errors; atlases name their URI |
| `world3d` | `LoadMeshLibraryFromGLTF` | Version, node meshes and accessors, by field |
| `gltf` | `Load`, `LoadFS` | The top-level item at fault, such as `nodes[3]` |
| `text` | `LoadBMFont`, `LoadBMFontImage` | The record at fault, in text and XML fonts |
| `flow` | `ParseYarn` | The script line at fault |
| `datatable` | `LoadCSV`, `LoadJSON`, `LoadYAML`, `LoadFile` | The cell at fault; `CellError` stays in the chain |
| `tooling/devstate` | `Store.LoadSnapshot` | Decode errors in the snapshot file |
| `debug` | `Console.LoadTunables` | The tunable at fault, under the storage key |
| `tooling/bench`, `cmd/sizecheck` | `bench.LoadFile`, `LoadBudget`, `LoadMobileBudget` | Decode errors |

A file that cannot be read has no line: its error names the file, keeps the read error in `Err` and still gives a fix.

## Writing a loader

1. Validate the decoded document with `loaderr.Errorf(field, fix, format, args...)`, building `field` with `loaderr.Path("layers", i, "objects", j)` or extending one with `loaderr.Join(at, "alpha")`. A validator does not know lines, so it names the field instead.
2. In the loader's entry point, pass every error through `loaderr.Locate(err, file, data, fix)` once (or `loaderr.LocateJSON(err, file, data, fix)` for JSON-only loaders that do not need YAML support). It finds the field's line and column in the YAML or JSON text, turns decode errors from `encoding/json` and YAML (syntax, unknown fields, wrong types, duplicate keys, an early end) into an `Error` with their line and a fix, names the file, and gives the loader's `fix` to an error that has none.
3. For a line-based format that is neither YAML nor JSON, such as BMFont text or Yarn, set `Line` directly.
4. Write a fix as an instruction: what to write, where, or which tool to export from again. "Set space to world or screen" helps; "invalid value" does not.
5. Walk maps in sorted key order, so the same problem is reported on every run when a file has several.
6. Test with `internal/loaderrtest`: `Expect(t, err, file, line)` checks that the error names the file and line and has a fix, and `ExpectField(t, err, file, data, field)` also checks the field.

A custom `UnmarshalYAML` knows the node it was handed: return a `*loaderr.Error` with the node's `Line` and `Column`, as `particles` does for durations and colors, and `Locate` keeps them. A custom `UnmarshalJSON` cannot say where it failed: the `encoding/json` that `nsgo build` links returns its error without the field's path, so keep the value it refuses and report it from the validator with the field instead.
