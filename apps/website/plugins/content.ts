// Build-time content: the docs from the repository's Markdown in each language, the API reference from the Go packages' doc comments, the home page's Go snippets, the playground's example sources, and the roadmap from GitHub's API. Each is a virtual module the routes import, so the pages carry the result and nothing is read from the repository or fetched from GitHub in the browser.

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
import type { ApiPackage, ApiReference, ApiSummary, DocHeading, DocPage, PlaygroundExample, PlaygroundFile, PlaygroundSource, Roadmap, RoadmapGroup, RoadmapIssue, RoadmapStage, Snippet } from '../src/content/types'
import type { Lang } from '../src/i18n'

const REPO = 'nsgo-dev/game'
const PLAN = { repo: 'nsgo-dev/nsgo', number: 1047 }
const SNIPPETS: Snippet['id'][] = ['loop', 'sprite', 'sound']
const THEMES = { light: 'github-light', dark: 'github-dark' } as const
// The docs' languages, each a directory of docs/: English has every page, and a page another language lacks shows the English one.
const LANGS = ['en', 'zh'] as const satisfies readonly Lang[]

const modules = {
  'virtual:docs': loadDocsModule,
  'virtual:snippets': loadSnippetsModule,
  'virtual:roadmap': loadRoadmapModule,
  'virtual:playground': loadPlaygroundModule,
} as const
// Each playground example's sources are a module of their own, virtual:playground/<id>, so a page loads only its example's.
const PLAYGROUND_SOURCE = 'virtual:playground/'
type ModuleId = keyof typeof modules

