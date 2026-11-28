// check-guide-links checks relative Markdown fragment links with the same Markdown parser and heading slugger the website uses. The test suite runs it over the repository as well as focused fixtures.

import { readdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import rehypeSlug from 'rehype-slug'
import remarkGfm from 'remark-gfm'
import remarkParse from 'remark-parse'
import remarkRehype from 'remark-rehype'
import { unified } from 'unified'

interface HastNode {
  type: string
  tagName?: string
  properties?: { id?: unknown; href?: unknown }
  children?: HastNode[]
  position?: { start?: { line?: number } }
}

interface Link {
  href: string
  line?: number
}

interface Markdown {
  headings: Set<string>
  links: Link[]
}

const processor = unified().use(remarkParse).use(remarkGfm).use(remarkRehype).use(rehypeSlug)
const headingTags = new Set(['h1', 'h2', 'h3', 'h4', 'h5', 'h6'])

export async function checkGuideLinks(root: string): Promise<string[]> {
  const sources = await sourceFiles(root)
  const cache = new Map<string, Promise<Markdown>>()
  const problems: string[] = []
  const parsed = (file: string) => {
    let result = cache.get(file)
    if (!result) {
      result = readFile(file, 'utf8').then(parseMarkdown)
      cache.set(file, result)
    }
    return result
  }

  for (const source of sources) {
    const sourceName = relativeName(root, source)
    for (const link of (await parsed(source)).links) {
      const target = markdownFragment(link.href)
      if (!target) continue
      let decodedPath: string
      let fragment: string
      try {
        decodedPath = decodeURIComponent(target.path)
        fragment = decodeURIComponent(target.fragment)
      } catch {
        problems.push(`${location(sourceName, link)}: ${link.href} has invalid percent encoding`)
        continue
      }
      const targetFile = path.resolve(path.dirname(source), decodedPath)
      const targetName = relativeName(root, targetFile)
      let targetMarkdown: Markdown
      try {
        targetMarkdown = await parsed(targetFile)
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === 'ENOENT') {
          problems.push(`${location(sourceName, link)}: ${link.href} targets missing file ${targetName}`)
          continue
        }
        throw error
      }
      if (!targetMarkdown.headings.has(fragment)) {
        problems.push(`${location(sourceName, link)}: ${decodedPath}#${fragment} has no matching heading in ${targetName}`)
      }
    }
  }
  return problems
}

async function parseMarkdown(source: string): Promise<Markdown> {
  const tree = (await processor.run(processor.parse(source))) as unknown as HastNode
  const result: Markdown = { headings: new Set(), links: [] }
  const visit = (node: HastNode) => {
    if (node.type !== 'element') {
      for (const child of node.children ?? []) visit(child)
      return
    }
    if (node.tagName && headingTags.has(node.tagName) && typeof node.properties?.id === 'string') result.headings.add(node.properties.id)
    if (node.tagName === 'a' && typeof node.properties?.href === 'string') {
      const line = node.position?.start?.line
      result.links.push({ href: node.properties.href, ...(line === undefined ? {} : { line }) })
    }
    for (const child of node.children ?? []) visit(child)
  }
  visit(tree)
  return result
}

function markdownFragment(href: string): { path: string; fragment: string } | undefined {
  if (href.startsWith('#') || href.startsWith('/') || href.startsWith('//') || /^[a-z][a-z0-9+.-]*:/i.test(href)) return undefined
  const hash = href.indexOf('#')
  if (hash < 0) return undefined
  const targetPath = href.slice(0, hash)
  const fragment = href.slice(hash + 1)
  if (!targetPath.endsWith('.md') || fragment === '') return undefined
  return { path: targetPath, fragment }
}

async function sourceFiles(root: string): Promise<string[]> {
  const files: string[] = []
  for (const lang of ['en', 'zh']) files.push(...(await markdownFiles(path.join(root, 'docs', lang), true, false)))
  files.push(...(await markdownFiles(path.join(root, 'skills'), true, false)))
  for (const entry of await directoryEntries(path.join(root, 'examples'))) {
    if (entry.isDirectory()) files.push(...(await markdownFiles(path.join(root, 'examples', entry.name), false, true)))
  }
  files.push(...(await markdownFiles(path.join(root, 'template'), true, true)))
  return files.sort()
}

async function markdownFiles(dir: string, recursive: boolean, readmesOnly: boolean): Promise<string[]> {
  const files: string[] = []
  for (const entry of await directoryEntries(dir)) {
    const file = path.join(dir, entry.name)
    if (recursive && entry.isDirectory()) files.push(...(await markdownFiles(file, true, readmesOnly)))
    if (entry.isFile() && (readmesOnly ? entry.name === 'README.md' : entry.name.endsWith('.md'))) files.push(file)
  }
  return files
}

async function directoryEntries(dir: string) {
  try {
    return await readdir(dir, { withFileTypes: true })
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === 'ENOENT') return []
    throw error
  }
}

function relativeName(root: string, file: string): string {
  return path.relative(root, file).split(path.sep).join('/')
}

function location(file: string, link: Link): string {
  return link.line === undefined ? file : `${file}:${link.line}`
}
