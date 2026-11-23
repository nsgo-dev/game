// The modules plugins/content.ts serves.

declare module 'virtual:docs' {
  // Each language's pages, in the same order.
  export const docs: Record<import('../i18n').Lang, import('./types').DocPage[]>
}

declare module 'virtual:snippets' {
  export const snippets: import('./types').Snippet[]
}

declare module 'virtual:roadmap' {
  export const roadmap: import('./types').Roadmap
}

declare module 'virtual:playground' {
  export const examples: import('./types').PlaygroundExample[]
  // sources loads an example's highlighted files.
  export const sources: Record<import('./playground').PlaygroundId, () => Promise<import('./types').PlaygroundSource>>
}

declare module 'virtual:api' {
  // The packages of the API reference, by import path.
  export const api: import('./types').ApiIndex
  // loadApiPackage is the package whose page is page, such as tooling/pack, or undefined when there is none.
  export function loadApiPackage(page: string): Promise<import('./types').ApiPackage | undefined>
}
