// api.json, the API reference as JSON for agents (lib/api.ts), at the site's root; a build without the reference has none.
import { apiBuild } from '../lib/api.ts';

export const getStaticPaths = () => (apiBuild().status === 'ok' ? [{ params: { file: 'api' } }] : []);

export function GET() {
	const build = apiBuild();
	return new Response(build.status === 'ok' ? build.json : '{}', { headers: { 'content-type': 'application/json' } });
}
