# Agent Note: Website multilingual i18n architecture

Status: implemented
Archived: 2026-11-25

## Problem

The official website (`apps/website`) previously supported only English (`/`) and Simplified Chinese (`/zh/`). Expanding to 10 mainstream languages (`en`, `zh`, `zh-tw`, `ja`, `ko`, `es`, `fr`, `de`, `pt`, `ru`) exposed four structural bottlenecks in the original two-language setup:

1. **Per-locale route duplication**: Every page under `src/routes/zh/` duplicated its English counterpart (10 files per locale). Scaling that pattern to 9 non-English locales would require 90 nearly identical route files.
2. **Duplicated locale lists**: `src/i18n.ts` imported `@tanstack/react-router`, forcing `plugins/content.ts` and `scripts/check-api.ts` to maintain separate hardcoded `['en', 'zh']` arrays.
3. **Unbounded `virtual:docs` build cost and bundle size**: `plugins/content.ts` previously assumed every locale had a `docs/<lang>/` directory and ran Shiki highlighting once per `(lang, file)` pair before serializing all rendered copies into `virtual:docs`. With 97 Markdown guides and 8 locales falling back to English, that would multiply Shiki runs and serialize 8 redundant copies of the English HTML into the client bundle.
4. **Hardcoded two-language browser redirect and external `nsgo.dev` links**: `__root.tsx` only redirected `zh*` browsers to `/zh/`, and `nsgoUrl(lang)` constructed `https://nsgo.dev/<lang>/`, which would produce 404s for locales `nsgo.dev` does not yet publish.

## Decision

The website uses a single parameterized locale architecture across routing, content compilation, UI copy, and browser language negotiation:

- **Router-free locale registry (`apps/website/src/locales.ts`)**: Defines `LANGS`, `NON_DEFAULT_LANGS`, `langNames`, `htmlLang`, `isLang`, `isNonDefaultLang`, `splitLocalePath`, and `matchBrowserLang`. `src/i18n.ts`, `plugins/content.ts`, `scripts/check-api.ts`, `src/copy.ts`, and `src/content/showcase.ts` all import from this single module.
- **Dynamic `/$lang` routes (`apps/website/src/routes/$lang.tsx` and `$lang/**`) vs per-locale route directories**: All 9 non-English locales share one route tree under `src/routes/$lang/`. The `/$lang` layout route validates `isNonDefaultLang(params.lang)` in `beforeLoad` and throws `notFound()` for unknown prefixes. An explicit static `src/routes/404.tsx` route takes precedence over `/$lang` so TanStack Start's static `404.html` SPA shell (`maskPath: '/404'`) prerenders cleanly with English metadata and `noindex`. Typed navigation goes through `src/components/LocaleLink.tsx`, which branches statically between root English paths and `/$lang/...` paths.
- **Deduplicated `virtual:docs` compilation (`apps/website/plugins/content.ts`)**: `optionalMarkdownFiles` treats missing `docs/<lang>/` directories as empty rather than failing. Only Markdown files that actually exist on disk (`docs/en/*.md`, `docs/zh/*.md`, and any future `docs/<lang>/*.md`) are rendered with Shiki and bundled. Fallback locales reuse the rendered English `DocPage` objects with `fallback: true` and rewrite internal `<a href="<base>docs/...">` attributes on demand via `localizeDocLinks`, keeping `docs[lang]` API-compatible and cached per locale.
- **Modular UI copy (`apps/website/src/copy/*.ts`) and showcase translations**: Each language lives in its own module under `src/copy/` conforming to `Copy` (`src/copy/en.ts`), with explicit pluralization rules (including 3-form Russian pluralization) and localized `dateLocale`. `SHOWCASE_ENTRIES` requires titles and descriptions for every `Lang`.
- **Multi-language first-visit redirect (`apps/website/src/routes/__root.tsx`)**: On English non-404 pages, the pre-paint inline script checks `localStorage['nsgo-lang']` first, then walks `navigator.languages` in preference order (`en*` stops the search; `zh-TW`/`zh-HK`/`zh-MO`/`zh-Hant` map to `zh-tw` unless `zh-Hans` is present; other `zh*` map to `zh`; primary subtags match `ja`, `ko`, `es`, `fr`, `de`, `pt`, `ru`).
- **External `nsgo.dev` fallback (`apps/website/src/links.ts`)**: `nsgoUrl(lang, path)` targets `/zh/` only for `zh` and `/` for all other languages until `nsgo.dev` publishes additional locales.
- **SEO alternates (`apps/website/src/i18n.ts`)**: `pageHead` emits `rel="canonical"`, `hrefLang="x-default"` pointing to English, and `rel="alternate"` for all 10 locales.

## Alternatives considered

### Why not duplicate `src/routes/<lang>/` for each language?

Keeping static directories (`src/routes/ja/`, `src/routes/ko/`, etc.) gives TanStack Router zero-param literal `to` paths for every language and avoids needing an explicit `404.tsx` guard against `/$lang`. It was rejected because 10 route files × 9 locales creates 90 boilerplate files that must stay in lockstep whenever a new route is added, whereas `/$lang` keeps the route count constant at 12 files regardless of how many languages are added.

### Why not pre-translate all 97 docs in `docs/` for all 8 new languages?

Translating every Markdown guide upfront would provide native-language documentation immediately without fallback banners. It was rejected because the repository already enforces English/Chinese parity across 97 actively evolving docs (`docs/en/` and `docs/zh/`), and maintaining 800+ Markdown translations while APIs are still changing would create severe translation drift; the existing fallback banner plus `optionalMarkdownFiles` lets individual `docs/<lang>/<slug>.md` pages be added incrementally as translations mature.

### Why not keep `virtual:docs` serializing full `Record<Lang, DocPage[]>` per language?

Pre-computing every language's full `DocPage[]` array at build time avoids any runtime link-rewriting regex. It was rejected because serializing 10 copies of 97 Shiki-highlighted HTML pages inflates the client bundle and multiplies build-time Shiki work by 5× when 8 of those locales currently show the same English HTML.

## Consequences

- **Benefits**:
  - Adding another language requires updating `src/locales.ts`, adding one `src/copy/<lang>.ts` file, and adding translations to `src/content/showcase.ts`—no new route files and no changes to `plugins/content.ts` or `scripts/check-api.ts`.
  - `docs/<lang>/<slug>.md` translations are picked up automatically when added, without touching website code.
  - `virtual:docs` Shiki work and bundle payload stay proportional to actual translated Markdown files on disk rather than `LANGS.length`.
- **Costs & limits**:
  - Static prerendering now crawls and writes HTML for all 10 locales (including ~91 Go API packages and 97 doc pages per locale), increasing static output size and CI prerender duration.
  - Fallback internal-link rewriting relies on matching `<a ... href="<base>docs/...">` in rendered HTML; if `rehype-stringify` ever changed attribute quoting or link format, `localizeDocLinks` and `plugins/content.ts` would need to be updated in lockstep.
  - `nsgoUrl` hardcodes `lang === 'zh'` and should be updated when `nsgo.dev` adds more languages.

## Verification

- `bun run test` runs `plugins/llms.test.ts` and `src/locales.test.ts` (16 tests covering `matchBrowserLang`, `splitLocalePath`, `localizeDocLinks`, `nsgoUrl`, copy/showcase completeness across all 10 locales, and Russian pluralization).
- `bun run build` runs `vite build` (full static prerender across all 10 locales), `tsc --noEmit`, and `bun scripts/check-api.ts` (verifying every exported Go API anchor exists on its prerendered page in all 10 locales).
- `bun run verify-notes` validates this note's tree placement and format.
