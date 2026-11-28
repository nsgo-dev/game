# Agent Note: MCP stale-write guard and retry key for scene writes

Status: implemented

## Problem

An agent writes from what it read seconds or minutes earlier. Outside a review turn, `scene_edit` or `object_set_field` from an external MCP client overwrites an edit the person made in between with no signal, and a client that retries a timed-out call applies the same edit twice (two duplicated objects, two added objects).

## Decision

`scene_edit` and `object_set_field` in `pkg/editor/editormcp` take two optional arguments; a call without them behaves as before.

- `baseVersion` is a scene version: the first 8 bytes of SHA-256 over the open scene as `Editor.SaveYAML` encodes it (`sceneVersion` and `contentVersion` in `write_guard.go`). `scene_read` reports it as `version` for the open scene, `selection_get` as `sceneVersion`, and both write tools report the version after the write. `checkBaseVersion` runs on the editor's goroutine immediately before the edit applies, so nothing changes the scene between the check and the write. A mismatch is an `EditError` whose problem starts with `changed since you read it` and names the current version; nothing applies. `scene_edit` refuses `baseVersion` with a `file` other than the open scene, since in-turn edits of other files are reconciled by the turn's review merge.
- `requestId` is a retry key, checked before holds and before `baseVersion`. `Server.beginWriteRequest` fingerprints the decoded arguments (SHA-256 of their JSON); a repeated call with the same key and fingerprint returns a copy of the stored result, a different fingerprint is refused, and a key whose call is still running is refused. A failed call releases its key. A call whose edit committed but whose reply failed stores a result saying it already applied, so a retry never applies twice. The server keeps the last 256 completed keys (`maxWriteRequests`), oldest forgotten first, for its lifetime, guarded by `Server.mu`.

## Alternatives considered

### `Editor.Revision` as the precondition

The editor already has a revision counter, and the first version of this change compared it. `Editor.notify` bumps it on every state change, including selection, hover, camera and panel changes, so a client would be refused after nearly any interaction by the person, and none of the read tools reported it. A content hash of the scene changes only when the scene does.

### A version per object

The issue proposes a version per object, so an edit to an unrelated object does not refuse the write. It needs a `base` map on every write tool and per-object hashes in every read. The scene-level version is a strict subset of that contract (it refuses more often, never less), covers adds and removes that have no prior object, and leaves room to add per-object versions later without changing `baseVersion`'s meaning.

### Refusing a repeated `requestId` with an error

Simpler to store only the key, but a client retrying after a lost reply then sees a failure for a write that succeeded, and with `baseVersion` it sees a stale-version conflict for its own write. Returning the stored result is what makes the retry safe.

## Consequences

- Benefits: an external client can refuse to overwrite the person's edit and retry a lost call safely, with no change for clients that pass neither argument; selection and view changes never cause a conflict.
- Costs: any edit to the open scene, including to an unrelated object, refuses a write with an older `baseVersion`; computing the version encodes the scene once per guarded write and per `scene_read` / `selection_get`. Other write tools (`file_edit`, the object menu tools, `tileset_edit`) do not take the arguments yet, and request results live in memory only, so a server restart forgets them.

## Testing

`TestSceneEditRefusesStaleBaseVersion` and `TestSceneEditRequestIDAppliesOnce` in `server_test.go` and `TestObjectSetFieldBaseVersionAndRequestID` in `inspector_test.go` cover the version staying fixed across selection and panning, a person edit between read and write being refused with the scene, history and file unchanged, the reported version making the write succeed, a retried call applying once, a reused key with other arguments being refused, and a failed call releasing its key.
