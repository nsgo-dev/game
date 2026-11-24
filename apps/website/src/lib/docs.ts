// The docs are the repository's Markdown in docs/<lang>/, written to read on GitHub: no frontmatter, the title as the first heading, and links to other pages as name.md and to the repository's files by their relative paths. These helpers turn them into Starlight pages without changing the files.
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const REPO = 'nsgo-dev/game';
export const DOCS_DIR = fileURLToPath(new URL('../../../../docs/', import.meta.url));

// docOf is the language and slug of a file under DOCS_DIR, or null for any other file.
export function docOf(file: string): { lang: string; slug: string } | null {
	const rel = path.relative(DOCS_DIR, path.resolve(file));
	if (rel.startsWith('..') || path.isAbsolute(rel) || !rel.endsWith('.md')) return null;
	const [lang, ...rest] = rel.split(path.sep);
	if (!lang || rest.length === 0) return null;
	return { lang, slug: rest.join('/').replace(/\.md$/, '') };
}

// plain is a line of Markdown as text: links as their words, without code ticks or emphasis.
export function plain(markdown: string): string {
	return markdown
		.replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
		.replace(/[`*_]/g, '')
		.replace(/\s+/g, ' ')
		.trim();
}

// meta is a page's title, its first heading, and its description, the first paragraph after it.
export function meta(source: string): { title: string; description: string } {
	const lines = source.split('\n');
	const h1 = lines.findIndex((l) => /^# /.test(l));
	const title = h1 >= 0 ? plain(lines[h1]!.slice(2)) : '';
	const paragraph: string[] = [];
	for (const line of lines.slice(h1 + 1)) {
		if (line.trim() === '') {
			if (paragraph.length) break;
			continue;
		}
		if (/^(#|```|\||>|[-*] |\d+\. |<)/.test(line) && !paragraph.length) break;
		paragraph.push(line);
	}
	return { title, description: plain(paragraph.join(' ')) };
}

// docLink points a link of the page slug at the site: another page by a path relative to the page's own, so it stays in the language the page is shown in, or a file of the repository on GitHub.
export function docLink(url: string, lang: string, slug: string): string {
	if (/^[a-z][a-z0-9+.-]*:/i.test(url) || url.startsWith('#') || url.startsWith('//')) return url;
	const [target = '', hash = ''] = url.split(/(?=#)/);
	if (target.endsWith('.md') && !target.includes('/')) {
		const to = target.slice(0, -'.md'.length);
		const up = slug === 'index' ? './' : '../';
		return `${up}${to === 'index' ? '' : `${to}/`}${hash}`;
	}
	if (target === '') return url;
	const file = path.posix.normalize(path.posix.join('docs', lang, path.posix.dirname(slug), target));
	return `https://github.com/${REPO}/blob/main/${file}${hash}`;
}

// absoluteLink points a link of an English page at the site's address below site, such as https://game.nsgo.dev/, or at GitHub, for readers outside the site such as llms.txt's.
export function absoluteLink(url: string, slug: string, site: string): string {
	if (url.startsWith('#')) return `${pageUrl(slug, site)}${url}`;
	const [target = '', hash = ''] = url.split(/(?=#)/);
	if (target.endsWith('.md') && !target.includes('/') && !/^[a-z][a-z0-9+.-]*:/i.test(target)) return `${pageUrl(target.slice(0, -'.md'.length), site)}${hash}`;
	return docLink(url, 'en', slug);
}

// pageUrl is an English docs page's address below site.
export const pageUrl = (slug: string, site: string) => `${site}docs/${slug === 'index' ? '' : `${slug}/`}`;
