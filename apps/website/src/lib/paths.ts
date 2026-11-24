// Addresses of the site's own pages under its base, the first language at the root and each other under its code, as nsgo-website places them.
import { LANGS, type Lang } from '../locales.ts';

export const BASE = import.meta.env.BASE_URL.replace(/\/?$/, '/');

export function localePath(lang: Lang, path = ''): string {
	return `${BASE}${lang === 'en' ? '' : `${lang}/`}${path}`;
}

// langPaths are the static paths of a page under src/pages/[...lang]/: English at the root, every other language under its code.
export const langPaths = () => LANGS.map((lang) => ({ params: { lang: lang === 'en' ? undefined : lang }, props: { lang } }));
