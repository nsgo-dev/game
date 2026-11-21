import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocView, docHead, findDoc } from '../../../components/DocView'

export const Route = createFileRoute('/zh/docs/')({
  loader: () => {
    const doc = findDoc('zh', 'index')
    if (!doc) throw notFound()
    return { slug: doc.slug }
  },
  head: ({ loaderData }) => docHead('zh', loaderData && findDoc('zh', loaderData.slug)),
  component: DocsIndex,
})

function DocsIndex() {
  const doc = findDoc('zh', Route.useLoaderData().slug)
  return doc ? <DocView doc={doc} /> : null
}
