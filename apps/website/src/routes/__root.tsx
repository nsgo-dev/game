/// <reference types="vite/client" />
import { createRootRoute, HeadContent, Outlet, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Footer } from '../components/Footer'
import { Header } from '../components/Header'
import { NotFound } from '../components/NotFound'
import { THEME_SCRIPT } from '../lib/theme'
import css from '../styles.css?url'


export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'light dark' },
      { name: 'theme-color', content: '#ffffff' },
      { title: 'NSGo Game: a Go game layer for NSGo' },
      { name: 'description', content: 'A game layer for NSGo apps, written in Go: a fixed-step loop, sprites and sound, then 3D.' },
    ],
    links: [
      { rel: 'stylesheet', href: css },
      { rel: 'icon', href: `${import.meta.env.BASE_URL}icon.png`, type: 'image/png' },
      { rel: 'apple-touch-icon', href: `${import.meta.env.BASE_URL}apple-touch-icon.png` },
    ],
  }),
  shellComponent: Document,
  component: Layout,
  notFoundComponent: NotFound,
})

function Document({ children }: { children: ReactNode }) {
  // The appearance is light until the inline script, which runs before the first paint, applies the stored one, so the html element's attributes differ from the server's by design.
  return (
    <html lang="en" data-theme="light" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function Layout() {
  return (
    <>
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <Header />
      <main id="main">
        <Outlet />
      </main>
      <Footer />
    </>
  )
}
