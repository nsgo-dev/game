import { readFileSync } from 'node:fs';
import { defineConfig } from 'astro/config';
import nsgoWebsite from 'nsgo-website';
import site from './site.config.ts';
import { gameContent } from './src/lib/integration.ts';

// BASE_PATH is where the site is served from: "/" on its own domain, "/game/" on nsgo-dev.github.io. SITE_ORIGIN is the origin it is served at; the deploy sets both from GitHub Pages, and other builds use the custom domain in public/CNAME at its root.
const base = (process.env.BASE_PATH ?? '').trim() || '/';
const origin = (process.env.SITE_ORIGIN ?? '').trim() || `https://${readFileSync(new URL('public/CNAME', import.meta.url), 'utf8').trim()}`;

export default defineConfig({
	site: origin,
	base,
	integrations: [nsgoWebsite(site), gameContent()],
});
