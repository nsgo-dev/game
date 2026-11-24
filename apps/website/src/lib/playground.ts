// What the playground's pages know of an example: its source files, main.go first and without tests, then its nsgo.yaml, and the web build public/play/<id>/ holds, which scripts/build-playground.ts makes. A deploy must not publish a playground that runs nothing, so under CI a missing build fails the site's build; a local build goes on and the example's page says how to make one.
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import path from 'node:path';
import { PLAYGROUND_BUILDS, PLAYGROUND_IDS, type PlaygroundId } from '../content/playground.ts';
import { SHOWCASE_ENTRIES, type ShowcaseEntry } from '../content/showcase.ts';
import { REPO_ROOT } from './repo.ts';

export interface ExampleFile {
	name: string;
	lang: 'go' | 'yaml';
	code: string;
	lines: number;
}

export interface Example {
	id: PlaygroundId;
	entry: ShowcaseEntry;
	files: ExampleFile[];
	lines: number;
	// wasm is the size in bytes of the build's WebAssembly, 0 without a build.
	wasm: number;
}

function files(id: PlaygroundId): ExampleFile[] {
	const dir = path.join(REPO_ROOT, 'examples', id);
	const names = readdirSync(dir, { withFileTypes: true })
		.filter((e) => e.isFile())
		.map((e) => e.name);
	const go = names.filter((n) => n.endsWith('.go') && !n.endsWith('_test.go')).sort((a, b) => Number(b === 'main.go') - Number(a === 'main.go') || a.localeCompare(b));
	if (go.length === 0) throw new Error(`playground: examples/${id} has no Go files`);
	return [...go, ...(names.includes('nsgo.yaml') ? ['nsgo.yaml'] : [])].map((name) => {
		const code = readFileSync(path.join(dir, name), 'utf8').trimEnd();
		return { name, lang: name.endsWith('.go') ? 'go' : 'yaml', code, lines: code.split('\n').length };
	});
}

// wasmSize is the size of the WebAssembly the web build in public/play/<id>/ runs, as its nsgo.json names it, or 0 when there is no usable build.
function wasmSize(id: PlaygroundId): number {
	const dir = path.join(process.cwd(), 'public', PLAYGROUND_BUILDS, id);
	const manifest = path.join(dir, 'nsgo.json');
	if (!existsSync(manifest)) return 0;
	try {
		const { wasm } = JSON.parse(readFileSync(manifest, 'utf8')) as { wasm?: unknown };
		if (typeof wasm !== 'string' || wasm === '') throw new Error('nsgo.json names no wasm');
		return statSync(path.join(dir, wasm)).size;
	} catch (err) {
		console.warn(`[playground] ignoring the build in ${dir}: ${err instanceof Error ? err.message : String(err)}`);
		return 0;
	}
}

let cache: Example[] | undefined;

export function examples(): Example[] {
	if (cache) return cache;
	cache = PLAYGROUND_IDS.map((id) => {
		const entry = SHOWCASE_ENTRIES.find((e) => e.id === id);
		if (!entry) throw new Error(`playground: ${id} has no showcase entry`);
		const f = files(id);
		return { id, entry, files: f, lines: f.reduce((sum, file) => sum + file.lines, 0), wasm: wasmSize(id) };
	});
	const missing = cache.filter((e) => e.wasm === 0).map((e) => e.id);
	if (missing.length) {
		const reason = `no web build of ${missing.join(', ')} in public/${PLAYGROUND_BUILDS}/; run bun run playground`;
		if (process.env.CI) throw new Error(`playground: ${reason}`);
		console.warn(`[playground] ${reason}`);
	}
	return cache;
}
