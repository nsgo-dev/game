import { createFileRoute, Link } from '@tanstack/react-router'
import { type KeyboardEvent, useRef, useState } from 'react'
import { snippets } from 'virtual:snippets'
import { Eyebrow, SpeedLines } from '../components/Brand'
import { CopyCommand } from '../components/CopyCommand'
import { Cta } from '../components/Footer'
import { Icon, type IconName } from '../components/Icon'
import type { Snippet } from '../content/types'
import { GITHUB_URL, NSGO_URL } from '../links'

export const Route = createFileRoute('/')({
  component: Home,
})

const PACKAGES: { id: Snippet['id']; name: string; file: string; path: string; summary: string; anchor: string }[] = [
  {
    id: 'loop',
    name: 'Loop',
    file: 'loop.go',
    path: 'github.com/nsgo-dev/game/packages/game',
    summary: 'Fixed-step updates on the display’s frames, one interpolated draw per frame. Pauses in the background.',
    anchor: 'loop',
  },
  {
    id: 'sprite',
    name: 'sprite',
    file: 'sprite.go',
    path: 'github.com/nsgo-dev/game/packages/game/sprite',
    summary: 'Atlases from TexturePacker or Aseprite, frame animations, and Z-ordered batches drawn into a Canvas.',
    anchor: 'sprite',
  },
  {
    id: 'sound',
    name: 'sound',
    file: 'sound.go',
    path: 'github.com/nsgo-dev/game/packages/game/sound',
    summary: 'A mixer with saved master and group volumes, per-sound limits, and music that crossfades.',
    anchor: 'sound',
  },
]

const TARGETS = ['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web']

const STAGES: { icon: IconName; label: string; title: string; body: string; link?: { href: string; text: string } }[] = [
  {
    icon: 'image',
    label: 'Stage 1',
    title: '2D',
    body: 'A fixed-step loop, input actions, sprites, tilemaps, scenes and sound, drawn into a Canvas, a pixel buffer or a shader.',
  },
  {
    icon: 'box',
    label: 'Stage 2',
    title: '3D',
    body: 'Meshes, cameras, lights and glTF models on a GPU view NSGo adds for it. The loop, input, sound and scenes carry over.',
    link: { href: 'https://github.com/nsgo-dev/nsgo/issues/1059', text: 'nsgo#1059' },
  },
]

function Home() {
  return (
    <>
      <section className="hero">
        <div className="hero__glow" aria-hidden="true" />
        <div className="container hero__inner">
          <div className="hero__copy">
            <span className="pill">
              <SpeedLines size={14} />
              Early: APIs will change
            </span>
            <h1>
              Games in Go,
              <span className="gradient-text">on every NSGo target.</span>
            </h1>
            <p className="hero__lead">
              A game layer for <a href={NSGO_URL}>NSGo</a> apps: a fixed-step loop, sprites and sound drawn into one native view, with the HUD and menus as ordinary NSGo views on top. It uses only NSGo’s public API, so an app
              that does not import it links none of it.
            </p>
            <div className="hero__actions">
              <Link className="btn btn--primary btn--large" to="/docs/">
                Read the docs
              </Link>
              <a className="btn btn--secondary btn--large" href={GITHUB_URL} target="_blank" rel="noopener noreferrer">
                GitHub
              </a>
            </div>
            <CopyCommand command="go get github.com/nsgo-dev/game/packages/game" />
          </div>
        </div>
      </section>

      <section className="section section--soft" id="stages">
        <div className="container">
          <header className="section__head">
            <Eyebrow>Roadmap</Eyebrow>
            <h2>Two stages</h2>
            <p>2D first, then 3D on the same loop, input, sound and scenes.</p>
          </header>
          <ul className="bento">
            {STAGES.map((stage) => (
              <li key={stage.title} className="bento__card">
                <span className="bento__icon">
                  <Icon name={stage.icon} size={18} />
                </span>
                <span className="bento__label">{stage.label}</span>
                <h3>{stage.title}</h3>
                <p>
                  {stage.body}
                  {stage.link && (
                    <>
                      {' '}
                      (<a href={stage.link.href}>{stage.link.text}</a>)
                    </>
                  )}
                </p>
              </li>
            ))}
            <li className="bento__card">
              <span className="bento__icon">
                <Icon name="globe" size={18} />
              </span>
              <span className="bento__label">Targets</span>
              <h3>Where NSGo runs</h3>
              <ul className="chips" aria-label="Targets">
                {TARGETS.map((target) => (
                  <li key={target}>{target}</li>
                ))}
              </ul>
            </li>
          </ul>
          <Link className="btn btn--secondary section__more" to="/roadmap/">
            Roadmap
            <Icon name="arrow" size={14} />
          </Link>
        </div>
      </section>

      <section className="section section--next" id="packages">
        <div className="container code">
          <div className="code__copy">
            <header className="section__head">
              <Eyebrow>Packages</Eyebrow>
              <h2>Three packages so far</h2>
              <p>Each is its own import, tested, and described in the reference.</p>
            </header>
            <ol className="code__points">
              {PACKAGES.map((p, index) => (
                <li key={p.id}>
                  <span className="code__n">{index + 1}</span>
                  <div>
                    <strong>
                      <Link to="/docs/$slug/" params={{ slug: 'packages' }} hash={p.anchor}>
                        {p.name}
                      </Link>
                    </strong>
                    <p>{p.summary}</p>
                    <code className="code__path">{p.path}</code>
                  </div>
                </li>
              ))}
            </ol>
          </div>
          <div className="code__demo">
            <Editor />
          </div>
        </div>
      </section>

      <Cta />
    </>
  )
}

// nsgo.dev's editor: a tab per file, the arrow keys moving between tabs.
function Editor() {
  const html = new Map(snippets.map((s) => [s.id, s.html]))
  const [selected, setSelected] = useState(0)
  const tabs = useRef<(HTMLButtonElement | null)[]>([])

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    event.preventDefault()
    const next = (selected + step + PACKAGES.length) % PACKAGES.length
    setSelected(next)
    tabs.current[next]?.focus()
  }

  return (
    <div className="editor">
      <div className="editor__tabs" role="tablist" aria-label="Examples" onKeyDown={onKey}>
        {PACKAGES.map((p, index) => (
          <button
            key={p.id}
            ref={(element) => {
              tabs.current[index] = element
            }}
            type="button"
            role="tab"
            id={`tab-${p.id}`}
            className="editor__tab"
            aria-selected={index === selected}
            aria-controls={`panel-${p.id}`}
            tabIndex={index === selected ? 0 : -1}
            onClick={() => setSelected(index)}
          >
            {p.file}
          </button>
        ))}
      </div>
      {PACKAGES.map((p, index) => (
        <div
          key={p.id}
          className="editor__panel"
          role="tabpanel"
          id={`panel-${p.id}`}
          aria-labelledby={`tab-${p.id}`}
          hidden={index !== selected}
          dangerouslySetInnerHTML={{ __html: html.get(p.id) ?? '' }}
        />
      ))}
    </div>
  )
}
