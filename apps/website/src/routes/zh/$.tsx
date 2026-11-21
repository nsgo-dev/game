import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from '../../components/NotFound'
import { copies } from '../../copy'

// Every address below /zh/ the site has no page for, in Chinese. GitHub Pages serves the one 404.html for these too, which renders this route in the browser.
export const Route = createFileRoute('/zh/$')({
  head: () => ({ meta: [{ title: copies.zh.notFound.title }, { name: 'robots', content: 'noindex' }] }),
  component: NotFound,
})
