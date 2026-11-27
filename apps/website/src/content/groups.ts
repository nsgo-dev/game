// The docs' sidebar groups, shared by the docs pages (components/DocView.tsx) and the build's llms.txt (plugins/llms.ts), so both list the pages the same way.

export type GroupKey = 'guides' | 'tutorial' | 'comingFrom' | 'core2d' | 'extended2d' | 'multiplayerModding' | 'threeD' | 'other'

const SIDEBAR_GROUPS: readonly { key: Exclude<GroupKey, 'other'>; slugs: readonly string[] }[] = [
  {
    key: 'guides',
    slugs: [
      'index',
      'getting-started',
      'templates',
      'template-platformer2d',
      'template-rpg2d',
      'template-roguelike2d',
      'template-towerdefense2d',
      'template-fighting2d',
      'template-cards2d',
      'template-idle',
      'template-rts2d',
      'template-action3d',
      'template-fps3d',
      'template-racing3d',
      'project',
      'formats',
      'schemas',
      'editor',
      'playground',
      'packages',
      'stability',
      'errors',
      'platforms',
      'shipping',
      'cli',
      'cli-reference',
      'library',
      'placeholder',
      'agent-skill',
      'agent-eval',
      'package-size-budgets',
      'gc-and-allocations',
      'determinism',
      'playtest',
      'statedump',
    ],
  },
  {
    key: 'tutorial',
    slugs: [
      'tutorial',
      'tutorial-01-empty-project',
      'tutorial-02-game-loop',
      'tutorial-03-input',
      'tutorial-04-sprites',
      'tutorial-05-rules',
      'tutorial-06-screens',
      'tutorial-07-sound',
      'tutorial-08-save',
      'tutorial-09-ship',
    ],
  },
  { key: 'comingFrom', slugs: ['from-godot', 'from-ebitengine', 'from-unity'] },
  {
    key: 'core2d',
    slugs: [
      'loop',
      'sprite',
      'sound',
      'input',
      'tilemap',
      'tileshape',
      'scene',
      'physics',
      'physics2d-rigid',
      'screen',
      'assets',
      'resources',
      'entities',
      'text',
      'particles',
      'save',
      'debug',
      'mcp',
      'flow',
      'tween',
      'animclip',
      'skeletal',
      'ai',
      'navbake',
      'procgen',
      'replay',
      'cutscene',
      'undo',
    ],
  },
  {
    key: 'extended2d',
    slugs: [
      'math2d',
      'gameui',
      'lights2d',
      'quality',
      'effects',
      'pixelfx',
      'worldmap',
      'rooms',
      'fog',
      'a11y',
      'l10n',
      'perf',
      'datatable',
      'stats',
      'platformer',
      'inventory',
      'ability',
      'quest',
      'loot',
      'cards',
      'bullets',
      'fighting',
      'framedata',
      'rts',
      'rhythm',
      'idle',
      'vn',
    ],
  },
  {
    key: 'multiplayerModding',
    slugs: ['netplay', 'server', 'accounts', 'cloudsave', 'leaderboard', 'leaderboard-selfhost', 'achievements', 'presence', 'liveops', 'modding'],
  },
  {
    key: 'threeD',
    slugs: ['3d-models-blender', 'math3d', 'scene3d', 'render3d', 'scenemodel', 'anim3d', 'physics3d', 'vehicle', 'world3d', 'navbake3d', 'voxel'],
  },
]

// groupDocs sorts pages into the sidebar's groups, each in the group's order, and puts every page no group names in "other", in the order given; a group with no pages is left out.
export function groupDocs<T extends { slug: string }>(pages: readonly T[]): { key: GroupKey; items: T[] }[] {
  const bySlug = new Map(pages.map((p) => [p.slug, p]))
  const assigned = new Set<string>()
  const groups: { key: GroupKey; items: T[] }[] = []

  for (const spec of SIDEBAR_GROUPS) {
    const items: T[] = []
    for (const slug of spec.slugs) {
      const page = bySlug.get(slug)
      if (page) {
        items.push(page)
        assigned.add(slug)
      }
    }
    if (items.length > 0) groups.push({ key: spec.key, items })
  }

  const remaining = pages.filter((p) => !assigned.has(p.slug))
  if (remaining.length > 0) groups.push({ key: 'other', items: remaining })
  return groups
}
