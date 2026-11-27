import type { CSSProperties } from 'react'
import { site } from 'virtual:site'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import { copies } from '../copy'
import { type Lang, pageHead, useLang } from '../i18n'
import { repoTreeUrl } from '../links'
import { docsLang } from './Packages'

// The tutorial's own page: its title and introduction, its chapters and the files every chapter keeps, all read from docs/<lang>/tutorial.md at build time.

export const tutorialHead = (lang: Lang) => {
  const tutorial = site.tutorial[docsLang(lang)]
  return pageHead(lang, 'tutorial/', `${tutorial.title} — NSGo Game`, tutorial.intro[0] ?? '')
}

// Where the stars fall from in the picture beside the title, and when.
const FALLERS: CSSProperties[] = [
  { left: '12%', animationDelay: '0s', '--still': '40px' } as CSSProperties,
  { left: '32%', animationDelay: '1.1s', '--still': '150px' } as CSSProperties,
  { left: '52%', animationDelay: '0.5s', '--still': '20px' } as CSSProperties,
  { left: '72%', animationDelay: '1.7s', '--still': '90px' } as CSSProperties,
  { left: '88%', animationDelay: '0.9s', '--still': '30px' } as CSSProperties,
]

export function Tutorial() {
  const lang = useLang()
  const copy = copies[lang]
  const own = docsLang(lang)
  const tutorial = site.tutorial[own]
  // A language without docs of its own shows the English text, marked as English.
  const textLang = own === 'en' && lang !== 'en' ? 'en' : undefined
  const first = tutorial.chapters[0]
  return (
    <div className="container">
      <section className="tutorial-hero rise" aria-labelledby="tutorial-title">
        <div className="tutorial-hero__copy">
          <span className="eyebrow">{copy.tutorial.eyebrow}</span>
          <h1 id="tutorial-title" className="display-1" lang={textLang}>
            {tutorial.title}
          </h1>
          {tutorial.intro[0] && (
            <p className="lead" lang={textLang}>
              {tutorial.intro[0]}
            </p>
          )}
          {tutorial.intro[1] && <p lang={textLang}>{tutorial.intro[1]}</p>}
          <div className="hero__actions">
            {first && (
              <LocaleLink page="doc" slug={first.slug} lang={lang} className="btn btn--primary btn--large">
                {copy.tutorial.start}
                <Icon name="arrow" size={16} className="arrow" />
              </LocaleLink>
            )}
            <a className="btn btn--secondary btn--large" href={repoTreeUrl('examples/tutorial')} target="_blank" rel="noopener noreferrer">
              {copy.tutorial.source}
            </a>
          </div>
        </div>
        <div className="tutorial-hero__art cave" aria-hidden="true">
          {FALLERS.map((style) => (
            <span key={String(style.left)} className="faller" style={style}>
              <span className="px sprite-gem" />
            </span>
          ))}
          <span className="hover" style={{ position: 'absolute', left: '44%', bottom: 96 }}>
            <span className="px sprite-run" />
          </span>
          <div className="ground" style={{ position: 'absolute', right: 0, bottom: 0, left: 0, height: 96 }} />
        </div>
      </section>

      <section className="chapters reveal" aria-labelledby="tutorial-title">
        <ol>
          {tutorial.chapters.map((chapter) => (
            <li key={chapter.n}>
              <span className="chapters__n" aria-hidden="true">
                {String(chapter.n).padStart(2, '0')}
              </span>
              <div className="chapters__text" lang={textLang}>
                <LocaleLink page="doc" slug={chapter.slug} lang={lang}>
                  {chapter.title}
                </LocaleLink>
                <span>{chapter.adds}</span>
              </div>
              <div className="chapters__meta">
                <span className="tag">{chapter.packages}</span>
                <a href={repoTreeUrl(`examples/tutorial/${chapter.dir}`)} target="_blank" rel="noopener noreferrer">
                  {chapter.dir}
                </a>
              </div>
            </li>
          ))}
        </ol>
      </section>

      {tutorial.parts.length > 0 && (
        <section className="parts reveal" aria-labelledby="tutorial-parts">
          <h2 id="tutorial-parts" className="display-2" lang={textLang}>
            {tutorial.partsTitle}
          </h2>
          <div className="parts__grid" lang={textLang}>
            {tutorial.parts.map((part) => (
              <div key={part.file}>
                <strong>{part.file}</strong>
                <span>{part.text}</span>
              </div>
            ))}
          </div>
          <div className="parts__run">
            <span>{copy.tutorial.run}</span>
            <pre className="terminal">
              <span>$ </span>cd examples/tutorial{'\n'}
              <span>$ </span>go tool nsgo dev ./ch05-rules{'\n'}
              <span>$ </span>go tool nsgo test ./...
            </pre>
          </div>
        </section>
      )}
    </div>
  )
}
