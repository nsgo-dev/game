# Agent Note: Defer web startup full-screen requests until first press and track external window transitions

Status: implemented

## Problem

`screen.Video.Apply` previously called `cx.SetFullScreen(true)` immediately at startup on the web when saved `DisplaySettings.Fullscreen` was `true` and recorded `sent = true, lastFull = true`. Because browsers reject `requestFullscreen()` outside a user gesture (and NSGo v0.3.4 only logs the rejection), the page remained windowed while `Video` believed full screen had been requested and never retried. Meanwhile, `Video.Run` did not watch `Env.FullScreen()`, so leaving full screen via Escape or browser/OS controls left `Video.Settings()` and `gameui.SettingsMenu` stale, requiring two toggles to re-enter full screen and writing stale `Fullscreen: true` on unrelated saves. Phone web browsers (such as iPhone Safari, which lacks an element full-screen API) also displayed a non-functional Window Mode row in `SettingsMenu` (`nsgo-dev/game#1482`).

## Decision

Keep browser gesture deferral and external window tracking in `pkg/screen/video.go`, and wire platform capability and window-state synchronization into `pkg/gameui/settings_menu.go` (partially overlapping with [ready-to-use SettingsMenu](../feature/2026-11-24-gameui-settings-menu.md)):

- **Web startup deferral (`pkg/screen/video.go`)**: on the initial `applyWindow` (`!v.sent`) when `Env.OS() == core.OSWeb`, `Fullscreen == true`, and `Env.FullScreen() == false`, `Video` does not call `SetFullScreen`; it marks `pending = true` and records `sent = true, lastFull = false`. Calling `Video.ApplyPending()` inside a press/touch/key handler—or wiring a view with `Video.Attach(*core.View)`—flushes the pending request during the user's first gesture. Applying `Fullscreen: false` clears `pending`, and subsequent interactive `Apply` calls with `Fullscreen: true` (such as clicking Window Mode in a settings menu) send `SetFullScreen(true)` immediately in one press.
- **External window state tracking (`Video.Run` and `Video.OnWindowChanged`)**: `Video.Run` subscribes an effect to `cx.Env().FullScreen()`. Requests initiated by `Video` record `awaitingFull` and `desiredFull` so the matching platform transition does not fire `OnWindowChanged`. Any transition outside a `Video` request updates `v.settings.Fullscreen`, `v.lastFull`, clears `v.pending`, and invokes `v.OnWindowChanged(v.settings)` untracked so menus and saved preferences follow the page.
- **Platform capability (`screen.CanFullScreen`) and `SettingsMenuConfig.Env`**: `screen.CanFullScreen(env)` returns `false` on `core.OSIOS`, `core.OSAndroid`, and `core.OSWeb` with `core.IdiomPhone` (until NSGo exposes a dedicated capability in `nsgo-dev/nsgo#2690`), and `true` otherwise (including `nil` env and unset `OS` in tests). When `SettingsMenuConfig.Env` is supplied, `SettingsMenu` omits the Window Mode row when `!screen.CanFullScreen(env)`, initializes `Display.Fullscreen` from `env.FullScreen()` on `OSWeb` so it never lies while a startup request is deferred, and reflects subsequent `env.FullScreen()` transitions without clobbering unapplied user edits.

## Alternatives considered

- **Drop saved full-screen restoration on the web altogether (like PlayCanvas/Babylon.js)**: avoids pending state in `Video`, but games that restore saved `DisplaySettings` at startup on desktop would silently lose the player's preference on the web instead of honoring it on the first press.
- **Reset `Video.Settings().Fullscreen` to `false` immediately during startup deferral on the web**: would make `Video.Settings()` match the windowed page before any press without needing `SettingsMenuConfig.Env`, but mutating the applied setting before the first press breaks `Apply` → `Settings()` consistency and risks erasing the player's saved preference if another setting is saved before the first canvas click.
- **Wire gesture detection inside `pkg/input.Actions` instead of `screen.Video.Attach` / `ApplyPending`**: couples `pkg/screen` to `pkg/input` and misses games or menus that use native `core.View` press handlers without `input.Actions`. `Video.Attach(*core.View)` follows the same view-chaining pattern as `input.Actions.Attach` while keeping `pkg/screen` independent of `pkg/input`.

## Consequences

- **Benefits**: Web builds never trigger ungesture-backed `requestFullscreen()` rejections at start; saved full screen activates on the first user press; Escape and browser/OS full-screen changes update `Video.Settings()`, `OnWindowChanged`, and `SettingsMenu` so toggling Window Mode always works in one press; iOS, Android, and phone web browsers hide the unusable Window Mode control.
- **Costs**: Games on the web must call `Video.Attach(&gameView)` or `Video.ApplyPending()` in a press handler to flush deferred startup full screen, and pass `Env: cx.Env()` to `SettingsMenuConfig` to get platform hiding and automatic window-mode syncing. Phone web capability detection uses `OSWeb` + `IdiomPhone` as an interim heuristic until `nsgo-dev/nsgo#2690` lands.
