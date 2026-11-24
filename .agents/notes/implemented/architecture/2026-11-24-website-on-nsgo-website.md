# Agent Note: The website on nsgo-website's demoscene look

Status: implemented

## Problem

`apps/website` was a TanStack Start and React app of its own: about 5,000 lines of routing, a Vite content plugin, a hand-written HUD style and its own header, footer, docs frame and roadmap. Every NSGo project's site is now built from [nsgo-dev/nsgo-website](https://github.com/nsgo-dev/nsgo-website), an Astro integration over Starlight whose `look: 'demoscene'` and `example-game/` template are meant for game engines. Keeping a separate stack meant the game layer's site looked and behaved unlike its siblings and carried all of that code alone.

## Decision

`apps/website` is an Astro site using the `nsgo-website` integration in its demoscene look, with the template's frame and the project's own pages:

- **Settings (`apps/website/site.config.ts`)**: the ten languages of `src/locales.ts`, the logo (NSGo's mark in its blue, `src/assets/logo.svg`, and in the look's amber on the dark theme, `src/assets/logo-dark.svg`), the header's navigation, 小莫's X account, the docs' sidebar from `src/content/groups.ts`, the roadmap of nsgo-dev/nsgo#1047 and the Apache-2.0 license. Words the settings need in every language come from `src/copy/<lang>.ts`.
- **Home page (`src/components/Home.astro`, `src/pages/index.astro`, `src/pages/[lang]/index.astro`)**: `home: false`, and the page is built from the package's demoscene components as `example-game/` does. Its own words are in `src/home/words.ts`; the package count is read from `docs/en/stability.md` and the code from `apps/website/snippets/` and `examples/fieldmedic`.
- **Docs read as they are**: `docs/en` and `docs/zh` stay GitHub-readable Markdown without frontmatter, as the Go tests and the README expect. `src/content.config.ts` wraps nsgo-website's `docsCollection()` loader so each entry's title is its first heading and its description, which goes only to meta tags, is its first paragraph. A Sätteri mdast plugin (`src/lib/markdown.ts`), which `src/lib/integration.ts` pushes onto Astro's default Markdown processor, drops that heading and rewrites `name.md` links to paths relative to the page, so a fallback page keeps its language, and other relative links to GitHub. Languages without docs get Starlight's English fallback.
- **Own pages (`src/pages/[...lang]/`)**: one file per page with a `[...lang]` rest parameter (`langPaths()` in `src/lib/paths.ts`) covers English at the root and nine languages under their codes. The playground and its examples (`src/lib/playground.ts`), the showcase, and the API reference (`src/lib/api.ts`, `src/lib/api-html.ts`, inside `StarlightPage` with the packages as its sidebar) are there; `src/pages/[file].json.ts` writes `api.json` and `src/pages/[file].txt.ts` `llms.txt` and `llms-full.txt` (`src/lib/llms.ts`).
- **Build-time contracts kept**: under `CI` a missing playground build or a failing `go run ./cmd/gameapi` fails the build; `scripts/check-api.ts` still checks every exported identifier's anchor on every language's page after `astro build`.

The package's pieces are imported only from its public exports (`nsgo-website/components`, `/site`, `/content`, `/styles/*`); anything a second project would need belongs in nsgo-website, proposed there rather than patched here.

## Pitfalls

- **Sätteri, not remark**: Astro 7's default Markdown processor refuses `markdown.remarkPlugins` unless `@astrojs/markdown-remark` is installed, so the docs' plugin is a Sätteri plugin added to the processor in `astro:config:setup`.
- **Isolated installs**: the workspace's Bun links each package's dependencies under it, so `@astrojs/starlight` is a direct dependency of the site; Starlight's route entrypoints are resolved from the site's root.
- **Starlight's `Tabs` in an own page** bundles Sätteri's native binding into the prerender chunk, which then fails to load; the playground's source tabs are the page's own markup.
- **Pages run from `dist/.prerender`**, so `import.meta.url` says nothing of where the repository is; `src/lib/repo.ts` resolves it from `process.cwd()`, which Astro sets to the site's folder.
- **Starlight places sidebar links under the base and the page's language**: a custom sidebar's `link` is written as the first language has it (`/docs/api/x/`), never with `localePath()`.

## Alternatives considered

### Keep the TanStack Start site and restyle it

It would have kept the playground, the API reference and the existing tests untouched. It was rejected because the user asked for nsgo-website's game version, and a restyled copy would still drift from every other NSGo site's header, footer, search, roadmap and share cards.

### Add frontmatter to every Markdown page

Starlight wants a `title` in each page's frontmatter, and adding it to 208 files would remove the loader wrapper and the heading-dropping plugin. It was rejected because the docs are read on GitHub and by `packages/game` tests, and they would then carry their title twice.

### Show the first paragraph as nsgo-website's lead

Setting `description` makes nsgo-website show it under the title as plain text. It was rejected because 30 pages have links in that paragraph; the paragraph stays in the body, styled as the lead in `src/styles/site.css`, and the description reaches only the meta tags.

### Keep only English and Chinese, as the template does

The template's two languages would have halved the build. It was rejected because the previous site served ten languages and their copy existed; the home page's new words were translated to all ten.

## Consequences

- Benefits: the frame, the light and dark themes, search, roadmap, share cards, language and theme handling come from nsgo-website and follow its updates; the site's own code is the home page, the playground, the showcase, the API reference and llms.txt.
- Costs: about 2,100 pages per build (the API reference alone is 100 packages × 10 languages), about 40 seconds locally; a new page of the site must exist under `src/pages/[...lang]/` to be in every language.
- Adding a language means adding it to `src/locales.ts`, a `src/copy/<lang>.ts`, the showcase's entries and `src/home/words.ts`.

## Verification

- `bun run test` runs `src/lib/llms.test.ts`, which checks that llms.txt and llms-full.txt list every page of `docs/en` with absolute links.
- `bun run typecheck` runs `astro check`; `bun run build` runs `astro build` and `scripts/check-api.ts`.
- A build with `BASE_PATH=/game/` was checked for internal links that lead nowhere: none.

[Historical snapshot: Website multilingual i18n architecture](../../archived/architecture/2026-11-24-website-multilingual-i18n.md)
