import { createFileRoute, notFound } from '@tanstack/react-router'
import { DocView, docHead, findDoc } from '../../../components/DocView'
import { routeLang } from '../../../i18n'

export const Route = createFileRoute('/$lang/docs/$slug')({
  loader: ({ params }) => {
    const lang = routeLang(params.lang)
    const doc = params.slug === 'index' ? undefined : findDoc(lang, params.slug)
    if (!doc) throw notFound()
    return { lang, slug: doc.slug }
  },
  head: ({ loaderData }) => (loaderData ? docHead(loaderData.lang, findDoc(loaderData.lang, loaderData.slug)) : {}),
  component: DocPage,
})

function DocPage() {
  const { lang, slug } = Route.useLoaderData()
  const doc = findDoc(lang, slug)
  return doc ? <DocView doc={doc} /> : null
}
