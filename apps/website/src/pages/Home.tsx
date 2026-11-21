import { Link } from '@tanstack/react-router'
import { type KeyboardEvent, useRef, useState } from 'react'
import { roadmap } from 'virtual:roadmap'
import { snippets } from 'virtual:snippets'
import { Eyebrow } from '../components/Brand'
import { CopyCommand } from '../components/CopyCommand'
import { Cta } from '../components/Footer'
import { HeroField } from '../components/HeroField'
import { Icon, type IconName } from '../components/Icon'
import { Segments } from '../components/Segments'
import type { Snippet } from '../content/types'
import { copies } from '../copy'
import { type Lang, pageHead, routes, useLang } from '../i18n'
import { GITHUB_URL, issueUrl, nsgoUrl } from '../links'

export const homeHead = (lang: Lang) => pageHead(lang, '', copies[lang].site.title, copies[lang].site.description)

// The game layer's packages as levels, in the order nsgo#1047 plans them: each names its task in this repository, and the shipped ones their import path and example. A level is cleared once its package ships; the rest are locked, with their area's progress from the roadmap.
type Level = { id: 'loop' | 'sprite' | 'sound' | 'input' | 'tilemap' | 'scene'; name: string; issue: number } & (
  | { shipped: true; snippet: Snippet['id']; file: string; path: string; anchor: string }
  | { shipped: false; area: string }
)

const LEVELS: Level[] = [
  { id: 'loop', name: 'Loop', issue: 2, shipped: true, snippet: 'loop', file: 'loop.go', path: 'github.com/nsgo-dev/game/packages/game', anchor: 'loop' },
  { id: 'sprite', name: 'sprite', issue: 4, shipped: true, snippet: 'sprite', file: 'sprite.go', path: 'github.com/nsgo-dev/game/packages/game/sprite', anchor: 'sprite' },
  { id: 'sound', name: 'sound', issue: 7, shipped: true, snippet: 'sound', file: 'sound.go', path: 'github.com/nsgo-dev/game/packages/game/sound', anchor: 'sound' },
  { id: 'input', name: 'input', issue: 3, shipped: false, area: 'Input' },
  { id: 'tilemap', name: 'tilemap', issue: 5, shipped: false, area: 'Tilemap' },
  { id: 'scene', name: 'scene', issue: 6, shipped: false, area: 'Scenes' },
]

const PACKAGES = LEVELS.filter((level): level is Extract<Level, { shipped: true }> => level.shipped)

// areaProgress is the closed and total issues of a roadmap area, or null when the build could not read the roadmap or it has no such area.
function areaProgress(area: string): { closed: number; total: number } | null {
  if (roadmap.status !== 'ok') return null
  const issues = roadmap.stages.flatMap((stage) => stage.groups.filter((group) => group.area === area).flatMap((group) => group.issues))
  if (!issues.length) return null
  return { closed: issues.filter((issue) => issue.state === 'closed').length, total: issues.length }
}

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
        <HeroField />
        <div className="container hero__inner">
          <div className="hero__copy">
            <span className="pill">
              <span className="pill__dot" aria-hidden="true" />
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
                <Icon name="play" size={14} />
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
              <li key={stage.title} className="bento__card hud">
                <span className="bento__top">
                  <span className="hud-label">
                    {copy.stages.label(index + 1)} · {stage.title}
                  </span>
                  <span className="bento__icon">
                    <Icon name={stage.icon} size={16} />
                  </span>
                </span>
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
            <li className="bento__card hud">
              <span className="bento__top">
                <span className="hud-label">
                  {copy.stages.targetsLabel} · {TARGETS.length}
                </span>
                <span className="bento__icon">
                  <Icon name="globe" size={16} />
                </span>
              </span>
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
        <div className="container">
          <header className="section__head">
            <Eyebrow>{copy.packages.eyebrow}</Eyebrow>
            <h2>{copy.packages.title}</h2>
            <p>{copy.packages.lead}</p>
          </header>
          <ol className="levels">
            {LEVELS.map((level, index) => (
              <LevelCard key={level.id} level={level} n={index + 1} />
            ))}
          </ol>
          <div className="code__demo">
            <Editor />
          </div>
        </div>
      </section>

      <Cta />
    </>
  )
}

function LevelCard({ level, n }: { level: Level; n: number }) {
  const lang = useLang()
  const copy = copies[lang].home.packages
  const progress = level.shipped ? null : areaProgress(level.area)
  return (
    <li className={level.shipped ? 'level hud' : 'level hud level--locked'}>
      <div className="level__top">
        <span className="hud-label">{copy.level(n)}</span>
        <span className={level.shipped ? 'chip chip--cleared' : 'chip chip--locked'}>
          <Icon name={level.shipped ? 'check' : 'lock'} size={12} />
          {level.shipped ? copy.cleared : copy.locked}
        </span>
      </div>
      <h3>
        {level.shipped ? (
          <Link to={routes[lang].doc} params={{ slug: 'packages' }} hash={level.anchor}>
            {level.name}
          </Link>
        ) : (
          level.name
        )}
      </h3>
      <p>{copy[level.id]}</p>
      {level.shipped ? (
        <code className="code__path">{level.path}</code>
      ) : (
        <div className="level__plan">
          {progress && (
            <div className="level__xp">
              <span className="sr-only">{copy.progress(progress.closed, progress.total)}</span>
              <span className="level__xp-text" aria-hidden="true">
                {progress.closed}/{progress.total}
              </span>
              <Segments closed={progress.closed} total={progress.total} />
            </div>
          )}
          <a href={issueUrl(level.issue)}>
            {copy.plan} #{level.issue}
            <Icon name="external" size={12} />
          </a>
        </div>
      )}
    </li>
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
    <div className="editor hud">
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
          dangerouslySetInnerHTML={{ __html: html.get(p.snippet) ?? '' }}
        />
      ))}
    </div>
  )
}
