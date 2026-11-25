import { type CSSProperties, type KeyboardEvent, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { site } from 'virtual:site'
import { snippets } from 'virtual:snippets'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import { ExampleMedia, mediaUrl } from '../components/Media'
import { type FeaturedId, featured } from '../content/featured'
import { isPlaygroundId } from '../content/playground'
import type { PackageCategory, Snippet } from '../content/types'
import { copies, count } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { repoTreeUrl } from '../links'
import { CATEGORY_ORDER, PackageLink } from './Packages'

export const homeHead = (lang: Lang) => pageHead(lang, '', copies[lang].site.title, copies[lang].site.description)

// The command that makes a new game, as docs/en/getting-started.md gives it.
const NEW_GAME = 'go run github.com/nsgo-dev/game/packages/game/cmd/game@latest new mygame'

const PLATFORMS = ['macOS', 'iOS', 'Windows', 'Linux', 'Android', 'Web']

// The loop's numbers under the reel, beside the words for each in the page's language.
const READOUTS = ['60 Hz', '320×180', '< 8.3 ms']

// The examples the reel plays, one after another.
const REEL: FeaturedId[] = ['caverun', 'cryptquest', 'starburst', 'fountain']
const REEL_MS = 6000

// The code the home page shows, a tab per snippet under apps/website/snippets/.
const CODE: { id: Snippet['id']; file: string }[] = [
  { id: 'game', file: 'game.go' },
  { id: 'loop', file: 'loop.go' },
  { id: 'sprite', file: 'sprite.go' },
  { id: 'sound', file: 'sound.go' },
]

const STAGE_PACKAGES = [
  ['game.Loop', 'sprite', 'tilemap', 'scene', 'physics', 'particles'],
  ['scene3d', 'render3d', 'gltf', 'anim3d', 'physics3d', 'math3d'],
]

export function Home() {
  const lang = useLang()
  const copy = copies[lang]
  const home = copy.home
  return (
    <>
      <section className="container hero" aria-labelledby="home-title">
        <div className="hero__top">
          <div className="hero__title rise">
            <span className="pill">
              <span className="pill__tag">Go 1.27+</span>
              {home.eyebrow}
            </span>
            <h1 id="home-title">
              <span>{home.title[0]}</span>
              <span>{home.title[1]}</span>
            </h1>
          </div>
          <div className="hero__side rise-2">
            <p className="hero__lead">{home.lead}</p>
            <div className="hero__actions">
              <LocaleLink page="doc" slug="getting-started" lang={lang} className="btn btn--primary btn--large">
                {home.start}
                <Icon name="arrow" size={16} className="arrow" />
              </LocaleLink>
              <LocaleLink page="playground" lang={lang} className="btn btn--secondary btn--large">
                <Icon name="play" size={13} />
                {home.playground}
              </LocaleLink>
            </div>
            <TypedCommand command={NEW_GAME} />
          </div>
        </div>
        <Reel lang={lang} />
      </section>

      <section className="container facts" aria-label={home.platforms}>
        <div className="facts__platforms">
          <span>{home.platforms}</span>
          <ul>
            {PLATFORMS.map((p) => (
              <li key={p}>{p}</li>
            ))}
          </ul>
        </div>
        <div className="facts__numbers">
          {READOUTS.map((value, i) => (
            <div key={value}>
              <strong>{value}</strong>
              <span>{home.readouts[i]}</span>
            </div>
          ))}
        </div>
        <p className="facts__consoles">{home.consoles}</p>
      </section>

      <section className="container section reveal" aria-labelledby="home-examples">
        <div className="section__head">
          <div className="section__intro">
            <h2 id="home-examples">{home.examplesTitle}</h2>
            <p className="section__lead">{home.examplesLead}</p>
          </div>
          <LocaleLink page="examples" lang={lang} className="link-arrow">
            {home.allExamples}
            <Icon name="arrow" size={16} />
          </LocaleLink>
        </div>
        <div className="tiles">
          <Tile id="cryptquest" lang={lang} size="tile--wide tile--tall" />
          <Tile id="starburst" lang={lang} size="tile--narrow tile--tall" />
          <Tile id="fountain" lang={lang} size="tile--wide tile--short" />
          <Tile id="slidegem" lang={lang} size="tile--narrow tile--short" />
        </div>
        <p className="more-examples">
          <span>
            <a href={repoTreeUrl('examples/fieldmedic')} target="_blank" rel="noopener noreferrer">
              Field Medic
            </a>{' '}
            · {home.fieldmedic}
          </span>
          <span>
            <LocaleLink page="tutorial" lang={lang}>
              Star Catcher
            </LocaleLink>{' '}
            · {home.starCatcher}
          </span>
        </p>
      </section>

      <section className="container section split reveal" aria-labelledby="home-how">
        <div className="split__copy">
          <h2 id="home-how">{home.howTitle}</h2>
          <ol className="how">
            {home.how.map((item, i) => (
              <li key={item.title}>
                <span className="how__icon">
                  <span className="px sprite-icon" style={{ backgroundPosition: `${-32 * i}px 0` }} aria-hidden="true" />
                </span>
                <div>
                  <h3>{item.title}</h3>
                  <p>{item.body}</p>
                </div>
              </li>
            ))}
          </ol>
        </div>
        <figure className="hud-figure">
          <div className="hud-figure__frame">
            <img className="pixelated" src={mediaUrl('media/caverun-still.webp')} alt="Cave Run" loading="lazy" />
            {HUD_BOXES.map((box) => (
              <span key={box.left} className="hud-figure__box" style={box} aria-hidden="true" />
            ))}
          </div>
          <figcaption>
            <span>
              <span className="legend-hud" aria-hidden="true" />
              {home.hudNote}
            </span>
            <span>
              <span className="legend-canvas" aria-hidden="true" />
              {home.canvasNote}
            </span>
          </figcaption>
        </figure>
      </section>

      <section className="container section reveal" aria-labelledby="home-stages">
        <div className="section__intro">
          <h2 id="home-stages">{home.stagesTitle}</h2>
          <p className="section__lead">{home.stagesLead}</p>
        </div>
        <div className="stages">
          {home.stages.map((stage, i) => (
            <article key={stage.status} className="stage-card">
              <div className="stage-card__art">
                {i === 0 ? (
                  <img src={mediaUrl('media/caverun-still.webp')} alt="" loading="lazy" />
                ) : (
                  <>
                    <div className="stage-card__grid" aria-hidden="true" />
                    <div className="stage-card__stage" aria-hidden="true">
                      <div className="cube">
                        <span />
                        <span />
                        <span />
                        <span />
                        <span />
                        <span />
                      </div>
                    </div>
                  </>
                )}
                <span className="stage-card__status">{stage.status}</span>
              </div>
              <div className="stage-card__body">
                <h3>{i === 0 ? '2D' : '3D'}</h3>
                <p>{stage.body}</p>
                <ul className="tags">
                  {STAGE_PACKAGES[i]?.map((pkg) => (
                    <li key={pkg} className="tag">
                      {pkg}
                    </li>
                  ))}
                </ul>
                {i === 1 && (
                  <LocaleLink page="examples" lang={lang} hash="lantern3d" className="stage-card__link">
                    <Icon name="play" size={12} />
                    {copy.nav.examples} · Lantern 3D
                  </LocaleLink>
                )}
              </div>
            </article>
          ))}
        </div>
      </section>

      <section className="container section split split--code reveal" aria-labelledby="home-code">
        <div className="split__copy">
          <h2 id="home-code">{home.codeTitle}</h2>
          <p className="section__lead">{home.codeLead}</p>
          <ul className="checks">
            {home.codePoints.map((point) => (
              <li key={point}>
                <Icon name="check" size={18} />
                {point}
              </li>
            ))}
          </ul>
        </div>
        <CodePanel />
      </section>

      <PackageGroups lang={lang} />

      <section className="container section reveal" aria-labelledby="home-start">
        <h2 id="home-start">{home.startTitle}</h2>
        <ol className="steps">
          {[NEW_GAME, 'cd mygame && go tool nsgo dev', 'go tool nsgo dev -target web'].map((command, i) => (
            <li key={command}>
              <span className="steps__title">
                <span className="steps__n">{i + 1}</span>
                {home.steps[i]}
              </span>
              <code>{command}</code>
            </li>
          ))}
        </ol>
        <div className="start__notes">
          <span>{home.requirements}</span>
          <span>
            {home.migrate}
            <LocaleLink page="doc" slug="from-unity" lang={lang}>
              Unity
            </LocaleLink>
            <LocaleLink page="doc" slug="from-godot" lang={lang}>
              Godot
            </LocaleLink>
            <LocaleLink page="doc" slug="from-ebitengine" lang={lang}>
              Ebitengine
            </LocaleLink>
          </span>
        </div>
      </section>

      <section className="container finale" aria-labelledby="home-finale">
        <div className="finale__cave cave reveal">
          <div className="finale__copy">
            <span className="px sprite-gem hover" aria-hidden="true" />
            <h2 id="home-finale">{home.finalTitle}</h2>
            <p>{home.finalLead}</p>
            <div className="finale__actions">
              <LocaleLink page="doc" slug="getting-started" lang={lang} className="btn btn--primary btn--large">
                {home.finalStart}
                <Icon name="arrow" size={16} className="arrow" />
              </LocaleLink>
              <LocaleLink page="roadmap" lang={lang} className="btn btn--glass btn--large">
                {home.finalRoadmap}
              </LocaleLink>
            </div>
          </div>
          <div className="finale__scene" aria-hidden="true">
            <span className="px sprite-crystal wide-only" style={{ left: '6%', bottom: 96 }} />
            <span className="px sprite-crystal wide-only" style={{ right: '22%', bottom: 96, transform: 'scaleX(-1)' }} />
            <span className="px sprite-portal" style={{ right: '6%', bottom: 96 }} />
            <span className="finale__mover" style={{ bottom: 170, animationDelay: '-1.5s' }}>
              <span className="px sprite-gem" />
            </span>
            <span className="finale__mover" style={{ bottom: 150, animationDelay: '-4.2s' }}>
              <span className="px sprite-gem" />
            </span>
            <span className="finale__mover" style={{ bottom: 96 }}>
              <span className="px sprite-slime" />
            </span>
            <span className="finale__hop" style={{ left: '18%', bottom: 96 }}>
              <span className="px sprite-run" />
            </span>
            <div className="finale__ground ground ground--scroll" />
          </div>
        </div>
      </section>
    </>
  )
}

// Where Cave Run's HUD views sit over its canvas in the still, in percent of the picture.
const HUD_BOXES: CSSProperties[] = [
  { left: '0.4%', top: '0.6%', width: '12%', height: '6%' },
  { left: '48.5%', top: '0.6%', width: '9.5%', height: '6%' },
  { left: '0.6%', top: '90.5%', width: '9.5%', height: '8.5%' },
  { left: '93%', top: '90.5%', width: '6.4%', height: '8.5%' },
]

// TypedCommand types the command for a new game into its box once the page is up, unless less motion is asked for; without script it shows it whole.
function TypedCommand({ command }: { command: string }) {
  const copy = copies[useLang()]
  const [typed, setTyped] = useState(command.length)
  const [copied, setCopied] = useState(false)
  const timer = useRef<number | undefined>(undefined)

  useLayoutEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    setTyped(0)
    const id = window.setInterval(() => {
      setTyped((n) => {
        if (n + 1 >= command.length) window.clearInterval(id)
        return n + 1
      })
    }, 32)
    return () => window.clearInterval(id)
  }, [command])

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const write = async () => {
    try {
      await navigator.clipboard.writeText(command)
    } catch {
      // The command can still be selected.
      return
    }
    setCopied(true)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setCopied(false), 1600)
  }

  return (
    <div className="command">
      <code>
        <span className="command__prompt" aria-hidden="true">
          ${' '}
        </span>
        <span className="sr-only">{command}</span>
        <span aria-hidden="true">{command.slice(0, typed)}</span>
        <span className="caret" aria-hidden="true">
          ▍
        </span>
      </code>
      <button type="button" className="command__copy" onClick={write} aria-live="polite">
        {copied ? copy.ui.copied : copy.ui.copy}
      </button>
    </div>
  )
}

