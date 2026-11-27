import { createFileRoute } from '@tanstack/react-router'
import { Tutorial, tutorialHead } from '../pages/Tutorial'

export const Route = createFileRoute('/tutorial')({
  head: () => tutorialHead('en'),
  component: Tutorial,
})
