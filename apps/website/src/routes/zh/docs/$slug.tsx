import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocView, docHead, findDoc } from '../../../components/DocView'

export const Route = createFileRoute('/zh/docs/$slug')({
  loader: ({ params }) => {
    const doc = params.slug === 'index' ? undefined : findDoc('zh', params.slug)
    if (!doc) throw notFound()
    return { slug: doc.slug }
  },
  head: ({ loaderData }) => docHead('zh', loaderData && findDoc('zh', loaderData.slug)),
  component: DocPage,
})

function DocPage() {
  const doc = findDoc('zh', Route.useLoaderData().slug)
  return doc ? <DocView doc={doc} /> : null
}
