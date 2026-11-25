# Agent Note: Editor Chinese UI, plugin localization, and per-user language and appearance settings

Status: implemented

## Problem

The project documents every feature in English and Simplified Chinese, but `packages/game/editor` hardcoded English strings directly in UI views, menus, and commands, offered no localization hook for editor plugins, and could only follow the host operating system's light or dark appearance without persisting user preferences across restarts.

## Decision

The editor stores its built-in English and Simplified Chinese UI strings in embedded versioned `l10n` YAML string tables (`packages/game/editor/locales/en.yaml` and `packages/game/editor/locales/zh.yaml`, reusing the format established in [l10n YAML string tables](./2026-11-25-l10n-yaml-editor.md)) and persists per-user language and appearance preferences outside any game project in a new versioned YAML format (`EditorSettings`, `SettingsVersion = 1`).

### Key design choices

- **Embedded `l10n` YAML catalogs (`en` and `zh`)**: `locales/en.yaml` (`locale: en`) and `locales/zh.yaml` (`locale: zh`) are embedded via `//go:embed` and parsed strictly through `l10n.Catalog.LoadYAML` at package init. Storing Simplified Chinese under `zh` lets `l10n.Catalog`'s BCP-47 fallback chain match `zh`, `zh-CN`, `zh-Hans`, and `zh-Hans-CN` without duplicating tables. `TestLocaleCatalogsComplete` enforces canonical `l10n.EncodeYAML` form, exact key and placeholder parity between `en` and `zh`, and AST-scans every non-test Go file in `packages/game/editor` so any missing `T(...)` or `Plural(...)` key fails CI immediately.
- **Live switching without restart**: `EditorView` binds a reactive `chromeKey` memo over `(ed.ResolvedLanguage(), cx.Env().Dark())` around the editor workspace while keeping shortcuts, the desktop application menu binding (`bindApplicationMenu` includes `ResolvedLanguage()` in its memo key), window lifecycle hooks, and `EditorSettingsDialogView` mounted at the root scope. Normal scene edits never change `chromeKey`, so input focus and scroll positions stay intact, while switching language or appearance immediately updates every widget—including NSGo widgets whose labels are static fields (`ui.ToggleButton`, `ui.ToolbarGroup`, `ui.Command`)—without restarting.
- **Plugin localization**: `Plugin` accepts an optional `Catalog *l10n.Catalog` and/or `LocaleYAML [][]byte` (validated strictly at `RegisterPlugin` time). Panels, tools, and commands support optional `*Key` fields (`TitleKey`, `LabelKey`, `DescriptionKey`, `CategoryKey`) and fallback key lookup in the plugin's catalog, and plugin views call `Editor.PluginT` / `Editor.PluginPlural` while reading `rev` so plugin UI follows editor language switches live.
- **Per-user `EditorSettings` YAML format (`version: 1`)**: stored outside any project at `DefaultSettingsPath()` (`$UserConfigDir/nsgo-game/editor.yaml`). `ParseEditorSettings` uses `KnownFields(true)`, refuses trailing YAML documents, YAML anchors/aliases/merge keys, missing/zero/future versions, and invalid enum values, and returns `*loaderr.Error`. `EncodeEditorSettings` writes deterministic 2-space YAML prefixed with `SettingsSchemaComment` (`https://game.nsgo.dev/schemas/editor-settings.v1.json`), checked by `formattest.Run` and `cmd/gameschema -check`.
- **Appearance via NSGo `core.Scope.SetAppearance`**: `Editor.SetAppearance` and `Editor.AttachScope` apply `core.AppearanceSystem`, `core.AppearanceLight`, or `core.AppearanceDark` through `cx.SetAppearance`, keeping `Editor.Dark()` and `cx.Env().Dark()` synchronized and surviving restarts via `Editor.LoadSettings` / `Editor.SaveSettings`. `Editor.SettingsPath` defaults to empty in `editor.New` so unit tests never touch the developer's home config directory; `go tool game editor` loads user settings via `Editor.LoadUserSettings()`.
- **MCP agent language context**: `editormcp`'s `selection_get` structured output includes `language` (`ed.ResolvedLanguage()`) so connected agents can respond in the editor's active language.

## Alternatives considered

- **Using NSGo's code-generated `nsgo/i18n` package directly inside `packages/game/editor`**: `nsgo/i18n` binds tightly to a single app-wide catalog via `i18n.SetApp` and generated code tied to `cx.Env().Locale()`, which makes per-editor-instance runtime overrides in unit tests, live switching independent of OS locale, and dynamic plugin catalogs harder to isolate. Reusing `packages/game/l10n`'s versioned YAML string tables keeps all translation files plain-text deterministic YAML validated by the same schema (`l10n.v1.json`) while giving every `Editor` instance and `Plugin` clean, test-isolated live switching.
- **Placing `EditorSettings` inside `game.yaml`**: rejected because language and light/dark appearance are per-person preferences on this computer, not shared project settings committed to version control.
- **Updating only `ContentFunc`/`TextFunc` fields without a language/appearance `core.Dynamic` boundary**: rejected because several NSGo `ui` widgets (`ui.ToggleButton`, `ui.ToolbarGroup`, `ui.Menu`, `ui.Command`) only accept static string fields and existing panel styles capture `ui.ColorsOf(cx)` colors at mount time; keying the workspace `core.Dynamic` exclusively on `(lang, dark)` avoids remounting during normal edits while guaranteeing full live updates on language or appearance toggles.

## Consequences

- **Benefits**: Full English and Simplified Chinese editor UI with live switching, compile-time-like CI enforcement of translation key and placeholder completeness, first-class plugin localization, deterministic schema-validated per-user settings outside projects, and reliable dark/light appearance override across restarts.
- **Costs**: Adding a new UI string to `packages/game/editor` now requires adding matching keys to both `locales/en.yaml` and `locales/zh.yaml` (enforced by `TestLocaleCatalogsComplete`). Switching language or appearance remounts the workspace subtree (resetting ephemeral local widget scroll offsets while preserving all `Editor` state and keeping the Editor Settings dialog open).
