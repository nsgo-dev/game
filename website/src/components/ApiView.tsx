import { Fragment, type ReactNode, useEffect, useRef } from 'react'
import { api } from 'virtual:api'
import type { ApiFunc, ApiIdent, ApiPackage, ApiSummary, ApiType, ApiValue } from '../content/types'
import { copies } from '../copy'
import { htmlLang, type Lang, pageHead, useLang } from '../i18n'
import { GITHUB_URL } from '../links'
import { Icon } from './Icon'
import { LocaleLink } from './LocaleLink'

const NARROW = '(max-width: 820px)'
const FOLD_SCRIPT = `if (matchMedia('${NARROW}').matches) document.querySelector('.docs__nav').open = false;`

// The ids of a package page's sections, as pkg.go.dev names them; an exported identifier starts with a capital letter, so none is one of these.
const SECTION = { overview: 'pkg-overview', index: 'pkg-index', consts: 'pkg-constants', vars: 'pkg-variables', funcs: 'pkg-functions', types: 'pkg-types' } as const

export function apiIndexHead(lang: Lang) {
  const copy = copies[lang].api
  return pageHead(lang, 'docs/api/', `${copy.title} — ${copy.titleSuffix}`, copy.description)
}

export function apiPackageHead(lang: Lang, pkg: ApiPackage | undefined) {
  if (!pkg) return {}
  return pageHead(lang, `docs/api/${pkg.page}/`, `${pkg.name} — ${copies[lang].api.titleSuffix}`, pkg.synopsis)
}

// apiPage is a package page's path from a splat, which may end in the slash every page's address does.
export const apiPage = (splat: string | undefined) => (splat ?? '').replace(/\/+$/, '')

// displayPage strips the leading pkg/ directory from a package's page path so the sidebar and index show sprite and tooling/pack rather than grouping every library package under pkg.
const displayPage = (page: string) => (page.startsWith('pkg/') ? page.slice('pkg/'.length) : page)

// groupPackages puts the packages at the top level first, then those below each directory, such as tooling/, under it.
function groupPackages(packages: ApiSummary[]): { dir: string; items: ApiSummary[] }[] {
  const groups = new Map<string, ApiSummary[]>()
  for (const pkg of packages) {
    const shown = displayPage(pkg.page)
    const slash = shown.indexOf('/')
    const dir = slash < 0 ? '' : shown.slice(0, slash)
    groups.set(dir, [...(groups.get(dir) ?? []), pkg])
  }
  return [...groups].map(([dir, items]) => ({ dir, items })).sort((a, b) => (a.dir === '' ? -1 : b.dir === '' ? 1 : a.dir.localeCompare(b.dir)))
}

function groupTitle(lang: Lang, dir: string): string {
  const copy = copies[lang].api
  if (dir === '') return copy.packages
  return dir === 'tooling' ? copy.tooling : dir
}

function PackageLink({ lang, page, children, className }: { lang: Lang; page: string; children: ReactNode; className?: string }) {
  return (
    <LocaleLink page="apiPackage" splat={page} lang={lang} className={className} activeProps={{ 'aria-current': 'page' }}>
      {children}
    </LocaleLink>
  )
}

