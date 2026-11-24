### Added

- `game/mcp`: add tunables and debug console MCP tools (`game_tunables_list`, `game_tunable_get`, `game_tunable_set`, `game_console_commands_list`, `game_console_exec`) configured via `Config.Console`, `Config.ConsoleFunc` and `Bridge.SetConsole` (#466).
- `game/debug`: add `Console.RegisterString`, `Console.Tunables`, `Console.Tunable`, `Console.SetTunable` and `Console.Commands` with `TunableInfo`, `TunableChange` and `CommandInfo`; `SaveTunables` and `LoadTunables` persist string tunables (#466).
