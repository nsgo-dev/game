import { copies } from '../copy'
import { useLang } from '../i18n'
import { GITHUB_URL } from '../links'
import { Logo } from './Brand'
import { LanguageSwitch } from './LanguageSwitch'
import { Search } from './Search'
import { LocaleLink } from './LocaleLink'

// The header: the mark and the name, the site's pages, then the language, GitHub and the way in. Narrow screens show the pages as a row of their own under the bar.
export function Header() {
  const lang = useLang()
  const copy = copies[lang].nav
  const current = { 'aria-current': 'page' } as const
  const pages = (
    <>
      <LocaleLink page="docs" lang={lang} activeProps={current}>
        {copy.docs}
      </LocaleLink>
      <LocaleLink page="tutorial" lang={lang} activeProps={current}>
        {copy.tutorial}
      </LocaleLink>
      <LocaleLink page="packages" lang={lang} activeProps={current}>
        {copy.packages}
      </LocaleLink>
      <LocaleLink page="examples" lang={lang} activeProps={current}>
        {copy.examples}
      </LocaleLink>
      <LocaleLink page="playground" lang={lang} activeProps={current}>
        {copy.playground}
      </LocaleLink>
      <LocaleLink page="roadmap" lang={lang} activeProps={current}>
        {copy.roadmap}
      </LocaleLink>
    </>
  )
  return (
    <header className="header" id="top">
      <div className="container header__bar">
        <div className="header__left">
          <LocaleLink page="home" lang={lang} aria-label="NSGo Game">
            <Logo />
          </LocaleLink>
          <nav className="header__nav" aria-label={copy.primary}>
            {pages}
          </nav>
        </div>
        <div className="header__right">
          <Search />
          <LanguageSwitch />
          <a className="header__github" href={GITHUB_URL} target="_blank" rel="noopener noreferrer">
            {copy.github}
          </a>
          <LocaleLink page="doc" slug="getting-started" lang={lang} className="btn btn--primary header__cta">
            {copy.getStarted}
          </LocaleLink>
        </div>
      </div>
      <nav className="header__mobile" aria-label={copy.primary}>
        {pages}
      </nav>
    </header>
  )
}
