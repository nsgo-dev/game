import { createFileRoute } from '@tanstack/react-router'
import { Showcase, showcaseHead } from '../../pages/Showcase'

export const Route = createFileRoute('/zh/showcase')({
  head: () => showcaseHead('zh'),
  component: Showcase,
})
