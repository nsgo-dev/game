import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Home, homeHead } from '../../pages/Home'

export const Route = createFileRoute('/$lang/')({
  head: ({ params }) => homeHead(routeLang(params.lang)),
  component: Home,
})