// ApiLayout is the docs' layout for the reference: its packages beside the article, and the article's contents on the other side.
function ApiLayout({ children, toc, page }: { children: ReactNode; toc?: { id: string; text: string; depth: 2 | 3 }[]; page?: string }) {
  const lang = useLang()
  const copy = copies[lang]
  const nav = useRef<HTMLDetailsElement>(null)
  useEffect(() => {
    if (nav.current && matchMedia(NARROW).matches) nav.current.open = false
  }, [page])
  const packages = api.status === 'ok' ? api.packages : []
  return (
    <div className="container docs">
      <aside className="docs__sidebar">
        <details className="docs__nav" ref={nav} open suppressHydrationWarning>
          <summary>
            {copy.api.nav}
            <Icon name="chevron" size={14} />
          </summary>
          <nav aria-label={copy.api.nav}>
            <div className="docs__group">
              <p className="docs__group-title">{copy.docs.reference}</p>
              <ul>
                <li>
                  <LocaleLink page="api" lang={lang} activeOptions={{ exact: true }} activeProps={{ 'aria-current': 'page' }}>
                    {copy.api.overview}
                  </LocaleLink>
                </li>
                <li>
                  <LocaleLink page="docs" lang={lang} activeOptions={{ exact: true }}>
                    {copy.nav.docs}
                  </LocaleLink>
                </li>
              </ul>
            </div>
            {groupPackages(packages).map((group) => (
              <div key={group.dir} className="docs__group">
                <p className="docs__group-title">{groupTitle(lang, group.dir)}</p>
                <ul>
                  {group.items.map((pkg) => {
                    const shown = displayPage(pkg.page)
                    return (
                      <li key={pkg.page}>
                        <PackageLink lang={lang} page={pkg.page} className="api__nav-link">
                          {group.dir === '' ? shown : shown.slice(group.dir.length + 1)}
                        </PackageLink>
                      </li>
                    )
                  })}
                </ul>
              </div>
            ))}
          </nav>
        </details>
        <script dangerouslySetInnerHTML={{ __html: FOLD_SCRIPT }} />
      </aside>

      <article className="docs__main api">{children}</article>

      {toc && toc.length > 0 && (
        <nav className="docs__toc" aria-label={copy.docs.onThisPage}>
          <p>{copy.docs.onThisPage}</p>
          <ul>
            {toc.map((item) => (
              <li key={item.id} className={`docs__toc-${item.depth}`}>
                <a href={`#${item.id}`}>{item.text}</a>
              </li>
            ))}
          </ul>
        </nav>
      )}
    </div>
  )
}

// ApiIndexView lists the reference's packages.
export function ApiIndexView() {
  const lang = useLang()
  const copy = copies[lang].api
  return (
    <ApiLayout>
      <h1 tabIndex={-1}>{copy.title}</h1>
      <p className="api__lead">
        {copy.lead[0]}
        <a href={`${import.meta.env.BASE_URL}api.json`}>
          <code>api.json</code>
        </a>
        {copy.lead[1]}
      </p>
      {api.status === 'ok' ? (
        groupPackages(api.packages).map((group) => (
          <section key={group.dir} className="api__section">
            <h2 id={group.dir === '' ? 'packages' : group.dir}>{groupTitle(lang, group.dir)}</h2>
            <dl className="api__packages">
              {group.items.map((pkg) => (
                <Fragment key={pkg.page}>
                  <dt>
                    <PackageLink lang={lang} page={pkg.page}>
                      {displayPage(pkg.page)}
                    </PackageLink>
                    <Platforms lang={lang} platforms={pkg.platforms} />
                  </dt>
                  <dd lang={htmlLang('en')}>{pkg.synopsis}</dd>
                </Fragment>
              ))}
            </dl>
          </section>
        ))
      ) : (
        <p className="docs__notice">
          {copy.unavailable[0]}
          {api.reason}
          {copy.unavailable[1]}
        </p>
      )}
    </ApiLayout>
  )
}