// Reel plays the examples' recordings in turn in a wide viewport, the one showing named under it with a button to play it in the playground; picking a name shows it and starts the turn again.
function Reel({ lang }: { lang: Lang }) {
  const copy = copies[lang]
  const [active, setActive] = useState(0)
  // cycle restarts the active tab's progress bar each time the reel moves.
  const [cycle, setCycle] = useState(0)
  const [playing, setPlaying] = useState(false)

  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    setPlaying(true)
    const id = window.setTimeout(() => {
      setActive((a) => (a + 1) % REEL.length)
      setCycle((c) => c + 1)
    }, REEL_MS)
    return () => window.clearTimeout(id)
  }, [cycle])

  const pick = (i: number) => {
    setActive(i)
    setCycle((c) => c + 1)
  }

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    event.preventDefault()
    const next = (active + step + REEL.length) % REEL.length
    pick(next)
    document.getElementById(`reel-tab-${next}`)?.focus()
  }

  const current = featured(REEL[active]!)
  return (
    <div className={playing ? 'reel reel--playing rise-big' : 'reel rise-big'} style={{ '--reel-ms': `${REEL_MS}ms` } as CSSProperties}>
      <div className="reel__viewport" id="reel-panel" role="tabpanel" aria-labelledby={`reel-tab-${active}`}>
        {REEL.map((id, i) => (
          <ExampleMedia key={id} example={featured(id)} className={i === active ? 'reel__media is-on' : 'reel__media'} playing={i === active} eager={i === 0} />
        ))}
        <span className="reel__shade" aria-hidden="true" />
        <div className="reel__caption">
          <div className="reel__name">
            <span className="live">
              <span className="live__dot" aria-hidden="true" />
              {copy.home.live}
            </span>
            <strong>{current.title}</strong>
          </div>
          <PlayLink id={current.id} lang={lang} />
        </div>
      </div>
      <div className="reel__tabs" role="tablist" aria-label={copy.nav.examples} onKeyDown={onKey}>
        {REEL.map((id, i) => (
          <button
            key={id}
            id={`reel-tab-${i}`}
            type="button"
            role="tab"
            className="reel__tab"
            aria-selected={i === active}
            aria-controls="reel-panel"
            tabIndex={i === active ? 0 : -1}
            onClick={() => pick(i)}
          >
            {/* Keyed by the turn, so the bar under the example showing fills again from the start. */}
            <span className="reel__bar" aria-hidden="true">
              <span key={i === active ? cycle : 'idle'} />
            </span>
            <span className="reel__tab-name">
              <span>0{i + 1}</span>
              <strong>{featured(id).title}</strong>
            </span>
            <span className="reel__genre">{copy.genres[id]}</span>
          </button>
        ))}
      </div>
    </div>
  )
}

