import { createFileRoute } from '@tanstack/react-router'
import { ApiIndexView, apiIndexHead } from '../../../components/ApiView'

export const Route = createFileRoute('/docs/api/')({
  head: () => apiIndexHead('en'),
  component: ApiIndexView,
})
