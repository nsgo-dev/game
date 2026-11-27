# Agent Note: CLDR 48.2 integer cardinal plural rules and dead-form auditing in pkg/l10n

Status: implemented

## Problem

`pkg/l10n`'s `selectPluralForm` hard-coded cardinal plural rules for only a small set of languages (`ja/zh/ko/th/vi/id/ms`, `fr/pt/hi`, `ar`, `ru/uk/sr/hr/be`, `pl`, `he/iw`) and fell back to English (`n == 1`) for every other locale. Locales such as Czech (`cs`), Slovak (`sk`), Lithuanian (`lt`), Latvian (`lv`), Romanian (`ro`), Slovenian (`sl`), Irish (`ga`), Welsh (`cy`), Maltese (`mt`), Scottish Gaelic (`gd`), Breton (`br`), Icelandic (`is`), Macedonian (`mk`), and plural-less languages (`lo`, `my`, `km`, `yue`) selected the wrong plural category, while `sr`/`hr` were grouped with `ru` and selected `many` instead of CLDR's `other`. In addition, no audit warned authors when a string table supplied a plural category that the locale never selects.

## Decision

### Generated integer-only CLDR 48.2 table (`tools/cldrplurals`, `pkg/l10n/plurals_cldr_gen.go`)

`tools/cldrplurals` parses a pinned Unicode CLDR 48.2 `common/supplemental/plurals.xml` (verified by SHA-256), specializes every cardinal rule for non-negative integer counts (`n = i = |count|`, decimal and compact operands `v = w = f = t = c = e = 0`), statically folds constant relations, deduplicates identical integer rule sets into 30 compact switch cases, and emits both `pkg/l10n/plurals_cldr_gen.go` (`CLDRVersion`, `cldrPluralRuleIndex`, `cldrRuleCategoryMasks`, `evalCLDRPluralRule`) and `pkg/l10n/plurals_cldr_gen_test.go` (covering all 228 CLDR locale entries with every `@integer` sample from `plurals.xml`).

- **BCP-47 subtag lookup** (`pkg/l10n/plural.go`): `PluralCategoryFor` normalizes the tag and walks from most-specific to primary language so regional CLDR exceptions (`pt-PT` vs `pt`, `kok-Latn`) match before primary language fallback (`cs-CZ` -> `cs`, `pt-BR` -> `pt`), and unlisted locales fall back to CLDR `root` (`PluralOther`).
- **Explicit-`zero` override**: `selectPluralForm` continues to return `forms.Zero` when `count == 0` and `forms.Zero != ""` for any locale, matching existing `Catalog.Plural` behavior and documented YAML string tables (see [YAML localization string tables](../../implemented/feature/2026-11-25-l10n-yaml-editor.md)).
- **Dead-form auditing**: `UnusedPluralForms`, `CheckStringTablePlurals`, and `Catalog.CheckPlurals` flag any non-empty `One`, `Two`, `Few`, or `Many` form that is unreachable for integer counts under `cldrRuleCategoryMasks`. `PluralZero` is never flagged (always reachable via the explicit-`zero` override), and `PluralOther` is never flagged (universal fallback template and required by `Catalog.T` and editor missing checks). `pkg/tooling/assetcheck` runs `CheckStringTablePlurals` on every discovered YAML string table in `loadLocaleTables` and reports dead forms with file and line location.

## Alternatives considered

1. **Depend on `golang.org/x/text/feature/plural` at runtime.**
   - Strongest argument: avoids writing a CLDR XML parser/generator and supports decimal plural operands out of the box.
   - Rejected because `pkg/l10n` explicitly avoids adding runtime dependencies for plural rules, `golang.org/x/text/feature/plural` lags behind CLDR 48.2 and pulls extra tables into every game binary, and `l10n.Plural` only accepts `count int` anyway; a zero-dependency generated table keeps binary size small and matches `nsgo`'s CLDR 48.2 baseline.
2. **Flag `PluralZero` or `PluralOther` as unused when CLDR integer rules omit them.**
   - Strongest argument: strictly reflects CLDR's `@integer` category set (e.g. `en` has no CLDR `zero`, and `ru`/`pl` have no integer `other`).
   - Rejected because `l10n` honors explicit `zero` at `count == 0` in all locales (documented in `docs/en/l10n.md` and used in existing tables), and `other` is the universal fallback for omitted categories, `Catalog.T` on plural keys, and the editor's missing-translation check.

## Consequences

- **Benefits**: Every CLDR 48.2 locale selects the exact CLDR integer cardinal category with zero runtime dependencies and full `@integer` sample test coverage; refreshing for a newer CLDR release is a single `tools/cldrplurals` run with `-check` verification; authors receive file:line diagnostics in `assetcheck` and `Catalog.CheckPlurals` for unreachable plural forms (including decimal-only `many` in `cs`/`sk`/`lt`).
- **Costs**: Decimal counts are out of scope until a floating-point plural API exists; `sr`/`hr` tables that previously relied on the buggy `many` fallback (instead of `other`) will now select `other` and be flagged by the dead-form audit.
