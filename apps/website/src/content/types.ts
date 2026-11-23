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

// An issue under an area, with its depth below the area shown at most one level deep: 0 for the area's sub-issues, 1 for anything deeper.
export interface RoadmapItem extends RoadmapIssue {
  depth: 0 | 1
}

// An area of a stage: one of the stage's sub-issues with every issue below it. The stage's sub-issues that have none of their own share one group whose epic is null.
export interface RoadmapGroup {
  epic: RoadmapIssue | null
  // area is the epic's title up to its colon, such as "Input"; empty for the group of single issues.
  area: string
  items: RoadmapItem[]
  // open and closed count the epic and every item.
  open: number
  closed: number
}

// A sub-issue of the plan, "Games stage N: <title>".
export interface RoadmapStage {
  number: number
  // title is the part of the issue's title after "Games stage N:", in English.
  title: string
  // description is the first paragraph of the issue's Context, in English.
  description: string
  url: string
  groups: RoadmapGroup[]
  // open and closed count every issue below the stage.
  open: number
  closed: number
}

export type Roadmap =
  | { status: 'ok'; fetchedAt: string; stages: RoadmapStage[]; unplanned: RoadmapIssue[] }
  | { status: 'unavailable'; reason: string }

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
