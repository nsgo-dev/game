### Added

- `pkg/editor/editormcp`: `selection_get` reports the selected object's `edit`, its origin (`local`, `instanced` or `inherited`), the scene file that defines it, and for an instanced or inherited object the overrides entry an edit to it writes (`recordsIn`); `scene_read` with `objects` lists the same for every object of the open scene (#1929).
