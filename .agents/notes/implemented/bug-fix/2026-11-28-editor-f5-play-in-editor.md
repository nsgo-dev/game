# Agent Note: Keep CmdPlayStop (mod+p / f5) on Play-in-editor with a project open

Status: implemented

## Problem

`CmdPlayStop` (`project.play`, bound to `mod+p` and `f5`) registers `e.TogglePlay()` as its handler, and both the header Play button and `Project > Play` run Play-in-editor. However, `Editor.dispatchBoundShortcut` in `pkg/editor/commands.go` carried a hidden special-case branch that diverted `CmdPlayStop` to `e.RunGame()` whenever a project was open and `!e.Playing()`, and silently dropped the second press while `e.GameRunning()` was true (`nsgo-dev/game#1610`). Because `CmdRunGame` already owns `mod+r`, keyboard shortcuts diverged from the command registry, menu bar, and toolbar, and pressing `mod+p` or `f5` a second time could not stop what the first press started.

## Decision

Remove the `CmdPlayStop` override from `dispatchBoundShortcut` so keyboard dispatch executes the registered command handler unconditionally:

- **Single source of truth for Play / Stop (`pkg/editor/commands.go`)**: `mod+p` and `f5` always invoke `CmdPlayStop.Run` (`e.TogglePlay()`), starting Play-in-editor on the first press and stopping it on the second whether or not a project is open, matching the header `toolbar-play` button, `Project > Play`, and the Command Palette ([unified command registry](../feature/2026-11-25-editor-shortcuts-menu-palette.md)).
- **Explicit external run binding**: `CmdRunGame` (`mod+r`) stays the keyboard shortcut for launching `go tool game dev` via `RunGame` when a project is open (with `shift+f5` stopping the running game process), and the docs (`docs/en/editor.md`, `docs/zh/editor.md`), locale labels (`toolbar.runLabel`), and [editor game runner note](../feature/2026-11-25-editor-run-log-panel.md) reflect those bindings.

## Alternatives considered

- **Keep `f5` launching `RunGame()` via a separate command or binding**: gives Godot-style `F5` external launch, but splits `CmdPlayStop` (`mod+p` vs `f5`) or conflicts with users expecting `F5` and `⌘P` to toggle the primary Play-in-editor preview advertised on the header button. Removing the hidden branch honors the existing command registry (`mod+p` / `f5` for Play / Stop, `mod+r` for Run Game) without introducing a second shortcut path.
- **Make the second `mod+p` / `f5` press call `StopGame()`**: fixes the silent no-op on the second press, but still leaves `mod+p` and `f5` executing a completely different action from `CmdPlayStop.Run`, the header Play button, and `Project > Play`.

## Consequences

- **Benefits**: Keyboard shortcuts, the header Play button, `Project > Play`, and the Command Palette all run `TogglePlay()` consistently with or without a project open; `mod+r` remains the clear, documented shortcut for launching `go tool game dev`.
- **Costs**: Muscle memory expecting `F5` to spawn `go tool game dev` must use `mod+r` (or **Project > Run Game** / the **Run** button) instead.
