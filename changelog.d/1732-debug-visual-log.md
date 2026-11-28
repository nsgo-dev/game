### Added

- `gamedebug.VLog` (`pkg/tooling/gamedebug`) is a bounded visual log of entity messages, `gamedebug.Category` snapshots and debug shapes, each tagged with its fixed step, filtered with `Entries` and `Shapes`, saved as versioned JSON Lines with `WriteJSONL` and read back strictly with `gamedebug.ReadVLog`; under `-tags release` it compiles to no-ops (#1732).
