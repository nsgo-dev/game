### Added

- `game/editor`: localized the editor UI into English and Simplified Chinese via embedded versioned `l10n` YAML catalogs with live switching without restart, added plugin translation catalog support (`Plugin.Catalog`, `Plugin.LocaleYAML`, `Editor.PluginT`), and added per-user versioned YAML editor settings (`EditorSettings`, `EditorSettingsDialogView`) for language and light/dark/system appearance via `core.Scope.SetAppearance` (#921).
