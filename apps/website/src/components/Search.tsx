import { useEffect, useRef } from 'react'
import { copies } from '../copy'
import { useLang } from '../i18n'

// Pagefind's search over the docs: the build indexes every docs page after the prerender (package.json's build), one index per language, and the header's button opens its interface in a dialog, loading it only then.

interface PagefindUIInstance {
  triggerSearch?: (term: string) => void
  destroy?: () => void
}

declare global {
  interface Window {
    PagefindUI?: new (options: Record<string, unknown>) => PagefindUIInstance
  }
}

const base = import.meta.env.BASE_URL

function loadPagefind(): Promise<void> {
  if (window.PagefindUI) return Promise.resolve()
  return new Promise((resolve, reject) => {
    const css = document.createElement('link')
    css.rel = 'stylesheet'
    css.href = `${base}pagefind/pagefind-ui.css`
    document.head.append(css)
    const script = document.createElement('script')
    script.src = `${base}pagefind/pagefind-ui.js`
    script.onload = () => resolve()
    script.onerror = () => reject(new Error('no search index'))
    document.head.append(script)
  })
}

export function Search() {
  const copy = copies[useLang()].nav
  const dialog = useRef<HTMLDialogElement>(null)
  const box = useRef<HTMLDivElement>(null)
  const ui = useRef<PagefindUIInstance | null>(null)

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        open()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      ui.current?.destroy?.()
    }
  }, [])

  const open = () => {
    dialog.current?.showModal()
    if (ui.current) {
      box.current?.querySelector('input')?.focus()
      return
    }
    loadPagefind()
      .then(() => {
        if (!window.PagefindUI || !box.current || ui.current) return
        ui.current = new window.PagefindUI({ element: box.current, showSubResults: true, showImages: false, resetStyles: false, autofocus: true })
      })
      // A dev server has no index until a build writes one; the dialog stays empty.
      .catch(() => {})
  }

  return (
    <>
      <button type="button" className="search-button" onClick={open} aria-label={copy.search}>
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <circle cx="11" cy="11" r="7" />
          <path d="M20 20l-3.5-3.5" />
        </svg>
        <span className="search-button__label">{copy.search}</span>
      </button>
      <dialog
        ref={dialog}
        className="search-dialog"
        aria-label={copy.search}
        onClick={(event) => {
          if (event.target === dialog.current) dialog.current?.close()
        }}
        onClose={() => window.getSelection()?.removeAllRanges()}
      >
        <div className="search-dialog__panel">
          <div ref={box} />
        </div>
      </dialog>
    </>
  )
}
