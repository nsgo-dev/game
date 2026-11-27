import { createFileRoute, notFound } from '@tanstack/react-router'
import { findPlaygroundExample, PlaygroundExamplePage, playgroundExampleHead } from '../../pages/Playground'

export const Route = createFileRoute('/playground/$example')({
  loader: ({ params }) => {
    const example = findPlaygroundExample(params.example)
    if (!example) throw notFound()
    return { id: example.id }
  },
  head: ({ loaderData }) => playgroundExampleHead('en', loaderData?.id),
  component: ExamplePage,
})

function ExamplePage() {
  const example = findPlaygroundExample(Route.useLoaderData().id)
  return example ? <PlaygroundExamplePage example={example} /> : null
}
