# Agent Note: Go toolchain discovery and Start Screen gating for launcher-opened editor

Status: implemented

## Problem

Standalone **NSGo Game Editor** installs opened from macOS Finder, Dock, Spotlight, or a Linux desktop launcher inherit a minimal system `PATH` (for example `/usr/bin:/bin:/usr/sbin:/sbin` on macOS) that excludes `/usr/local/go/bin` (configured via `/etc/paths.d`, which `launchd` does not read), `/opt/homebrew/bin`, and version-manager shims (`mise`, `asdf`). Because every project creation, Play, asset build, export, check, playtest, remote MCP, and profiler command executed bare `exec.Command("go", ...)` with the inherited environment, the first click on Create or Play failed with `exec: "go": executable file not found in $PATH`, and machines without Go received no actionable guidance (#1668).

## Decision

Introduce a shared internal package `internal/gotoolchain` and wire every editor and CLI `go` subprocess through it, paired with a Start Screen toolchain notice and Create/Play gating in `pkg/editor`:

- **Shared discovery (`internal/gotoolchain`)**: `Resolve` / `ResolveMin` probes once per process (and `Refresh` / `RefreshMin` invalidates and re-probes on user Retry) in three stages: current `PATH`, standard Go install and shim directories (`/usr/local/go/bin`, `/opt/homebrew/bin`, `/usr/local/bin`, `GOROOT/bin`, `mise`/`asdf`/`gvm`, `~/go/bin`, Linux `/snap/bin` and `/usr/lib/go/bin`; Windows `GOROOT\bin`, `%ProgramFiles%\Go\bin`, `%LOCALAPPDATA%\Programs\Go\bin`, Scoop, `mise`, `~/go/bin`, `C:\Go\bin`), and a cached Unix login-shell `PATH` fallback (`$SHELL -lc 'printf %s "$PATH"'`, 1.5 s timeout).
- **Resolved binary + enriched `PATH` on every exec site**: `exec.Command("go", ...)` looks up `"go"` in the parent process's `PATH` before inspecting `cmd.Env`, so `gotoolchain.Command` / `CommandContext` passes the resolved executable path directly AND sets `cmd.Env` with the resolved directory prepended to `PATH` so nested `go tool nsgo` / `go tool game` invocations succeed too. Every `go` exec site in `pkg/editor`, `pkg/editor/editormcp`, and `internal/gamecli` uses `gotoolchain`, while `RunGameSpec.Name` stays `"go"` for API/test compatibility with `startGameProcess` resolving via `gotoolchain.BinaryForEnv`.
- **Template version check**: `internal/gamecli.templateMinGoVersion()` reads the `go` directive from all embedded project template `go.mod.tmpl` files and configures `Editor.SetMinGoVersion`, comparing the discovered `go version` output using standard `go/version.Compare`.
- **Start Screen notice and disabled actions**: When `!ed.GoToolchainOK()`, `StartScreenView` displays a warning notice (`start-go-notice`) above the form with `"NSGo Game Editor needs Go <minVersion> or later"`, a `go.dev/dl` link button (`Editor.OpenGoDownloadPage`), and a `Retry` button (`Editor.RetryGoToolchainCheck`). Create (`start-create`), Start with AI (`start-ai-btn`), Play (`toolbar-play`, `toolbar-run`), and `CmdRunGame` are disabled and their programmatic entry points return `ErrGoToolchainUnavailable` until Retry finds a compatible Go.

## Alternatives considered

- **Reuse `pkg/editor/local_agent_detect.go` directly without a shared package**: Keeping discovery private inside `pkg/editor` would leave `internal/gamecli` (`createProject`'s `runGo` and `isStdPackage`, plus `runGoIn`) unable to share it without creating a circular or ad-hoc API. Placing discovery in `internal/gotoolchain` lets `pkg/editor`, `pkg/editor/editormcp`, and `internal/gamecli` share one implementation and one cache.
- **Mutate the editor process's own `os.Setenv("PATH", ...)` at startup**: Modifying global process `PATH` at startup is harder to test cleanly in parallel unit tests, cannot cleanly support re-probing after a user installs Go and clicks Retry without mutating global state, and risks race conditions with concurrent goroutines reading `os.Environ()`. Enriching per-command `Env` from a mutex-protected cache avoids mutating global process state.

## Consequences

- **Benefits**: Standalone desktop installs launched from Finder, Dock, Spotlight, or desktop menus reliably find Go across official installers, Homebrew, version managers, and login-shell setups; newcomers without Go (or with an older Go than the templates require) immediately see what is needed, where to download it, and can Retry without restarting the editor.
- **Costs**: On Unix systems where Go is genuinely absent from both `PATH` and standard install directories, the first toolchain check invokes the user's login shell once (bounded by a 1.5 s timeout). Non-desktop targets (`!canRunGameDesktop`) skip local `go` probing by default so Web/mobile editor previews do not show an irrelevant install banner.
