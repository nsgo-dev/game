import { Link } from '@tanstack/react-router'
import { SpeedLines } from './Brand'
import { Icon } from './Icon'

// nsgo.dev's 404: the mark's speed lines, a pill and the way back.
export function NotFound() {
  return (
    <section className="placeholder" aria-labelledby="not-found-title">
      <SpeedLines className="placeholder__lines" size={56} />
      <span className="pill">404</span>
      <h1 id="not-found-title">Page not found</h1>
      <p className="placeholder__lead">Nothing is at this address.</p>
      <nav className="placeholder__actions" aria-label="Where to go">
        <Link className="btn btn--secondary" to="/">
          Home
          <Icon name="arrow" size={14} />
        </Link>
        <Link className="btn btn--secondary" to="/docs/">
          Docs
          <Icon name="arrow" size={14} />
        </Link>
      </nav>
    </section>
  )
}
