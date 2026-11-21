// The appearance the site shows, kept as nsgo.dev keeps it: `nsgo-theme` holds light, dark or system; absent is light. THEME_SCRIPT applies it before the first paint; setPreference switches it.

export type ThemePreference = 'light' | 'dark' | 'system'
export type Theme = 'light' | 'dark'

const KEY = 'nsgo-theme'
const DARK_COLOR = '#0b0c0f'
const LIGHT_COLOR = '#ffffff'
export const THEME_EVENT = 'nsgo-theme'

// Runs inline in the head, after the theme-color meta, so the page never paints in the wrong appearance.
export const THEME_SCRIPT = `(function () {
  var theme = 'light';
  var pref = null;
  try {
    pref = localStorage.getItem('${KEY}');
    if (pref === 'dark' || (pref === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)) theme = 'dark';
  } catch (error) {}
  var root = document.documentElement;
  root.dataset.theme = theme;
  root.dataset.themePreference = pref === 'dark' || pref === 'system' ? pref : 'light';
  root.style.colorScheme = theme;
  var meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', theme === 'dark' ? '${DARK_COLOR}' : '${LIGHT_COLOR}');
})();`

export function readPreference(): ThemePreference {
  try {
    const stored = localStorage.getItem(KEY)
    if (stored === 'dark' || stored === 'system') return stored
  } catch {
    // Storage can be unavailable; show light.
  }
  return 'light'
}

export function applyTheme(): void {
  const preference = readPreference()
  const dark = preference === 'dark' || (preference === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  const theme: Theme = dark ? 'dark' : 'light'
  const root = document.documentElement
  root.dataset.theme = theme
  root.dataset.themePreference = preference
  root.style.colorScheme = theme
  document.querySelector('meta[name="theme-color"]')?.setAttribute('content', dark ? DARK_COLOR : LIGHT_COLOR)
  document.dispatchEvent(new CustomEvent<ThemePreference>(THEME_EVENT, { detail: preference }))
}

export function setPreference(preference: ThemePreference): void {
  try {
    localStorage.setItem(KEY, preference)
  } catch {
    // The appearance still applies for this visit.
  }
  applyTheme()
}
