/// <reference types="vite/client" />
import { createRootRoute, HeadContent, Outlet, Scripts } from '@tanstack/react-router'
import { type ReactNode, useEffect } from 'react'
import { Footer } from '../components/Footer'
import { Header } from '../components/Header'
import { NotFound } from '../components/NotFound'
import { copies } from '../copy'
import { htmlLang, LANG_KEY, NON_DEFAULT_LANGS, useLang, useLangPath, useNotFound } from '../i18n'
import { THEME_SCRIPT } from '../lib/theme'
import css from '../styles.css?url'

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'light dark' },
      { name: 'theme-color', content: '#f3f5f9' },
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

// langScript sends a first visit on an English page to the visitor's preferred supported language, respecting any choice saved via the language menu. path is null on non-English pages and on pages with no localized twin.
const langScript = (base: string, path: string | null) => `(function () {
  var path = ${JSON.stringify(path)};
  if (path === null) return;
  var base = ${JSON.stringify(base)};
  var supported = ${JSON.stringify(NON_DEFAULT_LANGS)};
  var stored = null;
  try {
    stored = localStorage.getItem('${LANG_KEY}');
  } catch (error) {}
  if (stored === 'en') return;
  var target = null;
  if (stored && supported.indexOf(stored) >= 0) {
    target = stored;
  } else if (!stored) {
    var list = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language];
    for (var i = 0; i < list.length; i++) {
      var raw = String(list[i] || '').trim().toLowerCase().replace(/_/g, '-');
      if (!raw) continue;
      var parts = raw.split('-');
      var primary = parts[0];
      if (primary === 'en') break;
      if (primary === 'zh') {
        var hans = parts.indexOf('hans') >= 0;
        var hant = !hans && (parts.indexOf('hant') >= 0 || parts.indexOf('tw') >= 0 || parts.indexOf('hk') >= 0 || parts.indexOf('mo') >= 0);
        target = hant ? 'zh-tw' : 'zh';
        break;
      }
      if (supported.indexOf(primary) >= 0) {
        target = primary;
        break;
      }
    }
  }
  if (target) location.replace(base + target + '/' + path + location.search + location.hash);
})();`

function Document({ children }: { children: ReactNode }) {
  const lang = useLang()
  const path = useLangPath()
  const missing = useNotFound()
  const redirectPath = lang === 'en' && !missing ? path : null
  // The appearance is light until the inline script, which runs before the first paint, applies the stored one, so the html element's attributes differ from the server's by design.
  return (
    <html lang={htmlLang(lang)} data-theme="light" suppressHydrationWarning>
      <head>
        <HeadContent />
        <script dangerouslySetInnerHTML={{ __html: THEME_SCRIPT }} />
        <script dangerouslySetInnerHTML={{ __html: langScript(import.meta.env.BASE_URL, redirectPath) }} />
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
  // 404.html is prerendered in English and hydrates at every address the site has no page for, below /<lang>/ too, where hydration keeps the html element's lang; set it to the address's.
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
