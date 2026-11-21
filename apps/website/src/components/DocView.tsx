import { Link } from '@tanstack/react-router'
import { type ReactNode, useEffect, useRef, useState } from 'react'
import { docs } from 'virtual:docs'
import type { DocPage } from '../content/types'
import { copies } from '../copy'
import { htmlLang, type Lang, pageHead, routes, useLang } from '../i18n'
import { GITHUB_URL } from '../links'
import { Icon, iconMarkup } from './Icon'

const NARROW = '(max-width: 820px)'

// Before the first paint on narrow screens, fold the pages above the article, as nsgo.dev's docs do.
const FOLD_SCRIPT = `if (matchMedia('${NARROW}').matches) document.querySelector('.docs__nav').open = false;`

export function findDoc(lang: Lang, slug: string): DocPage | undefined {
  return docs[lang].find((d) => d.slug === slug)
}

export function docHead(lang: Lang, doc: DocPage | undefined) {
  if (!doc) return {}
  return pageHead(lang, doc.slug === 'index' ? 'docs/' : `docs/${doc.slug}/`, `${doc.title} — ${copies[lang].docs.titleSuffix}`, doc.description)
}

function DocLink({ lang, doc, children, className }: { lang: Lang; doc: DocPage; children: ReactNode; className?: string }) {
  const current = { 'aria-current': 'page' } as const
  return doc.slug === 'index' ? (
    <Link to={routes[lang].docs} className={className} activeOptions={{ exact: true }} activeProps={current}>
      {children}
    </Link>
  ) : (
    <Link to={routes[lang].doc} params={{ slug: doc.slug }} className={className} activeProps={current}>
      {children}
    </Link>
  )
}

// nsgo.dev's docs page: the pages beside the article, its contents on the other side, a copy button on each code block, and the previous and next pages below.
export function DocView({ doc }: { doc: DocPage }) {
  const lang = useLang()
  const copy = copies[lang]
  const pages = docs[lang]
  const nav = useRef<HTMLDetailsElement>(null)
  const prose = useRef<HTMLDivElement>(null)
  const current = useCurrentHeading(doc)
  const index = pages.findIndex((d) => d.slug === doc.slug)
  const prev = index > 0 ? pages[index - 1] : undefined
  const next = pages[index + 1]

  useEffect(() => {
    if (nav.current && matchMedia(NARROW).matches) nav.current.open = false
  }, [doc.slug])

  useEffect(() => addCopyButtons(prose.current, copy.ui), [doc.slug, copy.ui])

  return (
    <div className="container docs">
      <aside className="docs__sidebar">
        <details className="docs__nav" ref={nav} open suppressHydrationWarning>
          <summary>
            {copy.docs.pages}
            <Icon name="chevron" size={14} />
          </summary>
          <nav aria-label={copy.docs.nav}>
            <div className="docs__group">
              <ul>
                {pages.map((d) => (
                  <li key={d.slug}>
                    <DocLink lang={lang} doc={d}>
                      {d.title}
                    </DocLink>
                  </li>
                ))}
              </ul>
            </div>
          </nav>
        </details>
        <script dangerouslySetInnerHTML={{ __html: FOLD_SCRIPT }} />
      </aside>

      <article className="docs__main" lang={doc.fallback ? htmlLang('en') : undefined}>
        {doc.fallback && (
          <p className="docs__notice" lang={htmlLang(lang)}>
            {copy.docs.fallback}
          </p>
        )}
        <h1 tabIndex={-1}>{doc.title}</h1>
        <div className="prose" ref={prose} dangerouslySetInnerHTML={{ __html: doc.html }} />

        <a className="docs__edit" lang={htmlLang(lang)} href={`${GITHUB_URL}/edit/main/${doc.source}`} target="_blank" rel="noopener noreferrer">
          {copy.docs.edit}
          <Icon name="external" size={13} />
        </a>

        {(prev || next) && (
          <nav className="docs__pager" aria-label={copy.docs.pages}>
            {prev && (
              <DocLink lang={lang} doc={prev}>
                <span>{copy.docs.previous}</span>
                <strong>{prev.title}</strong>
              </DocLink>
            )}
            {next && (
              <DocLink lang={lang} doc={next} className="docs__pager-next">
                <span>{copy.docs.next}</span>
                <strong>{next.title}</strong>
              </DocLink>
            )}
          </nav>
        )}
      </article>

      {doc.headings.length > 0 && (
        <nav className="docs__toc" aria-label={copy.docs.onThisPage}>
          <p>{copy.docs.onThisPage}</p>
          <ul>
            {doc.headings.map((heading) => (
              <li key={heading.id} className={`docs__toc-${heading.depth}`}>
                <a href={`#${heading.id}`} className={heading.id === current ? 'is-current' : undefined}>
                  {heading.text}
                </a>
              </li>
            ))}
          </ul>
        </nav>
      )}
    </div>
  )
}

// useCurrentHeading is the id of the last heading above the top fifth of the window, or the last one once the page reaches its end.
function useCurrentHeading(doc: DocPage): string | undefined {
  const [current, setCurrent] = useState<string | undefined>(doc.headings[0]?.id)
  useEffect(() => {
    const elements = doc.headings.map((h) => document.getElementById(h.id)).filter((e): e is HTMLElement => e !== null)
    const mark = () => {
      const end = window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2
      let index = end ? elements.length - 1 : 0
      if (!end)
        elements.forEach((element, i) => {
          if (element.getBoundingClientRect().top < window.innerHeight / 5) index = i
        })
      setCurrent(elements[index]?.id)
    }
    mark()
    window.addEventListener('scroll', mark, { passive: true })
    return () => window.removeEventListener('scroll', mark)
  }, [doc])
  return current
}

// addCopyButtons frames each code block of the rendered Markdown as an editor pane: a title bar naming its language, with a copy button in the bar rather than in the code, so it stays put while the code scrolls sideways.
function addCopyButtons(root: HTMLElement | null, words: { copy: string; copied: string }): () => void {
  if (!root) return () => {}
  const timers: number[] = []
  root.querySelectorAll<HTMLPreElement>('pre').forEach((pre) => {
    if (pre.parentElement?.classList.contains('prose__code')) return
    const wrap = document.createElement('div')
    wrap.className = 'prose__code'
    pre.replaceWith(wrap)
    const bar = document.createElement('div')
    bar.className = 'prose__bar'
    const name = document.createElement('span')
    name.className = 'prose__lang'
    name.textContent = /language-([\w-]+)/.exec(`${pre.className} ${pre.querySelector('code')?.className ?? ''}`)?.[1] ?? 'text'
    bar.append(name)
    const button = document.createElement('button')
    button.type = 'button'
    button.className = 'prose__copy'
    button.setAttribute('aria-label', words.copy)
    button.title = words.copy
    button.innerHTML = iconMarkup('copy', 14)
    button.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(pre.innerText)
      } catch {
        // The code can still be selected.
        return
      }
      button.classList.add('is-copied')
      button.setAttribute('aria-label', words.copied)
      button.innerHTML = iconMarkup('check', 14)
      timers.push(
        window.setTimeout(() => {
          button.classList.remove('is-copied')
          button.setAttribute('aria-label', words.copy)
          button.innerHTML = iconMarkup('copy', 14)
        }, 1600),
      )
    })
    bar.append(button)
    wrap.append(bar, pre)
  })
  return () => timers.forEach((t) => window.clearTimeout(t))
}
