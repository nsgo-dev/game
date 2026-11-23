import { createFileRoute, notFound } from '@tanstack/react-router'
import { loadApiPackage } from 'virtual:api'
import { ApiPackageView, apiPackageHead, apiPage } from '../../../../components/ApiView'

// A package's page of the API reference, at its path below the reference, such as /zh/docs/api/tooling/pack/.
export const Route = createFileRoute('/zh/docs/api/$')({
  loader: async ({ params }) => {
    const pkg = await loadApiPackage(apiPage(params._splat))
    if (!pkg) throw notFound()
    return { pkg }
  },
  head: ({ loaderData }) => apiPackageHead('zh', loaderData?.pkg),
  component: PackagePage,
})

function PackagePage() {
  return <ApiPackageView pkg={Route.useLoaderData().pkg} />
}
