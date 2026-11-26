import { copies } from '../copy'
import { type Lang, useLang } from '../i18n'
import { AWESOME_URL, CHANGELOG_URL, CONTRIBUTING_URL, GITHUB_URL, LICENSE_URL, LLMS_FULL, LLMS_INDEX, nsgoUrl, TELEGRAM_URL, X_URL } from '../links'
import { Logo } from './Brand'
import { LocaleLink, type LocaleTarget } from './LocaleLink'

// A link goes to one of the site's pages, or to another site, opened in a new tab.
type FooterLink = { label: string; to: LocaleTarget } | { label: string; href: string }

function columns(lang: Lang): { title: string; links: FooterLink[] }[] {
  const copy = copies[lang]
  const words = copy.footer
  return [
    {
      title: words.columns[0] ?? '',
      links: [
        { label: words.gettingStarted, to: { page: 'doc', slug: 'getting-started' } },
        { label: words.tutorial, to: { page: 'tutorial' } },
        { label: words.packages, to: { page: 'packages' } },
        { label: copy.api.nav, to: { page: 'api' } },
        { label: words.stability, to: { page: 'doc', slug: 'stability' } },
      ],
    },
    {
      title: words.columns[1] ?? '',
      links: [
        { label: words.playground, to: { page: 'playground' } },
        { label: words.examples, to: { page: 'examples' } },
        { label: words.showcase, to: { page: 'showcase' } },
        { label: words.editor, to: { page: 'doc', slug: 'editor' } },
        { label: words.templates, to: { page: 'doc', slug: 'templates' } },
        { label: words.changelog, href: CHANGELOG_URL },
      ],
    },
    {
      title: words.columns[2] ?? '',
      links: [
        { label: words.github, href: GITHUB_URL },
        { label: words.nsgo, href: nsgoUrl(lang) },
        { label: 'Awesome NSGo', href: AWESOME_URL },
        { label: 'Telegram', href: TELEGRAM_URL },
        { label: 'X · 小莫', href: X_URL },
        { label: words.feedback, href: `${GITHUB_URL}/issues` },
        { label: words.contributing, href: CONTRIBUTING_URL },
      ],
    },
  ]
}

export function Footer() {
  const lang = useLang()
  const words = copies[lang].footer
  const base = import.meta.env.BASE_URL
  return (
    <footer className="footer">
      <div className="container footer__inner">
        <div className="footer__top">
          <div className="footer__brand">
            <LocaleLink page="home" lang={lang} aria-label="NSGo Game">
              <Logo size={26} />
            </LocaleLink>
            <p>{words.tagline}</p>
          </div>
          <div className="footer__columns">
            {columns(lang).map((column) => (
              <nav key={column.title} className="footer__column" aria-labelledby={`footer-${column.title}`}>
                <h2 id={`footer-${column.title}`}>{column.title}</h2>
                {column.links.map((link) =>
                  'to' in link ? (
                    <LocaleLink key={link.label} {...link.to} lang={lang}>
                      {link.label}
                    </LocaleLink>
                  ) : (
                    <a key={link.label} href={link.href} target="_blank" rel="noopener noreferrer">
                      {link.label}
                    </a>
                  ),
                )}
              </nav>
            ))}
          </div>
        </div>
        <div className="footer__bottom">
          <a href={LICENSE_URL} target="_blank" rel="noopener noreferrer">
            {words.license}
          </a>
          <nav aria-label="llms.txt">
            <a href={`${base}${LLMS_INDEX}`}>{words.llms}</a>
            <a href={`${base}${LLMS_FULL}`}>{words.llmsFull}</a>
            <span>game.nsgo.dev</span>
          </nav>
        </div>
      </div>
    </footer>
  )
}
