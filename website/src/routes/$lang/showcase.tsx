import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Showcase, showcaseHead } from '../../pages/Showcase'

export const Route = createFileRoute('/$lang/showcase')({
  head: ({ params }) => showcaseHead(routeLang(params.lang)),
  component: Showcase,
})
