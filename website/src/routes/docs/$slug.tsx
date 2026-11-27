import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadDoc } from 'virtual:docs'
import { DocView, docHead } from '../../components/DocView'

export const Route = createFileRoute('/docs/$slug')({
  loader: async ({ params }) => {
    const doc = params.slug === 'index' ? undefined : await loadDoc('en', params.slug)
    if (!doc) throw notFound()
    return { doc }
  },
  head: ({ loaderData }) => docHead('en', loaderData?.doc),
  component: DocPage,
})

function DocPage() {
  return <DocView doc={Route.useLoaderData().doc} />
}
