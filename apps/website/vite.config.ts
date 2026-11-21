import { readFileSync } from 'node:fs'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { siteContent } from './plugins/content.ts'

// BASE_PATH is where the site is served from: "/" on its own domain, "/game/" on nsgo-dev.github.io. The router, the assets and the Markdown's links all follow it.
const base = normalizeBase(process.env.BASE_PATH)
// SITE_ORIGIN is the origin the site is served at, for the absolute addresses of its canonical and hreflang links; the deploy sets it from GitHub Pages, and other builds use the custom domain in public/CNAME.
const origin = (process.env.SITE_ORIGIN ?? '').trim() || `https://${readFileSync(new URL('public/CNAME', import.meta.url), 'utf8').trim()}`

export default defineConfig({
  base,
  define: { __SITE_ORIGIN__: JSON.stringify(origin) },
  plugins: [
    siteContent(),
    tanstackStart({
      prerender: {
        enabled: true,
        crawlLinks: true,
        failOnError: true,
        // The same page under another #fragment is one file.
        filter: (page) => !page.path.includes('#'),
      },
      // GitHub Pages serves 404.html for any address the site has no page for. It is the SPA shell, which renders whatever route the address matches (the not-found page) in the browser, so it hydrates at every address.
      spa: { enabled: true, maskPath: '/404', prerender: { outputPath: '/404' } },
      sitemap: { enabled: false },
    }),
    viteReact(),
  ],
})

function normalizeBase(value: string | undefined): string {
  const trimmed = (value ?? '').trim().replace(/^\/+|\/+$/g, '')
  return trimmed === '' ? '/' : `/${trimmed}/`
}
