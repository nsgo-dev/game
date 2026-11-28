# Agent Note: Per-destination publish records

Status: implemented

## Problem

Store uploads from the Build and Release screen and the person-only steps that follow them existed only in editor memory (`ReleaseUploadItem`, `ReleaseUploadReviewSnapshot` in `pkg/editor/build_release_publish.go`). Restarting the editor lost which version and build number went to which destination, the artifact that was sent and where it landed, so the next release had to be reconstructed from the store consoles (#1930).

## Decision

The editor writes one strict, versioned YAML record per project and destination at `.game/publish/<destination>.yaml`, beside `.game/editor.yaml`, under the `.game` folder projects leave out of version control. `pkg/editor/publish_record.go` owns the format and the state model:

- `PublishRecord` holds `version: 1`, the destination, `lastUpload` (version, `buildNumber` from `nsgo.yaml`, artifact name, SHA-256, size, RFC 3339 upload time, result URL, outcome) and `confirmations` (step ID, the version a per-version step applies to, confirmation time). It never holds a credential.
- `LoadPublishRecord` refuses unknown fields, a missing or unsupported `version` and a record whose destination does not match its file; a missing file is an empty record. `SavePublishRecord` marshals in struct field order and writes a `0600` temp file in the same directory that is then renamed over the record, so an interrupted write leaves the previous record readable. A load and save round trip changes nothing.
- `DefaultPublishSteps` lists the ordered steps: `account` (confirmed, per app), `upload` (checked, per version), `testers`, `review` and `release` (confirmed, per version).
- `RecordUpload` streams the artifact through SHA-256, replaces `lastUpload` and drops every per-version confirmation; per-app ones stay. `ConfirmPublishStep` refuses a checked or unknown step and a per-version step before any upload, and scopes a per-version confirmation to the last uploaded version.
- `PublishRecord.Status` derives `done` or `todo` per step, the current step, and proposes the next version and build number as a bump of the last component of the last upload's; with no upload it proposes nothing rather than a guessed `1.0.0`.
- `executeConfirmedReleaseUploads` updates the record after each successful upload. A record that cannot be read or written does not turn the successful upload into a failure; the item's status message says the record was not saved.

## Scope

This is the record and its state model only. The Build and Release screen does not yet show the status, nothing in the editor calls `ConfirmPublishStep`, there is no `release_publish_status` editormcp tool and no agent-actor refusal for confirmations, the agent skill has no publishing reference, and checked steps do not read nsgo-dev/nsgo#2623 yet. Those remain open on #1930.

## Alternatives considered

Keeping state in the project's `game.yaml` or `nsgo.yaml` would make it travel with the project, but it would put machine-local store progress into files people review and commit, and every upload would dirty the working tree. A JSON sidecar would be as easy to decode, but the editor's own state (`.game/editor.yaml`) already uses strict, versioned YAML with deterministic field order, and one format for `.game` keeps the loaders alike.

## Consequences

The editor keeps upload history across sessions and can derive a proposed next version and build number from it instead of guessing. The record is local to one checkout: two machines publishing the same game each keep their own, and syncing them is out of scope. Store APIs are not queried, so steps a person confirms are only as accurate as the person's confirmation. Hashing the artifact after the upload reads it once more from disk.
