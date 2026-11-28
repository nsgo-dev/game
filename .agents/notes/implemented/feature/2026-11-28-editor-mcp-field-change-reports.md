# Agent Note: Editor MCP field change reports

Status: implemented

## Problem

Editor MCP edit responses only described an operation and revision. Agents could not verify a field's previous value, tell a no-op from a change, or tell whether an inherited or instanced edit was recorded as an override (#1934).

## Decision

`scene_edit` returns one `FieldChange` per field a `set`, `move`, `add_component`, `set_property` or `remove_property` edit touches, and `object_set_field` returns its single `FieldChange`, both outside and inside an agent turn. Each record holds the value read back from the resolved scene before and after the edit, `changed`, and override metadata from `Editor.ObjectOverrideInfo`.

Values are converted through YAML and then JSON (`jsonValue` in `pkg/editor/editormcp/changes.go`): struct values such as `sprite` and `light` report the scene file's keys, which are the keys `scene_edit` takes, and the conversion copies pointer and map values, so a value read before an edit is not changed by an edit that mutates the object in place. `changed` compares the JSON encodings of the two values. An edit that sets an object's `id` reads its values after the edit from the renamed object. Values are read with `Editor.FindResolvedObject`, so an instanced or inherited object reads from the resolved scene; `scene_edit` and `object_set_field` still refuse such IDs (`applyEdit` and `objectSetField` look them up with `Editor.FindObject`, which searches only the open scene's own objects), so `override` is wired but absent until those tools accept them.

## Alternatives considered

Returning the whole scene YAML would expose unrelated data and force agents to diff YAML. Reporting only the revision would not identify no-op writes or override routing. Reporting the Go values directly would show Go field names (`Atlas`, `Frame`) for struct fields, which the agent never writes, and a pointer field captured before an in-place edit would show the edited value as `before`.

## Consequences

Responses are larger. The existing text summaries remain unchanged, while structured consumers can explain each edit and where it was stored. Not covered yet: accepting instanced and inherited object IDs in these tools, a top-level `changed` for a call whose every edit was a no-op, skipping that call's empty undo step, and receipts for `add`, `remove`, `reparent`, `move_to_layer`, `file_edit` and the tile tools.
