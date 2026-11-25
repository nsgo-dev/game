// The site's languages, laid out as nsgo.dev lays out its own: English at the base, other languages under their code, so the Chinese home page is /zh/, Japanese /ja/, and so on.

import { useRouterState } from '@tanstack/react-router'
import { htmlLang, isLang, isNonDefaultLang, LANG_KEY, LANGS, type Lang, langNames, matchBrowserLang, NON_DEFAULT_LANGS, type NonDefaultLang, splitLocalePath } from './locales'

export { htmlLang, isLang, isNonDefaultLang, LANG_KEY, LANGS as langs, langNames, matchBrowserLang, NON_DEFAULT_LANGS }
export type { Lang, NonDefaultLang }

// The origin the site is deployed at, from vite.config.ts, for the absolute addresses of hreflang and canonical links.
declare const __SITE_ORIGIN__: string

// splitPath parts a router path, such as /zh/docs/packages/, into its language and the path below it, docs/packages/. The router's paths do not hold the base path, but an address the browser shows may, so splitPath strips it too.
export function splitPath(pathname: string): { lang: Lang; path: string } {
  return splitLocalePath(pathname, import.meta.env.BASE_URL)
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

// useNotFound reports whether the address has no page: a splat or 404 route's, or one a loader refused.
export function useNotFound(): boolean {
  return useRouterState({
    select: (state) => state.matches.some((match) => match.routeId === '/$' || match.routeId === '/404' || match.routeId === '/$lang/$' || match.status === 'notFound'),
  })
}

// routeLang narrows a $lang route parameter after the /$lang layout route's beforeLoad has validated it.
export function routeLang(param: string): NonDefaultLang {
  if (!isNonDefaultLang(param)) throw new Error(`unexpected route lang: ${param}`)
  return param
}

// pageHead is a page's title and description, its canonical address, and its address in each language, as nsgo.dev's pages carry them.
export function pageHead(lang: Lang, path: string, title: string, description: string) {
  return {
    meta: [
      { title },
      { name: 'description', content: description },
      { property: 'og:title', content: title },
      { property: 'og:description', content: description },
      { property: 'og:type', content: 'website' },
      { property: 'og:url', content: siteUrl(lang, path) },
      { property: 'og:image', content: siteUrl('en', `share/${lang}.png`) },
      { name: 'twitter:card', content: 'summary_large_image' },
    ],
    links: [
      { rel: 'canonical', href: siteUrl(lang, path) },
      { rel: 'alternate', hrefLang: 'x-default', href: siteUrl('en', path) },
      ...LANGS.map((other) => ({ rel: 'alternate', hrefLang: htmlLang(other), href: siteUrl(other, path) })),
    ],
  }
}
