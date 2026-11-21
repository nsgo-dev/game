import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from '../components/NotFound'

// Every address the site has no page for. Prerendered from /404 to 404.html, which GitHub Pages serves for those addresses, so the page hydrates as the same route there.
export const Route = createFileRoute('/$')({
  head: () => ({ meta: [{ title: 'Page not found — NSGo Game' }, { name: 'robots', content: 'noindex' }] }),
  component: NotFound,
})
