import { readFileSync } from 'node:fs'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { siteContent } from './plugins/content.ts'
import { llmsTxt } from './plugins/llms.ts'
import { PLAYGROUND_BUILDS } from './src/content/playground.ts'
import { NON_DEFAULT_LANGS } from './src/locales.ts'
import { LLMS_FULL, LLMS_INDEX } from './src/links.ts'

// BASE_PATH is where the site is served from: "/" on its own domain, "/game/" on nsgo-dev.github.io. The router, the assets and the Markdown's links all follow it.
const base = normalizeBase(process.env.BASE_PATH)
// SITE_ORIGIN is the origin the site is served at, for the absolute addresses of its canonical and hreflang links; the deploy sets it from GitHub Pages, and other builds use the custom domain in public/CNAME.
const origin = (process.env.SITE_ORIGIN ?? '').trim() || `https://${readFileSync(new URL('public/CNAME', import.meta.url), 'utf8').trim()}`

const LLMS_FILES = [LLMS_INDEX, LLMS_FULL]

export default defineConfig({
  base,
  define: { __SITE_ORIGIN__: JSON.stringify(origin) },
  plugins: [
    siteContent(),
    llmsTxt({ origin }),
    tanstackStart({
      prerender: {
        enabled: true,
        crawlLinks: true,
        failOnError: true,
        // The same page under another #fragment is one file, the playground's builds are the examples' own pages, which a page of the site must not replace, and api.json, the API reference for agents, and the llms files the footer links to are files of the build rather than pages.
        filter: (page) => !page.path.includes('#') && !isPlaygroundBuild(page.path) && !page.path.endsWith('.json') && !LLMS_FILES.some((file) => page.path.split('?')[0]!.endsWith(`/${file}`)),
      },
      // GitHub Pages serves 404.html for any address the site has no page for. It is the SPA shell, which renders whatever route the address matches (the not-found page) in the browser, so it hydrates at every address.
      // Every language's home page starts the crawl, since the language menu is a select whose options are not links.
      pages: NON_DEFAULT_LANGS.map((lang) => ({ path: `/${lang}/` })),
      spa: { enabled: true, maskPath: '/404', prerender: { outputPath: '/404' } },
      sitemap: { enabled: false },
    }),
    viteReact(),
  ],
})

// isPlaygroundBuild reports whether a crawled path is under the builds' directory. The crawler follows links as written, with the base in front (/game/play/<id>/ when the base is /game/), while the site's own routes come without it, so both forms are checked.
function isPlaygroundBuild(pagePath: string): boolean {
  return pagePath.startsWith(`/${PLAYGROUND_BUILDS}/`) || pagePath.startsWith(`${base}${PLAYGROUND_BUILDS}/`)
}

function normalizeBase(value: string | undefined): string {
  const trimmed = (value ?? '').trim().replace(/^\/+|\/+$/g, '')
  return trimmed === '' ? '/' : `/${trimmed}/`
}
