# Agent Note: Host-registered TSX modules and custom capabilities for mods

Status: implemented

## Problem

Script mods in `pkg/modding` previously could import only the single built-in `"game"` module and request only the closed 7-bit `Capability` bitmask (`readState` … `storage`). Games and runtime plugins exposing domain-specific APIs (such as inventory queries, custom dialogs, or editor extensions) had no typed module seam or host-defined capability model; they either forked `pkg/modding` or tunneled stringly-typed payloads through `getState`/`emit`. In addition, NSGo's `tsx.RegisterFunctions` writes to a process-global registry that panics once any TSX runtime has started (`started.Load() == true`) and appends duplicate exports if called twice for the same module name, which breaks multi-session managers and multi-test binaries if `NewManager` registers closures directly.

## Decision

`pkg/modding` adds host-defined capabilities (`HostCapability`, `ManagerOptions.Capabilities`) and host-registered TSX modules (`ModModule`, `ManagerOptions.Modules`, `RegisterModule`) while keeping `mod.yaml` at `manifestVersion: 1` and preserving all existing `pkg/modding` APIs (including the `tsx.Sandbox.DisableStorage` enforcement from [modding disable storage](../bug-fix/2026-11-27-modding-disable-storage.md)).

### Manifest format and capability contract

- `mod.yaml`'s `capabilities` sequence accepts both canonical built-in names (decoded into `ModManifest.Capabilities`) and dotted host capability names in `<namespace>.<name>` form (two or more dot-separated lower camel case ASCII segments validated by `ValidHostCapability`, decoded into `ModManifest.HostCapabilities`).
- `EncodeManifest` writes built-in names first in canonical `capabilityTable` order, followed by `HostCapabilities` sorted in byte order, omitting `capabilities` only when both are empty. Round trips through `ParseManifest` and `EncodeManifest` are byte-for-byte deterministic.
- Keeping `manifestVersion: 1` is intentional: older hosts reject unknown capability names in `mod.yaml`, which is the safe outcome for a mod that depends on host APIs the older host does not provide.
- Unknown host capabilities are validated at load time per mod rather than aborting `DiscoverFS`: a mod requesting a host capability absent from `ManagerOptions.Capabilities` fails alone in `StateFailed` with `ErrUnknownCapability` while sibling mods load normally.
- `tools/gameschema` publishes `apps/website/public/schemas/mod.v1.json` (`modding.SchemaURL`), reflecting `ModManifest` with `capabilities` typed as a string array so editors accept both built-in and dotted host names while skipping the Go-only `HostCapabilities` struct field (`yaml:"-"`).

### Host module registration and dispatch contract

- `NewManager` validates `ManagerOptions.Capabilities` and `ManagerOptions.Modules` up front (panicking on duplicate module names, collisions with `"game"` or NSGo's reserved `"nsgo"` prefix, invalid names, empty or non-function `Funcs`, or modules without a declared capability). A `ModModule` declares either a single `Capability` or an any-of `Capabilities` slice (for modules such as an editor plugin module whose exports span multiple permissions checked individually via `ModAPI.RequireHost`).
- Every registered host export is wrapped via `reflect.MakeFunc` into a signature-preserving trampoline (injecting `*core.Scope` before regular arguments—or after `context.Context` for async functions—when the host function omits it, which `tsx.Declarations` and `tsx.runtime.goArgs` both strip transparently).
- Each trampoline resolves the calling mod via `Caller(cx)`, looks up the owning `Manager`'s live function map (isolating state across concurrent or sequential managers in the same process), checks the module's capability gate via `ModAPI.RequireHost` (returning `*PermissionError` wrapping `ErrPermission` and `*core.Error{Code: core.CodeAccessDenied}`), and invokes that manager's closure.
- A process-wide signature registry deduplicates `tsx.RegisterFunctions` calls per module name and verifies export signature compatibility across managers. Hosts that create `Manager` instances after a TSX runtime has already started call `RegisterModule` in `init()` so NSGo's TSX registry receives the module signatures before startup, while hosts calling `NewManager` before any TSX runtime starts need no separate `init()` call.
- A mod's `tsx.Sandbox.Modules` allowlist contains `"game"` plus every host module whose capability (or any entry of `Capabilities`) the mod both requests and is granted after `ManagerOptions.GrantHost` (`nil` means no narrowing; non-nil `[]string{}` denies all) and `Manager.SetHostGrant`. Ungranted imports fail at load time, and narrowing a loaded mod via `SetHostGrant` updates `ModAPI.hostCaps` in place so the next host call fails immediately with `*PermissionError`.
- `WriteModAPIDTS(dir, opts)` writes `game.d.ts` from `GameAPIDTS` plus one `<module>.d.ts` per host module from `tsx.Declarations()`.

## Alternatives considered

- **Registering host closures directly with `tsx.RegisterFunctions` inside `NewManager`**: Simpler in code, because no `reflect.MakeFunc` trampoline is needed. Rejected because NSGo's `tsx.RegisterFunctions` appends duplicates on repeated calls and panics once any TSX runtime has started (`tsx.started`), which breaks games that recreate a `Manager` on a new session and breaks multi-test suites where an earlier test already loaded a mod; direct closure registration also leaks state between managers sharing a module name.
- **Bumping `mod.yaml` to `manifestVersion: 2` for host capabilities**: Would separate host-capability manifests by version. Rejected because `manifestVersion: 1` parsers already reject unknown capability names, so older hosts already fail cleanly on mods that need host capabilities without forcing existing manifests to migrate.
- **Reloading a mod on `SetHostGrant`**: Mirrors `SetGrant`'s sandbox rebuild for `CapAudio`/`CapStorage`. Rejected because every host module function executes in Go through the trampoline and `ModAPI.RequireHost`, so updating `ModAPI.hostCaps` in place enforces narrowed permissions on the very next call without restarting the mod or crashing a running mod into `StateFailed` when one sub-permission is revoked.

## Consequences

- **Benefits**: Games and runtime plugins can expose typed, capability-gated TSX modules without forking `pkg/modding`; ungranted imports fail deterministically at load; `SetHostGrant` enforces narrowing immediately on live mods; multiple `Manager` instances in one process stay isolated; `WriteModAPIDTS` generates up-to-date `.d.ts` files directly from Go signatures; and `mod.yaml` stays strict, deterministic, and schema-checked.
- **Costs and limits**: Because NSGo freezes `tsx`'s module registry once the first TSX runtime starts, a host that creates its first `Manager` for a new module name *after* another TSX runtime has already started must pre-register that module's signatures with `RegisterModule` in `init()`, and all managers in one process using the same module name must keep identical Go export signatures.
