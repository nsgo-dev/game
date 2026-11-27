import { createFileRoute, notFound } from '@tanstack/react-router'
import { routeLang } from '../../../i18n'
import { findPlaygroundExample, PlaygroundExamplePage, playgroundExampleHead } from '../../../pages/Playground'

export const Route = createFileRoute('/$lang/playground/$example')({
  loader: ({ params }) => {
    const lang = routeLang(params.lang)
    const example = findPlaygroundExample(params.example)
    if (!example) throw notFound()
    return { lang, id: example.id }
  },
  head: ({ loaderData }) => (loaderData ? playgroundExampleHead(loaderData.lang, loaderData.id) : {}),
  component: ExamplePage,
})

function ExamplePage() {
  const example = findPlaygroundExample(Route.useLoaderData().id)
  return example ? <PlaygroundExamplePage example={example} /> : null
}