// A package's page of the API reference is the module virtual:api/<page>, which the reference's route imports on demand, so no page's script carries another package's API.
const API_MODULE = 'virtual:api'
const API_PACKAGE = `${API_MODULE}/`

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
  // The client and server builds both load each module; the roadmap is fetched, and the API reference generated, once for both.
  const cache = new Map<ModuleId, Promise<string>>()
  let api: Promise<ApiBuild> | undefined
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
    resolveId(id) {
      if (id.startsWith(PLAYGROUND_SOURCE) && isPlaygroundId(id.slice(PLAYGROUND_SOURCE.length))) return `\0${id}`
      if (id === API_MODULE || id.startsWith(API_PACKAGE)) return `\0${id}`
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
      if (!(name in modules) || !ctx) return undefined
      const moduleId = name as ModuleId
      if (moduleId === 'virtual:docs') {
        for (const lang of LANGS) {
          const dir = path.join(ctx.docsDir, lang)
          for (const file of await markdownFiles(dir)) this.addWatchFile(path.join(dir, file))
        }
      }
      if (moduleId === 'virtual:playground') {
        // The line counts follow the sources, and a build made while the dev server runs shows up once its nsgo.json is written.
        for (const id of PLAYGROUND_IDS) {
          const dir = path.join(ctx.examplesDir, id)
          for (const file of await playgroundFiles(dir)) this.addWatchFile(path.join(dir, file))
          this.addWatchFile(path.join(ctx.publicDir, PLAYGROUND_BUILDS, id, 'nsgo.json'))
        }
      }
      if (moduleId === 'virtual:snippets') {
        for (const snippet of SNIPPETS) this.addWatchFile(path.join(ctx.snippetsDir, `${snippet}.go.txt`))
      }
      let code = cache.get(moduleId)
      if (!code || moduleId !== 'virtual:roadmap') {
        code = modules[moduleId](ctx)
        cache.set(moduleId, code)
      }
      return code
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
  'loop.md',
  'sprite.md',
  'sound.md',
  '3d-models-blender.md',
  'shipping.md',
  'cli.md',
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

// Each language lists English's pages in English's order, its own translation where it has one.
async function loadDocsModule(ctx: Context): Promise<string> {
  const english = await markdownFiles(path.join(ctx.docsDir, 'en'))
  const docs = {} as Record<Lang, DocPage[]>
  for (const lang of LANGS) {
    const own = new Set(await markdownFiles(path.join(ctx.docsDir, lang)))
    docs[lang] = []
    for (const file of english) {
      const from: Lang = own.has(file) ? lang : 'en'
      const source = await readFile(path.join(ctx.docsDir, from, file), 'utf8')
      docs[lang].push(await renderDoc(file.slice(0, -'.md'.length), source, { lang, from }, ctx))
    }
  }
  return `export const docs = ${JSON.stringify(docs)}`
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
async function renderDoc(slug: string, source: string, { lang, from }: { lang: Lang; from: Lang }, ctx: Context): Promise<DocPage> {
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

// ---- Roadmap ----

// The roadmap is the issue tree under the plan, read with GitHub's GraphQL API: the plan's sub-issues are its stages, a stage's sub-issues its areas, and everything below an area is listed under it. A level of the tree is one request for up to 100 issues, so a build takes a handful of requests rather than one per issue. GraphQL needs a token; without GITHUB_TOKEN a local build shows the roadmap as unavailable.

const GRAPHQL_URL = 'https://api.github.com/graphql'
// Up to 100 issues a request asks for the sub-issues of, and up to 100 sub-issues for each: 10,000 nodes, well inside GitHub's limit of 500,000.
const PAGE = 100

const ISSUE_FIELDS = `fragment IssueFields on Issue { id number title state url repository { nameWithOwner } subIssuesSummary { total } }`
const SUB_ISSUES = `subIssues(first: ${PAGE}) { pageInfo { hasNextPage endCursor } nodes { ...IssueFields } }`

interface GqlPageInfo {
  hasNextPage: boolean
  endCursor: string | null
}

interface GqlIssue {
  id: string
  number: number
  title: string
  state: 'OPEN' | 'CLOSED'
  url: string
  repository: { nameWithOwner: string }
  subIssuesSummary: { total: number }
}

interface GqlConnection<T> {
  pageInfo: GqlPageInfo
  nodes: T[]
}

type GqlStage = GqlIssue & { body: string; subIssues: GqlConnection<GqlIssue> }

// TreeNode is an issue of the plan's tree with the sub-issues read so far.
interface TreeNode {
  id: string
  issue: RoadmapIssue
  // total is how many sub-issues GitHub says the issue has.
  total: number
  children: TreeNode[]
}

async function loadRoadmapModule(): Promise<string> {
  let roadmap: Roadmap
  try {
    roadmap = await fetchRoadmap()
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error)
    // A deploy must not replace the roadmap with an empty one; a local build goes on without it.
    if (process.env.CI) throw new Error(`roadmap: ${reason}`)
    console.warn(`[roadmap] skipped: ${reason}`)
    roadmap = { status: 'unavailable', reason }
  }
  return `export const roadmap = ${JSON.stringify(roadmap)}`
}

async function fetchRoadmap(): Promise<Roadmap> {
  const token = process.env.GITHUB_TOKEN
  if (!token) throw new Error('GITHUB_TOKEN is not set, and GitHub’s GraphQL API needs a token')
  const client = new GraphQL(token)
  const [stages, gameIssues] = await Promise.all([fetchTree(client), fetchGameIssues(client)])
  const planned = new Set<string>()
  const roadmapStages = stages.map((stage, index) => {
    const built = buildStage(stage, index)
    for (const node of descendants(stage.node)) planned.add(issueKey(node.issue))
    return built
  })
  const unplanned = gameIssues.filter((issue) => !planned.has(issueKey(issue))).sort((a, b) => a.number - b.number)
  console.info(`[roadmap] ${client.requests} GraphQL requests; ${roadmapStages.map((s) => `stage ${s.number} ${s.closed}/${s.open + s.closed}`).join(', ')}; ${unplanned.length} unplanned`)
  return { status: 'ok', fetchedAt: new Date().toISOString(), stages: roadmapStages, unplanned }
}

// fetchTree reads the plan's stages with their bodies and two levels below them in one request, then each further level, a request per 100 issues that have sub-issues.
async function fetchTree(client: GraphQL): Promise<{ node: TreeNode; body: string }[]> {
  const data = await client.query<{ repository: { issue: { subIssues: GqlConnection<GqlStage> } | null } }>(
    `query($owner: String!, $name: String!, $number: Int!) {
      repository(owner: $owner, name: $name) {
        issue(number: $number) {
          subIssues(first: ${PAGE}) { pageInfo { hasNextPage endCursor } nodes { ...IssueFields body ${SUB_ISSUES} } }
        }
      }
    }
    ${ISSUE_FIELDS}`,
    { owner: PLAN.repo.split('/')[0], name: PLAN.repo.split('/')[1], number: PLAN.number },
  )
  const plan = data.repository.issue
  if (!plan) throw new Error(`${PLAN.repo}#${PLAN.number} was not found`)
  if (plan.subIssues.pageInfo.hasNextPage) throw new Error(`${PLAN.repo}#${PLAN.number} has more than ${PAGE} stages`)
  // Issues whose sub-issues are still to be read, and those with more pages of them.
  let pending: TreeNode[] = []
  const more: { node: TreeNode; after: string }[] = []
  const attach = (node: TreeNode, connection: GqlConnection<GqlIssue>) => {
    for (const raw of connection.nodes) {
      const child = treeNode(raw)
      node.children.push(child)
      if (child.total > 0) pending.push(child)
    }
    if (connection.pageInfo.hasNextPage && connection.pageInfo.endCursor) more.push({ node, after: connection.pageInfo.endCursor })
  }
  const stages = plan.subIssues.nodes.map((raw) => {
    const node = treeNode(raw)
    attach(node, raw.subIssues)
    return { node, body: raw.body }
  })
  while (pending.length || more.length) {
    const level = pending
    pending = []
    for (let i = 0; i < level.length; i += PAGE) {
      const batch = level.slice(i, i + PAGE)
      const result = await client.query<{ nodes: ({ id: string; subIssues: GqlConnection<GqlIssue> } | null)[] }>(
        `query($ids: [ID!]!) { nodes(ids: $ids) { ... on Issue { id ${SUB_ISSUES} } } }
        ${ISSUE_FIELDS}`,
        { ids: batch.map((node) => node.id) },
      )
      result.nodes.forEach((raw, j) => {
        if (raw) attach(batch[j]!, raw.subIssues)
      })
    }
    // An issue with more than 100 sub-issues reads the rest a page at a time.
    for (const { node, after } of more.splice(0)) {
      const result = await client.query<{ node: { subIssues: GqlConnection<GqlIssue> } | null }>(
        `query($id: ID!, $after: String!) { node(id: $id) { ... on Issue { subIssues(first: ${PAGE}, after: $after) { pageInfo { hasNextPage endCursor } nodes { ...IssueFields } } } } }
        ${ISSUE_FIELDS}`,
        { id: node.id, after },
      )
      if (result.node) attach(node, result.node.subIssues)
    }
  }
  return stages
}

// fetchGameIssues reads every issue of the game's repository, to list those in no stage.
async function fetchGameIssues(client: GraphQL): Promise<RoadmapIssue[]> {
  const [owner, name] = REPO.split('/')
  const issues: RoadmapIssue[] = []
  let after: string | null = null
  do {
    const data: { repository: { issues: GqlConnection<Omit<GqlIssue, 'id' | 'subIssuesSummary'>> } } = await client.query(
      `query($owner: String!, $name: String!, $after: String) {
        repository(owner: $owner, name: $name) {
          issues(first: ${PAGE}, after: $after, orderBy: { field: CREATED_AT, direction: ASC }) {
            pageInfo { hasNextPage endCursor }
            nodes { number title state url repository { nameWithOwner } }
          }
        }
      }`,
      { owner, name, after },
    )
    const page = data.repository.issues
    for (const raw of page.nodes) issues.push(toIssue(raw))
    after = page.pageInfo.hasNextPage ? page.pageInfo.endCursor : null
  } while (after)
  return issues
}

function treeNode(raw: GqlIssue): TreeNode {
  return { id: raw.id, issue: toIssue(raw), total: raw.subIssuesSummary.total, children: [] }
}

function toIssue(raw: Pick<GqlIssue, 'number' | 'title' | 'state' | 'url' | 'repository'>): RoadmapIssue {
  return { repo: raw.repository.nameWithOwner, number: raw.number, title: raw.title, state: raw.state === 'CLOSED' ? 'closed' : 'open', url: raw.url }
}

function issueKey(issue: RoadmapIssue): string {
  return `${issue.repo}#${issue.number}`
}

// descendants lists the issues below node, depth first in GitHub's order.
function descendants(node: TreeNode): TreeNode[] {
  return node.children.flatMap((child) => [child, ...descendants(child)])
}

const STAGE_TITLE = /^Games stage (\d+):\s*(.+)$/i

// buildStage turns a stage's subtree into areas: each sub-issue with sub-issues of its own is an area listing everything below it, and the rest share one group after them.
function buildStage({ node, body }: { node: TreeNode; body: string }, index: number): RoadmapStage {
  const match = STAGE_TITLE.exec(node.issue.title)
  const title = match ? match[2]! : node.issue.title
  const stage: RoadmapStage = {
    number: match ? Number(match[1]) : index + 1,
    title: title.charAt(0).toUpperCase() + title.slice(1),
    description: stageDescription(body),
    url: node.issue.url,
    groups: [],
    open: 0,
    closed: 0,
  }
  const single: RoadmapGroup = { epic: null, area: '', items: [], open: 0, closed: 0 }
  for (const child of node.children) {
    if (child.children.length === 0) {
      single.items.push({ ...child.issue, depth: 0 })
      single[child.issue.state]++
      continue
    }
    const group: RoadmapGroup = { epic: child.issue, area: areaName(child.issue.title), items: [], open: 0, closed: 0 }
    group[child.issue.state]++
    const visit = (parent: TreeNode, depth: 0 | 1) => {
      for (const item of parent.children) {
        group.items.push({ ...item.issue, depth })
        group[item.issue.state]++
        visit(item, 1)
      }
    }
    visit(child, 0)
    stage.groups.push(group)
  }
  if (single.items.length) stage.groups.push(single)
  for (const group of stage.groups) {
    stage.open += group.open
    stage.closed += group.closed
  }
  return stage
}

// areaName is an epic's title up to its colon, without a leading tag such as "[Feature]": "Input: named actions…" is Input.
function areaName(title: string): string {
  const untagged = title.replace(/^\[[^\]]*\]\s*/, '')
  const colon = untagged.indexOf(':')
  return colon > 0 ? untagged.slice(0, colon) : untagged
}

// stageDescription is the first paragraph of the stage issue's Context section, or of its body, without the sentence naming the plan it belongs to.
function stageDescription(body: string): string {
  const text = body.replace(/\r\n/g, '\n')
  const context = /^#{1,6}\s*Context\s*\n([\s\S]*?)(?=^#{1,6}\s|$(?![\s\S]))/im.exec(text)
  const paragraphs = (context ? context[1]! : text).split(/\n\s*\n/).map((p) => p.trim().replace(/\s*\n\s*/g, ' '))
  const first = paragraphs.find((p) => p && !p.startsWith('#')) ?? ''
  return first
    .replace(/^A stage of [\w/-]*#\d+\.\s*/i, '')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
}

class GraphQL {
  requests = 0
  constructor(private readonly token: string) {}

  async query<T>(query: string, variables: Record<string, unknown>): Promise<T> {
    this.requests++
    const res = await fetch(GRAPHQL_URL, {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${this.token}`,
        'Content-Type': 'application/json',
        'User-Agent': 'game-website-build',
      },
      body: JSON.stringify({ query, variables }),
      signal: AbortSignal.timeout(30_000),
    })
    if (!res.ok) {
      const hint = res.status === 403 || res.status === 429 ? ' (rate limited)' : ''
      throw new Error(`GraphQL answered ${res.status} ${res.statusText}${hint}`)
    }
    const json = (await res.json()) as { data?: T; errors?: { message: string }[] }
    if (json.errors?.length) throw new Error(`GraphQL: ${json.errors.map((e) => e.message).join('; ')}`)
    if (!json.data) throw new Error('GraphQL answered without data')
    return json.data
  }
}
