# Agent Note: The website back on TanStack Start, in the game site design

Status: implemented

## Problem

`apps/website` moved to Astro and nsgo-website's demoscene look in 1ab4a381. A new design for the game layer's own site followed: a dark site with a home page around the examples' recordings, a docs layout, a tutorial page, a package catalog with filters and search, and an examples page, in ten languages, with docs, tutorial and package text in English and Chinese only. The design is the site's own, not a shared look, and its pages are interactive React views (the reel, the code tabs, the catalog's filter and search), which nsgo-website's frame does not provide.

## Decision

`apps/website` is the TanStack Start and React app it was before 1ab4a381, restored from 1ab4a381~1, with everything main added since carried forward and the new design built as its own components and stylesheet.

- **Routes (`src/routes/`)**: English at the root and nine languages under `/$lang/`, as before. The design's pages are `/tutorial/`, `/packages/` and `/examples/` (`src/pages/Tutorial.tsx`, `Packages.tsx`, `Examples.tsx`), beside the home page, the docs, the API reference, the playground, the showcase and the roadmap. The language menu is a select, so `vite.config.ts` starts the prerender crawl at every language's home page.
- **Content from the repository (`plugins/site.ts`, the module `virtual:site`)**: the catalog is every row of `docs/en/stability.md`, its description from the table in `docs/<lang>/index.md`, else the first sentence of its guide, else the first sentence of its Go doc comment, and its group from its path (`category`). The tutorial page reads `docs/<lang>/tutorial.md`: its title, the two paragraphs before the chapters, the chapter table and the list of files under "How the game is built". The examples page lists the packages each featured example imports, read from its Go files. Nothing on these pages is copied from the design's mockups but their words.
- **Traditional Chinese**: docs, tutorial and package text in `zh-tw` are `docs/zh` converted with OpenCC's `cn` to `twp` converter (`opencc-js`, a build-time dependency), as the design's generator did; other languages without docs show English, marked `lang="en"`.
- **Docs split by page (`plugins/content.ts`)**: `virtual:docs` is an index of each language's slugs and titles and a loader per page, `virtual:docs/<lang>/<slug>`, which the docs routes await in their loaders. Before, every docs page shipped all languages' rendered Markdown in one script of about 10 MB; with Traditional Chinese added it would have been 15 MB.
- **The site's own words (`src/copy/pages/<lang>.ts`)**: the header's, footer's and the design's pages' words in ten languages, from the design's copy, merged with the rest of each language's copy in `src/copy.ts`. A title with `{n}` takes the catalog's package count.
- **Media (`public/media/`)**: the recordings of the examples' web builds as H.264 MP4 with WebP posters (about 1.7 MB in all, played muted only while on screen and never with reduced motion), and Cave Run's CC0 sprites from `examples/caverun/assets`. `snippets/game.go.txt` is the design's component code, checked to compile against `packages/game` when it was added.
- **Carried over from the Astro version**: the docs groups for the templates, `tileshape`, `navbake3d`, `scenemodel` and `agent-skill`; the template listings in `src/content/showcase.ts`; the JSON Schemas in `public/schemas/`; the playground loading a build by its `index.html`; NSGo in the footer's copyright; the share cards in `public/share/` as each page's `og:image`; the SVG favicon, in the design's blue; and Chinese docs pages showing the page's group above the title with the first paragraph as a lead.
- **Search (`src/components/Search.tsx`)**: after the prerender, `pagefind --site dist/client` indexes the docs pages (the article carries `data-pagefind-body`), an index per language, and the header's button, or Ctrl/Cmd+K, opens Pagefind's interface in a dialog, loading it only then.
- **Community links**: the footer links NSGo, Awesome NSGo, Telegram, 小莫 on X, the issues for feedback and the contributing guide, which the Astro version's header carried.
- **Build-time contracts kept**: under `CI` a missing playground build, a failing `go run ./cmd/gameapi` or an unreadable roadmap fails the build; `scripts/check-api.ts` checks every exported identifier's anchor on every language's API page; the build writes `api.json`, `llms.txt` and `llms-full.txt`; the Pages artifact is `dist/client`.

## Pitfalls

- **The prerender crawls links only**: a page reachable only through the language select, or a page no link names, is not prerendered unless `pages` in `vite.config.ts` names it.
- **A docs route must await its page**: `loadDoc` is asynchronous, and the page's HTML reaches the client in the route's loader data, so a component never imports `virtual:docs/<lang>/<slug>` itself.
- **Public assets in CSS are written from the root (`url('/media/…')`)**: Vite puts the base path in front at build time.

## Alternatives considered

### Keep Astro and restyle nsgo-website's frame

It would keep Starlight's search and the light theme. It was rejected because the design is a site of its own rather than the shared look, and its interactive pages would be islands inside a frame whose header, footer and docs layout the design replaces anyway.

### Hard-code the design's content

The mockups carry 131 packages, nine chapters and their descriptions as data. They were generated from the repository on one day and already miss eight packages; reading the same files at build time keeps the pages true as the docs change.

### Keep the docs in one module

It needs no change to the routes. It was rejected because every docs page would download every language's rendered docs.

### Add Fountain and Lantern 3D to the playground

The design gives every example a play button. Fountain is a measurement scene its README keeps off the showcase, and Lantern 3D draws through NSGo's GPU view, whose web build the playground has never run; building them in CI is a decision of its own, so their play buttons are left out and the others point at the playground.

## Consequences

- Benefits: the design's pages exist as real, prerendered pages in ten languages, generated from the docs, so the package count, the catalog, the tutorial and the examples' packages follow the repository; docs pages load one page's content; zh-tw readers get the docs in Traditional Chinese.
- Costs: the site's frame, search and language handling are this repository's again; there is no light theme, which the Astro version had and the design does not draw; the prerender is about 2,600 pages; `opencc-js` is a new build-time dependency.
- Adding a language means a `src/copy/<lang>.ts`, a `src/copy/pages/<lang>.ts`, the showcase's entries and `src/locales.ts`.

## Verification

- `bun run test` runs `plugins/llms.test.ts`, `plugins/site.test.ts` and `src/locales.test.ts`; `plugins/site.test.ts` parses the real `stability.md`, `index.md` and `tutorial.md` in both languages and checks the catalog's size against the stability table.
- `bun run build` runs `vite build`, the prerender, `tsc --noEmit` and `scripts/check-api.ts`.

[Historical snapshot: The website on nsgo-website's demoscene look](../../archived/architecture/2026-11-24-website-on-nsgo-website.md)
