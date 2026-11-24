// English copy, the canonical shape every other language file under src/copy/ matches. Names of packages, types and code stay in English.

export const en = {
  nav: {
    docs: 'Docs',
    playground: 'Playground',
    showcase: 'Showcase',
    roadmap: 'Roadmap',
  },
  site: {
    title: 'NSGo Game: a Go game layer for NSGo',
    description: 'A game layer for NSGo apps, written in Go: a fixed-step loop, sprites and sound, then 3D.',
  },
  home: {
    badge: 'Early: APIs will change',
    // Around the link to NSGo.
    lead: [
      'A game layer for ',
      ' apps: a fixed-step loop, sprites and sound drawn into one native view, with the HUD and menus as ordinary NSGo views on top. It uses only NSGo’s public API, so an app that does not import it links none of it.',
    ],
    stages: {
      eyebrow: 'Roadmap',
      title: 'Five stages',
      lead: 'From what NSGo itself needs for games to getting them to players, each stage tracked in issues.',
      label: (n: number) => `Stage ${n}`,
      // A stage's name and one line by its number; a stage past these shows its issue's English title and description.
      names: ['NSGo primitives games need', '2D game layer', '3D', 'Tools, editor and project format', 'Shipping, operations and community'],
      lines: [
        'Input, frame timing, canvas, audio, haptics and sensors in NSGo itself.',
        'The loop, sprites, tilemaps, scenes, physics, sound and saves.',
        'A GPU view in NSGo, then meshes, cameras, lights and 3D physics.',
        'The CLI, MCP for agents, the editor and a project of text files.',
        'Releases, store uploads, live operations, examples and docs.',
      ],
      more: 'Roadmap',
    },
  },
  docs: {
    groups: {
      guides: 'Guides',
      tutorial: 'Tutorial',
      comingFrom: 'Coming from Another Engine',
      core2d: '2D Core',
      extended2d: '2D Systems',
      multiplayerModding: 'Multiplayer & Modding',
      threeD: '3D',
      other: 'More',
    },
    reference: 'Reference',
  },
  api: {
    title: 'API reference',
    description: 'Every exported identifier of the game’s Go packages, from their doc comments, with the same reference as JSON for agents.',
    // Around the link to the JSON.
    lead: ['Every package of the game layer, from the doc comments in its source, with an anchor for each exported identifier. Agents can read the same reference as JSON at ', '.'],
    nav: 'API reference',
    overview: 'Overview',
    packages: 'Packages',
    tooling: 'Tooling',
    index: 'Index',
    constants: 'Constants',
    variables: 'Variables',
    functions: 'Functions',
    types: 'Types',
    // Before the targets a declaration or a package is limited to.
    only: 'Only on',
    source: 'Source',
    // Above a package's page in a language other than English; empty in English.
    notice: '',
    unavailable: ['This build could not read the Go packages (', ').'],
  },
  roadmap: {
    description: 'The plan for games on NSGo in five stages: every open and closed issue under it in nsgo-dev/game and NSGo, by area.',
  },
  playground: {
    description: 'Run the examples in your browser beside their source, then run them on your machine to edit them.',
    heading: 'Playground',
    intro: 'The examples, built for the web, beside their source. To change one, run it on your machine: it rebuilds and reloads each time you save.',
    examples: 'Examples',
    open: 'Open',
    files: (n: number) => `${n} ${n === 1 ? 'file' : 'files'}`,
    lines: (n: number) => `${n.toLocaleString('en')} lines`,
    run: 'Run',
    restart: 'Restart',
    newTab: 'Open in a new tab',
    running: (name: string) => `${name}, running`,
    download: (mb: string) => `${mb} MB of WebAssembly`,
    focus: 'Click the game to give it the keyboard.',
    notBuilt: 'This copy of the site has no web build of this example. Build it with:',
    source: 'Source files',
    loading: 'Loading the source…',
    github: 'View on GitHub',
    local: {
      title: 'Edit it on your machine',
      body: 'With Go 1.27 or later, clone the repository and start the example with the dev server. It opens in your browser, and rebuilds and reloads each time you save a file; without -target web it runs as a native app.',
    },
  },
  showcase: {
    description: 'Games, official examples, and starters built with NSGo Game across desktop, mobile, and the web.',
    heading: 'Showcase',
    intro: 'Games, built-in examples, and starter projects built on NSGo Game across desktop, mobile, and the web.',
    listLabel: 'Showcase entries',
    entry: (n: number) => `Entry ${String(n).padStart(2, '0')}`,
    kinds: {
      example: 'Example',
      template: 'Template',
      community: 'Community',
    },
    tagsLabel: 'Tags',
    targetsLabel: 'Targets',
    by: 'By',
    source: 'Source',
    play: 'Play',
    submit: {
      eyebrow: 'Community',
      title: 'Built a game with NSGo Game?',
      body: 'Open a showcase submission on GitHub with your game’s title, targets, license, and link to have it listed here.',
      action: 'Submit your game',
      contributing: 'Contributing guide',
    },
  },
}

export type Copy = typeof en
