import { Link } from '@tanstack/react-router'
import { copies } from '../copy'
import { routes, useLang } from '../i18n'
import { SpeedLines } from './Brand'
import { Icon } from './Icon'

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
        <Link className="btn btn--secondary" to={routes[lang].home}>
          {copy.nav.home}
          <Icon name="arrow" size={14} />
        </Link>
        <Link className="btn btn--secondary" to={routes[lang].docs}>
          {copy.nav.docs}
          <Icon name="arrow" size={14} />
        </Link>
      </nav>
    </section>
  )
}
