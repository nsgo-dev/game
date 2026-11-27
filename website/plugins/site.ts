// Build-time content of the site's own pages, read from the repository: the package catalog from docs/<lang>/stability.md, docs/<lang>/index.md and the guides, the tutorial's chapters from docs/<lang>/tutorial.md, and the packages each featured example imports. Traditional Chinese is converted from Simplified Chinese with OpenCC; languages without docs show English.

import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import * as OpenCC from 'opencc-js'
import { FEATURED_IDS, type FeaturedId } from '../src/content/featured.ts'
import type { CatalogPackage, DocsLang, PackageCategory, SiteContent, TutorialChapter, TutorialContent, TutorialPart } from '../src/content/types'

// toTraditional converts Simplified Chinese to Traditional Chinese as Taiwan writes it, phrases included (OpenCC's s2twp).
export const toTraditional: (text: string) => string = OpenCC.Converter({ from: 'cn', to: 'twp' })

const MODULE = 'github.com/nsgo-dev/'

// A row of the package table in docs/<lang>/stability.md: the package, its status, and the page that is its guide, if any. A test in internal/repocheck keeps the table complete and the Chinese one equal to it.
const STABILITY_ROW = /^\| `([^`]+)` \| (Stable|Experimental) \| (?:\[[^\]]+\]\(([a-z0-9-]+)\.md\))? ?\|$/

export interface StabilityRow {
  // path is relative to github.com/nsgo-dev, such as game or game/pkg/sprite.
  path: string
  stable: boolean
  guide?: string
}

export function parseStability(markdown: string): StabilityRow[] {
  const rows: StabilityRow[] = []
  for (const line of markdown.split('\n')) {
    const m = STABILITY_ROW.exec(line)
    if (!m) continue
    rows.push({ path: m[1]!, stable: m[2] === 'Stable', ...(m[3] ? { guide: m[3] } : {}) })
  }
  return rows
}

// plainText drops Markdown links, emphasis and code marks from a line of a table or a paragraph.
export function plainText(markdown: string): string {
  return markdown
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/`([^`]*)`/g, '$1')
    .replace(/\s+/g, ' ')
    .trim()
}

// cells splits a Markdown table row into its trimmed cells.
function cells(line: string): string[] {
  return line
    .trim()
    .replace(/^\||\|$/g, '')
    .split('|')
    .map((c) => c.trim())
}

// parseIndexTable reads the package table of docs/<lang>/index.md: each package's import path relative to github.com/nsgo-dev, such as game/pkg/sprite, and what it holds.
export function parseIndexTable(markdown: string): Map<string, string> {
  const out = new Map<string, string>()
  for (const line of markdown.split('\n')) {
    if (!line.startsWith('| `')) continue
    const [, importPath = '', , what = ''] = cells(line)
    const full = importPath.replace(/`/g, '')
    if (!full.startsWith(MODULE)) continue
    out.set(full.slice(MODULE.length), plainText(what))
  }
  return out
}

// firstSentence is the first sentence of a page's first paragraph of prose, after its title.
export function firstSentence(markdown: string): string {
  for (const block of markdown.split(/\n\s*\n/).slice(1)) {
    const p = block.trim()
    if (!p || /^(#|\||```|>|-|\*|<|\d+\.)/.test(p)) continue
    const text = plainText(p)
    const m = /^(.+?(?:[.!?](?=\s|$)|[。！？]))/.exec(text)
    return m ? m[1]! : text
  }
  return ''
}

const CORE = new Set('game sprite sound input tilemap scene physics screen project assets resource text placeholder math2d save random entities sceneformat loaderr datatable mempool spatial'.split(' '))
const UI = new Set('gameui lights2d postfx worldmap fog quality a11y l10n pixelfx'.split(' '))
const TOOLS = new Set('cmd tooling mcp replay statedump modding editor gametest importsettings jsonschema'.split(' '))
const ONLINE_CMDS = new Set(['accountsd', 'cloudsaved', 'leaderboardd'])
const THREE_D = new Set('math3d scene3d anim3d world3d voxel render3d physics3d gltf scenemodel ktx2'.split(' '))

