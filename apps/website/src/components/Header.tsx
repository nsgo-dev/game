import { Link } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { copies } from '../copy'
import { routes, useLang } from '../i18n'
import { GITHUB_URL, nsgoUrl } from '../links'
import { Logo } from './Brand'
import { Icon } from './Icon'
import { LanguageSwitch } from './LanguageSwitch'
import { ThemeSwitch } from './ThemeSwitch'

// nsgo.dev's header: the mark and the name, the pages, then the language and appearance menus and GitHub. Narrow screens fold the pages under a menu button.
export function Header() {
  const lang = useLang()
  const copy = copies[lang].nav
  const to = routes[lang]
  const [open, setOpen] = useState(false)
  const [scrolled, setScrolled] = useState(false)

  useEffect(() => {
    const onScroll = () => setScrolled(window.scrollY > 4)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }
    const wide = window.matchMedia('(min-width: 901px)')
    const onWide = () => setOpen(false)
    onScroll()
    window.addEventListener('scroll', onScroll, { passive: true })
    document.addEventListener('keydown', onKey)
    wide.addEventListener('change', onWide)
    return () => {
      window.removeEventListener('scroll', onScroll)
      document.removeEventListener('keydown', onKey)
      wide.removeEventListener('change', onWide)
    }
  }, [])

  const close = () => setOpen(false)
  const className = ['header', scrolled && 'is-scrolled', open && 'is-open'].filter(Boolean).join(' ')
  const current = { 'aria-current': 'page' } as const

  return (
    <header className={className} id="top">
      <div className="container header__inner">
        <Link className="brand" to={to.home} aria-label="NSGo Game">
          <Logo size={26} />
          <span className="brand__name">NSGo Game</span>
        </Link>

        <nav id="site-menu" className="header__links" aria-label={copy.primary}>
          <Link to={to.home} activeOptions={{ exact: true }} activeProps={current} onClick={close}>
            {copy.home}
          </Link>
          <Link to={to.docs} activeProps={current} onClick={close}>
            {copy.docs}
          </Link>
          <Link to={to.showcase} activeProps={current} onClick={close}>
            {copy.showcase}
          </Link>
          <Link to={to.roadmap} activeProps={current} onClick={close}>
            {copy.roadmap}
          </Link>
          <a href={nsgoUrl(lang)} target="_blank" rel="noopener noreferrer" onClick={close}>
            {copy.nsgo}
            <Icon name="external" size={13} className="header__external" />
          </a>
        </nav>

        <div className="header__actions">
          <LanguageSwitch />
          <ThemeSwitch />
          <a className="header__github" href={GITHUB_URL} target="_blank" rel="noopener noreferrer" aria-label={copy.github}>
            <Icon name="github" size={18} />
          </a>
          <button
            type="button"
            className="icon-button header__burger"
            aria-expanded={open}
            aria-controls="site-menu"
            aria-label={open ? copy.close : copy.menu}
            onClick={() => setOpen((was) => !was)}
          >
            <Icon name="menu" size={18} className="header__burger-open" />
            <Icon name="close" size={18} className="header__burger-close" />
          </button>
        </div>
      </div>
    </header>
  )
}
