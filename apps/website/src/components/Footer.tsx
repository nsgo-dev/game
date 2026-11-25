import { copies } from '../copy'
import { type Lang, useLang } from '../i18n'
import { CONTRIBUTING_URL, GITHUB_URL, LICENSE_URL, LLMS_FULL, LLMS_INDEX, NSGO_GITHUB_URL, nsgoUrl, PLAN_URL } from '../links'
import { Logo, SpeedLines } from './Brand'
import { Icon } from './Icon'
import { LocaleLink, type SimplePage } from './LocaleLink'

// A link goes to one of the site's routes, to one of the build's files below the base path (opened in place), or to another site (opened in a new tab).
type FooterLink = { label: string; page: SimplePage } | { label: string; file: string } | { label: string; href: string }

function columns(lang: Lang): { title: string; links: FooterLink[] }[] {
  const copy = copies[lang]
  return [
    {
      title: copy.footer.columns.project,
      links: [
        { label: copy.nav.home, page: 'home' },
        { label: copy.nav.docs, page: 'docs' },
        { label: copy.nav.playground, page: 'playground' },
        { label: copy.nav.showcase, page: 'showcase' },
        { label: copy.nav.roadmap, page: 'roadmap' },
        { label: copy.footer.llms, file: LLMS_INDEX },
        { label: copy.footer.llmsFull, file: LLMS_FULL },
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
          <LocaleLink page="docs" lang={lang} className="btn btn--primary btn--large">
            <Icon name="play" size={14} />
            {copy.home.docs}
          </LocaleLink>
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
            <LocaleLink page="home" lang={lang} className="brand">
              <Logo size={26} />
              <span className="brand__name">NSGo Game</span>
            </LocaleLink>
            <p>{copy.tagline}</p>
          </div>
          {columns(lang).map((column) => (
            <div key={column.title} className="footer__column">
              <h3>{column.title}</h3>
              <ul>
                {column.links.map((link) => (
                  <li key={link.label}>
                    {'page' in link ? (
                      <LocaleLink page={link.page} lang={lang}>
                        {link.label}
                      </LocaleLink>
                    ) : 'file' in link ? (
                      <a href={`${import.meta.env.BASE_URL}${link.file}`}>{link.label}</a>
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
