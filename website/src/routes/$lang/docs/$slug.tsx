import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadDoc } from 'virtual:docs'
import { DocView, docHead } from '../../../components/DocView'
import { routeLang } from '../../../i18n'

export const Route = createFileRoute('/$lang/docs/$slug')({
  loader: async ({ params }) => {
    const lang = routeLang(params.lang)
    const doc = params.slug === 'index' ? undefined : await loadDoc(lang, params.slug)
    if (!doc) throw notFound()
    return { lang, doc }
  },
  head: ({ loaderData }) => (loaderData ? docHead(loaderData.lang, loaderData.doc) : {}),
  component: DocPage,
})

function DocPage() {
  return <DocView doc={Route.useLoaderData().doc} />
}
