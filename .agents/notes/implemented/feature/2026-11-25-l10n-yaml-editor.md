# Agent Note: YAML localization string tables and editor panel

Status: implemented

## Problem

`game/l10n` only loaded string tables from unversioned JSON (`Catalog.LoadJSON`), violating the repository rule that author-edited localization tables are versioned, strictly validated, deterministic YAML files. In addition, the scene editor had no side-by-side table UI for managing message keys, locales, missing translations, and CLDR plural forms across locale files.

## Decision

### Versioned YAML string table format (`packages/game/l10n/yaml.go`)

Each locale lives in its own versioned YAML file (`StringTable`, `StringTableVersion = 1`) with four top-level keys: `version`, `locale`, `strings` (`map[string]string`), and `plurals` (`map[string]PluralForms`).

- `ParseYAML` decodes with `yaml.NewDecoder` and `KnownFields(true)`, refusing unknown top-level keys, unknown CLDR plural categories, duplicate keys, trailing YAML documents, YAML anchors/aliases/merge keys, non-decimal `version` tokens (`1.0`, `0x1`), null scalar entries, non-canonical BCP-47 locale tags, empty keys, keys defined in both `strings` and `plurals`, and empty `PluralForms`. Errors are `*loaderr.Error` values carrying line, column, field path, and fix hints.
- `EncodeYAML` validates and emits deterministic 2-space YAML prefixed with `# yaml-language-server: $schema=https://game.nsgo.dev/schemas/l10n.v1.json` (`SchemaComment`), fields in schema order, sorted map keys, and CLDR plural forms in canonical order (`zero`, `one`, `two`, `few`, `many`, `other`), guaranteeing byte-for-byte lossless round-trips (`EncodeYAML(ParseYAML(b)) == b`).
- `Catalog.LoadYAML` parses and merges atomically into `table.Locale`; `Catalog.ApplyStringTable`, `Catalog.StringTable`, and `Catalog.MarshalYAML` mirror `AssetCatalog`'s manifest API. Existing `Catalog.LoadJSON` remains unchanged.
- `cmd/gameschema` publishes `apps/website/public/schemas/l10n.v1.json` directly from `l10n.StringTable` via `jsonschema.Reflect`.

### Localization panel (`packages/game/editor/localization_panel.go`)

`Editor` owns an optional `l10n *localizationState` edited through snapshot-based `l10nCmd` entries on `Editor.History`, keeping every operation (opening/closing, adding/removing keys and locales, toggling plural mode, editing strings and CLDR plural forms) undoable and redoable.

- `LocalizationPanelView` docks below the viewport when open (toggled via the toolbar **Strings** button or `OpenLocalizations`/`NewLocalizations`).
- Keys render as rows and sorted canonical BCP-47 locales render as columns. Untranslated cells (empty string or plural missing `other`) are highlighted with a warning border and `Missing` indicator and are omitted from encoded YAML so runtime `Catalog` lookups fall back along the BCP-47 chain.
- `SaveLocalizations` encodes every locale atomically before writing `<dir>/<locale>.yaml` with `writeFileAtomic`, and deletes stale `.yaml` files for locales previously loaded from `dir` and removed.

## Alternatives considered

1. **Single polymorphic `strings` map accepting either a string or a nested plural object (mirroring JSON).**
   - Strongest argument: one map keeps simple and plural keys in a single alphabetical list, closer to the JSON shape.
   - Rejected because `tooling/jsonschema` intentionally omits `oneOf`/`anyOf` and reflects directly from typed Go structs; splitting `strings` and `plurals` gives strict Draft-07 schema validation and autocompletion of CLDR categories with zero reflector special cases, removes JSON's heuristic ambiguity between nested namespaces and plural maps, and works directly with `yaml.Decoder.KnownFields(true)`.
2. **One multi-locale YAML file holding all languages together.**
   - Strongest argument: one file eliminates cross-file sync when adding a key.
   - Rejected because issue #917 explicitly specifies one file per locale so translators and diffs stay scoped to a single language and games can load or bundle locales independently.

## Consequences

- **Benefits**: Author-editable YAML string tables satisfy the repository's plain-text contract (`formattest.Run`, `gameschema -check`, `TestFormatsPage`), interoperate with `Catalog` identically to JSON, and are editable side by side in the editor with full undo/redo and missing-translation auditing.
- **Costs**: Existing JSON tables are preserved for compatibility, meaning `l10n` documents and supports both YAML and JSON string tables. Plural keys and string keys live in separate sections of each locale file (`strings` vs `plurals`) and cannot share a key name within the same locale.
