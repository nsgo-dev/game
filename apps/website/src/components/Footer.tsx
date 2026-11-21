import { Link } from '@tanstack/react-router'
import { GITHUB_URL, LICENSE_URL, NSGO_DOCS_URL, NSGO_GITHUB_URL, NSGO_URL, PLAN_URL } from '../links'
import { Logo, SpeedLines } from './Brand'
import { Icon } from './Icon'

type FooterLink = { label: string; to: '/' | '/docs/' | '/roadmap/' } | { label: string; href: string }

const COLUMNS: { title: string; links: FooterLink[] }[] = [
  {
    title: 'Project',
    links: [
      { label: 'Home', to: '/' },
      { label: 'Docs', to: '/docs/' },
      { label: 'Roadmap', to: '/roadmap/' },
    ],
  },
  {
    title: 'NSGo',
    links: [
      { label: 'Website', href: NSGO_URL },
      { label: 'Docs', href: NSGO_DOCS_URL },
      { label: 'Plan (nsgo#1047)', href: PLAN_URL },
    ],
  },
  {
    title: 'Community',
    links: [
      { label: 'GitHub', href: GITHUB_URL },
      { label: 'Issues', href: `${GITHUB_URL}/issues` },
      { label: 'NSGo on GitHub', href: NSGO_GITHUB_URL },
    ],
  },
  {
    title: 'Legal',
    links: [{ label: 'License (Apache-2.0)', href: LICENSE_URL }],
  },
]

// The call to action nsgo.dev's home page shows above its footer.
export function Cta() {
  return (
    <section className="container cta-wrap">
      <div className="cta">
        <SpeedLines className="cta__lines" />
        <div className="cta__copy">
          <h2>Make a game with NSGo</h2>
          <p>Read how a frame runs, then follow the plan on GitHub.</p>
        </div>
        <div className="cta__actions">
          <Link className="btn btn--light btn--large" to="/docs/">
            Read the docs
          </Link>
          <a className="btn btn--ghost btn--large" href={GITHUB_URL} target="_blank" rel="noopener noreferrer">
            GitHub
          </a>
        </div>
      </div>
    </section>
  )
}

// nsgo.dev's footer: the brand, then four columns of links.
export function Footer() {
  return (
    <footer className="footer">
      <div className="container">
        <div className="footer__grid">
          <div className="footer__brand">
            <Link className="brand" to="/">
              <Logo size={26} />
              <span className="brand__name">NSGo Game</span>
            </Link>
            <p>A game layer for NSGo apps, written in Go.</p>
          </div>
          {COLUMNS.map((column) => (
            <div key={column.title} className="footer__column">
              <h3>{column.title}</h3>
              <ul>
                {column.links.map((link) => (
                  <li key={link.label}>
                    {'to' in link ? (
                      <Link to={link.to}>{link.label}</Link>
                    ) : (
                      <a href={link.href} target="_blank" rel="noopener noreferrer">
                        {link.label}
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
        <div className="footer__bottom">
          <span>© 2026 ONEO. Apache-2.0.</span>
          <a href="#top" className="footer__top">
            Back to top
            <Icon name="arrow-up" size={12} />
          </a>
        </div>
      </div>
    </footer>
  )
}
