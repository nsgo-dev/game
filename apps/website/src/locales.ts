// The site's languages: English at the base (/), every other language under its code (/zh/, /ja/, ...). site.config.ts hands them to nsgo-website, and the site's own pages are made once for each.

export const LANGS = ['en', 'zh', 'zh-tw', 'ja', 'ko', 'es', 'fr', 'de', 'pt', 'ru'] as const
export type Lang = (typeof LANGS)[number]

export const NON_DEFAULT_LANGS = ['zh', 'zh-tw', 'ja', 'ko', 'es', 'fr', 'de', 'pt', 'ru'] as const
