import { Link } from '@tanstack/react-router'
import { copies } from '../copy'
import { type Lang, routes, useLang } from '../i18n'
import { CONTRIBUTING_URL, GITHUB_URL, LICENSE_URL, NSGO_GITHUB_URL, nsgoUrl, PLAN_URL } from '../links'
import { Logo, SpeedLines } from './Brand'
import { Icon } from './Icon'

type FooterLink = { label: string; to: (typeof routes)[Lang]['home' | 'docs' | 'showcase' | 'roadmap'] } | { label: string; href: string }

function columns(lang: Lang): { title: string; links: FooterLink[] }[] {
  const copy = copies[lang]
  const to = routes[lang]
  return [
    {
      title: copy.footer.columns.project,
      links: [
        { label: copy.nav.home, to: to.home },
        { label: copy.nav.docs, to: to.docs },
        { label: copy.nav.showcase, to: to.showcase },
        { label: copy.nav.roadmap, to: to.roadmap },
      ],
    },
    {
      title: copy.footer.columns.nsgo,
      links: [
        { label: copy.footer.website, href: nsgoUrl(lang) },
        { label: copy.nav.docs, href: nsgoUrl(lang, 'docs/') },
        { label: copy.footer.plan, href: PLAN_URL },
      ],
    },
    {
      title: copy.footer.columns.community,
      links: [
        { label: copy.nav.github, href: GITHUB_URL },
        { label: copy.footer.issues, href: `${GITHUB_URL}/issues` },
        { label: copy.footer.contributing, href: CONTRIBUTING_URL },
        { label: copy.footer.nsgoGithub, href: NSGO_GITHUB_URL },
      ],
    },
    {
      title: copy.footer.columns.legal,
      links: [{ label: copy.footer.license, href: LICENSE_URL }],
    },
  ]
}

// The call to action nsgo.dev's home page shows above its footer.
export function Cta() {
  const lang = useLang()
  const copy = copies[lang]
  return (
    <section className="container cta-wrap">
      <div className="cta hud">
        <SpeedLines className="cta__lines" />
        <div className="cta__copy">
          <h2>{copy.cta.title}</h2>
          <p>{copy.cta.body}</p>
        </div>
        <div className="cta__actions">
          <Link className="btn btn--primary btn--large" to={routes[lang].docs}>
            <Icon name="play" size={14} />
            {copy.home.docs}
          </Link>
          <a className="btn btn--secondary btn--large" href={GITHUB_URL} target="_blank" rel="noopener noreferrer">
            {copy.nav.github}
          </a>
        </div>
      </div>
    </section>
  )
}

// nsgo.dev's footer: the brand, then four columns of links.
export function Footer() {
  const lang = useLang()
  const copy = copies[lang].footer
  return (
    <footer className="footer">
      <div className="container">
        <div className="footer__grid">
          <div className="footer__brand">
            <Link className="brand" to={routes[lang].home}>
              <Logo size={26} />
              <span className="brand__name">NSGo Game</span>
            </Link>
            <p>{copy.tagline}</p>
          </div>
          {columns(lang).map((column) => (
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
          <span>{copy.bottom}</span>
          <a href="#top" className="footer__top">
            {copy.top}
            <Icon name="arrow-up" size={12} />
          </a>
        </div>
      </div>
    </footer>
  )
}
