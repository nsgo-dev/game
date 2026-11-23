### Changed

- `game/replay`: recordings are YAML text (format version 2) with a header and one line per changed step that names its actions and axes, so they can be read, diffed and edited by hand; unknown keys and names are refused, output is deterministic, and the gzip and compact JSON forms (`EncodeGzip`, `DecodeGzip`, version 1) are removed (#476).
