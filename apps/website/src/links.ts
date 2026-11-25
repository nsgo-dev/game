import type { Lang } from './locales'

export const GITHUB_URL = 'https://github.com/nsgo-dev/game'
export const NSGO_URL = 'https://nsgo.dev'
export const NSGO_GITHUB_URL = 'https://github.com/nsgo-dev/nsgo'
// NSGO_LLMS_URL is nsgo.dev's index of its docs for language models.
export const NSGO_LLMS_URL = `${NSGO_URL}/llms.txt`
export const PLAN_URL = 'https://github.com/nsgo-dev/nsgo/issues/1047'
// The NSGo community's places, as the header of every NSGo site links them.
export const AWESOME_URL = 'https://awesomensgo.dev/'
export const TELEGRAM_URL = 'https://t.me/nsgo_dev'
export const X_URL = 'https://x.com/suzukaze_haduki'
export const LICENSE_URL = `${GITHUB_URL}/blob/main/LICENSE`
export const CONTRIBUTING_URL = `${GITHUB_URL}/blob/main/CONTRIBUTING.md`
export const CHANGELOG_URL = `${GITHUB_URL}/blob/main/CHANGELOG.md`
export const SHOWCASE_SUBMIT_URL = `${GITHUB_URL}/issues/new?template=showcase_submission.yml`
// The build's docs for language models (plugins/llms.ts), below the site's base path: an index of the English pages and every page in full.
export const LLMS_INDEX = 'llms.txt'
export const LLMS_FULL = 'llms-full.txt'
export const issueUrl = (n: number) => `${GITHUB_URL}/issues/${n}`
export const repoTreeUrl = (path: string) => `${GITHUB_URL}/tree/main/${path}`

// nsgo.dev currently publishes English at / and Simplified Chinese at /zh/; other languages fall back to English until nsgo.dev adds them.
export const nsgoUrl = (lang: Lang, path = '') => `${NSGO_URL}/${lang === 'zh' ? 'zh/' : ''}${path}`
