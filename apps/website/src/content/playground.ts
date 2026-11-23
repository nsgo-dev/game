// The examples the playground runs, by their directory under examples/. The build of the site reads their sources (plugins/content.ts) and scripts/build-playground.ts builds each for the web target into public/play/<id>/; their titles and descriptions are the showcase's. This file imports nothing, so the build script and the Vite plugin can read it too.

export const PLAYGROUND_IDS = ['caverun', 'slidegem', 'cryptquest', 'starburst'] as const

export type PlaygroundId = (typeof PLAYGROUND_IDS)[number]

// PLAYGROUND_BUILDS is the directory below the site's base the builds are served from, beside the pages rather than under /playground/, whose addresses are the pages'.
export const PLAYGROUND_BUILDS = 'play'

export const isPlaygroundId = (id: string): id is PlaygroundId => (PLAYGROUND_IDS as readonly string[]).includes(id)
