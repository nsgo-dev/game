import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Roadmap, roadmapHead } from '../../pages/Roadmap'

export const Route = createFileRoute('/$lang/roadmap')({
  head: ({ params }) => roadmapHead(routeLang(params.lang)),
  component: Roadmap,
})
