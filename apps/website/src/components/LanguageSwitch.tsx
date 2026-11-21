import { copies } from '../copy'
import { htmlLang, LANG_KEY, langNames, langs, localeHref, useLang, useLangPath, useNotFound } from '../i18n'
import { Icon } from './Icon'
import { Menu } from './Menu'

// The language menu of nsgo.dev's header, beside the appearance menu: the current language's name, dropping down each language, which opens the same page in it. The choice is kept, so a visit from a Chinese browser goes to the Chinese pages only until English is chosen.
export function LanguageSwitch() {
  const lang = useLang()
  const path = useLangPath()
  // An address with no page has no twin in another language, so the menu goes to the other language's home page, as nsgo.dev's 404 does.
  const missing = useNotFound()
  return (
    <Menu
      label={copies[lang].nav.language}
      trigger={
        <span className="menu__trigger-label">
          <Icon name="languages" size={16} />
          <span>{langNames[lang]}</span>
        </span>
      }
    >
      {langs.map((other) => (
        <a
          key={other}
          className="menu__item"
          role="menuitemradio"
          aria-checked={other === lang}
          href={localeHref(other, missing ? '' : path)}
          hrefLang={htmlLang(other)}
          lang={htmlLang(other)}
          onClick={() => {
            try {
              localStorage.setItem(LANG_KEY, other)
            } catch {
              // The link still opens the language.
            }
          }}
        >
          {langNames[other]}
          <Icon name="check" size={14} className="menu__check" />
        </a>
      ))}
    </Menu>
  )
}
