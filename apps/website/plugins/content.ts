// Build-time content: the docs from the repository's Markdown in each language, the API reference from the Go packages' doc comments, the home page's Go snippets, and the playground's example sources. Each is a virtual module the routes import, so the pages carry the result and nothing is read from the repository in the browser.

import { execFile } from 'node:child_process'
import { readdir, readFile, stat } from 'node:fs/promises'
import path from 'node:path'
import rehypeShiki from '@shikijs/rehype'
import rehypeSlug from 'rehype-slug'
import rehypeStringify from 'rehype-stringify'
import remarkGfm from 'remark-gfm'
import remarkParse from 'remark-parse'
import remarkRehype from 'remark-rehype'
import { codeToHtml } from 'shiki'
import { type Plugin as UnifiedPlugin, unified } from 'unified'
import type { Plugin } from 'vite'
import { PLAYGROUND_BUILDS, PLAYGROUND_IDS, type PlaygroundId, isPlaygroundId } from '../src/content/playground.ts'
import type { ApiPackage, ApiReference, ApiSummary, DocHeading, DocPackage, DocPage, DocSummary, PackageStatus, PlaygroundExample, PlaygroundFile, PlaygroundSource, Snippet } from '../src/content/types'
import { LANGS, type Lang, NON_DEFAULT_LANGS } from '../src/locales.ts'
import { loadSite, toTraditional } from './site.ts'

const REPO = 'nsgo-dev/game'
const SNIPPETS: Snippet['id'][] = ['game', 'loop', 'sprite', 'sound']
const THEMES = { light: 'github-light', dark: 'github-dark' } as const

const modules = {
  'virtual:snippets': loadSnippetsModule,
  'virtual:playground': loadPlaygroundModule,
  'virtual:site': loadSiteModule,
} as const
// Each playground example's sources are a module of their own, virtual:playground/<id>, so a page loads only its example's.
const PLAYGROUND_SOURCE = 'virtual:playground/'
type ModuleId = keyof typeof modules

// A package's page of the API reference is the module virtual:api/<page>, which the reference's route imports on demand, so no page's script carries another package's API.
const API_MODULE = 'virtual:api'
const API_PACKAGE = `${API_MODULE}/`
const DOCS_MODULE = 'virtual:docs'

interface Context {
  root: string
  base: string
  docsDir: string
  snippetsDir: string
  examplesDir: string
  publicDir: string
  // goModule is the directory of the Go module the API reference documents.
  goModule: string
}

