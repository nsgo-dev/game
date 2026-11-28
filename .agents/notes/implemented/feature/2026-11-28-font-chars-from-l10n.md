# Agent Note: Bake font target characters from string tables and report undrawable text

Status: implemented

## Problem

A `fonts` target in `game.yaml` only baked characters pasted into `chars` (or printable ASCII when empty). For CJK and other localized games, authors had to copy thousands of characters into `game.yaml` by hand and keep them synchronized with every string edit. When the baked glyph list fell behind, `text.Font.Draw` and `Measure` silently skipped unmapped runes, leaving missing characters in-game with no build warning, static check failure, or runtime diagnostic.

## Decision

Extend `project.FontTarget` (and `importsettings.Font`) so a TTF/OTF font target can derive its baked character set from project text sources while keeping incremental builds tight and giving both static (`game check`, `game assets`) and debug-runtime (`pkg/text`) diagnostics for undrawable characters.

- **`game.yaml` and `.import.yaml` fields**: `FontTarget` adds `charsFrom` (list of project-relative paths to YAML/JSON l10n string tables, `*.table.yaml` data tables, or `*.scene.yaml` scenes), `charsFile` (plain UTF-8 character list), and optional `locale` (canonical BCP-47 tag checked by `game check`). `importsettings.Font` mirrors `charsFrom` and `charsFile`. All three (`chars`, `charsFrom`, `charsFile`) merge together; `DefaultFontChars` applies only when all three are omitted. Glyph-image folder fonts refuse `charsFrom` and `charsFile` just like `size` and `chars`. `project.PathRefs` tracks `charsFrom` and `charsFile` so `refs`, `game check`, and `game mv` validate and rewrite them.
- **Placeholder and markup stripping**: `l10n.StripPlaceholders` removes `{name}` interpolation placeholders while preserving escaped `{{`/`}}`, and `assetbuild.ExtractTextChars` passes the result through `text.ParseRich(...).PlainText()` and strips control characters. Both `assetbuild` and `assetcheck` share `assetbuild.CollectFontChars` and `assetbuild.ExtractTextChars` so baking and checking agree byte for byte.
- **Character-set cache keying**: A font target's build cache key hashes only the resulting sorted, deduplicated character string (`fontSettings.Chars`), never the raw bytes of `charsFrom` or `charsFile` files. Editing copy, comments, or placeholder names without changing the set of unique characters keeps the target up to date without rebaking.
- **Build warnings and static checks**: `assetbuild` records requested runes absent from the source `.ttf`/`.otf` font on `Target.Warnings` (persisted in `.game/assets-cache.yaml` so cached runs keep reporting them) and `game assets` prints them to stderr. `assetcheck` checks every font target meant for a locale (explicit `locale` or inferred from l10n tables listed in `charsFrom`) against that locale's string tables: it reads the baked `.fnt` when present on disk (catching both stale bakes and source-font gaps), or falls back to `CollectFontChars` intersected with the source font's cmap when `.fnt` has not been baked yet. When multiple font targets each claim distinct string tables of the same locale via `charsFrom`, each target checks its own tables without cross-complaining on tables owned by another font target.
- **Debug-build missing-glyph logging**: `pkg/text` provides `SetLogMissingGlyphs`, `LogMissingGlyphsEnabled`, and `SetMissingGlyphLogger` in `!release` builds, deduplicating per `(font instance, rune)` across `Font`, `SDFFont` (sharing state through `layoutFont()`), and `LayoutRich`. Under `-tags release`, all hooks compile to empty no-op stubs and `missingGlyphState` is a zero-byte struct.

## Alternatives considered

### Hashing raw `charsFrom` file bytes into the asset cache key

Including `k.input(src, rawBytes)` for every `charsFrom` file would match how `assetbuild` hashes other inputs. It was rejected because any copy tweak, comment edit, key reorder, or placeholder rename that introduces no new character would invalidate the cache and rebake large CJK font atlases unnecessarily.

### Inferring a font target's locale from its `.fnt` filename

Matching output names such as `ui-zh.fnt` against locale tags would avoid adding an optional `locale` field. It was rejected because filename heuristics create false positives on names like `ui-in.fnt` or `title-de.fnt`; inferring locales only from explicit `charsFrom` l10n tables and an explicit optional `locale` field stays unambiguous and adheres to the repository's no-magic rule.

### Making missing glyphs in the source font a hard error in `game assets`

Failing `game assets` whenever a source `.ttf`/`.otf` lacks a character in `charsFrom` would prevent shipping an incomplete font. It was rejected because Issue #1741 specifies that `game assets` warns on missing source glyphs while still baking the available glyphs, and `game check` reports the undrawable localized strings as check problems in CI.

## Consequences

- **Benefits**: CJK and localized games bake minimal bitmap fonts directly from l10n string tables, data tables, and scenes with zero manual character lists; copy edits that reuse existing glyphs trigger zero rebuilds; `game assets` warns immediately when a source font lacks a character; `game check` pins undrawable keys and characters to exact YAML lines; debug builds can log any remaining runtime glyph misses once per rune and font with zero overhead in `-tags release` builds.
- **Costs**: `pkg/project` now imports `pkg/l10n` for canonical BCP-47 locale validation and `pkg/tooling/assetcheck` imports `pkg/tooling/assetbuild` to share character extraction; `.game/assets-cache.yaml` gains an optional `warnings` list per target entry; schema files (`game.v1.json`, `import.v1.json`) grow with the new font fields.

## Verification

- Unit tests cover `l10n.StripPlaceholders`, `StringTable.CollectChars`, `ValidateLocale`, `text` debug logging (`!release`) and no-op stubs (`-tags release`), `project` / `importsettings` validation and `PathRefs`, `assetbuild` collection/determinism/incremental-rebuild/warnings, `assetcheck` baked and pre-bake missing-glyph reporting, and `gamecli` stderr warnings.
- `go run ./tools/gameschema -check`, `(cd tools && go run ./changelog check)`, `bun run verify-notes`, `go tool nsgo check -target host -jobs 1`, and `go tool nsgo test` across affected packages and `internal/repocheck` all pass.
