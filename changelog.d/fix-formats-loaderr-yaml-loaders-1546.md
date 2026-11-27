### Fixed

- YAML loaders across gameplay, engine, editor, online, and tooling packages now return `*loaderr.Error` with file, line, column, field path, and fix guidance while preserving existing sentinel errors, enforced repository-wide by `internal/formattest` and `internal/repocheck` (#1546).
