# Agent Note: Editor passing-feedback toasts, Spec channel table, and queue runtime

Status: implemented

## Problem

The scene editor previously reported passing feedback (copied text, save failures, export progress, external disk reloads) through ad-hoc `Editor.Status` strings or inline panel notices. Although [editor Spec light theme](./2026-11-26-editor-spec-light-theme.md) introduced `SpecToastCard` styling and [editor focus rings and F6 regions](./2026-11-26-editor-focus-ring-and-regions.md) registered `RegionToast`, the editor lacked a canonical Spec channel API, a `ToastHandle` lifecycle for progress-to-success transitions and cancellation, Spec auto-dismiss timers with hover/focus/background pausing, 180 ms / 120 ms motion with `ReduceMotion`, full-window fallback placement when no viewport is visible, focused-only `Esc` dismissal with focus restoration, and routing of existing save/export/copy/disk events.

## Decision

The editor implements a single Spec passing-feedback toast runtime (`packages/game/editor/toasts.go`, `packages/game/editor/spec_feedback_ui.go`) driven by `Editor.EmitToastEvent` and backed by `SpecFeedbackChannels()` matching `docs/design/editor/Spec.dc.html` and `docs/design/editor/Toasts.dc.html`.

### Spec channel routing and API

- `SpecFeedbackChannels()` encodes the 12-row Feedback channels table so every event has one canonical home (`toast`, `progress_toast`, `agent_card`, `review`, `problems`, `system_notification`); decisions never route to a toast.
- `Editor.EmitToastEvent(ToastEvent) *ToastHandle` resolves the Spec tone, default verb action, platform shortcut hint (`⌘Z` / `Ctrl+Z`), timeout (`ToastDuration`), and deterministic deduplication ID (`toast:<kind>[:<target>]`).
- `*ToastHandle` supports `SetProgress`, in-place `Complete` (turning a running progress toast into a `ToastSuccess` toast and starting its 4 s / 6 s timer), in-place `Fail` (turning into a persistent `ToastError` with `Retry`), `Cancel` (invoking `onCancel` at most once and dismissing), and `Dismiss`.
- Existing editor events route through `EmitToastEvent`:
  - Explicit saves (`CmdSave` / `⌘S`, `saveFromUI`, `saveTabByIDFromUI`, `saveAllFromUI`) emit `ToastEventSaveFailed` with a working `Retry` action and clear it on success; failed autosaves remain strictly in the status bar per [editor autosave status](./2026-11-26-editor-autosave-status.md) and `Checkpoints.dc.html`.
  - `StartExportBuild` in `export_panel.go` emits `ToastEventExportRunning` with `Cancel` (wired to `CancelExportBuild`), streams percent updates via `SetProgress`, transitions on success to `"Exported <path>"` with platform file-manager reveal (`Show in Finder` / `Show in Explorer` / `Show in Files`), transitions on failure to `ToastEventExportFailed` with `Retry`, and dismisses silently when canceled by the user.
  - Clipboard copy helpers (`CopyConnectInstallCommand`, `CopyWebBuildURL`, `CopyPRDescription`, `CopyObjects`, `CopyObjectYAML`, `CopyFirstRunDiagnostics`) emit `ToastEventCopied` (`ToastInfo`).
  - External disk reloads (`setReloadNotice` in `disk_watch.go`) emit `ToastEventDiskChanged` (`ToastWarning` with `Show`).

### Queue, placement, timing, motion, and focus contracts

