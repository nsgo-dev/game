// Builds each playground example for NSGo's web target and copies the site it writes into public/play/<id>/, which Vite serves in development and copies into the build. It needs Go and the version of the nsgo tool the examples require, as `go tool nsgo build` does. With example IDs as arguments it builds only those.
//
//   bun run playground            # every example
//   bun run playground caverun    # one

import { spawnSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { PLAYGROUND_BUILDS, PLAYGROUND_IDS, type PlaygroundId, isPlaygroundId } from '../src/content/playground.ts'

const website = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const examples = path.resolve(website, '../../examples')
const builds = path.join(website, 'public', PLAYGROUND_BUILDS)
// OUTPUT is where in an example's directory its web build goes, under dist/, which Git ignores; it is emptied first, so the one site in it is this build's.
const OUTPUT = path.join('dist', 'playground')

function main(args: string[]): void {
  const unknown = args.filter((id) => !isPlaygroundId(id))
  if (unknown.length) fail(`unknown example ${unknown.join(', ')}; the playground's are ${PLAYGROUND_IDS.join(', ')}`)
  const ids: readonly PlaygroundId[] = args.length ? (args as PlaygroundId[]) : PLAYGROUND_IDS
  mkdirSync(builds, { recursive: true })
  for (const id of ids) build(id)
}

function build(id: PlaygroundId): void {
  const dir = path.join(examples, id)
  const output = path.join(dir, OUTPUT)
  rmSync(output, { recursive: true, force: true })
  console.info(`[playground] building ${id} for the web`)
  const result = spawnSync('go', ['tool', 'nsgo', 'build', '-target', 'web', '-output', OUTPUT, '.'], { cwd: dir, stdio: 'inherit' })
  if (result.error) fail(`${id}: ${result.error.message}`)
  if (result.status !== 0) fail(`${id}: nsgo build exited with ${result.status ?? result.signal}`)
  // nsgo build names the site's directory <name>-<version>-web, from nsgo.yaml, beside releases/.
  const sites = readdirSync(output, { withFileTypes: true }).filter((e) => e.isDirectory() && e.name.endsWith('-web') && existsSync(path.join(output, e.name, 'nsgo.json')))
  if (sites.length !== 1) fail(`${id}: expected one web site in ${OUTPUT}, found ${sites.length}`)
  const target = path.join(builds, id)
  rmSync(target, { recursive: true, force: true })
  cpSync(path.join(output, sites[0]!.name), target, { recursive: true })
  console.info(`[playground] ${id} → public/${PLAYGROUND_BUILDS}/${id}/`)
}

function fail(message: string): never {
  console.error(`[playground] ${message}`)
  process.exit(1)
}

main(process.argv.slice(2))
