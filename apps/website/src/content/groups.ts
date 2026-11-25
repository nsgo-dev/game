// The docs' sidebar groups, shared by the docs pages (components/DocView.tsx) and the build's llms.txt (plugins/llms.ts), so both list the pages the same way.

export type GroupKey = 'guides' | 'tutorial' | 'comingFrom' | 'core2d' | 'extended2d' | 'multiplayerModding' | 'threeD' | 'other'

const SIDEBAR_GROUPS: readonly { key: Exclude<GroupKey, 'other'>; slugs: readonly string[] }[] = [
  { key: 'guides', slugs: ['index', 'getting-started', 'templates', 'template-fighting2d', 'template-rpg2d', 'project', 'playground', 'packages', 'stability', 'shipping', 'cli', 'cli-reference', 'agent-skill'] },
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
    slugs: ['loop', 'sprite', 'sound', 'input', 'tilemap', 'tileshape', 'scene', 'physics', 'screen', 'assets', 'resources', 'text', 'particles', 'save', 'debug', 'mcp', 'flow', 'tween', 'animclip', 'ai', 'procgen', 'replay', 'cutscene', 'undo'],
  },
  { key: 'extended2d', slugs: ['math2d', 'gameui', 'lights2d', 'quality', 'effects', 'worldmap', 'fog', 'a11y', 'l10n', 'perf'] },
  { key: 'multiplayerModding', slugs: ['net', 'modding'] },
  { key: 'threeD', slugs: ['math3d', 'scene3d', 'anim3d', 'world3d', 'navbake3d', 'voxel', 'render3d', 'scenemodel'] },
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
