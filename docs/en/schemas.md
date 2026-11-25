# Schemas for YAML files

Every file a game's author writes is YAML text, and each format has a JSON Schema so editors and AI tools can complete keys, show each field's description on hover and flag mistakes while the file is being written, before the game loads it.

```go
import "github.com/nsgo-dev/game/packages/game/tooling/jsonschema"
```

## Published schemas

| Format | Schema | Files |
| --- | --- | --- |
| Scene, version 1 | `https://game.nsgo.dev/schemas/scene.v1.json` | `*.scene.yaml`, read by `sceneformat.Parse` |
| Scene, version 2 | `https://game.nsgo.dev/schemas/scene.v2.json` | `*.scene.yaml` that instance or inherit other scene files, read by `sceneformat.Parse` |
| Import settings, version 1 | `https://game.nsgo.dev/schemas/import.v1.json` | `*.import.yaml` beside an asset, read by `importsettings.Parse`, by `game assets` and, for its credit, by `game release` and `game credits` ([Command Line](cli.md#assets)) |
| Voxel world, version 1 | `https://game.nsgo.dev/schemas/voxel.v1.json` | `*.voxel.yaml`, read by `voxel.Parse` and `voxel.LoadYAML` |
| Localization string table, version 1 | `https://game.nsgo.dev/schemas/l10n.v1.json` | per-locale `*.yaml` files such as `locales/en.yaml`, read by `l10n.ParseYAML` and `l10n.Catalog.LoadYAML` |
| Terrain definition, version 1 | `https://game.nsgo.dev/schemas/terrain.v1.json` | `*.terrain.yaml`, read by `world3d.ParseTerrain` and `world3d.LoadTerrainFS` |
| 3D grid map, version 1 | `https://game.nsgo.dev/schemas/gridmap.v1.json` | `*.gridmap.yaml`, read by `world3d.ParseGridMap` and `world3d.LoadGridMapFS` |
| 3D foliage layers, version 1 | `https://game.nsgo.dev/schemas/foliage.v1.json` | `*.foliage.yaml`, read by `world3d.ParseFoliage` and `world3d.LoadFoliageFS` |
| Project file, version 1 | `https://game.nsgo.dev/schemas/game.v1.json` | `game.yaml` at the project's root, read by `project.Parse` |
| Behavior tree, version 1 | `https://game.nsgo.dev/schemas/bt.v1.json` | `*.bt.yaml`, read by `ai.ParseBehaviorTree` and `ai.LoadBehaviorTreeFS` |
| Data table | generated per row type with `datatable.Schema[T]` | `*.yaml`, read by `datatable.LoadYAML` |

The other formats get their schemas as their YAML loaders land: mod manifests (#474), the project file (#488), shared resources (#485), particles (#477), skeletal rigs (#475) and localization tables (#473).

A schema's URL names its format's version. A file that says `version: 1` keeps validating against `scene.v1.json`, which refuses the keys version 2 added, now that `scene.v2.json` is published beside it; `Encode` names the schema of the document's own version.

## Pointing an editor at a schema

A scene file names its schema on its first line, and `sceneformat.Encode` writes that line on every save:

```yaml
# yaml-language-server: $schema=https://game.nsgo.dev/schemas/scene.v1.json
version: 1
name: Cave Room
```

The comment is read by the YAML language server (`yaml-language-server`) that VS Code's YAML extension (`redhat.vscode-yaml`) runs, as do Neovim, Helix and Zed when set up with it, so the file validates with nothing else to set up. `Parse` ignores it like any other comment. `importsettings.Encode` writes the same kind of line, naming `import.v1.json`, at the top of an import settings sidecar.

For files without the comment, map a pattern to the schema in the editor's settings instead, such as VS Code's `.vscode/settings.json`:

```json
{
  "yaml.schemas": {
    "https://game.nsgo.dev/schemas/scene.v1.json": "*.scene.yaml"
  }
}
```

## What a schema checks

The scene schema is generated from the Go types `sceneformat.Parse` decodes into, so its keys, types and descriptions are the code's:

- `version` is required and must be `1`.
- Every object refuses keys it does not define, as `Parse` does, so a misspelt key is flagged where it is written.
- Each property's description is its Go doc comment.
- Each number stays within what its Go type holds, such as a `float32` coordinate below about 3.4e38 and an `int` `order` within 64 bits, so a value `Parse` would overflow is flagged.
- The constraints a type cannot say come from a `schema` struct tag on the field: required keys (`version`, a layer's `name`, an object's `id`, a sprite's `atlas` and `frame`, camera bounds' `width` and `height`), the allowed `space` values, and number ranges such as `alpha` in [0, 1] and a positive `zoom`.

A schema sees one file at a time, so `Parse` still checks what spans the document: unique layer names and object IDs, tilemap `ref`s that name an entry of `tilemaps`, one tilemap mode at a time, inline grid sizes, and colors `core.ParseColor` accepts. In a few places the schema is stricter than `Parse`, such as refusing `null` for a field `Parse` reads as its zero value; every scene `Encode` writes validates against it, which a fuzz test holds.

The import settings schema likewise checks each field on its own: known keys, the allowed `filter`, `format` and audio `format` values, non-negative sizes, rates, paddings and model scales, and a credit's required `author` and `license`. It does not say which fields go together, since the schema generator emits no `oneOf`, `not` or dependency keywords and a loop's end cannot be compared with its start in JSON Schema, so `importsettings.Parse` and `game assets` still refuse a sidecar with no section or with more than one of `image`, `audio`, `font` and `model`, `peakDB` together with `rmsDB`, a font's `chars` holding a control character, a loop whose end is not after its start, a loop with `aac` or `mp3` output, and a `license` that is not an SPDX license expression, while an editor shows such a file as valid.

## Checking schemas in CI

`cmd/gameschema` writes the published schemas into `apps/website/public/schemas`, which the website serves at `https://game.nsgo.dev/schemas/`. After changing a format's Go types or doc comments, regenerate them from `packages/game`:

```sh
go run ./cmd/gameschema
```

The Check workflow runs `go run ./cmd/gameschema -check`, which fails when a committed schema differs from what the Go types generate, when one is missing, or when the directory holds a schema it no longer generates. A test in `cmd/gameschema` does the same check under `go tool nsgo test`.

## Checking formats in CI

A schema checks a file someone writes; a format's tests check the code that reads and writes it. Every package that reads a YAML format runs the shared checks of `internal/formattest` from a `conformance_test.go`, on testdata files in the format's canonical form, the bytes its encoder writes:

```go
func TestFormatConformance(t *testing.T) {
	formattest.Run(t, formattest.Format[*quest.Book]{
		Decode: quest.Parse,
		Encode: quest.Encode,
		Files:  []string{"testdata/*.yaml"},
		Doc:    "quest",
	})
}
```

For each file, the checks require that:

- loading and saving it gives back the same bytes;
- saving the same value again, saving a second load of the file and saving a load of what was saved all give the same bytes, so the output never depends on map order, the time or randomness;
- loading refuses the file with an extra top-level key, without its `version`, with `version: 0` and with the version after the newest one the format reads, with an error that names the key.

`Doc` names the format's entry on [File Formats](formats.md), the line `<!-- format: quest -->` before its heading in `docs/en/formats.md` and `docs/zh/formats.md`, and `Run` fails when either page lacks the entry or the entry has a YAML example that does not load and save to the same bytes, printing what the format saves for it. The path before an optional `#name` must be the package's, and a package with two formats names the second one, as in `achievements#progress`. `TestFormatsPage` in `packages/game` checks the rest of the page: that both languages list the same entries in the same order; that every entry that is no `Doc` is in its `codeFormats` list with the declaration that reads the format, which the package must still declare, and that the entry's JSON or Yarn examples load with that reader; that every package that decodes JSON has an entry unless its `notJSONFormats` list says why; and that every exported integer constant whose name ends in `Version` maps to an entry in its `formatVersions` list. `Doc` may be a string literal or a string constant, so the test can read it.

`TestFormatPackagesRunConformance` in `packages/game` fails when a package decodes YAML with `go.yaml.in/yaml/v3`, however leniently, and none of its tests calls `formattest.Run`, so `go tool nsgo test` in CI fails a new format that skips the checks. A package that decodes YAML without defining a format of its own, such as `loaderr`, is listed in the test's `notFormats` with the reason. The test looks at whole packages, so a package with a second format must run the checks on that one too; one call satisfies it. A hand-written fixture whose comments or layout other tests rely on stays as it is, and its canonical form goes in a file of its own, such as `testdata/canonical/`, for the checks. A format whose version is under another key, such as a mod manifest's `manifestVersion`, sets `VersionField`, and one that reads several versions sets `MaxVersion`. To add a testdata file, write the document, then run the test once with `FORMATTEST_UPDATE=1`, which rewrites each file with what the format saves for it before checking it; comments the encoder does not write are dropped.

## Data table schemas

A data table's rows are a game's own Go type, so `datatable.Schema[T]` builds the schema for it, for the game to write next to its tables:

```go
docs, err := jsonschema.PackageDocs("internal/tables") // the doc comments of the package that declares Enemy
if err != nil {
	return err
}
s, err := datatable.Schema[tables.Enemy](jsonschema.Options{
	ID:    "https://example.com/schemas/enemies.json",
	Title: "Enemies",
	Docs:  docs,
})
if err != nil {
	return err
}
data, err := s.Marshal()
```

The schema describes the [YAML data table format](datatable.md): a mapping with a required `version`, which must be `1`, and `rows`, a list of rows that may be left out or `null` for an empty table; any other top-level key is flagged. Each row is keyed by the columns `LoadYAML` matches: a field's `table` tag, else its `json` tag, else its Go name, spelled as `MarshalYAML` writes them, and the row schema is at `#/definitions/<row type>`. A row type in which two fields share a column is an error, as it is for `LoadYAML`. A `time.Duration` cell is a string such as `"1.5s"` or an integer of nanoseconds. The schema flags a column that matches no field and a cell of the wrong type; `LoadYAML` also refuses merge keys (`<<`), which a YAML language server resolves before validating, so the schema cannot flag them.

## Building and checking schemas in Go

- `jsonschema.Reflect(t, opts)` builds a draft-07 schema from a Go type: struct fields become properties named by their `yaml` tags (or by `Options.FieldName`), named structs become `definitions` shared through `$ref`, integers and `float32` are bounded by their Go type's range, and unknown keys are refused unless `Options.AllowUnknown` is set. Two fields under one key are an error unless `Options.FirstFieldWins` keeps the first. A field's `schema` tag takes `required`, `const=V`, `enum=A|B`, `minimum=N`, `maximum=N`, `exclusiveMinimum=N`, `exclusiveMaximum=N`, `minLength=N` and `minItems=N`.
- `jsonschema.PackageDocs(dir)` reads a package's doc comments into `Options.Docs`, so each property is described by its field's comment.
- `Schema.Marshal()` writes indented JSON with a fixed key order, so the same types always give the same bytes.
- `Schema.Validate(v)` and `Schema.ValidateYAML(data)` check a value against the keywords `Reflect` writes and return a `*ValidationError` listing every issue with its JSON Pointer path, for example `/layers/0/objects/1/alpha: 1.5 is greater than the maximum 1`, or `/layers/0/nmae: unknown key "nmae" (known keys: name, space, …)`. `ValidateYAML` reads YAML as a language server does: keys are strings, timestamps are strings, merge keys are merged, a key repeated within one mapping is an error, and aliases are expanded up to a bound.
