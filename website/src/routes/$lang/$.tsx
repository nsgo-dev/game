import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from '../../components/NotFound'
import { copies } from '../../copy'
import { routeLang } from '../../i18n'

// Every address below /<lang>/ the site has no page for, in that language. GitHub Pages serves the one 404.html for these too, which renders this route in the browser.
export const Route = createFileRoute('/$lang/$')({
  head: ({ params }) => ({ meta: [{ title: copies[routeLang(params.lang)].notFound.title }, { name: 'robots', content: 'noindex' }] }),
  component: NotFound,
})
