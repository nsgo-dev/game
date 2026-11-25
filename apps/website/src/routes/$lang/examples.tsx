import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Examples, examplesHead } from '../../pages/Examples'

export const Route = createFileRoute('/$lang/examples')({
  head: ({ params }) => examplesHead(routeLang(params.lang)),
  component: Examples,
})
