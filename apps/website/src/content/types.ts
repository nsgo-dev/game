// The API reference: what cmd/gameapi in packages/game writes from the Go packages' source and doc comments (tooling/apiref's Reference, whose doc comments say more of each field), run with -html.

export interface ApiReference {
  version: number
  module: string
  packages: (ApiPackage & { doc: string })[]
}

// An exported identifier and its anchor on its package's page: Name, or Type.Name for a method, a field or an interface method.
export interface ApiIdent {
  name: string
  anchor: string
}

interface ApiDecl {
  // decl is the declaration's Go source, gofmt-formatted, without its doc comment.
  decl: string
  // docHTML is the doc comment rendered as HTML; its links to another package are relative to this package's page.
  docHTML?: string
  // file is the declaration's file below packages/game, and line its line.
  file: string
  line: number
  // platforms are the targets that declare it, absent when every target that builds the package does.
  platforms?: string[]
}

export interface ApiValue extends ApiDecl {
  idents: ApiIdent[]
}

export interface ApiFunc extends ApiDecl, ApiIdent {
  // recv is a method's receiver, such as *Batch.
  recv?: string
}

export interface ApiMember extends ApiIdent {
  kind: 'field' | 'method'
}

export interface ApiType extends ApiDecl, ApiIdent {
  members: ApiMember[]
  consts: ApiValue[]
  vars: ApiValue[]
  funcs: ApiFunc[]
  methods: ApiFunc[]
}

// A package's page: the reference's package without the doc comments' text, which the page shows as HTML.
export interface ApiPackage {
  importPath: string
  name: string
  // page is the package's path below the reference's root, such as sprite or tooling/pack.
  page: string
  synopsis: string
  docHTML?: string
  platforms?: string[]
  consts: ApiValue[]
  vars: ApiValue[]
  funcs: ApiFunc[]
  types: ApiType[]
}

export interface ApiSummary {
  importPath: string
  name: string
  page: string
  synopsis: string
  platforms: string[]
}

export type ApiIndex = { status: 'ok'; packages: ApiSummary[] } | { status: 'unavailable'; reason: string }
