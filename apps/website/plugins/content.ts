// Build-time content: the docs from the repository's Markdown, the home page's Go snippets, and the roadmap from GitHub's API. Each is a virtual module the routes import, so the pages carry the result and nothing is read or fetched in the browser.

import { readdir, readFile } from 'node:fs/promises'
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
import type { DocHeading, DocPage, Roadmap, RoadmapGroup, RoadmapIssue, RoadmapStage, Snippet } from '../src/content/types'

const REPO = 'nsgo-dev/game'
const PLAN = { repo: 'nsgo-dev/nsgo', number: 1047 }
const SNIPPETS: Snippet['id'][] = ['loop', 'sprite', 'sound']
const THEMES = { light: 'github-light', dark: 'github-dark' } as const

const modules = {
  'virtual:docs': loadDocsModule,
  'virtual:snippets': loadSnippetsModule,
  'virtual:roadmap': loadRoadmapModule,
} as const
type ModuleId = keyof typeof modules

interface Context {
  root: string
  base: string
  docsDir: string
  snippetsDir: string
}

export function siteContent(): Plugin {
  let ctx: Context | undefined
  // The client and server builds both load each module; the roadmap is fetched once for both.
  const cache = new Map<ModuleId, Promise<string>>()
  return {
    name: 'game-site-content',
    configResolved(config) {
      ctx = {
        root: config.root,
        base: config.base,
        docsDir: path.resolve(config.root, '../../docs'),
        snippetsDir: path.resolve(config.root, 'snippets'),
      }
    },
    resolveId(id) {
      return id in modules ? `\0${id}` : undefined
    },
    async load(id) {
      if (!id.startsWith('\0')) return undefined
      const name = id.slice(1)
      if (!(name in modules) || !ctx) return undefined
      const moduleId = name as ModuleId
      if (moduleId === 'virtual:docs') {
        for (const file of await markdownFiles(ctx.docsDir)) this.addWatchFile(path.join(ctx.docsDir, file))
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

async function markdownFiles(dir: string): Promise<string[]> {
  const files = (await readdir(dir)).filter((f) => f.endsWith('.md'))
  // index first, the rest by name.
  return files.sort((a, b) => (a === 'index.md' ? -1 : b === 'index.md' ? 1 : a.localeCompare(b)))
}

async function loadDocsModule(ctx: Context): Promise<string> {
  const pages: DocPage[] = []
  for (const file of await markdownFiles(ctx.docsDir)) {
    const source = await readFile(path.join(ctx.docsDir, file), 'utf8')
    pages.push(await renderDoc(file.slice(0, -'.md'.length), source, ctx))
  }
  return `export const docs = ${JSON.stringify(pages)}`
}

// MdNode is the part of an mdast node this file reads and writes.
interface MdNode {
  type: string
  children?: MdNode[]
  url?: string
  depth?: number
  value?: string
}

async function renderDoc(slug: string, source: string, ctx: Context): Promise<DocPage> {
  const meta = { title: slug, description: '' }
  const headings: DocHeading[] = []
  const file = await unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(docTransform(meta, ctx) as UnifiedPlugin)
    .use(remarkRehype)
    .use(rehypeSlug)
    .use(collectHeadings(headings) as UnifiedPlugin)
    .use(rehypeShiki, { themes: THEMES, defaultColor: false })
    .use(rehypeStringify)
    .process(source)
  return { slug, title: meta.title, description: meta.description, html: String(file), headings }
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

// docTransform takes the first heading out as the page's title and its first paragraph as its description, and points the Markdown's links at the site's docs routes or at the files on GitHub.
function docTransform(meta: { title: string; description: string }, ctx: Context) {
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
      if ((node.type === 'link' || node.type === 'definition') && node.url) node.url = rewriteLink(node.url, ctx)
    })
  }
}

function rewriteLink(url: string, ctx: Context): string {
  if (/^[a-z][a-z0-9+.-]*:/i.test(url) || url.startsWith('#') || url.startsWith('//')) return url
  const [target = '', hash = ''] = url.split(/(?=#)/)
  if (target.endsWith('.md') && !target.includes('/')) {
    const slug = target.slice(0, -'.md'.length)
    return `${ctx.base}docs/${slug === 'index' ? '' : `${slug}/`}${hash}`
  }
  // Anything else is a file or directory of the repository, relative to docs/.
  const repoPath = path.posix.normalize(path.posix.join('docs', target))
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

// ---- Snippets ----

async function loadSnippetsModule(ctx: Context): Promise<string> {
  const snippets: Snippet[] = []
  for (const id of SNIPPETS) {
    const code = await readFile(path.join(ctx.snippetsDir, `${id}.go.txt`), 'utf8')
    snippets.push({ id, html: await codeToHtml(code.trimEnd(), { lang: 'go', themes: THEMES, defaultColor: false }) })
  }
  return `export const snippets = ${JSON.stringify(snippets)}`
}

// ---- Roadmap ----

interface ApiIssue {
  number: number
  title: string
  state: string
  html_url: string
  repository_url: string
  pull_request?: unknown
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
  const [gameIssues, planIssues] = await Promise.all([
    fetchAll(`/repos/${REPO}/issues?state=all`),
    fetchAll(`/repos/${PLAN.repo}/issues/${PLAN.number}/sub_issues`),
  ])
  const issues = new Map<string, RoadmapIssue>()
  for (const raw of [...gameIssues, ...planIssues]) {
    if (raw.pull_request) continue
    const repo = raw.repository_url.split('/repos/')[1] ?? REPO
    issues.set(`${repo}#${raw.number}`, {
      repo,
      number: raw.number,
      title: raw.title,
      state: raw.state === 'closed' ? 'closed' : 'open',
      url: raw.html_url,
    })
  }
  const stages: RoadmapStage[] = [
    { id: '2d', title: '2D', groups: [], open: 0, closed: 0 },
    { id: '3d', title: '3D', groups: [], open: 0, closed: 0 },
  ]
  const groups = new Map<string, RoadmapGroup>()
  for (const issue of issues.values()) {
    const { area, stage } = classify(issue)
    const target = stages.find((s) => s.id === stage)!
    target[issue.state]++
    const key = `${stage}/${area}`
    let group = groups.get(key)
    if (!group) {
      group = { area, issues: [] }
      groups.set(key, group)
      target.groups.push(group)
    }
    group.issues.push(issue)
  }
  for (const stage of stages) {
    for (const group of stage.groups) group.issues.sort((a, b) => a.number - b.number)
    // The game's areas in the order their first issue was filed, then NSGo's.
    stage.groups.sort((a, b) => rank(a) - rank(b))
  }
  return { status: 'ok', fetchedAt: new Date().toISOString(), stages }
}

const ENGINE_AREA = 'NSGo'

// classify puts a game issue in the area its title starts with ("Input: actions from keys" is in Input) and a stage by that area; NSGo's issues are one area, in 3D when about 3D or the GPU view.
function classify(issue: RoadmapIssue): { area: string; stage: RoadmapStage['id'] } {
  if (issue.repo !== REPO) {
    return { area: ENGINE_AREA, stage: /\b3D\b|\bGPU view\b/.test(issue.title) ? '3d' : '2d' }
  }
  const colon = issue.title.indexOf(':')
  const area = colon > 0 ? (issue.title.slice(0, colon).split(' ')[0] ?? 'Other') : 'Other'
  return { area, stage: area === '3D' ? '3d' : '2d' }
}

function rank(group: RoadmapGroup): number {
  const first = Math.min(...group.issues.map((i) => i.number))
  if (group.area === ENGINE_AREA) return Number.MAX_SAFE_INTEGER - 1
  if (group.area === 'Other') return Number.MAX_SAFE_INTEGER
  return first
}

async function fetchAll(endpoint: string): Promise<ApiIssue[]> {
  const headers: Record<string, string> = {
    Accept: 'application/vnd.github+json',
    'X-GitHub-Api-Version': '2022-11-28',
    'User-Agent': 'game-website-build',
  }
  const token = process.env.GITHUB_TOKEN
  if (token) headers.Authorization = `Bearer ${token}`
  const all: ApiIssue[] = []
  for (let page = 1; ; page++) {
    const sep = endpoint.includes('?') ? '&' : '?'
    const res = await fetch(`https://api.github.com${endpoint}${sep}per_page=100&page=${page}`, {
      headers,
      signal: AbortSignal.timeout(20_000),
    })
    if (!res.ok) {
      const hint = res.status === 403 || res.status === 429 ? ' (rate limited; set GITHUB_TOKEN)' : ''
      throw new Error(`GET ${endpoint} answered ${res.status} ${res.statusText}${hint}`)
    }
    const batch = (await res.json()) as ApiIssue[]
    all.push(...batch)
    if (batch.length < 100) return all
  }
}
