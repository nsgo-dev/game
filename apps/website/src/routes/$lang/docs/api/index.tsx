import { createFileRoute } from '@tanstack/react-router'
import { ApiIndexView, apiIndexHead } from '../../../../components/ApiView'
import { routeLang } from '../../../../i18n'

export const Route = createFileRoute('/$lang/docs/api/')({
  head: ({ params }) => apiIndexHead(routeLang(params.lang)),
  component: ApiIndexView,
})
