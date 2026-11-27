import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Tutorial, tutorialHead } from '../../pages/Tutorial'

export const Route = createFileRoute('/$lang/tutorial')({
  head: ({ params }) => tutorialHead(routeLang(params.lang)),
  component: Tutorial,
})
