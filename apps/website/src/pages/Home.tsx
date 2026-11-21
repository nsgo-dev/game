import { Link } from '@tanstack/react-router'
import { type KeyboardEvent, useRef, useState } from 'react'
import { snippets } from 'virtual:snippets'
import { Eyebrow, SpeedLines } from '../components/Brand'
import { CopyCommand } from '../components/CopyCommand'
import { Cta } from '../components/Footer'
import { Icon, type IconName } from '../components/Icon'
import type { Snippet } from '../content/types'
import { copies } from '../copy'
import { type Lang, pageHead, routes, useLang } from '../i18n'
import { GITHUB_URL, nsgoUrl } from '../links'

export const homeHead = (lang: Lang) => pageHead(lang, '', copies[lang].site.title, copies[lang].site.description)

const PACKAGES: { id: Snippet['id']; name: string; file: string; path: string; anchor: string }[] = [
  {
    id: 'loop',
    name: 'Loop',
    file: 'loop.go',
    path: 'github.com/nsgo-dev/game/packages/game',
    anchor: 'loop',
  },
  {
    id: 'sprite',
    name: 'sprite',
    file: 'sprite.go',
    path: 'github.com/nsgo-dev/game/packages/game/sprite',
    anchor: 'sprite',
  },
  {
    id: 'sound',
    name: 'sound',
    file: 'sound.go',
    path: 'github.com/nsgo-dev/game/packages/game/sound',
    anchor: 'sound',
  },
]

const TARGETS = ['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web']

const STAGES: { icon: IconName; title: string; body: 'twoD' | 'threeD'; link?: { href: string; text: string } }[] = [
  { icon: 'image', title: '2D', body: 'twoD' },
  { icon: 'box', title: '3D', body: 'threeD', link: { href: 'https://github.com/nsgo-dev/nsgo/issues/1059', text: 'nsgo#1059' } },
]

export function Home() {
  const lang = useLang()
  const copy = copies[lang].home
  const to = routes[lang]
  return (
    <>
      <section className="hero">
        <div className="hero__glow" aria-hidden="true" />
        <div className="container hero__inner">
          <div className="hero__copy">
            <span className="pill">
              <SpeedLines size={14} />
              {copy.badge}
            </span>
            <h1>
              {copy.title[0]}
              <span className="gradient-text">{copy.title[1]}</span>
            </h1>
            <p className="hero__lead">
              {copy.lead[0]}
              <a href={nsgoUrl(lang)}>NSGo</a>
              {copy.lead[1]}
            </p>
            <div className="hero__actions">
              <Link className="btn btn--primary btn--large" to={to.docs}>
                {copy.docs}
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
            <Eyebrow>{copy.stages.eyebrow}</Eyebrow>
            <h2>{copy.stages.title}</h2>
            <p>{copy.stages.lead}</p>
          </header>
          <ul className="bento">
            {STAGES.map((stage, index) => (
              <li key={stage.title} className="bento__card">
                <span className="bento__icon">
                  <Icon name={stage.icon} size={18} />
                </span>
                <span className="bento__label">{copy.stages.label(index + 1)}</span>
                <h3>{stage.title}</h3>
                <p>
                  {copy.stages[stage.body]}
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
              <span className="bento__label">{copy.stages.targetsLabel}</span>
              <h3>{copy.stages.targetsTitle}</h3>
              <ul className="chips" aria-label={copy.stages.targetsLabel}>
                {TARGETS.map((target) => (
                  <li key={target}>{target}</li>
                ))}
              </ul>
            </li>
          </ul>
          <Link className="btn btn--secondary section__more" to={to.roadmap}>
            {copy.stages.more}
            <Icon name="arrow" size={14} />
          </Link>
        </div>
      </section>

      <section className="section section--next" id="packages">
        <div className="container code">
          <div className="code__copy">
            <header className="section__head">
              <Eyebrow>{copy.packages.eyebrow}</Eyebrow>
              <h2>{copy.packages.title}</h2>
              <p>{copy.packages.lead}</p>
            </header>
            <ol className="code__points">
              {PACKAGES.map((p, index) => (
                <li key={p.id}>
                  <span className="code__n">{index + 1}</span>
                  <div>
                    <strong>
                      <Link to={to.doc} params={{ slug: 'packages' }} hash={p.anchor}>
                        {p.name}
                      </Link>
                    </strong>
                    <p>{copy.packages[p.id]}</p>
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
  const copy = copies[useLang()].home.packages
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
      <div className="editor__tabs" role="tablist" aria-label={copy.examples} onKeyDown={onKey}>
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
