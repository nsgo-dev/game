import { createFileRoute, Link } from '@tanstack/react-router'
import { snippets } from 'virtual:snippets'
import type { Snippet } from '../content/types'
import { GITHUB_URL, NSGO_URL } from '../links'

export const Route = createFileRoute('/')({
  component: Home,
})

const PACKAGES: { id: Snippet['id']; name: string; path: string; summary: string; anchor: string }[] = [
  {
    id: 'loop',
    name: 'Loop',
    path: 'github.com/nsgo-dev/game/packages/game',
    summary: 'Fixed-step updates on the display’s frames, one interpolated draw per frame. Pauses in the background.',
    anchor: 'loop',
  },
  {
    id: 'sprite',
    name: 'sprite',
    path: 'github.com/nsgo-dev/game/packages/game/sprite',
    summary: 'Atlases from TexturePacker or Aseprite, frame animations, and Z-ordered batches drawn into a Canvas.',
    anchor: 'sprite',
  },
  {
    id: 'sound',
    name: 'sound',
    path: 'github.com/nsgo-dev/game/packages/game/sound',
    summary: 'A mixer with saved master and group volumes, per-sound limits, and music that crossfades.',
    anchor: 'sound',
  },
]

function Home() {
  const html = new Map(snippets.map((s) => [s.id, s.html]))
  return (
    <>
      <section className="hero" aria-labelledby="hero-title">
        <p className="status">
          <span className="dot" aria-hidden="true" />
          Early: APIs will change
        </p>
        <h1 id="hero-title">
          Games in Go,
          <br />
          on every NSGo target.
        </h1>
        <p className="lede">
          A game layer for <a href={NSGO_URL}>NSGo</a> apps: a fixed-step loop, sprites and sound drawn into one native view, with the HUD and menus as ordinary NSGo views on top. It uses only NSGo’s public API, so an app
          that does not import it links none of it.
        </p>
        <div className="actions">
          <Link className="button primary" to="/docs/">
            Read the docs
          </Link>
          <a className="button" href={GITHUB_URL}>
            GitHub
          </a>
        </div>
        <p className="targets" aria-label="Targets">
          {['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web'].map((t) => (
            <span key={t}>{t}</span>
          ))}
        </p>
      </section>

      <section className="stages" aria-labelledby="stages-title">
        <h2 id="stages-title" className="section-title">
          Two stages
        </h2>
        <ol className="stage-list">
          <li className="stage stage-2d">
            <span className="stage-tag">Stage 1 · 2D</span>
            <p>A fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn into a Canvas, a pixel buffer or a shader.</p>
          </li>
          <li className="stage stage-3d">
            <span className="stage-tag">Stage 2 · 3D</span>
            <p>
              Meshes, cameras, lights and glTF models on a GPU view NSGo adds for it (<a href="https://github.com/nsgo-dev/nsgo/issues/1059">nsgo#1059</a>). The loop, input, sound and scenes carry over.
            </p>
          </li>
        </ol>
        <Link className="more" to="/roadmap/">
          Roadmap
        </Link>
      </section>

      <section className="packages" aria-labelledby="packages-title">
        <h2 id="packages-title" className="section-title">
          Packages
        </h2>
        {PACKAGES.map((p) => (
          <article key={p.id} className="package" aria-labelledby={`pkg-${p.id}`}>
            <div className="package-text">
              <h3 id={`pkg-${p.id}`}>{p.name}</h3>
              <code className="import-path">{p.path}</code>
              <p>{p.summary}</p>
              <Link className="more" to="/docs/$slug/" params={{ slug: 'packages' }} hash={p.anchor}>
                Reference
              </Link>
            </div>
            <div className="code" dangerouslySetInnerHTML={{ __html: html.get(p.id) ?? '' }} />
          </article>
        ))}
      </section>
    </>
  )
}
