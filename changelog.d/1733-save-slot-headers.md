### Added

- `pkg/save`: `Store.SaveWithHeader` writes an optional JSON object header in the slot envelope alongside the data, and `Store.Headers` lists every slot's header without unmarshaling the save data, for load screens (#1733).

### Changed

- `pkg/save`: add ordered `SaveAsync` and `LoadAsync` file-backed operations with engine-goroutine callbacks and `Flush` for backgrounding and shutdown (#1733).
