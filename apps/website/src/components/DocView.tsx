import { Link } from '@tanstack/react-router'
import { docs } from 'virtual:docs'
import type { DocPage } from '../content/types'

export function findDoc(slug: string): DocPage | undefined {
  return docs.find((d) => d.slug === slug)
}

export function docHead(doc: DocPage | undefined) {
  if (!doc) return {}
  return {
    meta: [{ title: `${doc.title} · game docs` }, { name: 'description', content: doc.description }],
  }
}

export function DocView({ doc }: { doc: DocPage }) {
  return (
    <div className="docs">
      <nav className="docs-nav" aria-label="Docs">
        <ul>
          {docs.map((d) => (
            <li key={d.slug}>
              {d.slug === 'index' ? (
                <Link to="/docs/" activeOptions={{ exact: true }} activeProps={{ 'aria-current': 'page' }}>
                  {d.title}
                </Link>
              ) : (
                <Link to="/docs/$slug/" params={{ slug: d.slug }} activeProps={{ 'aria-current': 'page' }}>
                  {d.title}
                </Link>
              )}
            </li>
          ))}
        </ul>
      </nav>
      <article className="prose">
        <h1>{doc.title}</h1>
        <div dangerouslySetInnerHTML={{ __html: doc.html }} />
      </article>
    </div>
  )
}
