# Agent Note: Website playground mobile play mode and touch drag locking

Status: implemented

## Problem

On phones, the website playground (`apps/website/src/pages/Playground.tsx`) ran every example inside a 16:10 box on a scrolling page with only `touch-action: manipulation` on `html`, always displayed the keyboard focus hint, and provided no fullscreen control. Without fullscreen, Cave Run's landscape orientation request (`cx.RequestOrientation(core.OrientationLandscape)`) was refused by mobile browsers, and iPhone Safari never supports element fullscreen on non-video elements.

Empirical Chrome mobile touch emulation (390×844, `maxTouchPoints: 5`) also confirmed a deeper NSGo v0.3.4 web issue (filed upstream as `nsgo-dev/nsgo#2660`): NSGo's generated web CSS sets `* { touch-action: pan-x pan-y; }` with no `overscroll-behavior`, `touchAction()` skips views with `OnTouch`, and `touchmove` is only prevented after an asynchronous `ItemBlockNative`. Dragging across Cave Run or SlideGem ended in `pointercancel` after two `pointermove` events in both standalone and iframe views, and scrolled the outer page by ~95px inside a scrollable page.

## Decision

- `apps/website/src/content/playground.ts` keeps `PLAYGROUND_IDS` import-free for scripts and Vite plugins, and adds `PlaygroundOrientation` (`'landscape' | 'portrait' | 'any'`), `PLAYGROUND_ENTRIES`, and `playgroundEntry(id)`: `caverun`, `cryptquest`, and `starburst` declare `'landscape'`, and `slidegem` declares `'portrait'`.
- `apps/website/src/pages/playgroundMobile.ts` holds the React-free state reducer (`mobilePlayReducer`), pure predicates (`isMobilePlayActive`, `shouldLockPageScroll`, `shouldShowRotatePrompt`, `hintKind`), and browser helpers (`enterMobilePlay`, `requestStageFullscreen`, `exitStageFullscreen`, `tryLockOrientation`, `tryUnlockOrientation`, `lockPageScroll`, `lockFrameTouch`) so every branch is unit-tested in `apps/website/src/pages/playgroundMobile.spec.ts`.
- On coarse pointers (`matchMedia('(pointer: coarse)')`), `Playground.tsx` displays **Play fullscreen** (with native `requestFullscreen` on the stage, falling back to a fixed `100dvh` overlay when fullscreen is unavailable or refused, as on iPhone Safari) and replaces the keyboard focus hint with a touch hint across all 10 locales in `apps/website/src/copy/*.ts`.
- While mobile play mode is active (`fullscreen` or `overlay`), `lockPageScroll` adds `html.playground-playing` (`overflow: hidden; overscroll-behavior: none; height: 100dvh; touch-action: none` on `html`/`body`, saving and restoring `window.scrollY`), the stage sets `touch-action: none` and `overscroll-behavior: none`, and `lockFrameTouch` injects a removable `<style id="nsgo-playground-mobile-touch">` into the same-origin game iframe so browser pan and overscroll cannot cancel thumb drags before NSGo's upstream fix lands.
- When `enterMobilePlay` enters native fullscreen, it attempts `screen.orientation.lock(orientation)`. When the lock is unavailable or refused and the screen orientation does not match the example's preferred orientation, the stage shows a rotate-your-phone overlay with a **Play anyway** dismiss button; rotating to the preferred orientation clears the dismissal.
- `pkg/input/touchcontrols.go` (`TouchControls.Attach`) attaches a `core.Responder` without `LetNative` (chaining any existing `v.Responder`) that claims touches owned by the controls and refuses termination while a finger holds a control, emitting `driver.ItemBlockNative` until `nsgo-dev/nsgo#2660` is resolved upstream.
- `docs/en/playground.md` and `docs/zh/playground.md` document phone play, fullscreen/overlay fallback, scroll locking, and per-example orientation entries.

## Alternatives considered

- **Wait for `nsgo-dev/nsgo#2660` before fixing iframe drag cancellation** — avoids injecting a `<style>` tag into the same-origin playground iframe. Rejected because SlideGem and other `OnTouch`-only examples would still suffer `pointercancel` in mobile play mode until a new NSGo release is tagged and adopted.
- **Unconditionally claim all touches in `TouchControls.Attach`** — simpler than checking `t.pad.active()`. Rejected so touches outside the virtual controls do not unexpectedly steal gestures from other views sharing the container.
- **Float an Exit button directly over the game canvas** — maximizes viewport height by ~36px. Rejected because Cave Run places its Pause button at top-right and SlideGem places its Hint/New Game buttons across the top-right; a slim safe-area top bar (`playground__mobile-bar`) keeps Exit and Restart accessible without overlapping any example's HUD.

## Consequences

- **Benefits**: phones get a full-viewport game stage on both Android (native fullscreen + orientation lock) and iPhone Safari (`100dvh` fixed overlay + rotate prompt), thumb drags in mobile play mode neither scroll the page nor cancel the pointer, and `TouchControls` emits `ItemBlockNative` on touch-down.
- **Costs and known limits**: `lockFrameTouch` relies on the playground iframe being same-origin (`/play/<id>/`); cross-origin embeds still rely on `TouchControls`'s `core.Responder` and the upstream fix in `nsgo-dev/nsgo#2660`. Standalone web tabs of `OnTouch`-only games without `TouchControls` (such as SlideGem) also wait on `nsgo-dev/nsgo#2660`.

## Verification

- `go tool nsgo test ./pkg/input` verifies `TouchControls.Attach` installs a `core.Responder` without `LetNative`, claims touches on controls only, refuses termination while held, and chains existing responders.
- `bun --cwd apps/website test --timeout 180000` runs `src/pages/playgroundMobile.spec.ts` and `src/locales.spec.ts` covering all 10 locales, orientation metadata, fullscreen vs overlay fallback, orientation lock failure and rotate prompt, scroll lock, and iframe touch lock.
- Chrome mobile touch emulation (390×844) against the built playground verifies before/after drag behavior on Cave Run and SlideGem, overlay fallback, and the upright rotate prompt for Cave Run.
