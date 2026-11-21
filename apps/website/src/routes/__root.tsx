/// <reference types="vite/client" />
import { createRootRoute, HeadContent, Outlet, Scripts } from '@tanstack/react-router'
import { type ReactNode, useEffect } from 'react'
import { Footer } from '../components/Footer'
import { Header } from '../components/Header'
import { NotFound } from '../components/NotFound'
import { copies } from '../copy'
import { htmlLang, LANG_KEY, localeHref, useLang, useLangPath, useNotFound } from '../i18n'
import { THEME_SCRIPT } from '../lib/theme'
import css from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'light dark' },
      { name: 'theme-color', content: '#07090d' },
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

// langScript sends a first visit from a Chinese browser to the Chinese twin of an English page, as nsgo.dev does, unless English was chosen with the language menu. zhHref is null on a page with no twin.
const langScript = (zhHref: string | null) => `(function () {
  var zh = ${JSON.stringify(zhHref)};
  if (!zh) return;
  var stored = null;
  try {
    stored = localStorage.getItem('${LANG_KEY}');
  } catch (error) {}
  if (stored === 'en') return;
  var languages = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language];
  var chinese = languages.some(function (language) {
    return String(language).toLowerCase().indexOf('zh') === 0;
  });
  if (stored === 'zh' || (!stored && chinese)) location.replace(zh + location.hash);
})();`

function Document({ children }: { children: ReactNode }) {
  const lang = useLang()
  const path = useLangPath()
  const missing = useNotFound()
  const zhHref = lang === 'en' && !missing ? localeHref('zh', path) : null
  // The appearance is dark until the inline script, which runs before the first paint, applies the stored one, so the html element's attributes differ from the server's by design.
  return (
    <html lang={htmlLang(lang)} data-theme="dark" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
        <script dangerouslySetInnerHTML={{ __html: langScript(zhHref) }} />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  )
}

function Layout() {
  const lang = useLang()
  // 404.html is prerendered in English and hydrates at every address the site has no page for, below /zh/ too, where hydration keeps the html element's lang; set it to the address's.
  useEffect(() => {
    document.documentElement.lang = htmlLang(lang)
  }, [lang])
  return (
    <>
      <a className="skip-link" href="#main">
        {copies[lang].nav.skip}
      </a>
      <Header />
      <main id="main">
        <Outlet />
      </main>
      <Footer />
    </>
  )
}
