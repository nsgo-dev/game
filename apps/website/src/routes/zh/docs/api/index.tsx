import { createFileRoute } from '@tanstack/react-router'
import { ApiIndexView, apiIndexHead } from '../../../../components/ApiView'

export const Route = createFileRoute('/zh/docs/api/')({
  head: () => apiIndexHead('zh'),
  component: ApiIndexView,
})
