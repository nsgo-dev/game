import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadApiPackage } from 'virtual:api'
import { ApiPackageView, apiPackageHead, apiPage } from '../../../../components/ApiView'
import { routeLang } from '../../../../i18n'

export const Route = createFileRoute('/$lang/docs/api/$')({
  loader: async ({ params }) => {
    const lang = routeLang(params.lang)
    const pkg = await loadApiPackage(apiPage(params._splat))
    if (!pkg) throw notFound()
    return { lang, pkg }
  },
  head: ({ loaderData }) => (loaderData ? apiPackageHead(loaderData.lang, loaderData.pkg) : {}),
  component: PackagePage,
})

function PackagePage() {
  return <ApiPackageView pkg={Route.useLoaderData().pkg} />
}
