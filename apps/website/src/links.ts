// Addresses outside the site that its pages link to.

export const GITHUB_URL = 'https://github.com/nsgo-dev/game'
// NSGO_LLMS_URL is nsgo.dev's index of its docs for language models.
export const NSGO_LLMS_URL = 'https://nsgo.dev/llms.txt'
export const CONTRIBUTING_URL = `${GITHUB_URL}/blob/main/CONTRIBUTING.md`
export const CHANGELOG_URL = `${GITHUB_URL}/blob/main/CHANGELOG.md`
export const SHOWCASE_SUBMIT_URL = `${GITHUB_URL}/issues/new?template=showcase_submission.yml`
// The docs for language models (lib/llms.ts), at the site's root: an index of the English pages and every page in full.
export const LLMS_INDEX = 'llms.txt'
export const LLMS_FULL = 'llms-full.txt'
export const repoTreeUrl = (path: string) => `${GITHUB_URL}/tree/main/${path}`
