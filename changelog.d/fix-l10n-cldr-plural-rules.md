### Fixed

- `game/l10n`: replaced the hand-coded plural switch with integer cardinal plural rules generated from Unicode CLDR 48.2 (`tools/cldrplurals`), fixing Czech, Slovak, Lithuanian, Latvian, Romanian, Slovenian, Bosnian/Croatian/Serbian, Irish, Welsh, Maltese, Scottish Gaelic, Breton, and all other CLDR locales, and added dead plural-form auditing via `Catalog.CheckPlurals`, `CheckStringTablePlurals`, `UnusedPluralForms`, and `assetcheck` (#1580).
