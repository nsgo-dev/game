### Fixed

- `game new`: every template pins `github.com/nsgo-dev/game` v0.1.0, a version on the public module proxy, so a project made with `go run github.com/nsgo-dev/game/cmd/game@latest new` builds; v0.1.0's own templates pinned pre-release pseudo-versions the proxy and checksum database never served. The release workflow now refuses a template pin that is a pseudo-version or that the proxy cannot serve before publishing, and after publishing creates, tests and checks a starter, platformer2d and visualnovel project with the released `game new` against the public proxy (#1665).
