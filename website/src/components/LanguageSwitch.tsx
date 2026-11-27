import { copies } from '../copy'
import { htmlLang, LANG_KEY, type Lang, langNames, langs, localeHref, useLang, useLangPath, useNotFound } from '../i18n'

// The language menu: each language's name in itself; picking one opens the same page in it and keeps the choice, so a visit from a Chinese browser goes to the Chinese pages only until English is chosen.
export function LanguageSwitch() {
  const lang = useLang()
  const path = useLangPath()
  // An address with no page has no twin in another language, so the menu goes to the other language's home page.
  const missing = useNotFound()
  const pick = (other: Lang) => {
    try {
      localStorage.setItem(LANG_KEY, other)
    } catch {
      // The page still opens in the language.
    }
    window.location.assign(localeHref(other, missing ? '' : path) + window.location.hash)
  }
  return (
    <label className="language">
      <span className="sr-only">{copies[lang].nav.language}</span>
      <svg className="language__globe" width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M3 12h18M12 3a14 14 0 0 1 0 18a14 14 0 0 1 0-18z" />
      </svg>
      <select value={lang} onChange={(event) => pick(event.target.value as Lang)}>
        {langs.map((other) => (
          <option key={other} value={other} lang={htmlLang(other)}>
            {langNames[other]}
          </option>
        ))}
      </select>
      <svg className="language__chevron" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M6 9l6 6 6-6" />
      </svg>
    </label>
  )
}