// ApiPackageView is a package's page, laid out as pkg.go.dev lays out its own: the package's doc, an index, then its constants, variables, functions and types, each type with its values, constructors and methods. Every exported identifier has an element with its anchor as id.
export function ApiPackageView({ pkg }: { pkg: ApiPackage }) {
  const lang = useLang()
  const copy = copies[lang].api
  const toc: { id: string; text: string; depth: 2 | 3 }[] = []
  if (pkg.docHTML) toc.push({ id: SECTION.overview, text: copy.overview, depth: 2 })
  toc.push({ id: SECTION.index, text: copy.index, depth: 2 })
  if (pkg.consts.length) toc.push({ id: SECTION.consts, text: copy.constants, depth: 2 })
  if (pkg.vars.length) toc.push({ id: SECTION.vars, text: copy.variables, depth: 2 })
  if (pkg.funcs.length) toc.push({ id: SECTION.funcs, text: copy.functions, depth: 2 })
  if (pkg.types.length) {
    toc.push({ id: SECTION.types, text: copy.types, depth: 2 })
    for (const t of pkg.types) toc.push({ id: t.anchor, text: t.name, depth: 3 })
  }
  return (
    <ApiLayout toc={toc} page={pkg.page}>
      {copy.notice && <p className="docs__notice">{copy.notice}</p>}
      <h1 tabIndex={-1}>
        <span className="api__keyword">package</span> {pkg.name}
      </h1>
      <pre className="api__import">
        <code>import "{pkg.importPath}"</code>
      </pre>
      <Platforms lang={lang} platforms={pkg.platforms} block />

      {pkg.docHTML && (
        <section className="api__section">
          <h2 id={SECTION.overview}>{copy.overview}</h2>
          <Prose html={pkg.docHTML} />
        </section>
      )}

      <section className="api__section">
        <h2 id={SECTION.index}>{copy.index}</h2>
        <ul className="api__index" lang={htmlLang('en')}>
          {pkg.consts.length > 0 && (
            <li>
              <a href={`#${SECTION.consts}`}>{copy.constants}</a>
            </li>
          )}
          {pkg.vars.length > 0 && (
            <li>
              <a href={`#${SECTION.vars}`}>{copy.variables}</a>
            </li>
          )}
          {pkg.funcs.map((f) => (
            <IndexEntry key={f.anchor} item={f} />
          ))}
          {pkg.types.map((t) => (
            <li key={t.anchor}>
              <a href={`#${t.anchor}`}>type {t.name}</a>
              {t.funcs.length + t.methods.length > 0 && (
                <ul>
                  {[...t.funcs, ...t.methods].map((f) => (
                    <IndexEntry key={f.anchor} item={f} />
                  ))}
                </ul>
              )}
            </li>
          ))}
        </ul>
      </section>

      {pkg.consts.length > 0 && (
        <section className="api__section">
          <h2 id={SECTION.consts}>{copy.constants}</h2>
          {pkg.consts.map((v) => (
            <Value key={v.idents[0]?.anchor} lang={lang} value={v} />
          ))}
        </section>
      )}
      {pkg.vars.length > 0 && (
        <section className="api__section">
          <h2 id={SECTION.vars}>{copy.variables}</h2>
          {pkg.vars.map((v) => (
            <Value key={v.idents[0]?.anchor} lang={lang} value={v} />
          ))}
        </section>
      )}
      {pkg.funcs.length > 0 && (
        <section className="api__section">
          <h2 id={SECTION.funcs}>{copy.functions}</h2>
          {pkg.funcs.map((f) => (
            <Func key={f.anchor} lang={lang} func={f} level={3} />
          ))}
        </section>
      )}
      {pkg.types.length > 0 && (
        <section className="api__section">
          <h2 id={SECTION.types}>{copy.types}</h2>
          {pkg.types.map((t) => (
            <Type key={t.anchor} lang={lang} type={t} />
          ))}
        </section>
      )}
    </ApiLayout>
  )
}

function IndexEntry({ item }: { item: ApiFunc }) {
  return (
    <li>
      <a href={`#${item.anchor}`}>{item.decl.replace(/\s*\n\s*/g, ' ')}</a>
    </li>
  )
}

function Prose({ html }: { html: string }) {
  return <div className="prose api__doc" lang={htmlLang('en')} dangerouslySetInnerHTML={{ __html: html }} />
}

function Platforms({ lang, platforms, block }: { lang: Lang; platforms: string[] | undefined; block?: boolean }) {
  if (!platforms?.length) return null
  const Tag = block ? 'p' : 'span'
  return (
    <Tag className="api__platforms">
      {copies[lang].api.only} {platforms.join(', ')}
    </Tag>
  )
}

function SourceLink({ lang, file, line }: { lang: Lang; file: string; line: number }) {
  const label = copies[lang].api.source
  return (
    <a className="api__source" href={`${GITHUB_URL}/blob/main/${file}#L${line}`} target="_blank" rel="noopener noreferrer" aria-label={`${label}: ${file}:${line}`} title={`${file}:${line}`}>
      {label}
      <Icon name="external" size={12} />
    </a>
  )
}

