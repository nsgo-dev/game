### Fixed

- `pkg/anim3d`: `LoadEventsSidecar` now requires `version: 1` (`EventsSidecarVersion`), refuses unknown fields, duplicate keys, trailing data, and unversioned flat maps with located `*loaderr.Error`s, and `LoadEventsFromGLTFExtras` / `ClipFromGLTF` now return located errors for malformed `events` or `nsgo_events` entries while still ignoring non-object or unrelated `extras` keys (#1502).
