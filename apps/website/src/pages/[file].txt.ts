// llms.txt and llms-full.txt, the English docs for language models (lib/llms.ts), at the site's root with absolute links.
import type { APIContext } from 'astro';
import { LLMS_FULL, LLMS_INDEX } from '../links.ts';
import { buildLlms, loadPages } from '../lib/llms.ts';
import { BASE } from '../lib/paths.ts';
import { REPO_ROOT } from '../lib/repo.ts';

export const getStaticPaths = () => [LLMS_INDEX, LLMS_FULL].map((name) => ({ params: { file: name.slice(0, -'.txt'.length) } }));

export async function GET({ params, site }: APIContext) {
	const origin = `${site!.origin}${BASE}`;
	const files = buildLlms(await loadPages(`${REPO_ROOT}/docs/en`, origin), origin);
	return new Response(`${params.file}.txt` === LLMS_FULL ? files.full : files.index, { headers: { 'content-type': 'text/plain; charset=utf-8' } });
}