function Heading({ level, anchor, children, lang, item }: { level: 3 | 4; anchor: string; children: ReactNode; lang: Lang; item: { file: string; line: number; platforms?: string[] } }) {
  const H = level === 3 ? 'h3' : 'h4'
  return (
    <div className="api__heading">
      <H id={anchor} lang={htmlLang('en')}>
        <a href={`#${anchor}`}>{children}</a>
      </H>
      <Platforms lang={lang} platforms={item.platforms} />
      <SourceLink lang={lang} file={item.file} line={item.line} />
    </div>
  )
}

function Value({ lang, value }: { lang: Lang; value: ApiValue }) {
  return (
    <div className="api__item">
      <div className="api__heading api__heading--value">
        <Platforms lang={lang} platforms={value.platforms} />
        <SourceLink lang={lang} file={value.file} line={value.line} />
      </div>
      <Decl decl={value.decl} anchors={value.idents} />
      {value.docHTML && <Prose html={value.docHTML} />}
    </div>
  )
}

function Func({ lang, func, level }: { lang: Lang; func: ApiFunc; level: 3 | 4 }) {
  return (
    <div className="api__item">
      <Heading level={level} anchor={func.anchor} lang={lang} item={func}>
        func {func.recv ? `(${func.recv}) ` : ''}
        {func.name}
      </Heading>
      <Decl decl={func.decl} anchors={[]} />
      {func.docHTML && <Prose html={func.docHTML} />}
    </div>
  )
}

function Type({ lang, type }: { lang: Lang; type: ApiType }) {
  return (
    <div className="api__item api__type">
      <Heading level={3} anchor={type.anchor} lang={lang} item={type}>
        type {type.name}
      </Heading>
      <Decl decl={type.decl} anchors={type.members} />
      {type.docHTML && <Prose html={type.docHTML} />}
      {[...type.consts, ...type.vars].map((v) => (
        <Value key={v.idents[0]?.anchor} lang={lang} value={v} />
      ))}
      {[...type.funcs, ...type.methods].map((f) => (
        <Func key={f.anchor} lang={lang} func={f} level={4} />
      ))}
    </div>
  )
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// declLines finds the line of a declaration that declares each anchor's name: a constant or variable of a declaration, a field of a struct, embedded ones by their type's name, or a method of an interface, as gofmt prints them, at the start of a line of the declaration or of its block. Names on no line, which gofmt does not print that way, are listed in rest.
function declLines(decl: string, anchors: ApiIdent[]): { lines: { text: string; ids: string[] }[]; rest: string[] } {
  const lines = decl.split('\n').map((text) => ({ text, ids: [] as string[] }))
  const rest: string[] = []
  for (const { name, anchor } of anchors) {
    const pattern = new RegExp(`^(?:(?:const|var) |\\t)\\*?(?:\\w+\\.)?(?:\\w+, )*${escapeRegExp(name)}(?![\\w])`)
    const line = lines.find((l) => pattern.test(l.text))
    if (line) line.ids.push(anchor)
    else rest.push(anchor)
  }
  return { lines, rest }
}

// Decl shows a declaration's source with an element for each anchor in it: the line that declares the name, or an empty one before the source.
function Decl({ decl, anchors }: { decl: string; anchors: ApiIdent[] }) {
  const { lines, rest } = declLines(decl, anchors)
  return (
    <pre className="api__decl" lang={htmlLang('en')}>
      {rest.map((id) => (
        <span key={id} id={id} />
      ))}
      <code>
        {lines.map((line, i) => {
          const [first, ...others] = line.ids
          const text = i < lines.length - 1 ? `${line.text}\n` : line.text
          return (
            <Fragment key={i}>
              {others.map((id) => (
                <span key={id} id={id} />
              ))}
              {first ? (
                <span id={first} className="api__line">
                  {text}
                </span>
              ) : (
                text
              )}
            </Fragment>
          )
        })}
      </code>
    </pre>
  )
}
