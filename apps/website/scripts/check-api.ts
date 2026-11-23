// check-api reads the built site's api.json and checks that every exported identifier in it has an element with its anchor as id on its package's page, in each language. The build runs it after prerendering; a build without the reference, which a local build without Go makes, is checked only in CI.

import { existsSync } from 'node:fs'
import { readFile } from 'node:fs/promises'
import path from 'node:path'
import type { ApiFunc, ApiReference, ApiValue } from '../src/content/types'
import type { Lang } from '../src/i18n'

// The site's languages; src/i18n.ts, which lists them too, imports the router, which this script has no use for.
const langs = ['en', 'zh'] as const satisfies readonly Lang[]

const dist = path.resolve(import.meta.dirname, '../dist/client')
const json = path.join(dist, 'api.json')

if (!existsSync(json)) {
  if (process.env.CI) throw new Error(`check-api: ${json} is missing`)
  console.warn('[check-api] skipped: the build has no api.json')
  process.exit(0)
}

const ref = JSON.parse(await readFile(json, 'utf8')) as ApiReference
const anchorsOf = (values: ApiValue[], funcs: ApiFunc[]) => [...values.flatMap((v) => v.idents.map((i) => i.anchor)), ...funcs.map((f) => f.anchor)]
const problems: string[] = []
let checked = 0
for (const pkg of ref.packages) {
  const anchors = [
    ...anchorsOf([...pkg.consts, ...pkg.vars], pkg.funcs),
    ...pkg.types.flatMap((t) => [t.anchor, ...t.members.map((m) => m.anchor), ...anchorsOf([...t.consts, ...t.vars], [...t.funcs, ...t.methods])]),
  ]
  for (const lang of langs) {
    const page = path.join(dist, lang === 'en' ? '' : lang, 'docs/api', pkg.page, 'index.html')
    if (!existsSync(page)) {
      problems.push(`${pkg.importPath}: no page ${path.relative(dist, page)}`)
      continue
    }
    const ids = new Set([...(await readFile(page, 'utf8')).matchAll(/\sid="([^"]*)"/g)].map((m) => m[1]))
    for (const anchor of anchors) {
      checked++
      if (!ids.has(anchor)) problems.push(`${pkg.importPath}: ${path.relative(dist, page)} has no #${anchor}`)
    }
  }
}
if (problems.length) {
  console.error(problems.join('\n'))
  throw new Error(`check-api: ${problems.length} of ${checked} anchors missing`)
}
console.info(`[check-api] ${checked} anchors on ${ref.packages.length * langs.length} pages`)
