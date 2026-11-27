### Fixed

- `game/editor`: removed the hidden `CmdPlayStop` override in `dispatchBoundShortcut` so `mod+p` and `f5` consistently toggle Play-in-editor even when a project is open (matching the header Play button, `Project > Play`, and Command Palette), leaving `mod+r` (`CmdRunGame`) to launch `go tool game dev` (#1610).
