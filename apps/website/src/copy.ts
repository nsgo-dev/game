import { de } from './copy/de.ts'
import { type Copy as BaseCopy, en } from './copy/en.ts'
import { es } from './copy/es.ts'
import { fr } from './copy/fr.ts'
import { ja } from './copy/ja.ts'
import { ko } from './copy/ko.ts'
import { dePages } from './copy/pages/de.ts'
import { enPages, type PagesCopy } from './copy/pages/en.ts'
import { esPages } from './copy/pages/es.ts'
import { frPages } from './copy/pages/fr.ts'
import { jaPages } from './copy/pages/ja.ts'
import { koPages } from './copy/pages/ko.ts'
import { ptPages } from './copy/pages/pt.ts'
import { ruPages } from './copy/pages/ru.ts'
import { zhPages } from './copy/pages/zh.ts'
import { zhTwPages } from './copy/pages/zh-tw.ts'
import { pt } from './copy/pt.ts'
import { ru } from './copy/ru.ts'
import { zh } from './copy/zh.ts'
import { zhTw } from './copy/zh-tw.ts'
import type { Lang } from './locales.ts'

// The site's words, one module per language under src/copy/ and one for the header, footer, home, tutorial, packages and examples pages under src/copy/pages/. Names of packages, types and code stay in English.
export type Copy = Omit<BaseCopy, 'nav'> & Omit<PagesCopy, 'nav'> & { nav: BaseCopy['nav'] & PagesCopy['nav'] }

const merge = (base: BaseCopy, pages: PagesCopy): Copy => ({ ...base, ...pages, nav: { ...base.nav, ...pages.nav } })

export const copies: Record<Lang, Copy> = {
  en: merge(en, enPages),
  zh: merge(zh, zhPages),
  'zh-tw': merge(zhTw, zhTwPages),
  ja: merge(ja, jaPages),
  ko: merge(ko, koPages),
  es: merge(es, esPages),
  fr: merge(fr, frPages),
  de: merge(de, dePages),
  pt: merge(pt, ptPages),
  ru: merge(ru, ruPages),
}

// count puts the number of packages into a title that holds {n}.
export const count = (text: string, n: number): string => text.replace('{n}', String(n))
