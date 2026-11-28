# Agent Note: Editor MCP object origin and override routing in reads

Status: implemented

## Problem

A resolved scene holds objects its own file defines, objects a scene instance adds and objects its base scene adds. An agent reading the scene's YAML sees only the first kind, and learns that an edit to an instanced or inherited object is recorded as an override only by making it, often after guessing wrong and editing the source scene instead. The editor already routes these edits through `Editor.ObjectOverrideInfo` (`pkg/editor/overrides.go`), but nothing in `pkg/editor/editormcp` exposed it. Issue #1929, part of #1923.

## Decision

`pkg/editor/editormcp` exposes the routing as `ObjectEdit`, computed by `objectEdit` from `Editor.ObjectOverrideInfo` on the editor goroutine, in the same callback that reads the selection or the scene text, so the routing and the text describe one revision:

- `selection_get` sets `SelectedObject.Edit` for the selected object.
- `scene_read` takes an optional `objects` flag. With it, a read of the open scene (no `file`, or `file` naming the open scene) sets `FileText.Objects` to an `ObjectEdit` for every object of the resolved scene (`Editor.Built.Doc` when the build succeeded, else `Editor.Doc`), in layer and child order. With it on any other file the call is refused, since `ObjectOverrideInfo` describes only the open scene and any other file's objects are all its own.

`ObjectEdit` carries `id`, `origin` (`local`, `instanced` or `inherited`, from `ObjectOrigin.String`), `sourceScene` (the defining scene for an instanced or inherited object, or the scene a local instance object instances) and, for an instanced or inherited object only, `recordsIn`: `file` (the open scene), `instance` (the local object whose `overrides` hold the entry, empty for an inherited object) and `overridesKey`. The instance and the key are separate fields because object IDs contain `/` and the two kinds of entry live in different places (`ObjectDoc.Overrides` on the instance object versus `SceneDoc.Overrides`), which a single dotted path cannot tell apart.

## Alternatives considered

- **Listing objects on every `scene_read` of a scene file by parsing its text**: the first version of this change did that. Parsing the text finds only the file's own objects, never the instanced and inherited ones the routing matters for, and it attached the open scene's routing to same-named objects of other files. It also grew every scene read, which the issue asked to keep behind an option.
- **An `editable` flag with refusal reasons (playing, sketch hold, task holds, locks, permission rules)**: this is the `can` block of #1929. It needs one helper in `pkg/editor` shared with the inspector so the two cannot drift; a flag computed only here would be true for objects whose writes are then refused, so it is left out rather than reported wrongly.

## Consequences

- **Benefits**: an agent can see before editing that an object is instanced or inherited and which overrides entry an edit writes, from the same function the inspector uses for its instance bar and override routing.
- **Costs**: the default `scene_read` output is unchanged, so an agent must ask for `objects`; `selection_get` grows by one small object. The capability part of #1929 (`can` with reasons, `heldBy`) is not delivered and remains open on the issue.
