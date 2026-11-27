### Fixed

- `pkg/tooling/manifest`: `game manifest -embed` now escapes glob metacharacters (`[`, `*`, `?`) in generated `//go:embed` patterns so bracketed asset names such as `hero[1].png` embed their own bytes instead of matching sibling files, and `Scan` rejects asset paths and `EmbedDir` values that Go's `//go:embed` cannot embed (#1602).
