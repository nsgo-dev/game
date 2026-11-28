# Agent Note: Repository checks for public API doc comments

Status: implemented

## Problem

The website catalog and `api.json` derive package synopses and declaration documentation from Go doc comments, but missing or detached comments were only visible when someone ran `go doc` or inspected the generated API output. The `sound` and `sprite` packages had no overview, the `physics` overview was split into unattached comment groups, and the particle editor's public editing surface lacked the units and valid ranges needed by API consumers.

## Decision

`internal/repocheck/doc_comments_test.go` parses the root package and every non-test, non-internal package under `pkg/` with `go/parser`. It requires an attached `Package <name>` comment for each package and a doc comment on every exported function, exported type, and exported method whose receiver type is exported. Methods on unexported receiver types remain outside the public API check. Focused synthetic-source tests prove that missing function, method, and type comments are rejected and that a blank line before `package` detaches and invalidates a package comment.

The missing API comments live beside their declarations. Particle setter comments name the affected `particles.Config` field and state units and accepted ranges where applicable; `tilemap.LoadTiled` keeps its existing loader documentation but attaches it to the function rather than the adjacent pool variable.

## Alternatives considered

- **Relying on manual `go doc` review** — useful for spot checks, but it cannot prevent another exported declaration or package overview from silently losing its comment; the parser check runs with the existing repository checks and reports exact source positions.
- **Adding a third-party documentation linter** — unnecessary for the acceptance scope because the standard library AST already exposes attached declaration and package comments without adding a dependency or another tool configuration.

## Consequences

- **Benefits**: the generated API reference has complete package overviews for the reported packages, the particle editing API exposes units and ranges, and CI catches missing public package, function, method, and type comments before generation.
- **Costs and known limits**: the check parses source rather than type-checking build-specific package variants, and it intentionally follows the acceptance scope for packages, functions, methods, and types rather than enforcing prose quality or every constant and variable comment.