export function siteContent(): Plugin {
  let ctx: Context | undefined
  // The client and server builds both load the API reference; generate it once for both.
  let api: Promise<ApiBuild> | undefined
  // The docs are rendered once for the client and server builds, and again after a Markdown file changes.
  let docs: Promise<DocsBuild> | undefined
  const docsBuild = () => {
    if (!ctx) throw new Error('the docs were asked for before the config was resolved')
    docs ??= buildDocs(ctx)
    return docs
  }
  const apiBuild = () => {
    if (!ctx) throw new Error('the API reference was asked for before the config was resolved')
    api ??= buildApi(ctx)
    return api
  }
  return {
    name: 'game-site-content',
    configResolved(config) {
      ctx = {
        root: config.root,
        base: config.base,
        docsDir: path.resolve(config.root, '../../docs'),
        snippetsDir: path.resolve(config.root, 'snippets'),
        examplesDir: path.resolve(config.root, '../../examples'),
        publicDir: config.publicDir,
        goModule: path.resolve(config.root, '../../packages/game'),
      }
    },
    configureServer(server) {
      server.watcher.on('add', (file) => {
        if (!ctx || path.basename(file) !== 'nsgo.json' || !file.startsWith(path.join(ctx.publicDir, PLAYGROUND_BUILDS))) return
        for (const environment of Object.values(server.environments)) {
          const mod = environment.moduleGraph.getModuleById('\0virtual:playground')
          if (mod) environment.moduleGraph.invalidateModule(mod)
        }
        server.ws.send({ type: 'full-reload' })
      })
      server.middlewares.use(async (req, res, next) => {
        if (req.url?.split('?')[0] !== `${server.config.base}${API_JSON}`) return next()
        try {
          const build = await apiBuild()
          if (build.status !== 'ok') throw new Error(build.reason)
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(build.json)
        } catch (error) {
          next(error)
        }
      })
    },
    watchChange(id) {
      if (ctx && id.startsWith(ctx.docsDir)) docs = undefined
    },
    resolveId(id) {
      if (id.startsWith(PLAYGROUND_SOURCE) && isPlaygroundId(id.slice(PLAYGROUND_SOURCE.length))) return `\0${id}`
      if (id === API_MODULE || id.startsWith(API_PACKAGE)) return `\0${id}`
      if (id === DOCS_MODULE || id.startsWith(DOCS_PAGE)) return `\0${id}`
      return id in modules ? `\0${id}` : undefined
    },
    async generateBundle() {
      // The JSON for agents is a file of the client's build, beside the pages.
      if (this.environment.name !== 'client') return
      const build = await apiBuild()
      if (build.status === 'ok') this.emitFile({ type: 'asset', fileName: API_JSON, source: build.json })
    },
    async load(id) {
      if (!id.startsWith('\0')) return undefined
      const name = id.slice(1)
      if (name.startsWith(PLAYGROUND_SOURCE) && ctx) {
        const example = name.slice(PLAYGROUND_SOURCE.length) as PlaygroundId
        const dir = path.join(ctx.examplesDir, example)
        for (const file of await playgroundFiles(dir)) this.addWatchFile(path.join(dir, file))
        return loadPlaygroundSource(ctx, example)
      }
      if (name.startsWith(API_PACKAGE) && ctx) {
        const build = await apiBuild()
        const pkg = build.status === 'ok' ? build.packages.get(name.slice(API_PACKAGE.length)) : undefined
        if (!pkg) throw new Error(`${name}: no such package`)
        return `export default ${JSON.stringify(pkg)}`
      }
      if (name === API_MODULE && ctx) return loadApiModule(await apiBuild())
      if (name === DOCS_MODULE && ctx) {
        for (const lang of LANGS) {
          const dir = path.join(ctx.docsDir, lang)
          for (const file of await optionalMarkdownFiles(dir)) this.addWatchFile(path.join(dir, file))
        }
        return loadDocsModule(ctx, await docsBuild())
      }
      if (name.startsWith(DOCS_PAGE) && ctx) {
        const page = (await docsBuild()).pages.get(name.slice(DOCS_PAGE.length))
        if (!page) throw new Error(`${name}: no such page`)
        return `export default ${JSON.stringify(page)}`
      }
      if (!(name in modules) || !ctx) return undefined
      const moduleId = name as ModuleId
      if (moduleId === 'virtual:playground') {
        // The line counts follow the sources. A build's nsgo.json is watched only once it exists, because Vite resolves every watched file as an import and fails on a missing one; configureServer reloads the module when a build made while the dev server runs writes its nsgo.json.
        for (const id of PLAYGROUND_IDS) {
          const dir = path.join(ctx.examplesDir, id)
          for (const file of await playgroundFiles(dir)) this.addWatchFile(path.join(dir, file))
          const manifest = path.join(ctx.publicDir, PLAYGROUND_BUILDS, id, 'nsgo.json')
          if (await stat(manifest).then(() => true, () => false)) this.addWatchFile(manifest)
        }
      }
      if (moduleId === 'virtual:site') {
        for (const lang of ['en', 'zh'] as const) {
          const dir = path.join(ctx.docsDir, lang)
          for (const file of await optionalMarkdownFiles(dir)) this.addWatchFile(path.join(dir, file))
        }
      }
      if (moduleId === 'virtual:snippets') {
        for (const snippet of SNIPPETS) this.addWatchFile(path.join(ctx.snippetsDir, `${snippet}.go.txt`))
      }
      return modules[moduleId](ctx)
    },
  }
}

// ---- Docs ----

// Sidebar order for the docs; any additional pages sort alphabetically after these.
const DOC_ORDER = [
  'index.md',
  'getting-started.md',
  'playground.md',
  'packages.md',
  'stability.md',
  'loop.md',
  'sprite.md',
  'sound.md',
  '3d-models-blender.md',
  'shipping.md',
  'cli.md',
  'cli-reference.md',
] as const

// markdownFiles lists dir's Markdown files, those in DOC_ORDER first and the rest alphabetically.
export async function markdownFiles(dir: string): Promise<string[]> {
  const files = (await readdir(dir)).filter((f) => f.endsWith('.md'))
  const rankOf = (file: string) => {
    const idx = DOC_ORDER.indexOf(file as (typeof DOC_ORDER)[number])
    return idx >= 0 ? idx : Number.MAX_SAFE_INTEGER
  }
  return files.sort((a, b) => rankOf(a) - rankOf(b) || a.localeCompare(b))
}

