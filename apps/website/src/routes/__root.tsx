/// <reference types="vite/client" />
import { createRootRoute, HeadContent, Link, Outlet, Scripts } from '@tanstack/react-router'
import type { ReactNode } from 'react'
import { Logo } from '../components/Logo'
import { NotFound } from '../components/NotFound'
import { GITHUB_URL, NSGO_URL } from '../links'
import css from '../styles.css?url'


export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'light dark' },
      { title: 'game: a Go game layer for NSGo' },
      { name: 'description', content: 'A game layer for NSGo apps, written in Go: a fixed-step loop, sprites and sound, then 3D.' },
    ],
    links: [
      { rel: 'stylesheet', href: css },
      { rel: 'icon', href: `${import.meta.env.BASE_URL}favicon.svg`, type: 'image/svg+xml' },
    ],
  }),
  shellComponent: Document,
  component: Layout,
  notFoundComponent: NotFound,
})

function Document({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <head>
        <HeadContent />
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
      <a className="skip" href="#main">
        Skip to content
      </a>
      <header className="site-header">
        <div className="wrap header-row">
          <Link to="/" className="brand" aria-label="game home">
            <Logo />
            <span>game</span>
          </Link>
          <nav aria-label="Main">
            <ul className="nav">
              <li>
                <Link to="/docs/" activeProps={{ 'aria-current': 'page' }} activeOptions={{ exact: false }}>
                  Docs
                </Link>
              </li>
              <li>
                <Link to="/roadmap/" activeProps={{ 'aria-current': 'page' }}>
                  Roadmap
                </Link>
              </li>
              <li>
                <a href={GITHUB_URL}>GitHub</a>
              </li>
            </ul>
          </nav>
        </div>
      </header>
      <main id="main" className="wrap">
        <Outlet />
      </main>
      <footer className="site-footer">
        <div className="wrap footer-row">
          <span>Apache-2.0</span>
          <a href={GITHUB_URL}>nsgo-dev/game</a>
          <a href={NSGO_URL}>NSGo</a>
        </div>
      </footer>
    </>
  )
}
