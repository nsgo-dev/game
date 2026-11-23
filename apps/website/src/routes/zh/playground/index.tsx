import { createFileRoute } from '@tanstack/react-router'
import { PlaygroundIndex, playgroundHead } from '../../../pages/Playground'

export const Route = createFileRoute('/zh/playground/')({
  head: () => playgroundHead('zh'),
  component: PlaygroundIndex,
})
