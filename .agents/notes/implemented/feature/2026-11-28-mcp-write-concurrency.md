# Agent Note: MCP optimistic write guards

Status: implemented

## Problem

An agent could read a scene, wait while the person or another agent changed it, then apply a stale write. A transport retry could also apply the same mutating MCP call twice.

## Decision

`scene_edit` and `object_set_field` accept an optional editor `baseRevision` and a caller-supplied `requestId`. The editor goroutine checks the revision immediately before applying the edit and reserves a request ID for the lifetime of the server. A stale revision or already-reserved ID refuses the whole call. IDs are released when validation or mutation fails, so a corrected request can use a new key without poisoning the server state.

## Alternatives considered

Checking the revision in the MCP transport handler would leave a race before the editor goroutine applies the edit. Embedding a version in every scene object would change the scene format and would not cover component or property edits, so the existing editor revision is the shared optimistic token.

## Consequences

Clients must reread after a stale-write refusal and choose unique IDs for retries. Existing callers remain compatible because both fields are optional. The guard covers the two general-purpose scene mutators while specialized tools retain their existing contracts.
