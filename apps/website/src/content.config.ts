import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { docsCollection } from 'nsgo-website/content';
import { meta } from './lib/docs.ts';

// The docs collection of nsgo-website over docs/<lang>/, its pages' titles and descriptions read from their Markdown, which has no frontmatter (lib/docs.ts).
function docs() {
	const collection = docsCollection();
	const loader = (collection as unknown as { loader: unknown }).loader as { name: string; load: (context: any) => Promise<void> };
	return {
		...collection,
		loader: {
			...loader,
			load: (context: any) =>
				loader.load({
					...context,
					parseData: (entry: { data: Record<string, unknown>; filePath?: string }) => {
						if (entry.filePath && entry.data.title === undefined) {
							const source = readFileSync(path.resolve(fileURLToPath(context.config.root), entry.filePath), 'utf8');
							const { title, description } = meta(source);
							// The description goes to the page's meta tags only: the page shows the paragraph itself, with its links, where nsgo-website would show the description as plain text.
							const head = description ? (['description', 'og:description', 'twitter:description'] as const).map((name) => ({ tag: 'meta', attrs: { [name.startsWith('og:') ? 'property' : 'name']: name, content: description } })) : [];
							entry.data = { ...entry.data, title, head: [...((entry.data.head as unknown[]) ?? []), ...head] };
						}
						return context.parseData(entry);
					},
				}),
		},
	};
}

export const collections = { docs: docs() };
