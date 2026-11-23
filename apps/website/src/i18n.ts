// The site's languages, laid out as nsgo.dev lays out its own: English at the base, other languages under their code, so the Chinese home page is /zh/ and its docs /zh/docs/.

import { useRouterState } from '@tanstack/react-router'

export type Lang = 'en' | 'zh'
export const langs: Lang[] = ['en', 'zh']

// Each language's name in itself, as nsgo.dev's language menu shows it.
export const langNames: Record<Lang, string> = { en: 'English', zh: '简体中文' }

// htmlLang is the tag of lang and hreflang attributes.
export const htmlLang = (lang: Lang) => (lang === 'zh' ? 'zh-CN' : 'en')

// LANG_KEY keeps the language last chosen with the language menu, under the key nsgo.dev uses.
export const LANG_KEY = 'nsgo-lang'

// The origin the site is deployed at, from vite.config.ts, for the absolute addresses of hreflang and canonical links.
declare const __SITE_ORIGIN__: string

// The router's paths of each language's pages, for typed links.
export const routes = {
  en: { home: '/', docs: '/docs/', doc: '/docs/$slug/', playground: '/playground/', playgroundExample: '/playground/$example/', showcase: '/showcase/', roadmap: '/roadmap/' },
  zh: { home: '/zh/', docs: '/zh/docs/', doc: '/zh/docs/$slug/', playground: '/zh/playground/', playgroundExample: '/zh/playground/$example/', showcase: '/zh/showcase/', roadmap: '/zh/roadmap/' },
} as const

// splitPath parts a router path, such as /zh/docs/packages/, into its language and the path below it, docs/packages/. The router's paths do not hold the base path, but an address the browser shows may, so splitPath strips it too.
export function splitPath(pathname: string): { lang: Lang; path: string } {
  const base = import.meta.env.BASE_URL
  const rest = pathname.startsWith(base) ? pathname.slice(base.length) : pathname.replace(/^\/+/, '')
  if (rest === 'zh' || rest.startsWith('zh/')) return { lang: 'zh', path: rest.slice('zh/'.length) }
  return { lang: 'en', path: rest }
}

// localeHref is the address of the page at path below lang, with the base path, for a link the router does not handle.
export function localeHref(lang: Lang, path = ''): string {
  return `${import.meta.env.BASE_URL}${lang === 'en' ? '' : `${lang}/`}${path}`
}

export function siteUrl(lang: Lang, path = ''): string {
  return new URL(localeHref(lang, path), __SITE_ORIGIN__).href
}

// useLang is the language of the page showing, from its address.
export function useLang(): Lang {
  return useRouterState({ select: (state) => splitPath(state.location.pathname).lang })
}

// useLangPath is the path of the page showing below its language, which the language menu keeps.
export function useLangPath(): string {
  return useRouterState({ select: (state) => splitPath(state.location.pathname).path })
}

// useNotFound reports whether the address has no page: a splat route's, or one a loader refused.
export function useNotFound(): boolean {
  return useRouterState({ select: (state) => state.matches.some((match) => match.routeId === '/$' || match.routeId === '/zh/$' || match.status === 'notFound') })
}

// pageHead is a page's title and description, its canonical address, and its address in each language, as nsgo.dev's pages carry them.
export function pageHead(lang: Lang, path: string, title: string, description: string) {
  return {
    meta: [{ title }, { name: 'description', content: description }],
    links: [
      { rel: 'canonical', href: siteUrl(lang, path) },
      ...langs.map((other) => ({ rel: 'alternate', hrefLang: htmlLang(other), href: siteUrl(other, path) })),
    ],
  }
}
