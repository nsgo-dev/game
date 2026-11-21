import { createFileRoute } from '@tanstack/react-router'
import { Home, homeHead } from '../../pages/Home'

export const Route = createFileRoute('/zh/')({
  head: () => homeHead('zh'),
  component: Home,
})
