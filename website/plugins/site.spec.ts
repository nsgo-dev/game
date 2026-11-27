// Tests for the content of the site's own pages, run with `bun test`.

import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { describe, test } from 'node:test'
import { FEATURED_IDS } from '../src/content/featured.ts'
import { category, firstSentence, loadSite, parseIndexTable, parseStability, parseTutorial, plainText, toTraditional } from './site.ts'

const repo = (p: string) => new URL(`../../${p}`, import.meta.url).pathname

describe('parsing', () => {
  test('plainText drops links, emphasis and code marks', () => {
    assert.equal(plainText('A [`Loop`](loop.md) and **bold** `code`'), 'A Loop and bold code')
  })

  test('parseStability reads each row with its guide', () => {
    const rows = parseStability('| `game` | Stable | [Loop](loop.md) |\n| `game/x` | Experimental | |\nnot a row')
    assert.deepEqual(rows, [
      { path: 'game', stable: true, guide: 'loop' },
      { path: 'game/x', stable: false },
    ])
  })

  test('parseIndexTable keys descriptions by import path', () => {
    const table = parseIndexTable('| `sprite` | `github.com/nsgo-dev/game/pkg/sprite` | [sprite](sprite.md) | Atlases and `Batch` |')
    assert.equal(table.get('game/pkg/sprite'), 'Atlases and Batch')
  })

  test('firstSentence skips the title, lists and code', () => {
    assert.equal(firstSentence('# T\n\n- a list\n\n```go\nx\n```\n\nThe [first](a.md) one. The second.'), 'The first one.')
    assert.equal(firstSentence('# T\n\n第一句。第二句。'), '第一句。')
  })

  test('category sorts packages by path', () => {
    assert.equal(category('game'), 'core')
    assert.equal(category('game/pkg/sprite'), 'core')
    assert.equal(category('game/pkg/ai/perception3d'), '3d')
    assert.equal(category('game/pkg/ai/navbake3d'), '3d')
    assert.equal(category('game/pkg/tilemap/tileshape'), 'core')
    assert.equal(category('game/pkg/tooling/pack'), 'tools')
    assert.equal(category('game/pkg/ktx2'), '3d')
    assert.equal(category('game/pkg/importsettings'), 'tools')
    assert.equal(category('game/pkg/jsonschema'), 'tools')
    assert.equal(category('game/pkg/datatable'), 'core')
    assert.equal(category('game/pkg/online/leaderboard/selfhost'), 'online')
    assert.equal(category('game/pkg/online/netplay'), 'online')
    assert.equal(category('game/cmd/accountsd'), 'online')
    assert.equal(category('game/cmd/game'), 'tools')
    assert.equal(category('game/pkg/pixelfx/fire'), 'ui')
    assert.equal(category('game/pkg/rts'), 'play')
  })

  test('parseTutorial reads the title, introduction, chapters and files', () => {
    const t = parseTutorial(
      '# Tutorial: X\n\nFirst.\n\nSecond.\n\n## Chapters\n\n| Chapter | You add | Packages | Code |\n| --- | --- | --- | --- |\n| [1. Start](tutorial-01-start.md) | A `Canvas` | `core` | [`ch01-start`](../../examples/tutorial/ch01-start/) |\n\n## How\n\n- **`world.go`** holds the state.\n',
    )
    assert.equal(t.title, 'Tutorial: X')
    assert.deepEqual(t.intro, ['First.', 'Second.'])
    assert.deepEqual(t.chapters, [{ n: 1, title: 'Start', slug: 'tutorial-01-start', adds: 'A Canvas', packages: 'core', dir: 'ch01-start' }])
    assert.equal(t.partsTitle, 'How')
    assert.deepEqual(t.parts, [{ file: 'world.go', text: 'holds the state.' }])
  })

  test('toTraditional converts Simplified Chinese as Taiwan writes it', () => {
    assert.equal(toTraditional('软件和鼠标'), '軟體和滑鼠')
  })
})

describe('loadSite', () => {
  test('reads the repository: every package, both tutorials and the featured examples', { timeout: 60_000 }, async () => {
    const site = await loadSite({ docsDir: repo('docs'), examplesDir: repo('examples'), repoDir: repo('.') })
    const rows = parseStability(await readFile(repo('docs/en/stability.md'), 'utf8'))
    assert.equal(site.packages.length, rows.length)
    for (const pkg of site.packages) assert.ok(pkg.description.en, `${pkg.path} has no description`)
    for (const lang of ['en', 'zh', 'zh-tw'] as const) {
      const t = site.tutorial[lang]
      assert.equal(t.intro.length, 2, `${lang} intro`)
      assert.equal(t.chapters.length, site.tutorial.en.chapters.length, `${lang} chapters`)
      assert.ok(t.parts.length > 0, `${lang} parts`)
    }
    for (const id of FEATURED_IDS) assert.ok(site.featured[id].includes('game'), `${id} imports game`)
  })
})
