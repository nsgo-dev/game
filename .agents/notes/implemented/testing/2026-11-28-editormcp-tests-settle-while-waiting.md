# Agent Note: Settle the app while an editormcp test waits on a tool call

Status: implemented

## Problem

`TestDefaultTaskRunnerBuiltInAgentHoldsPermissionsAndSeparateReviews` and `TestPlanProposeQuestionsEditRemoveApproveAndTurnReviewOverMCP` failed on macOS CI with `timed out waiting for b2 to block on slime_02`, `timed out waiting for b1 to acquire slime_02` and `timed out waiting for plan_propose decision`. Raising the task runner's wait from 5 to 30 seconds did not help, so the tests were not slow: they were stuck.

Both build the server with `Options{Run: app.Post}` (or a scope's `Post`), so every `Server.Editor` call queues its function on the nsgotest app and waits for it. `nsgotest.App.Post` runs queued functions only inside a step (`Settle`, `WaitUntil`, input steps). The tests then waited in a plain `select` on a channel, and the event behind that channel needs at least one more editor call:

- the hold-blocked hook fires after `acquireObjectHolds` publishes the waiting state through `SetTaskHoldState` (`pkg/editor/editormcp/holds.go`);
- every tool result waits for `saveFailureWarning` (`pkg/editor/editormcp/server.go`), which reads the save status on the editor goroutine after the tool body; this was added with autosave on 2026-11-26 and made the plan test start to fail the same way;
- task b1's second model request, which closes `task1Held`, follows its `scene_edit` result.

The test passed only when the agent goroutine happened to post its editor calls while the preceding `Settle` was still looping; on the macOS runners it often did not, and the queued call then sat there until the timeout. The 10-minute hang after the first failure came from `defer srv.Close()`: the fake model server's handler for task 1 blocks until the test releases it, and a failed test never did.

## Decision

`awaitSettling` (`pkg/editor/editormcp/fixture_test.go`) receives from a channel while it settles the app every 5 ms, failing the test after the timeout. A test whose server hands editor calls to an nsgotest app or scope waits through `awaitSettling` (or `App.WaitUntil`) and never through a plain `select`. Tests that build the server with `Options{}` run editor calls headless on the caller's goroutine and do not need it.

The default task runner test releases task 1 through a `sync.Once` registered with `t.Cleanup` after `srv.Close`, so a failure releases the handler before the server closes, and its wait bound drops back to 10 seconds.

## Alternatives considered

- **Fire the hold-blocked hook before the editor call in `acquireObjectHolds`**: rejected because it fixes only one of the three waits; the b1 wait and the plan decision still depend on editor calls after the event, and in the app the editor loop always runs, so the product order is correct.
- **Raise the timeouts further**: rejected; the waits fail because nothing runs the queued call, not because it is slow.

## Consequences

- The waits no longer depend on the scheduler: the queued editor calls run as soon as they are posted.
- A real hang still fails within the bound, and the default task runner test fails within seconds instead of hanging until the test binary's 10-minute timeout.
- A new test that waits on a plain `select` with `Run: app.Post` can reintroduce the flake; nothing checks for it mechanically.

## Verification

- `go tool nsgo test ./pkg/editor/editormcp -run 'TestDefaultTaskRunnerBuiltInAgentHoldsPermissionsAndSeparateReviews|TestPlanProposeQuestionsEditRemoveApproveAndTurnReviewOverMCP' -count=100 -cpu=1,2,8 -race`
