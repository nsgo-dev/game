# Agent Note: Editor regions no longer rebuild on every notify

Status: implemented

## Problem

Since live problems ([live problems](../feature/2026-11-26-editor-live-problems-agent-fix.md)), the editor notifies from background work: a debounced project check runs about 80 ms after every edit, and the disk watcher triggers another one, each finishing with `notify()` on the UI goroutine whenever it completes. Two regions were built by a `core.Dynamic` that read `rev` during the build, so every notify, including one that changed nothing they show, disposed and rebuilt them:

- the layer inspector (`layerInspectorView`): `layerTilemapSection` called `layer()`, which reads `rev`, while building, so the whole layer inspector, its rows, and the add-tilemap form's signals were rebuilt;
- the disk conflict banner (`diskWatchBannerView`): its `Dynamic` read `rev` directly.

A press is a press-down, press-up and press sequence, and the UI settles between them. A check finishing in between remounted the button, so the release and the press reached a view that no longer existed and were dropped. `TestLayerInspectorRepeatYOverrideDotAndRevert` failed every run, and `TestDiskConflictReloadAndKeepMine` failed whenever a check was still in flight when the banner appeared (`after Keep mine: bar=true dirty=true`). Authors were affected the same way: a click could be lost, and a half-filled add-tilemap form was reset.

## Decision

- `layerTilemapSection` reads the layer with `core.Untracked(cx, layer)`. The enclosing inspector `Dynamic` already rebuilds when the tilemap's shape changes, because `inspectorSchemaKey` includes `layerTilemapSchemaTag`; the controls inside keep their own tracked bindings.
- `diskWatchBannerView` builds from a `core.NewMemoFunc` over `diskBannerState` (primary conflict, compare open, reload notice, diff open) compared with `reflect.DeepEqual`. Its `Dynamic` reads only the memo, so the banner rebuilds only when what it shows changes.
- Rule for editor UI: code that runs while a `core.Dynamic` builds must not read `rev` (or call a helper that does) unless the region really has to be rebuilt on every edit. Derive a comparable key with a memo, as `inspectorSchemaKey` does, and read the live values inside bindings.

## Testing

- `TestLayerInspectorKeepsRowsAcrossProblemCheck` forces a check with `CheckProblems` and asserts that the layer inspector's views keep their node IDs.
- `TestDiskConflictReloadAndKeepMine` waits for the in-flight check with `WaitForProblems` and asserts that the Keep mine button keeps its node ID before pressing it. It sets `AutoSaveDelay` to an hour so that the 2 s autosave introduced by autosaving kept changes cannot save the dirty scene between steps on a slow runner.

## Alternatives considered

- Wait for problem checks in the tests before every press: the tests would pass, but the lost click and the reset form would remain for authors.
- Stop notifying when a check finishes: the status bar, the scene tree badges and the viewport overlays need that notify to show the results.

## Consequences

- Live problem checks, autosave and other background notifies no longer rebuild these two regions. Other `Dynamic` regions that read `rev` while building still rebuild on every notify; the rule above applies to them when they are next changed.
- The banner compares its state with `reflect.DeepEqual` on every notify. The state is a few strings and at most one entry per changed object, so the comparison is cheap.