// PlayLink plays an example in the playground, or shows it on the examples page when the playground does not build it.
function PlayLink({ id, lang }: { id: FeaturedId; lang: Lang }) {
  const copy = copies[lang]
  const body = (
    <>
      <Icon name="play" size={13} />
      {copy.examples.play}
    </>
  )
  return isPlaygroundId(id) ? (
    <LocaleLink page="playgroundExample" example={id} lang={lang} className="btn btn--primary">
      {body}
    </LocaleLink>
  ) : (
    <LocaleLink page="examples" lang={lang} hash={id} className="btn btn--primary">
      {copy.nav.examples}
      <Icon name="arrow" size={14} className="arrow" />
    </LocaleLink>
  )
}

function Tile({ id, lang, size }: { id: FeaturedId; lang: Lang; size: string }) {
  const copy = copies[lang]
  const example = featured(id)
  const body = (
    <>
      <ExampleMedia example={example} />
      <span className="tile__shade" aria-hidden="true" />
      <span className="tile__text">
        <span className="tile__genre">{copy.genres[id]}</span>
        <span className="tile__name">{example.title}</span>
        <span className="tile__desc">{copy.blurbs[id]}</span>
      </span>
    </>
  )
  return isPlaygroundId(id) ? (
    <LocaleLink page="playgroundExample" example={id} lang={lang} className={`tile ${size}`}>
      {body}
    </LocaleLink>
  ) : (
    <LocaleLink page="examples" lang={lang} hash={id} className={`tile ${size}`}>
      {body}
    </LocaleLink>
  )
}

