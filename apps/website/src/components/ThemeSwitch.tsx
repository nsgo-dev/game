import { type KeyboardEvent as ReactKeyboardEvent, useEffect, useRef, useState } from 'react'
import { applyTheme, readPreference, setPreference, THEME_EVENT, type ThemePreference } from '../lib/theme'
import { Icon, type IconName } from './Icon'

const OPTIONS: { value: ThemePreference; icon: IconName; label: string }[] = [
  { value: 'light', icon: 'sun', label: 'Light' },
  { value: 'dark', icon: 'moon', label: 'Dark' },
  { value: 'system', icon: 'monitor', label: 'System' },
]

// The appearance menu of nsgo.dev's header: a button showing the chosen appearance's icon that drops down light, dark and system. A press elsewhere, Escape or choosing an item closes it; the arrow keys move between items.
export function ThemeSwitch() {
  const [open, setOpen] = useState(false)
  // Unknown until the page hydrates; the trigger's icon follows the html element's data-theme-preference meanwhile.
  const [preference, setPreferenceState] = useState<ThemePreference | null>(null)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)

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

  useEffect(() => {
    if (!open) return
    list.current?.querySelector<HTMLElement>('.menu__item')?.focus()
    const onPointer = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpen(false)
      trigger.current?.focus()
    }
    document.addEventListener('click', onPointer)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('click', onPointer)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const onListKey = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const step = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (!step || !list.current) return
    event.preventDefault()
    const items = Array.from(list.current.querySelectorAll<HTMLElement>('.menu__item'))
    const index = items.indexOf(document.activeElement as HTMLElement)
    items[(index + step + items.length) % items.length]?.focus()
  }

  return (
    <div className="menu menu--end theme-menu" ref={root}>
      <button
        ref={trigger}
        type="button"
        className="menu__trigger"
        aria-haspopup="true"
        aria-expanded={open}
        aria-label="Appearance"
        title="Appearance"
        onClick={() => setOpen((was) => !was)}
      >
        <span className="theme-menu__current">
          {OPTIONS.map((option) => (
            <Icon key={option.value} name={option.icon} size={16} className={`theme-menu__icon theme-menu__icon--${option.value}`} />
          ))}
        </span>
        <Icon name="chevron" size={14} className="menu__chevron" />
      </button>
      <div ref={list} className="menu__list" role="menu" hidden={!open} onKeyDown={onListKey}>
        {OPTIONS.map((option) => (
          <button
            key={option.value}
            type="button"
            className="menu__item"
            role="menuitemradio"
            aria-checked={preference === option.value}
            onClick={() => {
              setPreference(option.value)
              setOpen(false)
            }}
          >
            <Icon name={option.icon} size={15} />
            {option.label}
            <Icon name="check" size={14} className="menu__check" />
          </button>
        ))}
      </div>
    </div>
  )
}
