import { createFileRoute } from '@tanstack/react-router'
import { NotFound } from '../components/NotFound'
import { copies } from '../copy'

// Explicit /404 route for TanStack Start's static 404.html SPA shell so it takes precedence over /$lang.
export const Route = createFileRoute('/404')({
  head: () => ({ meta: [{ title: copies.en.notFound.title }, { name: 'robots', content: 'noindex' }] }),
  component: NotFound,
})
