import { createFileRoute } from '@tanstack/react-router'
import { Roadmap, roadmapHead } from '../pages/Roadmap'

export const Route = createFileRoute('/roadmap')({
  head: () => roadmapHead('en'),
  component: Roadmap,
})
