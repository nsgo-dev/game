import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from '../components/NotFound'
import { copies } from '../copy'

// Every address the site has no page for. Prerendered from /404 to 404.html, which GitHub Pages serves for those addresses, so the page hydrates as the same route there, or as /zh/$ below /zh/.
export const Route = createFileRoute('/$')({
  head: () => ({ meta: [{ title: copies.en.notFound.title }, { name: 'robots', content: 'noindex' }] }),
  component: NotFound,
})