- **Single visible slot & FIFO ordering**: `Editor.toasts` shows index 0 only and renders `+N` when additional entries wait. Duplicate IDs merge in place at their existing index so nothing ever jumps ahead of an active `ToastError` or `ToastProgress`.
- **Two mutually exclusive hosts**: `ToastView` renders inside the viewport 8 px above the bottom-center viewport bar while `Editor.viewportVisibleOnScreen()` is true; `WindowToastView` renders at the bottom center of the window, 24 px up, whenever a full-window screen (Start, Settings, Export, Cutscene, Changes, Merge Conflict, etc.) covers the viewport. Switching between hosts completes exit immediately so duplicate toast nodes never coexist.
- **Deterministic timing and pause**: `toastRuntime` binds a single-shot `cx.AfterFunc` timer driven by `cx.Now()`. `ToastDuration` enforces 4 s (success/info), 6 s (success/info with action), 8 s (warning), and 0 (error/progress stay indefinitely). Pointer hover, touch, keyboard focus inside `RegionToast`, or `cx.Env().Lifecycle() != core.LifecycleActive` pauses the remaining duration without losing progress.
- **Motion and accessibility**: `core.PresenceOf` + `core.Motion` animate 180 ms enter (opacity `0 -> 1`, translateY `8 -> 0`) and 120 ms exit (opacity `1 -> 0`), bypassing both under `ReduceMotion`. Hosts carry `core.RoleStatus` and `core.LiveRegionPolite`; `toastEscapeKeyView` binds `Escape` strictly while `focusRegionController.toastFocused` is true and calls `restoreFocusFromToast()` to return keyboard focus to the pre-toast control.

### NSGo v0.3.4 gaps (`nsgo-dev/nsgo#2507`)

`ui.Toaster` (`ui/toast.go`) and `base/toast.Manager` (`base/toast/manager.go`) cannot drive this contract directly because:
1. `Manager.Visible(1)` is LIFO and ticks hidden entries instead of FIFO queuing behind slot 0;
2. `Manager.Push` moves duplicate IDs to the newest position and has no rule keeping errors or running progress in front, nor a waiting count;
3. `Toaster.tick` pauses only on pointer hover/touch (`expanded`), not keyboard focus or non-active window lifecycle;
4. `ui.Toasts` mounts in a full-window `core.Overlay` with `core.RoleAlert`, global `Escape`, and fixed Sonner 400 ms / 200 ms motion.

Tracked upstream in `https://github.com/nsgo-dev/nsgo/issues/2507` and linked from `toastRuntime` in `packages/game/editor/toasts.go`.

## Alternatives considered

- **Wrapping `ui.Toaster{Limit: 1}` directly**: rejected because its LIFO `Visible(1)` replaces active errors/progress when a newer info toast arrives, ticks hidden timers while queued, and cannot pause on keyboard focus or background lifecycle without bypassing `base/toast.Manager` entirely.
- **Routing failed autosaves through `ToastEventSaveFailed`**: rejected because `docs/design/editor/Checkpoints.dc.html` explicitly keeps failed autosaves in the status bar and never in a toast; only explicit user saves (`⌘S` / Save) emit the persistent save-error toast.
- **Dismissing toasts on global `Esc`**: rejected because global `Esc` conflicts with canceling drags, closing popovers, and clearing viewport selection; binding `Esc` only while `RegionToast` holds focus preserves editor shortcuts and makes focus restoration deterministic.

## Consequences

- **Benefits**: One consistent Spec-token toast surface and single channel registry across the editor; deterministic manual-clock tests for timing, pausing, motion, and focus restoration; real cancelable export progress and retryable explicit save failure flows.
- **Costs**: The editor maintains a small standalone queue/timer state machine (`toastRuntime`) until `nsgo-dev/nsgo#2507` lands upstream; future features (review keep/revert, agent stop `⌘.`, permission rule saved migration) should emit through `Editor.EmitToastEvent` rather than adding parallel toast views.

## Verification

- `(cd packages/game && go run ./cmd/gameschema -check)`
- `(cd packages/game && go tool nsgo check -target macos,windows,linux,android,web && go tool nsgo check -target ios,ios_simulator)`
- `(cd packages/game && go tool nsgo test ./editor/...)`
- `(cd packages/game && go tool nsgo test -target macos -run TestToastsMacOSScreenshots ./editor/editorcmd)`
- `go run ./tools/changelog check && bun run verify-notes`
