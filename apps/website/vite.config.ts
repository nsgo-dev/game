import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vite'
import { siteContent } from './plugins/content.ts'

// BASE_PATH is where the site is served from: "/" on its own domain, "/game/" on nsgo-dev.github.io. The router, the assets and the Markdown's links all follow it.
const base = normalizeBase(process.env.BASE_PATH)

export default defineConfig({
  base,
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
