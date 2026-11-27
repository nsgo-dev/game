### Added

- `pkg/online/cloudsave`, `pkg/online/leaderboard/selfhost`: hand-written OpenAPI 3.1 descriptions (`openapi.yaml`), embedded and served unauthenticated at `GET /v1/openapi.yaml`, published under `https://game.nsgo.dev/schemas/`, and enforced against each service's HTTP handler by CI contract tests (#1303).