// category sorts a package into the catalog's groups by its path: 3D, tooling, online services, graphics and UI, the core, and gameplay for the rest.
export function category(pkgPath: string): PackageCategory {
  const rest = pkgPath === 'game' ? 'game' : pkgPath.replace(/^game\/(?:pkg\/)?/, '')
  const top = rest.split('/')[0]!
  if (rest.endsWith('3d') || THREE_D.has(top)) return '3d'
  if (top === 'cmd' && ONLINE_CMDS.has(rest.split('/')[1] ?? '')) return 'online'
  if (TOOLS.has(top)) return 'tools'
  if (top === 'online') return 'online'
  if (UI.has(top)) return 'ui'
  if (CORE.has(top)) return 'core'
  return 'play'
}

export const CATEGORIES: readonly PackageCategory[] = ['core', 'play', 'ui', 'tools', 'online', '3d']

// parseTutorial reads docs/<lang>/tutorial.md: its title, the two paragraphs before the chapters, the chapter table and the files of "how the game is built", the section whose list names each file in bold code.
export function parseTutorial(markdown: string): TutorialContent {
  const blocks = markdown.split(/\n\s*\n/)
  const title = plainText(/^# (.+)$/m.exec(markdown)?.[1] ?? '')
  const intro = blocks
    .slice(1)
    .filter((b) => !b.trim().startsWith('#'))
    .slice(0, 2)
    .map((b) => plainText(b))
  const chapters: TutorialChapter[] = []
  for (const line of markdown.split('\n')) {
    const m = /^\| \[(\d+)\.\s*([^\]]+)\]\(([a-z0-9-]+)\.md\) \|/.exec(line)
    if (!m) continue
    const [, adds = '', pkgs = '', code = ''] = cells(line)
    chapters.push({ n: Number(m[1]), title: m[2]!.trim(), slug: m[3]!, adds: plainText(adds), packages: plainText(pkgs), dir: plainText(code) })
  }
  const sections = markdown.split(/^## /m).slice(1)
  const parts: TutorialPart[] = []
  let partsTitle = ''
  for (const section of sections) {
    const items = [...section.matchAll(/^- \*\*`([^`]+)`\*\*[,，]?\s*(.+)$/gm)]
    if (items.length === 0) continue
    partsTitle = section.split('\n')[0]!.trim()
    for (const item of items) parts.push({ file: item[1]!, text: plainText(item[2]!) })
    break
  }
  if (!title || chapters.length === 0) throw new Error('tutorial.md: no title or no chapter table')
  return { title, intro, chapters, partsTitle, parts }
}

// convertTutorial writes a Simplified Chinese tutorial's text in Traditional Chinese; file names and code stay as they are.
function convertTutorial(t: TutorialContent): TutorialContent {
  return {
    title: toTraditional(t.title),
    intro: t.intro.map(toTraditional),
    chapters: t.chapters.map((c) => ({ ...c, title: toTraditional(c.title), adds: toTraditional(c.adds), packages: toTraditional(c.packages) })),
    partsTitle: toTraditional(t.partsTitle),
    parts: t.parts.map((p) => ({ ...p, text: toTraditional(p.text) })),
  }
}

// importedPackages lists the game layer's packages the Go files of dir and its directories import, tests and tools/ left out, by their path below github.com/nsgo-dev, such as game/pkg/sprite.
export async function importedPackages(dir: string): Promise<string[]> {
  const found = new Set<string>()
  const visit = async (d: string) => {
    for (const entry of await readdir(d, { withFileTypes: true })) {
      if (entry.isDirectory()) {
        if (entry.name !== 'tools' && entry.name !== 'dist' && !entry.name.startsWith('.')) await visit(path.join(d, entry.name))
        continue
      }
      if (!entry.name.endsWith('.go') || entry.name.endsWith('_test.go')) continue
      const source = await readFile(path.join(d, entry.name), 'utf8')
      for (const m of source.matchAll(/"github\.com\/nsgo-dev\/(game(?:\/[a-z0-9/]+)?)"/g)) found.add(m[1]!)
    }
  }
  await visit(dir)
  return [...found]
}

// packageSynopsis is the first sentence of a Go package's doc comment without its "Package name" opening, for a package no page of the docs describes.
export async function packageSynopsis(dir: string): Promise<string> {
  let entries: string[]
  try {
    entries = (await readdir(dir)).filter((f) => f.endsWith('.go') && !f.endsWith('_test.go')).sort((a, b) => Number(b === 'doc.go') - Number(a === 'doc.go') || a.localeCompare(b))
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return ''
    throw error
  }
  for (const file of entries) {
    const lines = (await readFile(path.join(dir, file), 'utf8')).split('\n')
    const start = lines.findIndex((l) => /^\/\/ (Package|Command) \w+ /.test(l))
    if (start < 0) continue
    const comment: string[] = []
    for (const line of lines.slice(start)) {
      if (!line.startsWith('//') || line.trim() === '//') break
      comment.push(line.replace(/^\/\/ ?/, ''))
    }
    const text = plainText(comment.join(' ')).replace(/^(Package|Command) \w+ /, '')
    const sentence = /^(.+?\.)(\s|$)/.exec(text)?.[1] ?? text
    return sentence.charAt(0).toUpperCase() + sentence.slice(1)
  }
  return ''
}

export interface SiteDirs {
  docsDir: string
  examplesDir: string
  // repoDir is the repository root (module github.com/nsgo-dev/game).
  repoDir: string
}

// loadSite reads the content of the site's own pages from the repository.
export async function loadSite({ docsDir, examplesDir, repoDir }: SiteDirs): Promise<SiteContent> {
  const read = (lang: 'en' | 'zh', file: string) => readFile(path.join(docsDir, lang, file), 'utf8')
  const rows = parseStability(await read('en', 'stability.md'))
  if (rows.length === 0) throw new Error('docs/en/stability.md has no package rows')
  const index = { en: parseIndexTable(await read('en', 'index.md')), zh: parseIndexTable(await read('zh', 'index.md')) }
  const guides = new Map<string, string>()
  const guideSentence = async (lang: 'en' | 'zh', guide: string | undefined) => {
    if (!guide) return ''
    const key = `${lang}/${guide}`
    if (!guides.has(key)) {
      let sentence = ''
      try {
        sentence = firstSentence(await read(lang, `${guide}.md`))
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error
      }
      guides.set(key, sentence)
    }
    return guides.get(key)!
  }
  const packages: CatalogPackage[] = []
  for (const row of rows) {
    const pkgDir = row.path === 'game' ? repoDir : path.join(repoDir, row.path.slice('game/'.length))
    const en = index.en.get(row.path) || (await guideSentence('en', row.guide)) || (await packageSynopsis(pkgDir))
    const zh = index.zh.get(row.path) || (await guideSentence('zh', row.guide))
    packages.push({
      path: row.path,
      name: row.path === 'game' ? 'game' : row.path.replace(/^game\/(?:pkg\/)?/, ''),
      stable: row.stable,
      category: category(row.path),
      ...(row.guide ? { guide: row.guide } : {}),
      description: { en, zh, 'zh-tw': zh && toTraditional(zh) },
    })
  }
  const zhTutorial = parseTutorial(await read('zh', 'tutorial.md'))
  const tutorial: Record<DocsLang, TutorialContent> = { en: parseTutorial(await read('en', 'tutorial.md')), zh: zhTutorial, 'zh-tw': convertTutorial(zhTutorial) }
  const featured = {} as Record<FeaturedId, string[]>
  const rank = new Map(packages.map((p, i) => [p.path, CATEGORIES.indexOf(p.category) * 1000 + i]))
  for (const id of FEATURED_IDS) {
    const imported = await importedPackages(path.join(examplesDir, id))
    featured[id] = imported.filter((p) => rank.has(p)).sort((a, b) => rank.get(a)! - rank.get(b)!)
  }
  return { packages, tutorial, featured }
}
