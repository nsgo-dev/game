// The English docs as Markdown for language models, as nsgo.dev publishes its own: llms.txt lists every page by the sidebar's groups with a link and a line about it, and llms-full.txt holds every page in full. src/pages/[file].txt.ts serves both beside the site's pages.

import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import remarkGfm from 'remark-gfm'
import remarkParse from 'remark-parse'
import { unified } from 'unified'

import { groupDocs } from '../content/groups.ts'
import { copies } from '../copy.ts'
import { CHANGELOG_URL, GITHUB_URL, LLMS_FULL, LLMS_INDEX, NSGO_LLMS_URL } from '../links.ts'
import { absoluteLink, pageUrl } from './docs.ts'

// LlmsPage is a page of docs/en as llms.txt and llms-full.txt give it.
export interface LlmsPage {
  slug: string
  title: string
  // description is the page's first paragraph as plain text on one line, as the page's description meta tag has it.
  description: string
  // url is the page's absolute address on the site.
  url: string
  // markdown is the page's Markdown with its links made absolute: other pages' to the site, the repository's files to GitHub.
  markdown: string
}

// loadPages reads every Markdown file of dir, in the docs' order, as pages of the site at site.
export async function loadPages(dir: string, site: string): Promise<LlmsPage[]> {
  const pages: LlmsPage[] = []
  const files = (await readdir(dir)).filter((f) => f.endsWith('.md')).sort()
  for (const file of files) {
    const slug = file.slice(0, -'.md'.length)
    pages.push(llmsPage(slug, await readFile(path.join(dir, file), 'utf8'), site))
  }
  return pages
}

// MdNode is the part of an mdast node llmsPage reads.
interface MdNode {
  type: string
  children?: MdNode[]
  url?: string
  depth?: number
  value?: string
  label?: string
  position?: { start: { offset?: number }; end: { offset?: number } }
}

// llmsPage takes a page's title from its first heading and its description from its first paragraph, as the site's docs pages do, and rewrites its links as the site does, but absolute below site.
export function llmsPage(slug: string, source: string, site: string): LlmsPage {
  const tree = unified().use(remarkParse).use(remarkGfm).parse(source) as MdNode
  const children = tree.children ?? []
  const h1 = children.find((n) => n.type === 'heading' && n.depth === 1)
  const paragraph = children.find((n) => n.type === 'paragraph')
  const edits: { start: number; end: number; text: string }[] = []
  const visit = (node: MdNode) => {
    const start = node.position?.start.offset
    const end = node.position?.end.offset
    if ((node.type === 'link' || node.type === 'definition') && node.url && start !== undefined && end !== undefined) {
      // A link to a heading of the same page has no page to rewrite, so it points at the page's own address.
      const url = absoluteLink(node.url, slug, site)
      // The destination is the URL's first occurrence after the link's text or the definition's label, so neither that text nor a title after the destination is taken for it.
      const after = destinationSearchStart(node, start)
      const at = source.indexOf(node.url, after)
      if (url !== node.url && at >= 0 && at + node.url.length <= end) edits.push({ start: at, end: at + node.url.length, text: url })
    }
    for (const child of node.children ?? []) visit(child)
  }
  visit(tree)
  let markdown = source
  for (const edit of edits.sort((a, b) => b.start - a.start)) markdown = markdown.slice(0, edit.start) + edit.text + markdown.slice(edit.end)
  return {
    slug,
    title: h1 ? oneLine(textOf(h1)) : slug,
    description: paragraph ? oneLine(textOf(paragraph)) : '',
    url: pageUrl(slug, site),
    markdown: markdown.replace(/\r\n/g, '\n').trim(),
  }
}

// buildLlms writes llms.txt and llms-full.txt for pages, and fails if either leaves a page out, so a build cannot publish a list that misses one.
export function buildLlms(pages: readonly LlmsPage[], site: string): { index: string; full: string } {
  if (pages.length === 0) throw new Error('llms.txt: docs/en has no pages')
  const copy = copies.en
  const sections = groupDocs(pages).map(({ key, items }) => {
    const lines = items.map((page) => `- [${page.title}](${page.url})${page.description ? `: ${page.description}` : ''}`)
    return `## ${copy.docs.groups[key]}\n\n${lines.join('\n')}`
  })
  const index = `${[
    '# NSGo Game',
    `> ${copy.site.description}`,
    `NSGo Game is early and its APIs will change: read the packages' source, \`go list -m -f '{{.Dir}}' github.com/nsgo-dev/game/packages/game\`, before relying on a signature. Each page below is a page of the site; ${site}${LLMS_FULL} holds their Markdown in full.`,
    ...sections,
    [
      '## Optional',
      '',
      `- [NSGo for language models](${NSGO_LLMS_URL}): the docs of the UI engine the game layer runs on`,
      `- [Roadmap](${site}roadmap/): what is planned and what is done, by stage`,
      `- [Changelog](${CHANGELOG_URL})`,
      `- [Source](${GITHUB_URL})`,
    ].join('\n'),
  ].join('\n\n')}\n`
  const full = `# NSGo Game documentation\n\n${pages.map((page) => `---\n\nSource: ${page.url}\n\n${page.markdown}`).join('\n\n')}\n`
  for (const page of pages) {
    if (!index.includes(`](${page.url})`)) throw new Error(`${LLMS_INDEX} leaves out docs/en/${page.slug}.md`)
    if (!full.includes(`\nSource: ${page.url}\n`)) throw new Error(`${LLMS_FULL} leaves out docs/en/${page.slug}.md`)
  }
  return { index, full }
}

// destinationSearchStart is the offset in the source after which a link's or definition's destination begins: past a link's last child, or past a definition's `[label]`.
function destinationSearchStart(node: MdNode, start: number): number {
  if (node.type === 'definition') return start + 1 + (node.label ?? '').length
  const last = node.children?.at(-1)?.position?.end.offset
  return last ?? start
}

function textOf(node: MdNode): string {
  if (node.value !== undefined) return node.value
  return (node.children ?? []).map(textOf).join('')
}

function oneLine(text: string): string {
  return text.replace(/\s+/g, ' ').trim()
}
