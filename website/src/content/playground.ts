// The examples the playground runs, by their directory under examples/. The build of the site reads their sources (plugins/content.ts) and scripts/build-playground.ts builds each for the web target into public/play/<id>/; their titles and descriptions are the showcase's. This file imports nothing, so the build script and the Vite plugin can read it too.

export const PLAYGROUND_IDS = ['caverun', 'slidegem', 'cryptquest', 'starburst'] as const

export type PlaygroundId = (typeof PLAYGROUND_IDS)[number]

// PlaygroundOrientation is the screen orientation an example expects on a phone, or 'any' when either works.
export type PlaygroundOrientation = 'landscape' | 'portrait' | 'any'

export interface PlaygroundEntry {
  id: PlaygroundId
  orientation: PlaygroundOrientation
}

export const PLAYGROUND_ENTRIES: readonly PlaygroundEntry[] = [
  { id: 'caverun', orientation: 'landscape' },
  { id: 'slidegem', orientation: 'portrait' },
  { id: 'cryptquest', orientation: 'landscape' },
  { id: 'starburst', orientation: 'landscape' },
]

export const playgroundEntry = (id: PlaygroundId): PlaygroundEntry => {
  const entry = PLAYGROUND_ENTRIES.find((item) => item.id === id)
  if (!entry) throw new Error(`playground: ${id} has no entry`)
  return entry
}

// PLAYGROUND_BUILDS is the directory below the site's base the builds are served from, beside the pages rather than under /playground/, whose addresses are the pages'.
export const PLAYGROUND_BUILDS = 'play'

export const isPlaygroundId = (id: string): id is PlaygroundId => (PLAYGROUND_IDS as readonly string[]).includes(id)
