// The API reference is what cmd/gameapi in the Go module writes from the packages' source and doc comments: run with -html for the pages, which show each doc comment as HTML, and without it for api.json, the JSON published for agents. Without Go a local build shows the reference as unavailable; a build under CI fails instead, so a deploy never publishes an empty reference.
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import type { ApiReference } from '../content/types.ts';
import { REPO_ROOT } from './repo.ts';

export type ApiBuild = { status: 'ok'; ref: ApiReference; json: string } | { status: 'unavailable'; reason: string };

const gameapi = (args: string[]) =>
	execFileSync('go', ['run', './cmd/gameapi', ...args], { cwd: path.join(REPO_ROOT, 'packages/game'), maxBuffer: 256 << 20, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });

let cache: ApiBuild | undefined;

export function apiBuild(): ApiBuild {
	if (cache) return cache;
	try {
		cache = { status: 'ok', ref: JSON.parse(gameapi(['-html'])) as ApiReference, json: gameapi([]) };
	} catch (error) {
		const e = error as { stderr?: string; message?: string };
		const reason = `go run ./cmd/gameapi: ${(e.stderr ?? '').trim() || e.message}`;
		if (process.env.CI) throw new Error(`api: ${reason}`);
		console.warn(`[api] skipped: ${reason}`);
		cache = { status: 'unavailable', reason };
	}
	return cache;
}

// groupPackages puts the packages at the module's top level first, then those below each directory, such as tooling/, under it.
export function groupPackages<T extends { page: string }>(packages: T[]): { dir: string; items: T[] }[] {
	const groups = new Map<string, T[]>();
	for (const pkg of packages) {
		const slash = pkg.page.indexOf('/');
		const dir = slash < 0 ? '' : pkg.page.slice(0, slash);
		groups.set(dir, [...(groups.get(dir) ?? []), pkg]);
	}
	return [...groups].map(([dir, items]) => ({ dir, items })).sort((a, b) => (a.dir === '' ? -1 : b.dir === '' ? 1 : a.dir.localeCompare(b.dir)));
}
