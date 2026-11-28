# Agent Note: Editor MCP object edit metadata

Status: implemented

## Problem

Resolved scene objects can come from local content, a scene instance, or an inherited base scene. Agents could read an object's YAML but could not tell which file owns it or where a change would be recorded.

## Decision

`selection_get` adds an `edit` descriptor to its selected object. `scene_read` adds an `objects` array for scene files. Both use the editor's existing `ObjectOverrideInfo` routing and expose the source scene, local scene or override path, editability, and a reason for override-backed edits.

## Alternatives considered

Inferring ownership from YAML alone would miss resolved instance and inheritance metadata. Returning only a source file would still leave agents unable to know where a local override is written, so the response includes both source and edit paths.

## Consequences

Existing text responses remain compatible while structured responses gain explicit routing metadata. Scene parsing adds a deterministic object scan to `scene_read`, and clients can avoid proposing edits against source files that are not the active edit target.
