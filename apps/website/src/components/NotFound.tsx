import { copies } from '../copy'
import { useLang } from '../i18n'
import { SpeedLines } from './Brand'
import { Icon } from './Icon'
import { LocaleLink } from './LocaleLink'

// nsgo.dev's 404: the mark's speed lines, a pill and the way back, in the language of the address.
export function NotFound() {
  const lang = useLang()
  const copy = copies[lang]
  return (
    <section className="placeholder" aria-labelledby="not-found-title">
      <SpeedLines className="placeholder__lines" size={56} />
      <span className="pill">404</span>
      <h1 id="not-found-title">{copy.notFound.heading}</h1>
      <p className="placeholder__lead">{copy.notFound.body}</p>
      <nav className="placeholder__actions" aria-label={copy.notFound.where}>
        <LocaleLink page="home" lang={lang} className="btn btn--secondary">
          {copy.nav.home}
          <Icon name="arrow" size={14} />
        </LocaleLink>
        <LocaleLink page="docs" lang={lang} className="btn btn--secondary">
          {copy.nav.docs}
          <Icon name="arrow" size={14} />
        </LocaleLink>
      </nav>
    </section>
  )
}