// CodePanel shows the snippets as an editor does, a tab per file.
function CodePanel() {
  const copy = copies[useLang()]
  const [selected, setSelected] = useState(0)
  const html = new Map(snippets.map((s) => [s.id, s.html]))
  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    event.preventDefault()
    const next = (selected + step + CODE.length) % CODE.length
    setSelected(next)
    document.getElementById(`code-tab-${next}`)?.focus()
  }
  const current = CODE[selected]!
  return (
    <div className="code-panel">
      <div className="editor__tabs" role="tablist" aria-label={copy.home.codeTitle} onKeyDown={onKey}>
        {CODE.map((file, i) => (
          <button
            key={file.id}
            id={`code-tab-${i}`}
            type="button"
            role="tab"
            className="editor__tab"
            aria-selected={i === selected}
            aria-controls="code-panel"
            tabIndex={i === selected ? 0 : -1}
            onClick={() => setSelected(i)}
          >
            {file.file}
          </button>
        ))}
      </div>
      <div className="editor__panel" id="code-panel" role="tabpanel" aria-labelledby={`code-tab-${selected}`} tabIndex={0} dangerouslySetInnerHTML={{ __html: html.get(current.id) ?? '' }} />
    </div>
  )
}

// PackageGroups lists every package of the catalog under its group, each linking to its guide.
function PackageGroups({ lang }: { lang: Lang }) {
  const copy = copies[lang]
  const groups = CATEGORY_ORDER.map((category: PackageCategory, i) => ({ category, name: copy.categories[i] ?? category, items: site.packages.filter((p) => p.category === category) }))
  return (
    <section className="container section reveal" aria-labelledby="home-packages">
      <div className="section__head">
        <div className="section__intro">
          <h2 id="home-packages">{count(copy.home.packagesTitle, site.packages.length)}</h2>
          <p className="section__lead">{copy.home.packagesLead}</p>
        </div>
        <LocaleLink page="packages" lang={lang} className="link-arrow">
          {copy.home.packagesLink}
          <Icon name="arrow" size={16} />
        </LocaleLink>
      </div>
      <div className="package-groups">
        {groups.map((group) => (
          <div key={group.category} className="package-group">
            <div className="package-group__head">
              <strong>{group.name}</strong>
              <span>{group.items.length}</span>
            </div>
            <ul>
              {group.items.map((pkg) => (
                <li key={pkg.path} title={pkg.name}>
                  <PackageLink pkg={pkg} lang={lang}>
                    {pkg.name}
                  </PackageLink>
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
    </section>
  )
}
