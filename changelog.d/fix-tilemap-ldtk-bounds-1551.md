### Fixed

- `pkg/tilemap`: `LoadLDtk`, `LoadLDtkAll`, and `LoadLDtkWorld` now validate level and layer grid dimensions, `intGridCsv` length, and dense tile allocation budgets (16 Mi cells per layer, 64 Mi total per load) with `*loaderr.Error`, fixing divide-by-zero panics and unbounded allocations from malformed `.ldtk` files (#1551).
