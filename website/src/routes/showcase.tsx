import { createFileRoute } from '@tanstack/react-router'
import { Showcase, showcaseHead } from '../pages/Showcase'

export const Route = createFileRoute('/showcase')({
  head: () => showcaseHead('en'),
  component: Showcase,
})
