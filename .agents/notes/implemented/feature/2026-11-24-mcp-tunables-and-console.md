# Agent Note: MCP tunables and debug console commands

Status: implemented

## Problem

Coding agents connected to a running game via `game/mcp` (introduced in [MCP scene, layer, entity and component inspection](2026-11-24-mcp-scene-entity-inspection.md)) can inspect scenes, layers, entities and components, but cannot read or adjust live tuning variables (`debug.Console` tunables) or invoke registered debug console commands without driving the interactive console UI view. Meanwhile, `debug.Console` only exposed text-formatted `list`/`help` output through `Exec`, which mutates command history and output entries and hides structured type/range/default metadata.

## Decision

Add structured inspection and typed mutation methods to `packages/game/debug.Console` (`RegisterString`, `Tunables`, `Tunable`, `SetTunable`, `Commands`, backed by `TunableInfo`, `TunableChange` and `CommandInfo` in `debug/types.go` with release-build stubs in `debug/console_release.go`) and expose five new MCP tools in `packages/game/mcp/console.go`: `game_tunables_list`, `game_tunable_get`, `game_tunable_set`, `game_console_commands_list` and `game_console_exec`, wired into `Bridge` via `Config.Console`, `Config.ConsoleFunc`, `Bridge.SetConsole` and `Bridge.Tools()`.

## Tool contract and design

- **Minimal `mcp.go` hookup**: All tool constants, schemas, handlers and result structs live in `packages/game/mcp/console.go` so parallel MCP tool PRs (#462–#469) touch only `Config`, setter methods and `Bridge.Tools()` in `mcp.go`.
- **Side-effect-free structured queries**: `game_tunables_list`, `game_tunable_get` and `game_console_commands_list` query `debug.Console.Tunables()`, `Tunable(name)` and `Commands()` directly under `Console.mu` without appending to `Console.History()` or `Console.Entries()`. Results are sorted deterministically by name, and numeric bounds include `0` explicitly (using non-nil `any` values so `omitempty` only omits absent bounds on `bool`/`string` tunables). Registration captures each tunable's initial `*ptr` value as `Default`.
- **Strict typing and range handling in `game_tunable_set`**: Arguments use `DisallowUnknownFields()` and decode `value` strictly against the tunable's registered type (`float`, `int`, `bool` or `string`), explicitly rejecting `null`, non-finite floats, fractional numbers for `int`, and mismatched JSON kinds. Out-of-range numeric values fail by default without mutating the bound pointer; passing `"clamp": true` clamps into `[min, max]`, including an integer beyond `int`'s range, and reports `"clamped": true`. `game/mcp` only decodes the JSON value into the tunable's Go type; `Console.SetTunable(name, value, clamp)` checks the type and range, clamps, writes and snapshots the result under one lock, so a tunable re-registered between the tool's lookup and its write is never clamped with stale bounds. Responses always return both `old_value` and applied `value`.
- **Structured console execution in `game_console_exec`**: Tool-level errors (unconfigured `Console`, unknown JSON fields, missing/empty `command`) return MCP handler errors. Command execution delegates to `debug.Console.Exec` and returns `{command, ok, output, lines, error}` so agents receive both normalized output lines and structured command failure diagnostics (`ok: false`, `error: "..."`) without crashing or conflating command syntax errors with transport/schema errors.
- **Release build compatibility**: `TunableInfo`, `TunableChange` and `CommandInfo` live in untagged `debug/types.go`, and `debug/console_release.go` provides matching zero-cost stubs (`SetTunable` returns an error there, since nothing is registered and a nil error would claim a write that never happened) so `-tags release` builds of both `game/debug` and `game/mcp` compile cleanly.

## Alternatives considered

### Parsing `Console.Exec("list")` and `Console.Exec("help")` inside `game/mcp`

Building the MCP tools solely on `debug.Console.Exec` would avoid adding methods to `packages/game/debug`. It was rejected because `Exec("list")` and `Exec("help")` mutate interactive console history and output logs on every read, conflate built-in and custom commands in formatted text, omit registration defaults, and couple `game/mcp` to human-readable string formatting.

### Returning MCP handler errors when `Console.Exec` returns an error

Propagating command errors from `game_console_exec` directly as Go `error` values would match `game_tunable_set`. It was rejected because console commands are interactive shell-like operations whose result payload explicitly carries `{ok, output, lines, error}` so agents can distinguish valid console execution failures (unknown command, bad command arguments) from MCP call errors (missing console, invalid tool schema) while still inspecting any captured output.

### Silently clamping out-of-range values in `game_tunable_set` by default

Always clamping numeric writes to `[min, max]` would prevent range errors during automated tuning sweeps. It was rejected because `debug.Console.Exec("set ...")` and `Console.LoadTunables` strictly refuse out-of-range values without mutating variables; preserving strict validation by default and offering opt-in `"clamp": true` with `"clamped": true` in the response keeps behavior consistent while still supporting clamping when desired.

## Consequences

- **Benefits**: Agents can discover registered tunables and commands, change live physics/gameplay parameters with immediate observable effects, and run custom debug commands with structured output and clear errors. `debug.Console` also gains string tunables and structured inspection APIs usable outside MCP.
- **Costs**: `packages/game/debug` adds five exported methods (`RegisterString`, `Tunables`, `Tunable`, `SetTunable`, `Commands`) and three structs (`TunableInfo`, `TunableChange`, `CommandInfo`) that must stay synchronized between `console.go` and `console_release.go`.

## Verification

Verified with `gofmt`, `go run ./cmd/gameschema -check`, `go tool nsgo check -target macos,windows,linux,android,web`, `go tool nsgo test ./mcp`, `go tool nsgo test ./debug`, `go tool nsgo test -tags release ./debug`, `go tool nsgo test -run TestStabilityTable .`, `go run ./tools/changelog check`, and `bun run verify-notes`.