// optionalMarkdownFiles returns dir's Markdown files, or [] when a locale has no docs/<lang>/ directory yet.
async function optionalMarkdownFiles(dir: string): Promise<string[]> {
  try {
    return await markdownFiles(dir)
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return []
    throw error
  }
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// localizeDocLinks rewrites rendered internal docs links (href="<base>docs/...") on a fallback English page so navigation stays in lang.
export function localizeDocLinks(html: string, lang: Lang, base: string): string {
  if (lang === 'en') return html
  const pattern = new RegExp(`(<a\\b[^>]*\\shref=")(${escapeRegExp(base)}docs\\/)`, 'g')
  return html.replace(pattern, `$1${base}${lang}/docs/`)
}

// The docs are a small index, each language's pages by slug and title, and a module per rendered page, virtual:docs/<lang>/<slug>, which a page's route loads alone. A language lists English's pages in English's order, its own translation where docs/<lang>/<slug>.md exists, and English otherwise; only files on disk are rendered, and a page shown in English in another language is the English module with its links moved into that language.
const DOCS_PAGE = 'virtual:docs/'

interface DocsBuild {
  index: Record<Lang, DocSummary[]>
  // pages holds each rendered page by "<lang>/<slug>", English's and each translation's.
  pages: Map<string, DocPage>
}

async function buildDocs(ctx: Context): Promise<DocsBuild> {
  const englishFiles = await markdownFiles(path.join(ctx.docsDir, 'en'))
  const packages = await readStability(ctx)
  const pages = new Map<string, DocPage>()
  for (const file of englishFiles) {
    const source = await readFile(path.join(ctx.docsDir, 'en', file), 'utf8')
    const slug = file.slice(0, -'.md'.length)
    pages.set(`en/${slug}`, { ...(await renderDoc(slug, source, { lang: 'en', from: 'en' }, ctx)), packages: packages.get(slug) ?? [] })
  }
  const english = englishFiles.map((file) => pages.get(`en/${file.slice(0, -'.md'.length)}`)!)
  const index = { en: english.map(summary) } as Record<Lang, DocSummary[]>
  for (const lang of NON_DEFAULT_LANGS) {
    // Traditional Chinese without docs of its own shows the Simplified Chinese pages converted by OpenCC.
    let from: Lang = lang
    let own = new Set(await optionalMarkdownFiles(path.join(ctx.docsDir, lang)))
    if (own.size === 0 && lang === 'zh-tw') {
      from = 'zh'
      own = new Set(await optionalMarkdownFiles(path.join(ctx.docsDir, from)))
    }
    for (const file of englishFiles) {
      if (!own.has(file)) continue
      const written = await readFile(path.join(ctx.docsDir, from, file), 'utf8')
      const source = from === lang ? written : toTraditional(written)
      const slug = file.slice(0, -'.md'.length)
      pages.set(`${lang}/${slug}`, { ...(await renderDoc(slug, source, { lang, from }, ctx)), fallback: false, packages: packages.get(slug) ?? [] })
    }
    index[lang] = english.map((page) => summary(pages.get(`${lang}/${page.slug}`) ?? { ...page, fallback: true }))
  }
  return { index, pages }
}

function summary(page: DocPage): DocSummary {
  return { slug: page.slug, title: page.title, fallback: page.fallback }
}

function loadDocsModule(ctx: Context, build: DocsBuild): string {
  const linkSource = `(<a\\b[^>]*\\shref=")(?:${escapeRegExp(ctx.base)}docs\\/)`
  const loaders = [...build.pages.keys()].map((key) => `  ${JSON.stringify(key)}: () => import(${JSON.stringify(DOCS_PAGE + key)}),`).join('\n')
  return `export const docIndex = ${JSON.stringify(build.index)}
const base = ${JSON.stringify(ctx.base)}
const linkPattern = new RegExp(${JSON.stringify(linkSource)}, 'g')
const loaders = {
${loaders}
}
export async function loadDoc(lang, slug) {
  const own = Object.hasOwn(loaders, lang + '/' + slug) ? loaders[lang + '/' + slug] : undefined
  if (own) return (await own()).default
  const english = Object.hasOwn(loaders, 'en/' + slug) ? loaders['en/' + slug] : undefined
  if (!english || !Object.hasOwn(docIndex, lang)) return undefined
  const page = (await english()).default
  return { ...page, fallback: true, html: page.html.replace(linkPattern, '$1' + base + lang + '/docs/') }
}`
}

// A row of the package table in docs/en/stability.md: the package, its status, and the page that is its guide, if any. A test in packages/game keeps the table complete and the Chinese one equal to it.
const STABILITY_ROW = /^\| `([^`]+)` \| (Stable|Experimental) \| (?:\[[^\]]+\]\(([a-z0-9-]+)\.md\))? ?\|$/

// readStability reads each package's status from docs/en/stability.md, by the slug of its guide.
async function readStability(ctx: Context): Promise<Map<string, DocPackage[]>> {
  const source = await readFile(path.join(ctx.docsDir, 'en', 'stability.md'), 'utf8')
  const bySlug = new Map<string, DocPackage[]>()
  let rows = 0
  for (const line of source.split('\n')) {
    const m = STABILITY_ROW.exec(line)
    if (!m) continue
    rows++
    const [, pkg = '', status = '', slug] = m
    if (!slug) continue
    const list = bySlug.get(slug) ?? []
    list.push({ path: pkg, status: status.toLowerCase() as PackageStatus })
    bySlug.set(slug, list)
  }
  if (rows === 0) throw new Error('docs/en/stability.md has no package rows')
  return bySlug
}

// MdNode is the part of an mdast node this file reads and writes.
interface MdNode {
  type: string
  children?: MdNode[]
  url?: string
  depth?: number
  value?: string
}

// lang is the language the page is shown in, from the one its Markdown is written in: the two differ when lang has no translation of it.
async function renderDoc(slug: string, source: string, { lang, from }: { lang: Lang; from: Lang }, ctx: Context): Promise<Omit<DocPage, 'packages'>> {
  const meta = { title: slug, description: '' }
  const headings: DocHeading[] = []
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(docTransform(meta, lang, from, ctx) as UnifiedPlugin)
    .use(remarkRehype)
    .use(rehypeSlug)
    .use(collectHeadings(headings) as UnifiedPlugin)
    // The language class names the block in its editor pane's title bar.
    .use(rehypeShiki, { themes: THEMES, defaultColor: false, addLanguageClass: true })
    .use(rehypeStringify)
    .process(source)
  return { slug, title: meta.title, description: meta.description, html: String(file), headings, fallback: from !== lang, source: `docs/${from}/${slug}.md` }
}

// HastNode is the part of a hast node collectHeadings reads.
interface HastNode {
  type: string
  tagName?: string
  properties?: { id?: unknown }
  value?: string
  children?: HastNode[]
}

// collectHeadings records each h2 and h3 with the id rehype-slug gave it, for the page's contents list.
function collectHeadings(headings: DocHeading[]) {
  return () => (tree: HastNode) => {
    const visit = (node: HastNode) => {
      if (node.type === 'element' && (node.tagName === 'h2' || node.tagName === 'h3') && typeof node.properties?.id === 'string') {
        headings.push({ depth: node.tagName === 'h2' ? 2 : 3, id: node.properties.id, text: hastText(node) })
        return
      }
      for (const child of node.children ?? []) visit(child)
    }
    visit(tree)
  }
}

function hastText(node: HastNode): string {
  if (node.type === 'text') return node.value ?? ''
  return (node.children ?? []).map(hastText).join('')
}

// docTransform takes the first heading out as the page's title and its first paragraph as its description, and points the Markdown's links at the site's docs routes in lang or at the files on GitHub.
function docTransform(meta: { title: string; description: string }, lang: Lang, from: Lang, ctx: Context) {
  return () => (tree: MdNode) => {
    const children = tree.children ?? []
    const h1 = children.findIndex((n) => n.type === 'heading' && n.depth === 1)
    if (h1 >= 0) {
      meta.title = textOf(children[h1]!)
      children.splice(h1, 1)
    }
    const paragraph = children.find((n) => n.type === 'paragraph')
    if (paragraph) meta.description = textOf(paragraph)
    walk(tree, (node) => {
      if ((node.type === 'link' || node.type === 'definition') && node.url) node.url = rewriteLink(node.url, lang, from, ctx.base)
    })
  }
}

// rewriteLink points a link of a page written in from at the site's docs route in lang, below base (a path such as "/game/", or an origin and path for an absolute address), or at the file on GitHub. A link to another page stays in lang, so a Chinese page, or the English one shown in its place, links to Chinese pages.
export function rewriteLink(url: string, lang: Lang, from: Lang, base: string): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(url) || url.startsWith('#') || url.startsWith('//')) return url
  const [target = '', hash = ''] = url.split(/(?=#)/)
  if (target.endsWith('.md') && !target.includes('/')) {
    const slug = target.slice(0, -'.md'.length)
    return `${base}${lang === 'en' ? '' : `${lang}/`}docs/${slug === 'index' ? '' : `${slug}/`}${hash}`
  }
  // Anything else is a file or directory of the repository, relative to the Markdown's directory, docs/<from>/.
  const repoPath = path.posix.normalize(path.posix.join('docs', from, target))
  return `https://github.com/${REPO}/blob/main/${repoPath}${hash}`
}

