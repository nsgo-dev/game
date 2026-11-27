import { createFileRoute } from '@tanstack/react-router'
import { Examples, examplesHead } from '../pages/Examples'

export const Route = createFileRoute('/examples')({
  head: () => examplesHead('en'),
  component: Examples,
})
