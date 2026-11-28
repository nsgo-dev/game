### Added

- `pkg/project`, `pkg/importsettings`, `pkg/tooling/assetbuild`: add `charsFrom`, `charsFile`, and `locale` to `assets.fonts` targets (and `charsFrom`/`charsFile` to `.import.yaml` font sidecars) to collect and merge characters from YAML/JSON l10n string tables (all strings and plural forms, stripping `{placeholders}` and rich-text markup), YAML data tables, scene text fields, and UTF-8 character files, rebuilding only when the resulting sorted character set changes and warning when the source font lacks a requested glyph (#1741).
- `pkg/tooling/assetcheck`: report every localized string and plural form containing characters that a font target meant for its locale cannot draw (#1741).
- `pkg/text`: add debug-build (`!release`) missing-glyph logging (`SetLogMissingGlyphs`, `LogMissingGlyphsEnabled`, `SetMissingGlyphLogger`, compiled out under `-tags release`) that logs each skipped rune once per rune and font, plus `Font.MissingRunes` and `SDFFont.MissingRunes` (#1741).
- `pkg/l10n`: add `StripPlaceholders`, `ValidateLocale`, `PluralForms.EachForm`, and `StringTable.CollectChars` helpers (#1741).
