# Agent Note: Shared CORS middleware and `allowedOrigins` for self-hosted online services

Status: implemented

## Problem

Web builds calling self-hosted services from another origin require CORS preflight handling and `Access-Control-Allow-Origin` response headers, especially because requests carry `Authorization: Bearer` and `Content-Type: application/json`. Previously only `pkg/online/cloudsave` supported `allowedOrigins` and CORS handling; `pkg/online/accounts` (`Service.Handler`) and `pkg/online/leaderboard/selfhost` (`Service.Handler`) had no `allowedOrigins` configuration and no `OPTIONS` preflight or `Access-Control-*` headers, so browser preflight requests returned `405 Method Not Allowed` and web builds could neither sign in to obtain tokens nor submit/read scores unless served from the exact same origin behind one reverse proxy.

## Decision

Extract origin validation and the HTTP CORS wrapper into a shared stdlib-only internal package, `pkg/online/internal/cors`, and use it across all three self-hosted services (`accounts`, `cloudsave`, and `leaderboard/selfhost`).

### Config format contract

- `accounts.Config` (`accounts.yaml`) and `selfhost.Config` (`leaderboards.yaml`) gain `AllowedOrigins []string` (`yaml:"allowedOrigins,omitempty"`), matching `cloudsave.Config` (`cloudsave.yaml`).
- `cors.ValidateOrigins` enforces the same rules in all three packages: each entry must be an `http` or `https` origin with a non-empty host and no path, query, fragment, userinfo, wildcard (`*`), trailing colon, or port outside 1..65535; duplicates are refused.
- `ParseConfig` normalizes an empty `allowedOrigins: []` to `nil`, and `Config.Marshal` omits `allowedOrigins` when empty, so existing configs without `allowedOrigins` remain byte-for-byte compatible and canonical files round-trip unchanged.
- `Service.New` and `Service.Config` clone `AllowedOrigins` so caller-side slice mutation after construction cannot alter the live handler policy.

### HTTP CORS contract

- `cors.Wrap` inspects `Origin` on every request. Native clients without `Origin` pass straight through with no `Vary` or `Access-Control-*` headers.
- Requests carrying `Origin` always receive `Vary: Origin`.
- Preflight requests (`OPTIONS` with non-empty `Access-Control-Request-Method`) receive `Vary: Access-Control-Request-Method`, `Vary: Access-Control-Request-Headers`, and `Cache-Control: no-store`. Listed origins receive `204 No Content` with `Access-Control-Allow-Origin: <origin>`, `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`, and `Access-Control-Max-Age: 600`; unlisted origins (including when `allowedOrigins` is empty) receive `403 Forbidden` with no `Access-Control-Allow-*` headers.
- Non-preflight requests from listed origins receive `Access-Control-Allow-Origin: <origin>` (and `Access-Control-Expose-Headers` when configured) before the inner mux runs, so JSON error responses such as `401 invalid_token` remain readable by web clients.
- Each service declares only the methods and headers its routes use:
  - `accounts`: `GET, POST`; `Authorization, Content-Type`.
  - `leaderboard/selfhost`: `GET, POST`; `Authorization, Content-Type`.
  - `cloudsave`: `GET, PUT, DELETE`; `Authorization, Content-Type, If-Match, If-None-Match`; exposes `ETag, Save-Version`.

## Alternatives considered

1. **Duplicate `withCORS` and `validateOrigin` in `accounts` and `leaderboard/selfhost`**: avoids adding a new internal package. Rejected because origin validation rules, `Vary` headers, and preflight status handling would drift across the three self-hosted services.
2. **Allow wildcard `*` in `allowedOrigins` or default to open CORS**: simplifies initial setup for web builds. Rejected because all three services handle authenticated bearer tokens and `cloudsave` already enforces explicit origin allowlists; refusing wildcards and defaulting to no web origins keeps all three services consistent and safe by default.
3. **Add a CLI `-allowed-origins` flag to `accountsd` and `leaderboardd` instead of YAML config**: lets operators pass origins on the command line. Rejected because `cloudsaved` keeps `allowedOrigins` strictly in YAML config, and the repository's rule is that human-edited configuration lives in deterministic, validated YAML files rather than duplicated between flags and files.

## Consequences

- **Benefits**: Web builds can sign in via `accountsd`, query and submit scores via `leaderboardd`, and sync saves via `cloudsaved` from any configured origin using one consistent `allowedOrigins` YAML schema and CORS behavior. `pkg/online/internal/cors` imports only the Go standard library, preserving `leaderboard/selfhost`'s minimal dependency footprint and `accounts/token`'s isolated import graph.
- **Costs**: Adding the `AllowedOrigins []string` slice field makes `accounts.Config` no longer comparable with Go's `==` operator (matching `cloudsave.Config` and `selfhost.Config`), and operators deploying web builds on a separate origin must add that origin to `allowedOrigins` in each service's YAML config because `init` writes an empty allowlist by default.
