import { site } from 'virtual:site'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import { ExampleMedia } from '../components/Media'
import { FEATURED, type FeaturedExample } from '../content/featured'
import { isPlaygroundId } from '../content/playground'
import { copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { repoTreeUrl } from '../links'

// The example games, each with its recording, what it is, the packages it imports (read from its source at build time), how to run it and where to play it.

export const examplesHead = (lang: Lang) => pageHead(lang, 'examples/', `${copies[lang].examples.title} — NSGo Game`, copies[lang].examples.lead)

export function Examples() {
  const lang = useLang()
  const copy = copies[lang]
  return (
    <div className="container">
      <header className="page-hero rise">
        <span className="px sprite-gem" aria-hidden="true" />
        <h1 className="display-1">{copy.examples.title}</h1>
        <p>{copy.examples.lead}</p>
      </header>
      <div className="showcase-list">
        {FEATURED.map((example, i) => (
          <Feature key={example.id} example={example} index={i} lang={lang} />
        ))}
      </div>
      <section className="more-showcase" aria-labelledby="examples-showcase">
        <div>
          <h2 id="examples-showcase">{copy.showcase.heading}</h2>
          <p>{copy.showcase.intro}</p>
        </div>
        <LocaleLink page="showcase" lang={lang} className="btn btn--secondary">
          {copy.showcase.heading}
          <Icon name="arrow" size={16} className="arrow" />
        </LocaleLink>
      </section>
    </div>
  )
}

function Feature({ example, index, lang }: { example: FeaturedExample; index: number; lang: Lang }) {
  const copy = copies[lang]
  const playable = isPlaygroundId(example.id)
  const media = (
    <>
      <ExampleMedia example={example} />
      {example.id === 'lantern3d' && <span className="feature__caption">{copy.examples.lanternCaption}</span>}
    </>
  )
  const titleId = `example-${example.id}`
  return (
    <article id={example.id} className="feature reveal" aria-labelledby={titleId}>
      {playable ? (
        <LocaleLink page="playgroundExample" example={example.id} lang={lang} className="feature__media" aria-label={`${copy.examples.play}: ${example.title}`}>
          {media}
        </LocaleLink>
      ) : (
        <div className="feature__media">{media}</div>
      )}
      <div className="feature__text">
        <div className="feature__kicker">
          <span>
            {String(index + 1).padStart(2, '0')} / {String(FEATURED.length).padStart(2, '0')}
          </span>
          <span>{copy.genres[example.id]}</span>
        </div>
        <h2 id={titleId}>{example.title}</h2>
        <p className="feature__desc">{copy.blurbs[example.id]}</p>
        <div className="feature__block">
          <span className="feature__label">{copy.examples.used}</span>
          <ul className="tags">
            {site.featured[example.id].map((pkg) => (
              <li key={pkg} className="tag">
                {pkg === 'game' ? 'game' : pkg.slice('game/'.length)}
              </li>
            ))}
          </ul>
        </div>
        <div className="feature__block">
          <span className="feature__label">{copy.examples.run}</span>
          <code className="feature__run">
            <span>$ </span>cd examples/{example.id} && go tool nsgo dev -target web
          </code>
        </div>
        <div className="feature__actions">
          {playable && (
            <LocaleLink page="playgroundExample" example={example.id} lang={lang} className="btn btn--primary">
              <Icon name="play" size={13} />
              {copy.examples.play}
            </LocaleLink>
          )}
          <a className="btn btn--secondary" href={repoTreeUrl(`examples/${example.id}`)} target="_blank" rel="noopener noreferrer">
            {copy.examples.source}
            <Icon name="external" size={14} />
          </a>
        </div>
      </div>
    </article>
  )
}
