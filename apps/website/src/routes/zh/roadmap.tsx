import { createFileRoute } from '@tanstack/react-router'
import { Roadmap, roadmapHead } from '../../pages/Roadmap'

export const Route = createFileRoute('/zh/roadmap')({
  head: () => roadmapHead('zh'),
  component: Roadmap,
})
