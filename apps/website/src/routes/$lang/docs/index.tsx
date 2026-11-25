import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadDoc } from 'virtual:docs'
import { DocView, docHead } from '../../../components/DocView'
import { routeLang } from '../../../i18n'

export const Route = createFileRoute('/$lang/docs/')({
  loader: async ({ params }) => {
    const lang = routeLang(params.lang)
    const doc = await loadDoc(lang, 'index')
    if (!doc) throw notFound()
    return { lang, doc }
  },
  head: ({ loaderData }) => (loaderData ? docHead(loaderData.lang, loaderData.doc) : {}),
  component: DocsIndex,
})

function DocsIndex() {
  return <DocView doc={Route.useLoaderData().doc} />
}
