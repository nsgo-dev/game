import { useEffect, useState } from 'react'
import { copies } from '../copy'
import { useLang } from '../i18n'
import { applyTheme, readPreference, setPreference, THEME_EVENT, type ThemePreference } from '../lib/theme'
import { Icon, type IconName } from './Icon'
import { Menu } from './Menu'

const OPTIONS: { value: ThemePreference; icon: IconName }[] = [
  { value: 'light', icon: 'sun' },
  { value: 'dark', icon: 'moon' },
  { value: 'system', icon: 'monitor' },
]

// The appearance menu of nsgo.dev's header: a button showing the chosen appearance's icon that drops down light, dark and system.
export function ThemeSwitch() {
  const copy = copies[useLang()].nav.theme
  // Unknown until the page hydrates; the trigger's icon follows the html element's data-theme-preference meanwhile.
  const [preference, setPreferenceState] = useState<ThemePreference | null>(null)

  useEffect(() => {
    const sync = () => setPreferenceState(readPreference())
    sync()
    const media = matchMedia('(prefers-color-scheme: dark)')
    const onSystem = () => {
      if (readPreference() === 'system') applyTheme()
    }
    document.addEventListener(THEME_EVENT, sync)
    media.addEventListener('change', onSystem)
    return () => {
      document.removeEventListener(THEME_EVENT, sync)
      media.removeEventListener('change', onSystem)
    }
  }, [])

  return (
    <Menu
      label={copy.label}
      className="theme-menu"
      trigger={
        <span className="theme-menu__current">
          {OPTIONS.map((option) => (
            <Icon key={option.value} name={option.icon} size={16} className={`theme-menu__icon theme-menu__icon--${option.value}`} />
          ))}
        </span>
      }
    >
      {OPTIONS.map((option) => (
        <button
          key={option.value}
          type="button"
          className="menu__item"
          role="menuitemradio"
          aria-checked={preference === option.value}
          onClick={() => setPreference(option.value)}
        >
          <Icon name={option.icon} size={15} />
          {copy[option.value]}
          <Icon name="check" size={14} className="menu__check" />
        </button>
      ))}
    </Menu>
  )
}
