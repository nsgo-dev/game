import { createFileRoute } from '@tanstack/react-router'
import { routeLang } from '../../i18n'
import { Packages, packagesHead } from '../../pages/Packages'

export const Route = createFileRoute('/$lang/packages')({
  head: ({ params }) => packagesHead(routeLang(params.lang)),
  component: Packages,
})
