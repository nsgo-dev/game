import { type KeyboardEvent, useEffect, useRef, useState } from 'react'
import { examples, sources } from 'virtual:playground'
import { CopyCommand } from '../components/CopyCommand'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import { PLAYGROUND_BUILDS, type PlaygroundId } from '../content/playground'
import { SHOWCASE_ENTRIES, type ShowcaseEntry } from '../content/showcase'
import type { PlaygroundExample, PlaygroundSource } from '../content/types'
import { type Copy, copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { GITHUB_URL } from '../links'

// The playground runs each example's web build, made ahead by scripts/build-playground.ts, in a frame beside its source. Nothing is compiled in the browser: to change an example, its page shows how to run it with the dev server, which rebuilds and reloads it on each save.

type PlaygroundCopy = Copy['playground']

export function findPlaygroundExample(id: string): PlaygroundExample | undefined {
  return examples.find((example) => example.id === id)
}

// An example's title and description are its showcase entry's.
function entryOf(id: PlaygroundId): ShowcaseEntry {
  const entry = SHOWCASE_ENTRIES.find((e) => e.id === id)
  if (!entry) throw new Error(`playground: ${id} has no showcase entry`)
  return entry
}

export const playgroundHead = (lang: Lang) => pageHead(lang, 'playground/', copies[lang].playground.title, copies[lang].playground.description)

export function playgroundExampleHead(lang: Lang, id: PlaygroundId | undefined) {
  if (!id) return {}
  const entry = entryOf(id)
  return pageHead(lang, `playground/${id}/`, copies[lang].playground.exampleTitle(entry.title[lang]), entry.description[lang])
}

// buildUrl is the address of an example's web build, the page nsgo build wrote for it.
const buildUrl = (id: PlaygroundId) => `${import.meta.env.BASE_URL}${PLAYGROUND_BUILDS}/${id}/`

const sourceUrl = (id: PlaygroundId, file: string) => `${GITHUB_URL}/blob/main/examples/${id}/${file}`

const totalLines = (example: PlaygroundExample) => example.files.reduce((sum, file) => sum + file.lines, 0)

export function PlaygroundIndex() {
  const lang = useLang()
  const copy = copies[lang].playground
  return (
    <div className="container playground">
      <header className="page-head">
        <h1>{copy.heading}</h1>
        <p>{copy.intro}</p>
      </header>
      <ol className="showcase-grid playground-grid" aria-label={copy.examples}>
        {examples.map((example) => {
          const entry = entryOf(example.id)
          return (
            <li key={example.id} className="showcase-card hud">
              <div className="showcase-card__top">
                <span className="hud-label">{copy.files(example.files.length)}</span>
                <span className="chip chip--cleared">Web</span>
              </div>
              <h2>
                <LocaleLink page="playgroundExample" example={example.id} lang={lang}>
                  {entry.title[lang]}
                </LocaleLink>
              </h2>
              <p className="showcase-card__desc">{entry.description[lang]}</p>
              <ul className="showcase-card__tags">
                {entry.tags.map((tag) => (
                  <li key={tag}>{tag}</li>
                ))}
              </ul>
              <div className="showcase-card__foot">
                <span className="showcase-card__meta">{copy.lines(totalLines(example))}</span>
                <div className="showcase-card__links">
                  <LocaleLink page="playgroundExample" example={example.id} lang={lang}>
                    {copy.open}
                    <Icon name="arrow" size={12} />
                  </LocaleLink>
                </div>
              </div>
            </li>
          )
        })}
      </ol>
    </div>
  )
}

export function PlaygroundExamplePage({ example }: { example: PlaygroundExample }) {
  const lang = useLang()
  const copy = copies[lang].playground
  const entry = entryOf(example.id)
  const name = entry.title[lang]
  return (
    <div className="container playground">
      <nav className="playground__examples" aria-label={copy.examples}>
        <LocaleLink page="playground" lang={lang} className="playground__home" activeOptions={{ exact: true }}>
          {copy.heading}
        </LocaleLink>
        {examples.map((other) => (
          <LocaleLink key={other.id} page="playgroundExample" example={other.id} lang={lang} activeProps={{ 'aria-current': 'page' }}>
            {entryOf(other.id).title[lang]}
          </LocaleLink>
        ))}
      </nav>
      <header className="page-head">
        <h1>{name}</h1>
        <p>{entry.description[lang]}</p>
      </header>
      {/* Keyed by the example, so moving to another starts with its own stopped game and first file. */}
      <div className="playground__body" key={example.id}>
        <Stage example={example} name={name} copy={copy} />
        <SourcePane example={example} copy={copy} />
      </div>
      <section className="playground__local" aria-labelledby="playground-local-title">
        <h2 id="playground-local-title">{copy.local.title}</h2>
        <p>{copy.local.body}</p>
        <div className="playground__commands">
          <CopyCommand command={`git clone ${GITHUB_URL}.git`} />
          <CopyCommand command={`cd game/examples/${example.id}`} />
          <CopyCommand command="go tool nsgo dev -target web" />
        </div>
      </section>
    </div>
  )
}

// Stage loads the game only when asked, since its WebAssembly is megabytes, and restarts it by loading its frame again.
function Stage({ example, name, copy }: { example: PlaygroundExample; name: string; copy: PlaygroundCopy }) {
  // run counts the starts, 0 before the first; each remounts the frame.
  const [run, setRun] = useState(0)
  const frame = useRef<HTMLIFrameElement>(null)
  const url = buildUrl(example.id)
  const megabytes = (example.wasm / 1_000_000).toFixed(1)
  // A game reads the keyboard only while its frame has the focus.
  const focus = () => {
    frame.current?.focus()
    frame.current?.contentWindow?.focus()
  }

  return (
    <section className="playground__stage hud" aria-label={name}>
      <div className="playground__screen">
        {run > 0 ? (
          <iframe key={run} ref={frame} className="playground__frame" src={url} title={copy.running(name)} allow="autoplay; fullscreen; gamepad" onLoad={focus} />
        ) : example.built ? (
          <div className="playground__poster">
            <button type="button" className="btn btn--primary btn--large" onClick={() => setRun(1)}>
              <Icon name="play" size={14} />
              {copy.run}
            </button>
            <span className="playground__size">{copy.download(megabytes)}</span>
          </div>
        ) : (
          <div className="playground__poster">
            <p>{copy.notBuilt}</p>
            <CopyCommand command={`bun run playground ${example.id}`} />
          </div>
        )}
      </div>
      <div className="playground__bar">
        <span className="playground__hint">{run > 0 ? copy.focus : ''}</span>
        <div className="playground__actions">
          {run > 0 && (
            <button type="button" className="btn btn--secondary" onClick={() => setRun((n) => n + 1)}>
              {copy.restart}
            </button>
          )}
          {example.built && (
            <a className="btn btn--secondary" href={url} target="_blank" rel="noopener">
              {copy.newTab}
              <Icon name="external" size={14} />
            </a>
          )}
        </div>
      </div>
    </section>
  )
}

// fileFromHash is the file name an address's fragment names. A shared link can carry a fragment that is not valid percent-encoding, such as #%, and that names the fragment as written rather than failing the page.
function fileFromHash(hash: string): string {
  const raw = hash.slice(1)
  try {
    return decodeURIComponent(raw)
  } catch {
    return raw
  }
}

// SourcePane shows the example's files as the home page's editor does, a tab each, loading their highlighted text once the page is up. The file shown is the address's fragment, such as #game.go, so a link can open the page on it.
function SourcePane({ example, copy }: { example: PlaygroundExample; copy: PlaygroundCopy }) {
  const [source, setSource] = useState<PlaygroundSource | null>(null)
  const [selected, setSelected] = useState(0)
  const tabs = useRef<(HTMLButtonElement | null)[]>([])
  const files = example.files

  useEffect(() => {
    let live = true
    void sources[example.id]().then((loaded) => {
      if (live) setSource(loaded)
    })
    const fromHash = files.findIndex((file) => file.name === fileFromHash(window.location.hash))
    if (fromHash >= 0) setSelected(fromHash)
    return () => {
      live = false
    }
  }, [example.id, files])

  const select = (index: number) => {
    setSelected(index)
    const file = files[index]
    // The router's state stays as it is: only the fragment changes, without a history entry or a scroll.
    if (file) window.history.replaceState(window.history.state, '', `#${encodeURIComponent(file.name)}`)
  }

  const onKey = (event: KeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0
    if (!step) return
    event.preventDefault()
    const next = (selected + step + files.length) % files.length
    select(next)
    tabs.current[next]?.focus()
  }

  const current = files[selected]
  const html = source?.files.find((file) => file.name === current?.name)?.html
  return (
    <section className="editor hud playground__source" aria-label={copy.source}>
      <div className="editor__tabs" role="tablist" aria-label={copy.source} onKeyDown={onKey}>
        {files.map((file, index) => (
          <button
            key={file.name}
            ref={(element) => {
              tabs.current[index] = element
            }}
            type="button"
            role="tab"
            id={`file-tab-${index}`}
            className="editor__tab"
            aria-selected={index === selected}
            aria-controls="file-panel"
            tabIndex={index === selected ? 0 : -1}
            onClick={() => select(index)}
          >
            {file.name}
          </button>
        ))}
      </div>
      {/* Focusable, so the keyboard scrolls its code. */}
      <div className="editor__panel playground__code" role="tabpanel" id="file-panel" aria-labelledby={`file-tab-${selected}`} aria-busy={!html} tabIndex={0}>
        {html ? <div dangerouslySetInnerHTML={{ __html: html }} /> : <p className="playground__loading">{copy.loading}</p>}
      </div>
      {current && (
        <div className="playground__file">
          <span>{copy.lines(current.lines)}</span>
          <a href={sourceUrl(example.id, current.name)} target="_blank" rel="noopener noreferrer">
            {copy.github}
            <Icon name="external" size={12} />
          </a>
        </div>
      )}
    </section>
  )
}
