# Agent Note: Spec-first OpenAPI 3.1 contracts for HTTP services

Status: implemented

## Problem

The self-hosted HTTP services (`pkg/online/cloudsave` and `pkg/online/leaderboard/selfhost`) previously described their wire protocol only in Go doc comments and Markdown tables. Third-party clients (non-Go games, web dashboards, backend score signers) had to read Go source to integrate, and nothing in CI prevented a handler change—a new route, renamed JSON field, added response header, or new error code—from silently drifting away from the documented contract.

## Decision

Each HTTP service keeps a hand-written OpenAPI 3.1 document in `openapi.yaml` beside its `http.go` as the source of truth for its HTTP contract, with an explicit `info.version`, strict object schemas (`additionalProperties: false`), bearer token security scheme, explicit response headers (`Cache-Control`, `ETag`, `Save-Version`, `Last-Modified`, `WWW-Authenticate`), and per-status error code enums. Each `Handler()` embeds its `openapi.yaml` with `//go:embed` and serves it unauthenticated at `GET /v1/openapi.yaml` (inside `withCORS` on `cloudsave` so allowed browser origins can fetch it). Both specs are also published next to the YAML format JSON Schemas in `apps/website/public/schemas/` (`cloudsave-openapi.v1.yaml` and `leaderboard-selfhost-openapi.v1.yaml`) and linked from `docs/en/` and `docs/zh/`.

Contract drift is caught at four layers in CI:

1. **OpenAPI 3.1 schema validation**: each package's `openapi_test.go` loads its embedded `openapi.yaml` with `github.com/pb33f/libopenapi` and validates it against the OpenAPI 3.1 specification using `github.com/pb33f/libopenapi-validator` (test-only dependencies imported exclusively from `_test.go` files so no shipped binary links them).
2. **Route exhaustiveness**: each `Service.newMux()` registers every route through a local recording `handle(pattern, h)` helper, and `TestOpenAPIRoutesMatchHandler` asserts a 1-to-1 match against `openapi.yaml`'s operations plus checks `http.go`'s AST so direct `mux.Handle`/`mux.HandleFunc` calls cannot bypass recording.
3. **Error code exhaustiveness**: `writeError` reads a package-level `errorMappings` table, and `TestOpenAPIErrorCodesMatchHandler` asserts every mapped status/code pair and every `errorResponse` literal in `http.go` matches the error enums in `openapi.yaml`.
4. **Live handler request/response and header validation**: `TestOpenAPIContract` drives every operation in `openapi.yaml` against `Handler()` via `httptest` for both success and documented error cases, running `libopenapi-validator` with `WithStrictMode()` and `WithFormatAssertions()` plus an explicit response-header check that rejects any undocumented header or missing required header. `internal/repocheck/openapi_test.go` verifies that the published copies in `apps/website/public/schemas/` match the embedded `openapi.yaml` files byte-for-byte and are linked from both English and Chinese guides.

When changing a service's HTTP API, update `openapi.yaml` first, copy it to `apps/website/public/schemas/`, update the handler and contract test table, and run `go test -run TestOpenAPI ./pkg/online/cloudsave ./pkg/online/leaderboard/selfhost` and `go tool nsgo test ./internal/repocheck`.

## Alternatives considered

- **Generating OpenAPI from Go structs or handler annotations** — couples wire schema names and comments to Go internals and can accidentally ratify unintended handler changes instead of flagging contract drift; hand-written YAML keeps contract diffs explicit in code review.
- **`github.com/getkin/kin-openapi`** — has only partial OpenAPI 3.1 support, whereas `libopenapi` and `libopenapi-validator` natively support OpenAPI 3.1 and JSON Schema 2020-12 (matching `tools/gameschema`).
- **Putting the validator only in `tools/`** — would have required exporting internal route and error tables from the service packages; importing the validator only in package-internal `_test.go` files keeps `newMux` and `errorMappings` unexported while still keeping every shipped binary free of the test dependency.

## Consequences

- **Benefits**: third-party clients can fetch `GET /v1/openapi.yaml` from a running deployment or download the published spec from `https://game.nsgo.dev/schemas/`, reviewers see every HTTP contract change in `openapi.yaml` diffs, and CI fails immediately if a route, JSON field, header, or error code drifts.
- **Costs and known limits**: editing `openapi.yaml` requires keeping its published copy under `apps/website/public/schemas/` in sync (enforced by `internal/repocheck`). `pkg/online/accounts/http.go` is not covered yet and remains a follow-up using the same pattern.
