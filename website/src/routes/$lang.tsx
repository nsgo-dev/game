import { createFileRoute, notFound, Outlet } from '@tanstack/react-router'
import { isNonDefaultLang } from '../locales'

// Validates the locale prefix for all non-English routes (/<lang>/...). Unknown top-level segments throw notFound() and render the English 404 page.
export const Route = createFileRoute('/$lang')({
  beforeLoad: ({ params }) => {
    if (!isNonDefaultLang(params.lang)) throw notFound()
  },
  component: Outlet,
})