function walk(node: MdNode, visit: (node: MdNode) => void) {
  visit(node)
  for (const child of node.children ?? []) walk(child, visit)
}

function textOf(node: MdNode): string {
  if (node.value !== undefined) return node.value
  return (node.children ?? []).map(textOf).join('')
}

// ---- API reference ----

// The reference is what cmd/gameapi in the Go module writes: run with -html for the pages, which show each doc comment as HTML, and without it for API_JSON, the JSON published for agents. Without Go, a local build shows the reference as unavailable; a deploy needs it.

const API_JSON = 'api.json'

type ApiBuild =
  | { status: 'ok'; json: string; summaries: ApiSummary[]; packages: Map<string, ApiPackage> }
  | { status: 'unavailable'; reason: string }

async function buildApi(ctx: Context): Promise<ApiBuild> {
  try {
    const [html, json] = await Promise.all([gameapi(ctx, ['-html']), gameapi(ctx, [])])
    const ref = JSON.parse(html) as ApiReference
    const packages = new Map<string, ApiPackage>()
    const summaries: ApiSummary[] = []
    for (const pkg of ref.packages) {
      // The pages show the doc comments as HTML, so the text of each is left out of their scripts.
      packages.set(pkg.page, JSON.parse(JSON.stringify(pkg, (key, value) => (key === 'doc' ? undefined : value))) as ApiPackage)
      summaries.push({ importPath: pkg.importPath, name: pkg.name, page: pkg.page, synopsis: pkg.synopsis, platforms: pkg.platforms ?? [] })
    }
    console.info(`[api] ${ref.packages.length} packages of ${ref.module}`)
    return { status: 'ok', json, summaries, packages }
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error)
    if (process.env.CI) throw new Error(`api: ${reason}`)
    console.warn(`[api] skipped: ${reason}`)
    return { status: 'unavailable', reason }
  }
}

