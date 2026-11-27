### Fixed

- `pkg/datatable`: `Registry.Register` and `MatchPattern` validate every non-`**` segment of a normalized file pattern (naming the offending segment on syntax errors) instead of testing only against a single-segment sample path, and `Registry.Match` panics on an unexpected pattern error rather than silently treating it as no match (#1609).
