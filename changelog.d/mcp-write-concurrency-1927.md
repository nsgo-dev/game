### Added

- `pkg/editor/editormcp`: `scene_edit` and `object_set_field` take an optional `baseVersion`, a content hash of the open scene that `scene_read`, `selection_get` and both write tools report, and refuse a write over a change made since that read without applying anything; an optional `requestId` makes a retried call return the first call's result instead of applying again (#1927).
