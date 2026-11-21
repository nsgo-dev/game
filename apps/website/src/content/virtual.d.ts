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
