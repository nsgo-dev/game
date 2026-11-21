import { Link } from '@tanstack/react-router'

export function NotFound() {
  return (
    <section className="not-found" aria-labelledby="not-found-title">
      <p className="not-found-code" aria-hidden="true">
        404
      </p>
      <h1 id="not-found-title">Page not found</h1>
      <p>Nothing is at this address.</p>
      <nav className="actions" aria-label="Where to go">
        <Link className="button primary" to="/">
          Home
        </Link>
        <Link className="button" to="/docs/">
          Docs
        </Link>
      </nav>
    </section>
  )
}
