### Added

- `game/editor`: added the multi-size, multi-language HUD and menu layout screen (`HUDLayoutView`, `OpenHUDScreen`, `SetHUDLocale`, `HUDOverlays`, `HUDTextWidths`, `HUDProblems`) rendering `*.screen.yaml` simultaneously across Phone 19.5:9 with notch, Tablet 4:3, Desktop 16:9, and Ultrawide 21:9 using Spec tokens, with live `gameui.CheckScreen` diagnostics, 1.5× localized text overflow zoom, undoable anchor/safe-area/fit-text/spacing/order edits, and direct touch-control dragging written back to the screen file in one undo step (#1047).
