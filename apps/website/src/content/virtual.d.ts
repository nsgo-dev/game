// The modules plugins/content.ts serves.

declare module 'virtual:docs' {
  export const docs: import('./types').DocPage[]
}

declare module 'virtual:snippets' {
  export const snippets: import('./types').Snippet[]
}

declare module 'virtual:roadmap' {
  export const roadmap: import('./types').Roadmap
}
