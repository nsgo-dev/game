### Fixed

- `cmd/game mcp`: parse `install` flags with the command's own flag set so `game mcp install -h`/`--help` prints `game mcp`'s usage once and exits 0, and unknown or malformed flags after `install` print one error and one usage and exit 2 (#1617).
