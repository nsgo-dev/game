import { Eyebrow, SpeedLines } from '../components/Brand'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import { isPlaygroundId } from '../content/playground'
import { SHOWCASE_ENTRIES, type ShowcaseEntry } from '../content/showcase'
import { type Copy, copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { CONTRIBUTING_URL, SHOWCASE_SUBMIT_URL } from '../links'

export const showcaseHead = (lang: Lang) => pageHead(lang, 'showcase/', copies[lang].showcase.title, copies[lang].showcase.description)

export function Showcase() {
  const lang = useLang()
  const copy = copies[lang].showcase
  return (
    <div className="container showcase">
      <header className="page-head">
        <h1>{copy.heading}</h1>
        <p>{copy.intro}</p>
      </header>

      <ol className="showcase-grid" aria-label={copy.listLabel}>
        {SHOWCASE_ENTRIES.map((entry, index) => (
          <ShowcaseCard key={entry.id} entry={entry} index={index + 1} lang={lang} copy={copy} />
        ))}
      </ol>

      <section className="cta hud showcase__cta" aria-labelledby="showcase-submit-title">
        <SpeedLines className="cta__lines" />
        <div className="cta__copy">
          <Eyebrow>{copy.submit.eyebrow}</Eyebrow>
          <h2 id="showcase-submit-title">{copy.submit.title}</h2>
          <p>{copy.submit.body}</p>
        </div>
        <div className="cta__actions">
          <a className="btn btn--primary btn--large" href={SHOWCASE_SUBMIT_URL} target="_blank" rel="noopener noreferrer">
            <Icon name="play" size={14} />
            {copy.submit.action}
          </a>
          <a className="btn btn--secondary btn--large" href={CONTRIBUTING_URL} target="_blank" rel="noopener noreferrer">
            {copy.submit.contributing}
            <Icon name="external" size={14} />
          </a>
        </div>
      </section>
    </div>
  )
}

function ShowcaseCard({ entry, index, lang, copy }: { entry: ShowcaseEntry; index: number; lang: Lang; copy: Copy['showcase'] }) {
  const chipClass = entry.kind === 'example' ? 'chip chip--cleared' : 'chip chip--accent'
  // An example the playground runs is played on its page there, in the reader's language.
  const playground = isPlaygroundId(entry.id) ? entry.id : undefined
  return (
    <li className="showcase-card hud">
      <div className="showcase-card__top">
        <span className="hud-label">{copy.entry(index)}</span>
        <span className={chipClass}>{copy.kinds[entry.kind]}</span>
      </div>
      <h2>
        {playground ? (
          <LocaleLink page="playgroundExample" example={playground} lang={lang}>
            {entry.title[lang]}
          </LocaleLink>
        ) : (
          <a href={entry.playUrl ?? entry.sourceUrl} target="_blank" rel="noopener noreferrer">
            {entry.title[lang]}
          </a>
        )}
      </h2>
      <p className="showcase-card__desc">{entry.description[lang]}</p>
      <ul className="showcase-card__tags" aria-label={copy.tagsLabel}>
        {entry.tags.map((tag) => (
          <li key={tag}>{tag}</li>
        ))}
      </ul>
      <ul className="chips showcase-card__targets" aria-label={copy.targetsLabel}>
        {entry.targets.map((target) => (
          <li key={target}>{target}</li>
        ))}
      </ul>
      <div className="showcase-card__foot">
        <span className="showcase-card__meta">
          {copy.by} {entry.author} · {entry.license}
        </span>
        <div className="showcase-card__links">
          {playground && (
            <LocaleLink page="playgroundExample" example={playground} lang={lang}>
              {copy.play}
              <Icon name="arrow" size={12} />
            </LocaleLink>
          )}
          {!playground && entry.playUrl && (
            <a href={entry.playUrl} target="_blank" rel="noopener noreferrer">
              {copy.play}
              <Icon name="external" size={12} />
            </a>
          )}
          <a href={entry.sourceUrl} target="_blank" rel="noopener noreferrer">
            {copy.source}
            <Icon name="external" size={12} />
          </a>
        </div>
      </div>
    </li>
  )
}