function gameapi(ctx: Context, args: string[]): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile('go', ['run', './cmd/gameapi', ...args], { cwd: ctx.goModule, maxBuffer: 256 << 20 }, (error, stdout, stderr) => {
      if (error) reject(new Error(`go run ./cmd/gameapi ${args.join(' ')}: ${stderr.trim() || error.message}`))
      else resolve(stdout)
    })
  })
}

// virtual:api lists the packages and imports each one's page module on demand.
function loadApiModule(build: ApiBuild): string {
  if (build.status !== 'ok') return `export const api = ${JSON.stringify({ status: 'unavailable', reason: build.reason })}\nexport const loadApiPackage = async () => undefined`
  const loaders = build.summaries.map((s) => `  ${JSON.stringify(s.page)}: () => import(${JSON.stringify(API_PACKAGE + s.page)}),`).join('\n')
  return `export const api = ${JSON.stringify({ status: 'ok', packages: build.summaries })}
const loaders = {
${loaders}
}
export async function loadApiPackage(page) {
  const load = Object.hasOwn(loaders, page) ? loaders[page] : undefined
  return load ? (await load()).default : undefined
}`
}

// ---- The site's own pages ----

async function loadSiteModule(ctx: Context): Promise<string> {
  return `export const site = ${JSON.stringify(await loadSite({ docsDir: ctx.docsDir, examplesDir: ctx.examplesDir, packagesDir: path.dirname(ctx.goModule) }))}`
}

