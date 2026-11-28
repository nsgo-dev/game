# Agent Note: Shipping guide follows game.yaml and validates store listings

Status: implemented

## Problem

The English and Chinese shipping guides told readers to edit `name`, `id`, `version` and `build.targets` directly in `nsgo.yaml`, although a `game new` project's `game.yaml` owns those fields and project tests reject disagreement. The guides also stopped at manual store uploads even though NSGo v0.3.4 provides launch screens, adaptive and themed Android icons, Apple privacy manifests, export-compliance settings, text store listings and `nsgo publish` automation.

## Decision

Keep `game.yaml` authoritative for identity, player-facing version, appearance and export settings, and make `gameproject sync` the documented write path into `nsgo.yaml`. Keep `build_number`, icons, launch screens, signing, privacy, export compliance and store-specific settings in `nsgo.yaml`. Update English and Chinese guides together and link to NSGo's building guide for the exhaustive store schema rather than maintaining a second copy. The portable target filtering and `CheckNSGo` compatibility rules remain owned by [Template release export presets, portable nsgo.yaml targets, and host-aware game release](../bug-fix/2026-11-28-templates-export-presets-and-release-host-filtering.md).

Mark the paired `game.yaml` and `nsgo.yaml` fences in both `shipping.md` pages and exercise them with `project.Parse` and `CheckNSGo` in `internal/repocheck/shipping_test.go`. Give `examples/tutorial/ch09-ship/store/` a real minimal listing with YAML and Markdown, and run `go tool nsgo publish -store all -dry-run` in the tutorial's existing CI matrix entry. Dry-run validation requires no credentials and performs no HTTP requests or uploads.

## Alternatives considered

- **Keep editing store identity directly in `nsgo.yaml`**: this matches NSGo without the game layer and avoids a synchronization command, but breaks the `game.yaml` contract and project tests, so it is rejected.
- **Copy NSGo's complete store-listing reference into the game guide**: a local reference would be convenient offline, but it would duplicate a large schema and character-limit table that can drift from the pinned NSGo release. The guide keeps the workflow and key limits and links to the upstream reference for every field.
- **Exercise real store uploads in CI**: this would cover credentials and remote API behavior, but would mutate external store records and require production secrets on documentation changes. The checked-in example uses NSGo's credential-free dry-run instead; signing and real uploads remain manual release steps.

## Consequences

Readers can change a new project's identity without breaking `CheckNSGo`, prepare the metadata NSGo already automates, and validate the tutorial listing locally and in CI. The documentation test depends on the two `shipping-project` marker comments and the immediately following YAML fences; changing those examples requires keeping both language versions synchronized. Dry-run coverage does not prove signing, store credentials, remote uploads or review submission, so the guide keeps those prerequisites explicit.

## Testing

- `go tool nsgo test ./internal/repocheck -run '^TestShippingProjectExamples$'` verifies both language examples against `CheckNSGo`.
- `(cd examples/tutorial/ch09-ship && go tool nsgo publish -store all -dry-run)` validates the tutorial listing for App Store Connect and Google Play without network access or credentials.
