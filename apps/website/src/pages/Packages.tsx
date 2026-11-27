import { type ReactNode, useState } from 'react'
import { api } from 'virtual:api'
import { site } from 'virtual:site'
import { Icon } from '../components/Icon'
import { LocaleLink } from '../components/LocaleLink'
import type { CatalogPackage, DocsLang, PackageCategory } from '../content/types'
import { copies, count } from '../copy'
import { type Lang, htmlLang, pageHead, useLang } from '../i18n'

// The catalog's groups in the order the pages show them; copy.categories names them in the same order.
export const CATEGORY_ORDER: readonly PackageCategory[] = ['core', 'play', 'ui', 'tools', 'online', '3d']

export const packagesHead = (lang: Lang) => {
  const copy = copies[lang]
  return pageHead(lang, 'packages/', `${count(copy.packages.title, site.packages.length)} — NSGo Game`, copy.packages.lead)
}

// The language a package's description is shown in: the docs' own, or English where the docs are not translated.
export const docsLang = (lang: Lang): DocsLang => (lang === 'zh' || lang === 'zh-tw' ? lang : 'en')

// The API reference's pages, when this build could read the Go packages.
const API_PAGES = new Set(api.status === 'ok' ? api.packages.map((p) => p.page) : [])

// apiPageOf maps a catalog package path (game or game/pkg/sprite) to its API reference page splat (game or pkg/sprite).
const apiPageOf = (pkg: CatalogPackage) => (pkg.path === 'game' ? 'game' : pkg.path.slice('game/'.length))

// PackageLink goes to a package's guide, or to its API reference when no page of the docs is its guide.
export function PackageLink({ pkg, lang, children, className }: { pkg: CatalogPackage; lang: Lang; children: ReactNode; className?: string }) {
  if (pkg.guide)
    return (
      <LocaleLink page="doc" slug={pkg.guide} lang={lang} className={className}>
        {children}
      </LocaleLink>
    )
  const page = apiPageOf(pkg)
  if (API_PAGES.has(page))
    return (
      <LocaleLink page="apiPackage" splat={page} lang={lang} className={className}>
        {children}
      </LocaleLink>
    )
  return <span className={className}>{children}</span>
}

// describe is a package's description in the page's language, with the language it is written in.
function describe(pkg: CatalogPackage, lang: Lang): { text: string; lang?: string } {
  const own = docsLang(lang)
  if (pkg.description[own]) return { text: pkg.description[own] }
  return { text: pkg.description.en, lang: lang === 'en' ? undefined : 'en' }
}

export function Packages() {
  const lang = useLang()
  const copy = copies[lang]
  const words = copy.packages
  const [category, setCategory] = useState<PackageCategory | 'all'>('all')
  const [query, setQuery] = useState('')
  const q = query.trim().toLowerCase()
  const all = [...site.packages].sort((a, b) => CATEGORY_ORDER.indexOf(a.category) - CATEGORY_ORDER.indexOf(b.category))
  const matching = all.filter((p) => !q || p.name.toLowerCase().includes(q) || describe(p, lang).text.toLowerCase().includes(q))
  const shown = matching.filter((p) => category === 'all' || p.category === category)
  const filters: { key: PackageCategory | 'all'; name: string; n: number }[] = [
    { key: 'all', name: words.all, n: matching.length },
    ...CATEGORY_ORDER.map((key, i) => ({ key, name: copy.categories[i] ?? key, n: matching.filter((p) => p.category === key).length })),
  ]
  return (
    <div className="container">
      <header className="page-hero rise">
        <span className="px sprite-gem" aria-hidden="true" />
        <h1 className="display-1">{count(words.title, site.packages.length)}</h1>
        <p>{words.lead}</p>
      </header>
      <section className="catalog" aria-label={copy.nav.packages}>
        <div className="catalog__bar">
          <div className="catalog__filters" role="group" aria-label={copy.nav.packages}>
            {filters.map((f) => (
              <button key={f.key} type="button" className="catalog__filter" aria-pressed={f.key === category} onClick={() => setCategory(f.key)}>
                {f.name}
                <span>{f.n}</span>
              </button>
            ))}
          </div>
          <label className="catalog__search">
            <span className="sr-only">{words.search}</span>
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20l-3.5-3.5" />
            </svg>
            <input type="search" value={query} placeholder={words.search} onChange={(event) => setQuery(event.target.value)} />
          </label>
        </div>
        {shown.length === 0 ? (
          <p className="catalog__empty" role="status">
            {words.noMatch}
          </p>
        ) : (
          <ul className="catalog__grid">
            {shown.map((pkg) => (
              <PackageCard key={pkg.path} pkg={pkg} lang={lang} />
            ))}
          </ul>
        )}
      </section>
    </div>
  )
}

function PackageCard({ pkg, lang }: { pkg: CatalogPackage; lang: Lang }) {
  const copy = copies[lang]
  const description = describe(pkg, lang)
  const index = CATEGORY_ORDER.indexOf(pkg.category)
  return (
    <li className="package-card">
      <div className="package-card__head">
        <PackageLink pkg={pkg} lang={lang} className="package-card__name">
          {pkg.name}
        </PackageLink>
        <span className="package-card__badges">
          <span className={`category category--${pkg.category}`}>{copy.categories[index]}</span>
          <LocaleLink page="doc" slug="stability" lang={lang} className={pkg.stable ? 'chip chip--cleared' : 'chip chip--accent'}>
            {pkg.stable ? copy.docs.status.stable : copy.docs.status.experimental}
          </LocaleLink>
        </span>
      </div>
      {description.text && (
        <p className="package-card__desc" lang={description.lang ? htmlLang(description.lang as Lang) : undefined}>
          {description.text}
        </p>
      )}
      <div className="package-card__foot">
        <code className="package-card__path">{pkg.path}</code>
        <span className="package-card__links">
          {pkg.guide && (
            <LocaleLink page="doc" slug={pkg.guide} lang={lang}>
              {copy.packages.guide} <Icon name="arrow" size={12} className="inline-icon" />
            </LocaleLink>
          )}
          {API_PAGES.has(apiPageOf(pkg)) && (
            <LocaleLink page="apiPackage" splat={apiPageOf(pkg)} lang={lang}>
              API
            </LocaleLink>
          )}
        </span>
      </div>
    </li>
  )
}