// ---- Snippets ----

async function loadSnippetsModule(ctx: Context): Promise<string> {
  const snippets: Snippet[] = []
  for (const id of SNIPPETS) {
    const code = await readFile(path.join(ctx.snippetsDir, `${id}.go.txt`), 'utf8')
    snippets.push({ id, html: await codeToHtml(code.trimEnd(), { lang: 'go', themes: THEMES, defaultColor: false }) })
  }
  return `export const snippets = ${JSON.stringify(snippets)}`
}

// ---- Playground ----

// playgroundFiles are the files an example's source tabs show: its main package's Go files, main.go first, without its tests, then its nsgo.yaml.
async function playgroundFiles(dir: string): Promise<string[]> {
  const entries = await readdir(dir, { withFileTypes: true })
  const names = entries.filter((e) => e.isFile()).map((e) => e.name)
  const goFiles = names.filter((n) => n.endsWith('.go') && !n.endsWith('_test.go')).sort((a, b) => Number(b === 'main.go') - Number(a === 'main.go') || a.localeCompare(b))
  if (goFiles.length === 0) throw new Error(`playground: ${dir} has no Go files`)
  return names.includes('nsgo.yaml') ? [...goFiles, 'nsgo.yaml'] : goFiles
}

const lineCount = (code: string) => code.split('\n').length

// loadPlaygroundModule lists each example's files and the size of the web build public/play/<id>/ holds. A deploy must not publish a playground that runs nothing, so in CI a missing build fails the site's build; a local build goes on and its page says how to make one.
async function loadPlaygroundModule(ctx: Context): Promise<string> {
  const examples: PlaygroundExample[] = []
  const missing: string[] = []
  for (const id of PLAYGROUND_IDS) {
    const dir = path.join(ctx.examplesDir, id)
    const files: PlaygroundFile[] = []
    for (const name of await playgroundFiles(dir)) files.push({ name, lines: lineCount((await readFile(path.join(dir, name), 'utf8')).trimEnd()) })
    const wasm = await playgroundWasmSize(path.join(ctx.publicDir, PLAYGROUND_BUILDS, id))
    if (wasm === 0) missing.push(id)
    examples.push({ id, files, built: wasm > 0, wasm })
  }
  if (missing.length) {
    const reason = `no web build of ${missing.join(', ')} in public/${PLAYGROUND_BUILDS}/; run bun run playground`
    if (process.env.CI) throw new Error(`playground: ${reason}`)
    console.warn(`[playground] ${reason}`)
  }
  const loaders = PLAYGROUND_IDS.map((id) => `${JSON.stringify(id)}: () => import(${JSON.stringify(PLAYGROUND_SOURCE + id)})`).join(', ')
  return `export const examples = ${JSON.stringify(examples)}\nexport const sources = { ${loaders} }`
}

// playgroundWasmSize is the size in bytes of the WebAssembly the web build in dir runs, as its nsgo.json names it, or 0 when dir holds no usable build.
async function playgroundWasmSize(dir: string): Promise<number> {
  let manifest: string
  try {
    manifest = await readFile(path.join(dir, 'nsgo.json'), 'utf8')
  } catch {
    return 0
  }
  // A build that is present but broken counts as missing, so a local build warns and goes on instead of dying, and CI still fails it through the missing-build check in loadPlaygroundModule.
  try {
    const { wasm } = JSON.parse(manifest) as { wasm?: unknown }
    if (typeof wasm !== 'string' || wasm === '') throw new Error('nsgo.json names no wasm')
    return (await stat(path.join(dir, wasm))).size
  } catch (err) {
    console.warn(`[playground] ignoring the build in ${dir}: ${err instanceof Error ? err.message : String(err)}`)
    return 0
  }
}

async function loadPlaygroundSource(ctx: Context, id: PlaygroundId): Promise<string> {
  const dir = path.join(ctx.examplesDir, id)
  const source: PlaygroundSource = { files: [] }
  for (const name of await playgroundFiles(dir)) {
    const code = (await readFile(path.join(dir, name), 'utf8')).trimEnd()
    const lang = name.endsWith('.go') ? 'go' : 'yaml'
    source.files.push({ name, lines: lineCount(code), html: await codeToHtml(code, { lang, themes: THEMES, defaultColor: false }) })
  }
  return `export const files = ${JSON.stringify(source.files)}`
}
