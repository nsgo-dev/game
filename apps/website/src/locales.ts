// The site's languages, router-free so Vite plugins, build scripts and tests can share them with the app.
// English lives at the base (/); every other language lives under its code (/zh/, /ja/, ...).

export const LANGS = ['en', 'zh', 'zh-tw', 'ja', 'ko', 'es', 'fr', 'de', 'pt', 'ru'] as const
export type Lang = (typeof LANGS)[number]

export const NON_DEFAULT_LANGS = ['zh', 'zh-tw', 'ja', 'ko', 'es', 'fr', 'de', 'pt', 'ru'] as const
export type NonDefaultLang = (typeof NON_DEFAULT_LANGS)[number]

// Each language's name in itself, as shown in the language menu.
export const langNames: Record<Lang, string> = {
  en: 'English',
  zh: '简体中文',
  'zh-tw': '繁體中文',
  ja: '日本語',
  ko: '한국어',
  es: 'Español',
  fr: 'Français',
  de: 'Deutsch',
  pt: 'Português',
  ru: 'Русский',
}

// BCP 47 tags for html lang and alternate hreflang attributes.
const HTML_LANGS: Record<Lang, string> = {
  en: 'en',
  zh: 'zh-CN',
  'zh-tw': 'zh-TW',
  ja: 'ja',
  ko: 'ko',
  es: 'es',
  fr: 'fr',
  de: 'de',
  pt: 'pt',
  ru: 'ru',
}

export const htmlLang = (lang: Lang): string => HTML_LANGS[lang]

// LANG_KEY keeps the language last chosen with the language menu, under the key nsgo.dev uses.
export const LANG_KEY = 'nsgo-lang'

const LANG_SET = new Set<string>(LANGS)
const NON_DEFAULT_SET = new Set<string>(NON_DEFAULT_LANGS)

export function isLang(value: string): value is Lang {
  return LANG_SET.has(value)
}

export function isNonDefaultLang(value: string): value is NonDefaultLang {
  return NON_DEFAULT_SET.has(value)
}

// splitLocalePath parts a pathname, such as /ja/docs/packages/ or /game/zh-tw/roadmap/, into its language and the path below it.
export function splitLocalePath(pathname: string, base: string): { lang: Lang; path: string } {
  const rest = pathname.startsWith(base) ? pathname.slice(base.length) : pathname.replace(/^\/+/, '')
  const slash = rest.indexOf('/')
  const first = slash < 0 ? rest : rest.slice(0, slash)
  if (isNonDefaultLang(first)) {
    return { lang: first, path: slash < 0 ? '' : rest.slice(slash + 1) }
  }
  return { lang: 'en', path: rest }
}

// matchBrowserLang picks the first supported language matching a browser's preference list (navigator.languages).
// Traditional Chinese tags (zh-TW, zh-HK, zh-MO, zh-Hant) map to zh-tw unless zh-Hans is explicit; other zh* tags map to zh.
export function matchBrowserLang(languages: readonly (string | null | undefined)[]): Lang {
  for (const entry of languages) {
    const raw = String(entry ?? '')
      .trim()
      .toLowerCase()
      .replace(/_/g, '-')
    if (!raw) continue
    const parts = raw.split('-')
    const primary = parts[0] ?? ''
    if (primary === 'en') return 'en'
    if (primary === 'zh') {
      const hans = parts.includes('hans')
      const hant = !hans && (parts.includes('hant') || parts.includes('tw') || parts.includes('hk') || parts.includes('mo'))
      return hant ? 'zh-tw' : 'zh'
    }
    if (isNonDefaultLang(primary)) return primary
  }
  return 'en'
}
