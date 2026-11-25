import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../../i18n'
import { PlaygroundIndex, playgroundHead } from '../../../pages/Playground'

export const Route = createFileRoute('/$lang/playground/')({
  head: ({ params }) => playgroundHead(routeLang(params.lang)),
  component: PlaygroundIndex,
})
