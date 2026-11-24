// The site's own build steps beside nsgo-website's: the docs' Markdown plugin.
import type { AstroIntegration } from 'astro';
import { docsPlugin } from './markdown.ts';

export function gameContent(): AstroIntegration {
	return {
		name: 'game-content',
		hooks: {
			'astro:config:setup': ({ config }) => {
				const processor = config.markdown.processor as { name: string; options: { mdastPlugins: unknown[] } } | undefined;
				if (!processor?.options?.mdastPlugins) throw new Error(`game-content: the Markdown processor ${processor?.name} takes no mdast plugins`);
				processor.options.mdastPlugins.push(docsPlugin);
			},
		},
	};
}
