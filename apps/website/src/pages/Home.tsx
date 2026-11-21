import { Link } from '@tanstack/react-router'
import { type KeyboardEvent, useRef, useState } from 'react'
import { roadmap } from 'virtual:roadmap'
import { snippets } from 'virtual:snippets'
import { Eyebrow } from '../components/Brand'
import { CopyCommand } from '../components/CopyCommand'
import { Cta } from '../components/Footer'
import { HeroField } from '../components/HeroField'
import { Icon } from '../components/Icon'
import { Segments } from '../components/Segments'
import type { Snippet } from '../content/types'
import { copies } from '../copy'
import { type Lang, pageHead, routes, useLang } from '../i18n'
import { GITHUB_URL, issueUrl, nsgoUrl } from '../links'

export const homeHead = (lang: Lang) => pageHead(lang, '', copies[lang].site.title, copies[lang].site.description)

// The game layer's packages as levels, in the order nsgo#1047 plans them: each names its task in this repository, and the shipped ones their import path and example. A level is cleared once its package ships; the rest are locked, with the progress of the roadmap area their task heads.
type Level = { id: 'loop' | 'sprite' | 'sound' | 'input' | 'tilemap' | 'scene'; name: string; issue: number } & (
  | { shipped: true; snippet: Snippet['id']; file: string; path: string; anchor: string }
  | { shipped: false }
)

const LEVELS: Level[] = [
  { id: 'loop', name: 'Loop', issue: 2, shipped: true, snippet: 'loop', file: 'loop.go', path: 'github.com/nsgo-dev/game/packages/game', anchor: 'loop' },
  { id: 'sprite', name: 'sprite', issue: 4, shipped: true, snippet: 'sprite', file: 'sprite.go', path: 'github.com/nsgo-dev/game/packages/game/sprite', anchor: 'sprite' },
  { id: 'sound', name: 'sound', issue: 7, shipped: true, snippet: 'sound', file: 'sound.go', path: 'github.com/nsgo-dev/game/packages/game/sound', anchor: 'sound' },
  { id: 'input', name: 'input', issue: 3, shipped: false },
  { id: 'tilemap', name: 'tilemap', issue: 5, shipped: false },
  { id: 'scene', name: 'scene', issue: 6, shipped: false },
]

const PACKAGES = LEVELS.filter((level): level is Extract<Level, { shipped: true }> => level.shipped)

// areaProgress is the closed and total issues of the roadmap area headed by this repository's issue, the issue and everything below it, or null when the build could not read the roadmap or no area is headed by it.
function areaProgress(issue: number): { closed: number; total: number } | null {
  if (roadmap.status !== 'ok') return null
  for (const stage of roadmap.stages) {
    const group = stage.groups.find((g) => g.epic?.repo === 'nsgo-dev/game' && g.epic.number === issue)
    if (group) return { closed: group.closed, total: group.open + group.closed }
  }
  return null
}

// The plan's stages: from the roadmap the build read, or by number alone when it could not read it.
type StageCard = { number: number; title?: string; description?: string; progress?: { closed: number; total: number } }

const STAGE_COUNT = 5

function stageCards(): StageCard[] {
  if (roadmap.status !== 'ok') return Array.from({ length: STAGE_COUNT }, (_, i) => ({ number: i + 1 }))
  return roadmap.stages.map((stage) => ({
    number: stage.number,
    title: stage.title,
    description: stage.description,
    progress: { closed: stage.closed, total: stage.open + stage.closed },
  }))
}

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
          <ol className="stages">
            {stageCards().map((stage) => (
              <StageCardView key={stage.number} stage={stage} />
            ))}
          </ol>
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

// A stage as a panel: its number and progress, its name and one line, linking to the stage on the roadmap. A stage the site has no words for shows its issue's English title and description.
function StageCardView({ stage }: { stage: StageCard }) {
  const lang = useLang()
  const copy = copies[lang].home
  const name = copy.stages.names[stage.number - 1]
  const line = copy.stages.lines[stage.number - 1]
  const { progress } = stage
  return (
    <li className="stage hud">
      <div className="stage__top">
        <span className="hud-label">{copy.stages.label(stage.number)}</span>
        {progress && (
          <span className="stage__xp">
            <span className="sr-only">{copy.packages.progress(progress.closed, progress.total)}</span>
            <span aria-hidden="true">
              {progress.closed}/{progress.total}
            </span>
          </span>
        )}
      </div>
      <h3>
        <Link to={routes[lang].roadmap} hash={`stage-${stage.number}`} lang={name ? undefined : 'en'}>
          {name ?? stage.title}
        </Link>
      </h3>
      {(line ?? stage.description) && <p lang={line ? undefined : 'en'}>{line ?? stage.description}</p>}
      {progress && <Segments closed={progress.closed} total={progress.total} />}
    </li>
  )
}

function LevelCard({ level, n }: { level: Level; n: number }) {
  const lang = useLang()
  const copy = copies[lang].home.packages
  const progress = level.shipped ? null : areaProgress(level.issue)
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
