import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadDoc } from 'virtual:docs'
import { DocView, docHead } from '../../components/DocView'

export const Route = createFileRoute('/docs/')({
  loader: async () => {
    const doc = await loadDoc('en', 'index')
    if (!doc) throw notFound()
    return { doc }
  },
  head: ({ loaderData }) => docHead('en', loaderData?.doc),
  component: DocsIndex,
})

function DocsIndex() {
  return <DocView doc={Route.useLoaderData().doc} />
}
