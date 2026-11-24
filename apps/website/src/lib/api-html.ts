// A package's page of the API reference as HTML, laid out as pkg.go.dev lays out its own: the package's doc, an index, then its constants, variables, functions and types, each type with its values, constructors and methods. Every exported identifier has an element with its anchor as id, which scripts/check-api.ts checks after the build.
import type { ApiFunc, ApiIdent, ApiPackage, ApiType, ApiValue } from '../content/types.ts';
import type { Copy } from '../copy.ts';
import { GITHUB_URL } from '../links.ts';

// The Go module's directory in the repository, which a declaration's file is relative to.
const MODULE_DIR = 'packages/game';

// The ids of a page's sections, as pkg.go.dev names them; an exported identifier starts with a capital letter, so none is one of these.
export const SECTION = { overview: 'pkg-overview', index: 'pkg-index', consts: 'pkg-constants', vars: 'pkg-variables', funcs: 'pkg-functions', types: 'pkg-types' } as const;

const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

type ApiCopy = Copy['api'];

export interface Heading {
	depth: number;
	slug: string;
	text: string;
}

// headings are the page's contents, for Starlight's table of contents.
export function headings(pkg: ApiPackage, copy: ApiCopy): Heading[] {
	const h: Heading[] = [];
	if (pkg.docHTML) h.push({ depth: 2, slug: SECTION.overview, text: copy.overview });
	h.push({ depth: 2, slug: SECTION.index, text: copy.index });
	if (pkg.consts.length) h.push({ depth: 2, slug: SECTION.consts, text: copy.constants });
	if (pkg.vars.length) h.push({ depth: 2, slug: SECTION.vars, text: copy.variables });
	if (pkg.funcs.length) h.push({ depth: 2, slug: SECTION.funcs, text: copy.functions });
	if (pkg.types.length) {
		h.push({ depth: 2, slug: SECTION.types, text: copy.types });
		for (const t of pkg.types) h.push({ depth: 3, slug: t.anchor, text: t.name });
	}
	return h;
}

export const platforms = (copy: ApiCopy, list: string[] | undefined, tag: 'p' | 'span' = 'span') =>
	list?.length ? `<${tag} class="api-platforms">${esc(copy.only)} ${esc(list.join(', '))}</${tag}>` : '';

const prose = (html: string) => `<div class="api-doc" lang="en">${html}</div>`;

const source = (copy: ApiCopy, file: string, line: number) =>
	`<a class="api-source" href="${GITHUB_URL}/blob/main/${MODULE_DIR}/${esc(file)}#L${line}" target="_blank" rel="noopener noreferrer" aria-label="${esc(`${copy.source}: ${file}:${line}`)}" title="${esc(`${file}:${line}`)}">${esc(copy.source)} ↗</a>`;

const heading = (copy: ApiCopy, level: 3 | 4, anchor: string, text: string, item: { file: string; line: number; platforms?: string[] }) =>
	`<div class="api-heading"><h${level} id="${esc(anchor)}" lang="en"><a href="#${esc(anchor)}">${esc(text)}</a></h${level}>${platforms(copy, item.platforms)}${source(copy, item.file, item.line)}</div>`;

const escapeRegExp = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');

// decl shows a declaration's source with an element for each anchor in it: the line that declares the name, as gofmt prints it (a constant or variable, a struct field, embedded ones by their type's name, or an interface method), or an empty element before the source for a name on no such line.
function decl(source: string, anchors: ApiIdent[]): string {
	const lines = source.split('\n').map((text) => ({ text, ids: [] as string[] }));
	const rest: string[] = [];
	for (const { name, anchor } of anchors) {
		const pattern = new RegExp(`^(?:(?:const|var) |\\t)\\*?(?:\\w+\\.)?(?:\\w+, )*${escapeRegExp(name)}(?![\\w])`);
		const line = lines.find((l) => pattern.test(l.text));
		if (line) line.ids.push(anchor);
		else rest.push(anchor);
	}
	const body = lines
		.map((line, i) => {
			const text = esc(i < lines.length - 1 ? `${line.text}\n` : line.text);
			const [first, ...others] = line.ids;
			return others.map((id) => `<span id="${esc(id)}"></span>`).join('') + (first ? `<span id="${esc(first)}" class="api-line">${text}</span>` : text);
		})
		.join('');
	return `<pre class="api-decl" lang="en">${rest.map((id) => `<span id="${esc(id)}"></span>`).join('')}<code>${body}</code></pre>`;
}

const value = (copy: ApiCopy, v: ApiValue) =>
	`<div class="api-item"><div class="api-heading api-heading-value">${platforms(copy, v.platforms)}${source(copy, v.file, v.line)}</div>${decl(v.decl, v.idents)}${v.docHTML ? prose(v.docHTML) : ''}</div>`;

const func = (copy: ApiCopy, f: ApiFunc, level: 3 | 4) =>
	`<div class="api-item">${heading(copy, level, f.anchor, `func ${f.recv ? `(${f.recv}) ` : ''}${f.name}`, f)}${decl(f.decl, [])}${f.docHTML ? prose(f.docHTML) : ''}</div>`;

const type = (copy: ApiCopy, t: ApiType) =>
	`<div class="api-item api-type">${heading(copy, 3, t.anchor, `type ${t.name}`, t)}${decl(t.decl, t.members)}${t.docHTML ? prose(t.docHTML) : ''}${[...t.consts, ...t.vars].map((v) => value(copy, v)).join('')}${[...t.funcs, ...t.methods].map((f) => func(copy, f, 4)).join('')}</div>`;

const indexEntry = (f: ApiFunc) => `<li><a href="#${esc(f.anchor)}">${esc(f.decl.replace(/\s*\n\s*/g, ' '))}</a></li>`;

const section = (id: string, title: string, body: string) => `<section class="api-section"><h2 id="${id}">${esc(title)}</h2>${body}</section>`;

export function packageHTML(pkg: ApiPackage, copy: ApiCopy): string {
	const out: string[] = [];
	out.push(`<pre class="api-import"><code>import "${esc(pkg.importPath)}"</code></pre>`, platforms(copy, pkg.platforms, 'p'));
	if (pkg.docHTML) out.push(section(SECTION.overview, copy.overview, prose(pkg.docHTML)));
	const index = [
		pkg.consts.length ? `<li><a href="#${SECTION.consts}">${esc(copy.constants)}</a></li>` : '',
		pkg.vars.length ? `<li><a href="#${SECTION.vars}">${esc(copy.variables)}</a></li>` : '',
		...pkg.funcs.map(indexEntry),
		...pkg.types.map((t) => {
			const fs = [...t.funcs, ...t.methods];
			return `<li><a href="#${esc(t.anchor)}">type ${esc(t.name)}</a>${fs.length ? `<ul>${fs.map(indexEntry).join('')}</ul>` : ''}</li>`;
		}),
	].join('');
	out.push(section(SECTION.index, copy.index, `<ul class="api-index" lang="en">${index}</ul>`));
	if (pkg.consts.length) out.push(section(SECTION.consts, copy.constants, pkg.consts.map((v) => value(copy, v)).join('')));
	if (pkg.vars.length) out.push(section(SECTION.vars, copy.variables, pkg.vars.map((v) => value(copy, v)).join('')));
	if (pkg.funcs.length) out.push(section(SECTION.funcs, copy.functions, pkg.funcs.map((f) => func(copy, f, 3)).join('')));
	if (pkg.types.length) out.push(section(SECTION.types, copy.types, pkg.types.map((t) => type(copy, t)).join('')));
	return out.join('');
}
