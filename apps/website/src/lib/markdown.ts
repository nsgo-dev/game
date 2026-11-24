// A Sätteri mdast plugin for the docs (lib/docs.ts): it drops the first heading, which Starlight shows as the page's title, and points the links at the site's pages or at GitHub. Astro's default Markdown processor runs it; the integration adds it there (lib/integration.ts).
import { fileURLToPath } from 'node:url';
import { docLink, docOf } from './docs.ts';

interface Node {
	type: string;
	depth?: number;
	url?: string;
}
interface Context {
	removeNode(node: Node): void;
	setField(node: Node, key: string, value: unknown): void;
}

export function docsPlugin({ fileURL }: { fileURL?: URL }) {
	const doc = fileURL ? docOf(fileURLToPath(fileURL)) : null;
	if (!doc) return undefined;
	let titled = false;
	const link = (node: Node, ctx: Context) => {
		if (node.url) {
			const url = docLink(node.url, doc.lang, doc.slug);
			if (url !== node.url) ctx.setField(node, 'url', url);
		}
	};
	return {
		name: 'game-docs',
		heading(node: Node, ctx: Context) {
			if (node.depth === 1 && !titled) {
				titled = true;
				ctx.removeNode(node);
			}
		},
		link,
		definition: link,
	};
}
