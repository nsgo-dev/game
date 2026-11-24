// The site's settings, for the nsgo-website integration and the share-cards command, in the demoscene look of its game template. The home page is the site's own (src/components/Home.astro); every fact on it comes from the repository's README, docs and code.
import { readdirSync } from 'node:fs';
import { defineSite } from 'nsgo-website/config';
import type { SidebarItem } from 'nsgo-website/config';
import { copies } from './src/copy.ts';
import { groupDocs, type GroupKey } from './src/content/groups.ts';
import { LANGS, type Lang } from './src/locales.ts';

const repo = 'https://github.com/nsgo-dev/game';

// per gives a word in each of the site's languages from the copy files.
const per = (pick: (copy: (typeof copies)[Lang]) => string) => Object.fromEntries(LANGS.map((l) => [l, pick(copies[l])])) as Record<Lang, string>;

// The docs' sidebar: every page of docs/en in the groups of src/content/groups.ts, then the API reference, which the site builds from the Go packages.
const pages = readdirSync(new URL('../../docs/en/', import.meta.url))
	.filter((f) => f.endsWith('.md'))
	.map((f) => ({ slug: f.slice(0, -'.md'.length) }))
	.sort((a, b) => a.slug.localeCompare(b.slug));
const sidebar: SidebarItem[] = [
	...groupDocs(pages).map(({ key, items }) => ({
		label: per((c) => c.docs.groups[key as GroupKey]),
		collapsed: key !== 'guides',
		items: items.map((p) => (p.slug === 'index' ? 'docs' : `docs/${p.slug}`)),
	})),
	{ label: per((c) => c.docs.reference), items: [{ label: per((c) => c.api.nav), link: '/docs/api/' }] },
];

export default defineSite({
	languages: [...LANGS],
	name: 'NSGo Game',
	title: per((c) => c.site.title),
	description: per((c) => c.site.description),
	// NSGo's mark, in its blue on the light theme and in the look's amber on the dark.
	logo: './src/assets/logo.svg',
	logoDark: './src/assets/logo-dark.svg',
	favicon: '/favicon.svg',
	appleTouchIcon: '/apple-touch-icon.png',
	look: 'demoscene',
	repo,
	path: 'apps/website',
	editLink: false,
	nav: [
		'docs',
		{ label: per((c) => c.nav.playground), href: 'playground/' },
		{ label: per((c) => c.nav.showcase), href: 'showcase/' },
		'roadmap',
		'community',
		'feedback',
	],
	accounts: [{ label: '小莫', note: '@suzukaze_haduki', href: 'https://x.com/suzukaze_haduki', icon: 'x.com', avatar: './src/assets/avatars/houko.png' }],
	docs: { dir: '../../docs', sidebar },
	roadmap: {
		owner: 'nsgo-dev',
		name: 'nsgo',
		tracking: 1047,
		description: per((c) => c.roadmap.description),
	},
	footer: {
		copyright: '© 2026 ONEO',
		license: { label: 'Apache-2.0', href: `${repo}/blob/main/LICENSE` },
	},
	home: false,
	customCss: ['./src/styles/site.css'],
	// The Kubai icons of the home page's sections (src/components/Home.astro).
	icons: ['file-code', 'file-diff', 'files', 'gamepad', 'trophy', 'network', 'cube', 'terminal', 'accessibility'],
});
