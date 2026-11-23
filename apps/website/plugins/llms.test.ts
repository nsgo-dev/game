// Tests for llms.txt and llms-full.txt, run with `bun test`.

import assert from 'node:assert/strict'
import { readdir } from 'node:fs/promises'
import { describe, test } from 'node:test'
import { buildLlms, llmsPage, loadPages } from './llms.ts'

const SITE = 'https://game.example/game/'
const DOCS_EN = new URL('../../../docs/en/', import.meta.url).pathname

describe('llmsPage', () => {
  test('takes the title and description and makes links absolute', () => {
    const page = llmsPage(
      'loop',
      '# The loop\n\nA fixed-step `Loop` for\nevery package. See [sprites](sprite.md#atlases), [the index](index.md), [an example](../../examples/hello) and [NSGo](https://nsgo.dev).\n\n[ref]: save.md\n\n```go\n// [not a link](scene.md)\n```\n',
      SITE,
    )
    assert.equal(page.slug, 'loop')
    assert.equal(page.title, 'The loop')
    assert.equal(page.description, 'A fixed-step Loop for every package. See sprites, the index, an example and NSGo.')
    assert.equal(page.url, `${SITE}docs/loop/`)
    assert.match(page.markdown, /\[sprites\]\(https:\/\/game\.example\/game\/docs\/sprite\/#atlases\)/)
    assert.match(page.markdown, /\[the index\]\(https:\/\/game\.example\/game\/docs\/\)/)
    assert.match(page.markdown, /\[an example\]\(https:\/\/github\.com\/nsgo-dev\/game\/blob\/main\/examples\/hello\)/)
    assert.match(page.markdown, /\[NSGo\]\(https:\/\/nsgo\.dev\)/)
    assert.match(page.markdown, /\[ref\]: https:\/\/game\.example\/game\/docs\/save\//)
    assert.match(page.markdown, /\/\/ \[not a link\]\(scene\.md\)/)
    assert.ok(page.markdown.startsWith('# The loop\n'))
  })

  test('falls back to the slug without a heading', () => {
    const page = llmsPage('extra', 'Only a paragraph.\n', SITE)
    assert.equal(page.title, 'extra')
    assert.equal(page.description, 'Only a paragraph.')
  })
})

describe('buildLlms', () => {
  test('groups the index as the sidebar does and keeps every page in full', () => {
    const pages = [llmsPage('index', '# Overview\n\nWhat it is.\n', SITE), llmsPage('loop', '# Loop\n\nThe loop.\n', SITE), llmsPage('zzz', '# Later\n', SITE)]
    const { index, full } = buildLlms(pages, SITE)
    assert.ok(index.startsWith('# NSGo Game\n\n> '))
    assert.ok(index.includes(`## Guides\n\n- [Overview](${SITE}docs/): What it is.`))
    assert.ok(index.includes(`## 2D Core\n\n- [Loop](${SITE}docs/loop/): The loop.`))
    assert.ok(index.includes(`## More\n\n- [Later](${SITE}docs/zzz/)\n`))
    assert.ok(index.includes(`${SITE}llms-full.txt`))
    assert.ok(index.endsWith('\n') && !index.endsWith('\n\n'))
    assert.equal(full, `# NSGo Game documentation\n\n---\n\nSource: ${SITE}docs/\n\n# Overview\n\nWhat it is.\n\n---\n\nSource: ${SITE}docs/loop/\n\n# Loop\n\nThe loop.\n\n---\n\nSource: ${SITE}docs/zzz/\n\n# Later\n`)
  })

  test('is deterministic', () => {
    const pages = [llmsPage('index', '# Overview\n', SITE)]
    assert.deepEqual(buildLlms(pages, SITE), buildLlms(pages, SITE))
  })

  test('refuses no pages', () => {
    assert.throws(() => buildLlms([], SITE), /no pages/)
  })

  test('lists every page of docs/en', async () => {
    const files = (await readdir(DOCS_EN)).filter((f) => f.endsWith('.md'))
    const pages = await loadPages(DOCS_EN, SITE)
    assert.deepEqual(pages.map((p) => `${p.slug}.md`).sort(), files.sort())
    const { index, full } = buildLlms(pages, SITE)
    for (const page of pages) {
      assert.ok(page.markdown.startsWith(`# ${page.title}\n`), `docs/en/${page.slug}.md starts with its title`)
      assert.ok(index.includes(`- [${page.title}](${page.url})`), `llms.txt lists ${page.slug}`)
      assert.ok(full.includes(`\nSource: ${page.url}\n\n${page.markdown}`), `llms-full.txt holds ${page.slug}`)
    }
    // Links between pages are absolute in both files, never the Markdown's relative ones.
    assert.doesNotMatch(full, /\]\([a-z0-9-]+\.md(#[^)]*)?\)/)
  })
})
