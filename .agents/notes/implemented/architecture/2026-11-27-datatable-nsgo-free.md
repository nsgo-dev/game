# Agent Note: `datatable` is NSGo-free; hot reload lives in `datatablewatch`

Status: implemented

## Problem

A dedicated server builds without NSGo (`docs/en/server.md` promises the plain Go toolchain and no NSGo runtime), but it needs its data tables, and `pkg/datatable` could not be imported headlessly: `Table.Watch` and `Table.WatchWithError` imported `pkg/tooling/hotreload`, which links `github.com/nsgo-dev/nsgo/core` and `github.com/nsgo-dev/nsgo/gpu`. `go list -deps ./pkg/datatable` therefore pulled 36 `github.com/nsgo-dev/nsgo/...` packages (35 with `GOOS=linux CGO_ENABLED=0`) — GPU, UI and desktop and mobile platform packages — into a headless Linux binary. Nothing tested the boundary, so the documented property depended on what each package happened to import.

## Decision

`Table.Watch` and `Table.WatchWithError` move from `pkg/datatable` to a new package `pkg/datatable/datatablewatch` as functions `datatablewatch.Watch(tbl, watcher, fn)` and `datatablewatch.WatchWithError(tbl, watcher, fn)`. The moved code uses only `Table`'s exported API (`Path`, `ReloadBytes`, `Rows`), so no new exports were needed. `pkg/datatable` keeps loading, saving and typed `Table` state and now depends only on `pkg/loaderr` and `pkg/jsonschema`, neither of which touches NSGo.

`pkg/datatable/datatablewatch` sits under its companion package rather than `pkg/tooling` because it is the optional dev-time half of `datatable` itself; the [repository layout](2026-11-27-repository-layout.md) rules cover top-level placement, and nested packages share their parent's label.

A dependency-boundary test at the repository root (`deps_test.go`) runs `go list -deps` on the headless packages and fails when any of them pulls in `github.com/nsgo-dev/nsgo`. The package list is one variable, so another headless package joins it later by appending a pattern.

## Alternatives considered

- **Build-tag stub inside `datatable`** — A `//go:build !release` file holding `Watch` keeps the methods and avoids a new package. Rejected because it does not fix the dependency: `hotreload`'s own release stub also imports `nsgo/core` and `nsgo/gpu` for its `Watcher` type, so even `go list -tags release -deps` still links NSGo, and a headless server build would need the tags to stay broken-free.
- **Decouple `hotreload` from NSGo first** — A watcher that posts through an injected callback instead of `core.Scope` would let `datatable` keep its methods. Rejected as out of scope: the watcher is engine-integrated dev tooling (shader, atlas and asset reloads all go through `core.Scope`), and moving two generic functions is the smallest change that restores the documented headless property.

## Consequences

- **Benefits**: `go list -deps ./pkg/datatable` lists no `github.com/nsgo-dev/nsgo/` package on darwin or on `GOOS=linux CGO_ENABLED=0`; a dedicated server links its tables without GPU, UI or platform code. The boundary is now enforced by a test, so a future import of `hotreload` in `datatable` fails CI instead of silently breaking the server promise.
- **Costs and known limits**: `datatable` is Experimental (`docs/en/stability.md`), so the method-to-function move is an allowed break, but every caller of `Table.Watch` changes shape (`tbl.Watch(w, fn)` becomes `datatablewatch.Watch(tbl, w, fn)`). The dependency test shells out to `go list`, so it needs the `go` binary on PATH like the other toolchain tests. If another headless package appears, it must be appended to the same list or the guarantee rots again.

## Verification

`go list -deps ./pkg/datatable | grep nsgo-dev/nsgo` is empty; `go list -deps ./pkg/datatable/datatablewatch` still lists the 36 NSGo packages, proving the watcher carries them. `TestHeadlessPackagesDoNotDependOnNSGo` at the repository root pins the boundary for `./pkg/online/server/...`, `./pkg/datatable` and `./pkg/jsonschema`. The two hot-reload tests moved to `datatablewatch` and pass (`TestTableHotReloadAndErrorRecovery`, `TestYAMLTableHotReload`).
