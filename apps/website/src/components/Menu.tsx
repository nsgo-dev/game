import { type KeyboardEvent as ReactKeyboardEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { Icon } from './Icon'

// nsgo.dev's Menu: a button that drops down a list of links or choices, the `.menu__item`s in children. A press elsewhere, Escape or choosing an item closes it; the arrow keys move between items.
export function Menu({ label, trigger, className, children }: { label: string; trigger: ReactNode; className?: string; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const button = useRef<HTMLButtonElement>(null)
  const list = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    list.current?.querySelector<HTMLElement>('.menu__item')?.focus()
    const onPointer = (event: MouseEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      setOpen(false)
      button.current?.focus()
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
    <div className={className ? `menu menu--end ${className}` : 'menu menu--end'} ref={root}>
      <button
        ref={button}
        type="button"
        className="menu__trigger"
        aria-haspopup="true"
        aria-expanded={open}
        aria-label={label}
        title={label}
        onClick={() => setOpen((was) => !was)}
      >
        {trigger}
        <Icon name="chevron" size={14} className="menu__chevron" />
      </button>
      <div
        ref={list}
        className="menu__list"
        role="menu"
        hidden={!open}
        onKeyDown={onListKey}
        onClick={(event) => {
          if ((event.target as HTMLElement).closest('.menu__item')) setOpen(false)
        }}
      >
        {children}
      </div>
    </div>
  )
}
