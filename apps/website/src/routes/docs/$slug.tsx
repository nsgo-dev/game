import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocView, docHead, findDoc } from '../../components/DocView'

export const Route = createFileRoute('/docs/$slug')({
  loader: ({ params }) => {
    const doc = params.slug === 'index' ? undefined : findDoc(params.slug)
    if (!doc) throw notFound()
    return { slug: doc.slug }
  },
  head: ({ loaderData }) => docHead(loaderData && findDoc(loaderData.slug)),
  component: DocPage,
})

function DocPage() {
  const doc = findDoc(Route.useLoaderData().slug)
  return doc ? <DocView doc={doc} /> : null
}
