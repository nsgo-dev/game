// The shapes of the content the build reads from the repository and GitHub (plugins/content.ts) and the routes render.

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
}

export interface DocHeading {
  depth: 2 | 3
  id: string
  text: string
}

export interface Snippet {
  id: 'loop' | 'sprite' | 'sound'
  html: string
}

export type IssueState = 'open' | 'closed'

export interface RoadmapIssue {
  repo: string
  number: number
  title: string
  state: IssueState
  url: string
}

export interface RoadmapGroup {
  area: string
  issues: RoadmapIssue[]
}

export interface RoadmapStage {
  id: '2d' | '3d'
  title: string
  groups: RoadmapGroup[]
  open: number
  closed: number
}

export type Roadmap =
  | { status: 'ok'; fetchedAt: string; stages: RoadmapStage[] }
  | { status: 'unavailable'; reason: string }
