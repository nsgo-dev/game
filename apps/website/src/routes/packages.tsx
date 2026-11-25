import { createFileRoute } from '@tanstack/react-router'
import { Packages, packagesHead } from '../pages/Packages'

export const Route = createFileRoute('/packages')({
  head: () => packagesHead('en'),
  component: Packages,
})
