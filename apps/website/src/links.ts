import type { Lang } from './i18n'

export const GITHUB_URL = 'https://github.com/nsgo-dev/game'
export const NSGO_URL = 'https://nsgo.dev'
export const NSGO_GITHUB_URL = 'https://github.com/nsgo-dev/nsgo'
export const PLAN_URL = 'https://github.com/nsgo-dev/nsgo/issues/1047'
export const LICENSE_URL = `${GITHUB_URL}/blob/main/LICENSE`
export const CONTRIBUTING_URL = `${GITHUB_URL}/blob/main/CONTRIBUTING.md`
export const SHOWCASE_SUBMIT_URL = `${GITHUB_URL}/issues/new?template=showcase_submission.yml`
export const issueUrl = (n: number) => `${GITHUB_URL}/issues/${n}`
export const repoTreeUrl = (path: string) => `${GITHUB_URL}/tree/main/${path}`

// nsgoUrl is a page of nsgo.dev in lang: it has the site's languages, laid out the same way.
export const nsgoUrl = (lang: Lang, path = '') => `${NSGO_URL}/${lang === 'en' ? '' : `${lang}/`}${path}`
