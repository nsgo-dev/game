### Fixed

- `pkg/editor`, `internal/gamecli`: resolve the `go` toolchain from `PATH`, standard install locations (`/usr/local/go/bin`, `C:\Program Files\Go\bin`), Homebrew, version-manager shims, and the login shell's `PATH` so Create, Play, checks, builds, and profiler work when the standalone editor is opened from Finder or a desktop launcher, and show a Start Screen notice with a `go.dev/dl` link and Retry button while disabling Create and Play when Go is missing or older than the templates require (#1668).
