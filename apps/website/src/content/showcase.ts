import type { Lang } from '../i18n'
import { repoTreeUrl } from '../links'

export type ShowcaseKind = 'example' | 'template' | 'community'
export type ShowcaseTarget = 'macOS' | 'iOS' | 'Windows' | 'Linux' | 'Android' | 'Web'

export interface ShowcaseEntry {
  id: string
  title: Record<Lang, string>
  description: Record<Lang, string>
  author: string
  kind: ShowcaseKind
  tags: string[]
  targets: ShowcaseTarget[]
  sourceUrl: string
  playUrl?: string
  license: string
}

const ALL_TARGETS: ShowcaseTarget[] = ['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web']

// Games and starters built on nsgo-dev/game. Add community entries here once accepted via the showcase submission issue form.
export const SHOWCASE_ENTRIES: ShowcaseEntry[] = [
  {
    id: 'caverun',
    title: { en: 'Cave Run', zh: 'Cave Run' },
    description: {
      en: 'Side-scrolling 2D platformer demonstrating fixed-step physics, tilemap collision, sprite animations, and sound effects on a single Canvas.',
      zh: '横版 2D 平台跳跃示例，展示在单个 Canvas 上使用固定步长物理、瓦片地图碰撞、精灵动画和音效。',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Platformer', 'Tilemap', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/caverun'),
    license: 'Apache-2.0',
  },
  {
    id: 'slidegem',
    title: { en: 'SlideGem', zh: 'SlideGem' },
    description: {
      en: 'Match-3 puzzle game with grid input, chained clear animations, score combos, and layered audio feedback under a native NSGo HUD.',
      zh: '三消益智游戏示例，结合网格输入、连锁消除动画、连击计分、分层音效以及原生 NSGo HUD。',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Puzzle', 'Match-3', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/slidegem'),
    license: 'Apache-2.0',
  },
  {
    id: 'cryptquest',
    title: { en: 'CryptQuest', zh: 'CryptQuest' },
    description: {
      en: 'Top-down dungeon crawler exercising camera tracking, tiled rooms, scene transitions, hit testing, and action-based controls.',
      zh: '俯视角地牢探险示例，演示相机跟随、瓦片房间、场景切换、命中测试与基于动作的输入控制。',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Dungeon', 'Top-Down', 'Tilemap', 'Scene'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/cryptquest'),
    license: 'Apache-2.0',
  },
  {
    id: 'starburst',
    title: { en: 'Starburst', zh: 'Starburst' },
    description: {
      en: 'Fast arcade space shooter stress-testing batched sprite rendering, pooled projectiles, particle bursts, and fixed-step responsiveness.',
      zh: '快节奏街机太空射击示例，用于验证精灵批量绘制、弹幕对象池、粒子爆发和固定步长响应。',
    },
    author: 'NSGo',
    kind: 'example',
    tags: ['Arcade', 'Shooter', 'Sprite', 'Sound'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('examples/starburst'),
    license: 'Apache-2.0',
  },
  {
    id: 'template',
    title: { en: 'Starter Template', zh: 'Starter Template' },
    description: {
      en: 'Minimal multi-target starter project wired with game.Loop, a Canvas view, native HUD overlay, and deterministic nsgotest setup.',
      zh: '最小化的多平台入门项目模板，已接好 game.Loop、Canvas 视图、原生 HUD 叠层以及确定性的 nsgotest 测试结构。',
    },
    author: 'NSGo',
    kind: 'template',
    tags: ['Starter', 'Loop', 'HUD', 'nsgotest'],
    targets: ALL_TARGETS,
    sourceUrl: repoTreeUrl('template'),
    license: 'Apache-2.0',
  },
]
