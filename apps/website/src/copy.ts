import { de } from './copy/de.ts'
import { type Copy, en } from './copy/en.ts'
import { es } from './copy/es.ts'
import { fr } from './copy/fr.ts'
import { ja } from './copy/ja.ts'
import { ko } from './copy/ko.ts'
import { pt } from './copy/pt.ts'
import { ru } from './copy/ru.ts'
import { zh } from './copy/zh.ts'
import { zhTw } from './copy/zh-tw.ts'
import type { Lang } from './locales.ts'

// The site's words, one module per language under src/copy/. Names of packages, types and code stay in English.
export type { Copy }

export const copies: Record<Lang, Copy> = {
  en,
  zh,
  'zh-tw': zhTw,
  ja,
  ko,
  es,
  fr,
  de,
  pt,
  ru,
}
