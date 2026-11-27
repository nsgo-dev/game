# Agent Note: Release gates on template pins and a public-proxy game new smoke test

Status: implemented

## Problem

v0.1.0 shipped templates whose `go.mod.tmpl` pinned pre-release pseudo-versions of `github.com/nsgo-dev/game` (such as `v0.0.0-20261125083919-59bc0354e207`) that proxy.golang.org and sum.golang.org answer 404 for. `createProject` in `internal/gamecli/new.go` runs `go get -tool github.com/nsgo-dev/game/cmd/game@<its own version>` in the new project, which first has to load the pinned requirement, so `go run github.com/nsgo-dev/game/cmd/game@latest new` failed for every template (#1665). Nothing in CI could see it: the `templates` job in `test.yml` renders each template inside the repository checkout, and the release workflow tested the repository at the tag, never a project made by the tag's own `game new` from the public proxy.

## Decision

`.github/workflows/release.yml` carries two jobs for this, each added as a job of its own so the release workflow's other changes merge around them.

- `template-pins` runs before publishing on every event and the `release` job needs it. It reads the `github.com/nsgo-dev/game` requirement of every tracked `go.mod.tmpl` (both the `template/` modules and their copies under `internal/gamecli/_templates/projects`), refuses a pseudo-version outright, and runs `go mod download github.com/nsgo-dev/game@<pin>` for each distinct pin outside the repository with `GOWORK=off`, `GOPROXY=https://proxy.golang.org` and `GOSUMDB=sum.golang.org`, so a pin the public proxy or checksum database cannot serve fails the release before anything is uploaded.
- `smoke` runs after `release` publishes a pushed tag (mode `release`), for `starter`, `platformer2d` and `visualnovel`. With no checkout, `GOWORK=off`, `GOFLAGS=-mod=mod`, the public proxy and checksum database and `GONOSUMDB`, `GOPRIVATE` and `GONOPROXY` unset, it waits up to six minutes for `go mod download github.com/nsgo-dev/game@<tag>` to succeed, then runs `go run github.com/nsgo-dev/game/cmd/game@<tag> new -template <name>`, checks the project requires the game module at exactly the tag, and runs `go tool nsgo test ./...` and `go tool game check` in it, as a newcomer following the README would.

A new project requires the game module at the version `game new` was built from (`buildVersion`), so the template pin is only a floor that `go get -tool` raises; but `go get` must load the floor first, which is why an unpublished floor breaks every project even though no project ends up on it. A tag `vX.Y.Z` therefore makes projects requiring `github.com/nsgo-dev/game vX.Y.Z`, and `smoke` checks that too.

The smoke job fails after the release is public. It cannot undo a broken release; it makes one visible within minutes, and the fix is the next patch tag, never moving the published tag ([release editor packages](../architecture/2026-11-27-release-editor-packages.md) records why tags are immutable).

## Alternatives considered

1. **Run the smoke test before publishing, from the pushed tag**: the module proxy serves a tag as soon as it is pushed, so `go run …@<tag> new` could gate the GitHub Release. It would still not stop the broken module version, which is public on the proxy from the push onwards, and it would put several minutes of project builds in front of every editor upload; `template-pins` already catches the cause before publishing, so the smoke test checks the whole path afterwards.
2. **A Go test in `internal/repocheck` or `internal/gamecli` that refuses pseudo-version pins**: runs on every pull request with no network, but it cannot tell whether a released version is on the proxy, and refusing pseudo-versions on `main` forbids templates from depending on unreleased game APIs between releases, a policy this change does not decide. The release-time check refuses pseudo-versions only where they break users.
3. **Smoke-test every template**: sixteen projects with their own builds and tests on each release; three that cover the starter, a 2D template and a UI-heavy template catch a broken pin, which is the same for every template, at a fifth of the cost. The `templates` job in `test.yml` already tests each template's code.

## Consequences

- **Benefits**: a pin nobody can download stops the release before the GitHub Release is created, and the first command of every documented path runs on every release exactly as a newcomer runs it.
- **Costs**: each release spends about ten runner minutes on the smoke matrix and waits on the public proxy, and a proxy or checksum database outage fails `template-pins` or `smoke` and needs a re-run.
- **Verification**: the smoke steps run against v0.1.0 reproduce #1665's failure (`reading https://proxy.golang.org/github.com/nsgo-dev/game/@v/v0.0.0-20261125083919-59bc0354e207.mod: 404 Not Found`, `game new … exit status 1`); `game new -version v0.1.0` built from `main` creates `starter`, `platformer2d` and `visualnovel` projects whose `go tool nsgo test ./...` and `go tool game check` pass against the public proxy; the `template-pins` script passes on `main` and refuses a pseudo-version pin and an unpublished `v0.0.9` pin.
