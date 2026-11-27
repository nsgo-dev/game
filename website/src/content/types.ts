// The shapes of the content the build reads from the repository (plugins/content.ts) and the routes render.

export interface DocPage {
  // slug is the file's name without .md; "index" is the docs' first page.
  slug: string
  title: string
  description: string
  // html is the page's Markdown rendered at build time, without its first heading, which is title.
  html: string
  // headings are the page's second- and third-level headings, in order, for its contents list.
  headings: DocHeading[]
  // fallback is true when the page's language has no translation of it, so it shows the English one.
  fallback: boolean
  // source is the Markdown's path in the repository, such as docs/zh/index.md, for the page's edit link.
  source: string
  // packages are the packages whose guide the page is, with their status, from the table in docs/en/stability.md.
  packages: DocPackage[]
}

// A page as the sidebar and the pager list it, without its content.
export interface DocSummary {
  slug: string
  title: string
  fallback: boolean
}

export type PackageStatus = 'stable' | 'experimental'

// A package and its stability status, such as game/pkg/sprite, stable.
export interface DocPackage {
  // path is relative to github.com/nsgo-dev, such as game/pkg/sprite.
  path: string
  status: PackageStatus
}

export interface DocHeading {
  depth: 2 | 3
  id: string
  text: string
}

export interface Snippet {
  id: 'game' | 'loop' | 'sprite' | 'sound'
  html: string
}

// A file of a playground example, as its source tab lists it.
export interface PlaygroundFile {
  // name is the file's path in the example's directory, such as game.go.
  name: string
  lines: number
}

// A playground example's files, highlighted for both themes; each example's is a module of its own, loaded by its page alone.
export interface PlaygroundSource {
  files: (PlaygroundFile & { html: string })[]
}

// What every playground page knows of an example without loading its sources: its files, and whether this build of the site holds its web build.
export interface PlaygroundExample {
  id: import('./playground').PlaygroundId
  files: PlaygroundFile[]
  built: boolean
  // wasm is the size in bytes of the build's WebAssembly, 0 without a build.
  wasm: number
}

// The API reference: what tools/gameapi writes from the Go packages' source and doc comments (internal/apiref's Reference, whose doc comments say more of each field), run with -html.

export interface ApiReference {
  version: number
  module: string
  packages: (ApiPackage & { doc: string })[]
}

// An exported identifier and its anchor on its package's page: Name, or Type.Name for a method, a field or an interface method.
export interface ApiIdent {
  name: string
  anchor: string
}

interface ApiDecl {
  // decl is the declaration's Go source, gofmt-formatted, without its doc comment.
  decl: string
  // docHTML is the doc comment rendered as HTML; its links to another package are relative to this package's page.
  docHTML?: string
  // file is the declaration's file below the repository root, and line its line.
  file: string
  line: number
  // platforms are the targets that declare it, absent when every target that builds the package does.
  platforms?: string[]
}

export interface ApiValue extends ApiDecl {
  idents: ApiIdent[]
}

export interface ApiFunc extends ApiDecl, ApiIdent {
  // recv is a method's receiver, such as *Batch.
  recv?: string
}

export interface ApiMember extends ApiIdent {
  kind: 'field' | 'method'
}

export interface ApiType extends ApiDecl, ApiIdent {
  members: ApiMember[]
  consts: ApiValue[]
  vars: ApiValue[]
  funcs: ApiFunc[]
  methods: ApiFunc[]
}

// A package's page: the reference's package without the doc comments' text, which the page shows as HTML.
export interface ApiPackage {
  importPath: string
  name: string
  // page is the package's path below the reference's root, such as sprite or tooling/pack.
  page: string
  synopsis: string
  docHTML?: string
  platforms?: string[]
  consts: ApiValue[]
  vars: ApiValue[]
  funcs: ApiFunc[]
  types: ApiType[]
}

export interface ApiSummary {
  importPath: string
  name: string
  page: string
  synopsis: string
  platforms: string[]
}

export type ApiIndex = { status: 'ok'; packages: ApiSummary[] } | { status: 'unavailable'; reason: string }

// ---- The site's own pages (plugins/site.ts) ----

// The languages the docs are written in, Traditional Chinese converted from Simplified; every other language shows English.
export type DocsLang = 'en' | 'zh' | 'zh-tw'

export type PackageCategory = 'core' | 'play' | 'ui' | 'tools' | 'online' | '3d'

// A package of the catalog, from docs/en/stability.md, with what it holds from the docs' index or its guide.
export interface CatalogPackage {
  // path is relative to github.com/nsgo-dev, such as game/pkg/sprite.
  path: string
  // name is the path below game, such as sprite, or game for the root package.
  name: string
  stable: boolean
  category: PackageCategory
  // guide is the slug of the docs page that is its guide.
  guide?: string
  // description is empty in a language whose docs say nothing of the package.
  description: Record<DocsLang, string>
}

export interface TutorialChapter {
  n: number
  title: string
  // slug is the chapter's docs page.
  slug: string
  adds: string
  packages: string
  // dir is the chapter's directory under examples/tutorial.
  dir: string
}

// A file of the tutorial's game and what it does, from "How the game is built".
export interface TutorialPart {
  file: string
  text: string
}

export interface TutorialContent {
  title: string
  intro: string[]
  chapters: TutorialChapter[]
  partsTitle: string
  parts: TutorialPart[]
}

export interface SiteContent {
  packages: CatalogPackage[]
  tutorial: Record<DocsLang, TutorialContent>
  // featured are the packages each featured example imports, in the catalog's order.
  featured: Record<import('./featured').FeaturedId, string[]>
}
