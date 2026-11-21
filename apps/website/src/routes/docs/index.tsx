import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocView, docHead, findDoc } from '../../components/DocView'

export const Route = createFileRoute('/docs/')({
  loader: () => {
    const doc = findDoc('index')
    if (!doc) throw notFound()
    return { slug: doc.slug }
  },
  head: ({ loaderData }) => docHead(loaderData && findDoc(loaderData.slug)),
  component: DocsIndex,
})

function DocsIndex() {
  const doc = findDoc(Route.useLoaderData().slug)
  return doc ? <DocView doc={doc} /> : null
}
