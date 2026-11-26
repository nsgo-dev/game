# Agent Note: Checking pending agent Go changes via Go build overlay (`GOFLAGS=-overlay`)

Status: implemented

## Problem

Issue #1034 introduces file-by-file review for Go code written by an agent alongside YAML/prefab changes (`docs/design/editor/ReviewCode.dc.html` and `Review.dc.html`). Two invariants must hold simultaneously:

1. **Disk and Play stay on the old code until the person confirms and keeps.** Go edits proposed during an agent turn must never overwrite project files on disk before `nsgo check` and `nsgo test` pass and the author confirms each Go file and presses Keep.
2. **Checks run against the pending version through `nsgo check` and `nsgo test`.** Both modified existing `.go` files and brand-new `_test.go` files must be vetted and tested as a coherent package, with live per-test progress and structured failure messages (`file:line`), without copying the entire repository or mutating working-tree files.

This extends the agent panel and MCP server established in [editor-agent-connect](../../implemented/architecture/2026-11-26-editor-agent-connect.md) and [editor-mcp-server](../../implemented/architecture/2026-11-25-editor-mcp-server.md), and contrasts with [editor-changes-panel](./2026-11-26-editor-changes-panel.md), which runs `nsgo check` and `nsgo test` directly against files already on disk.

## Decision

### Staged in-memory Go files + temporary Go build overlay

- `Editor.ProposeCodeReviewFiles` (and `editormcp`'s `code_review_propose`) snapshots any pre-existing file from disk into `CodeReviewFile.Original` once and stores the agent's proposed version exclusively in `CodeReviewFile.Pending`. Nothing is written to disk until `Editor.KeepCodeReview` runs.
- `BuildGoOverlay` writes each pending non-deleted `.go` file to a temporary directory and generates a standard Go `-overlay` JSON (`{"Replace": {origPath: stagedPath}}`, mapping deleted files to `""`). Because macOS temp and project paths frequently resolve through symlinks (`/var` → `/private/var`, `/tmp` → `/private/tmp`), every pending file maps both its cleaned `filepath.Abs` path and its `filepath.EvalSymlinks` path.
- `nsgo check` and `nsgo test` do not expose an explicit `-overlay` flag, but NSGo's `cmd/nsgo` passes `os.Environ()` (including `GOFLAGS`) through to `go list`, `go vet`, and `go test -json`. `runDefaultCodeReviewChecks` therefore passes `-overlay=<overlay.json>` via `GOFLAGS` (`withOverlayGOFLAGS`, preserving other `GOFLAGS` tokens and ensuring the overlay directory path contains no whitespace because `GOFLAGS` is space-delimited) to:
  1. `go tool nsgo check <pkg>` for each affected package derived from the pending Go file paths.
  2. `go tool nsgo test -json <pkg> -count=1` (with `-count=1` placed after `<pkg>` so `nsgo test`'s flag parser forwards it to `go test`), streaming JSON events line by line to report live `"nsgo test running · M of N"` progress and extract exact `TestName` and `file:line: message` failures.
- On mobile/web (`code_review_runner_other.go`), `BuildGoOverlay` and the default runner return explicit desktop-only errors so `-target ios` builds cleanly. `Editor.CodeReviewCheckRunner` remains injectable for deterministic tests.

### Off-UI-goroutine execution for MCP tools and async UI checks

- `PrepareCodeReviewCheckRun`, `ExecuteCodeReviewCheckRun`, and `ApplyCodeReviewCheckRun` split check execution into a UI-thread snapshot, an off-UI-thread overlay + subprocess run, and a generation-guarded UI-thread commit (`EditGen`). Both `RunCodeReviewChecksAsync` and `editormcp`'s `code_review_propose` / `code_review_run_checks` use this split so long `nsgo check`/`nsgo test` runs never stall the editor UI and stale results from an earlier round can never unlock `ConfirmCodeReviewFile`.

### Per-file confirmation, atomic Keep, and single-step Undo

- `ConfirmCodeReviewFile` refuses confirmation with `ErrCodeReviewChecksNotPassed` unless `Checks.AllPassed(sess.EditGen)` holds (`CheckPassed && TestPassed && Phase == passed && Generation == EditGen`). Any new Go edit in Round 2 bumps `EditGen`, clears prior Go confirmations, and stays inside the same `AgentTurn` (`PreserveTurn`).
- `KeepCodeReview` gathers all unapplied YAML files plus all **confirmed** Go files and executes one `codeReviewApplyCmd` (`fallibleCommand`) through `Editor.History`. `do()` writes files atomically with rollback on partial failure; one `Editor.Undo()` restores original bytes for modified files and deletes newly created files (and empty directories created for them).
- If unconfirmed Go files remain when Keep runs, Keep writes only the YAML (and any already-confirmed Go) files, closes `CodeReviewView`, and leaves `CodeReviewSession` alive with `KeptYAML = true` so `AgentPanelView` renders the waiting card naming those files and their play impact with a button to resume directly on them.

## Alternatives considered

### Copy the whole project directory into a temporary clone before running checks

Cloning the project directory avoids overlay path subtleties and would also isolate YAML files read via `os.ReadFile` by tests. It was rejected because game projects contain large binary assets (textures, audio, glTF models, baked navmeshes) where copying the tree on every Round 1/Round 2 edit is slow and disk-heavy, whereas a Go build overlay copies only the handful of pending `.go` files in milliseconds.

### Write pending Go files directly to disk and revert them on Cancel or Play

Writing to disk directly lets plain `nsgo check` and `nsgo test` run with no overlay plumbing. It was rejected because it violates the core invariant that Play and the project on disk continue running the old code while Go changes are unconfirmed, and an unexpected crash or external tool read would observe unreviewed agent code on disk.

### Add a `-overlay` flag to `nsgo check` and `nsgo test` upstream first

Adding an explicit `-overlay` CLI flag in `nsgo-dev/nsgo` would avoid `GOFLAGS` whitespace constraints. It was not required to ship #1034 because `nsgo@v0.3.4` already forwards `GOFLAGS=-overlay=...` to both `go vet` (in `nsgo check`) and `go test -json` (in `nsgo test`) and handles both modified and newly added `_test.go` files cleanly. If a future project path requires spaces in the overlay temp path on Windows, adding a first-class `-overlay` flag in `nsgo-dev/nsgo` remains a straightforward follow-up.

## Consequences

- **Benefits**: Pending Go files—including brand-new `_test.go` files—are vetted and tested with real `nsgo check` and `nsgo test -json` while keeping disk and Play untouched; Confirm cannot be bypassed by stale or failing results; Round 2 fixes stay inside the same turn with line-level tags; Keep and Undo are atomic across YAML and confirmed Go files; keeping YAML with unconfirmed Go leaves a clear Agent panel waiting card.
- **Costs**: Go's `-overlay` applies to Go source files and `//go:embed` files, not arbitrary runtime `os.ReadFile` calls on pending YAML files that have not been kept to disk yet; tests that load pending YAML from disk see the on-disk YAML until those YAML changes are kept. `GOFLAGS` is space-delimited, so `BuildGoOverlay` must place its temp directory in a whitespace-free path.

## Testing

- `packages/game/editor/code_review_test.go`: verifies real Go build overlay execution (failing Round 1 test, disk untouched, Confirm refused, Round 2 fix in same turn with `round 2` tag, passing checks, Confirm + Keep + single-step Undo/Redo), Keep with unconfirmed Go writing YAML only and leaving the Agent panel waiting card, Unified/Split diff layout, external editor open, Pause/Resume, Revert all, and real macOS `nsgotest` screenshots in Dark and Light themes.
- `packages/game/editor/editormcp/code_review_test.go`: end-to-end MCP test exercising `code_review_propose`, `code_review_status`, and `code_review_run_checks` over an in-memory MCP transport with a real Go build overlay from Round 1 failure through Round 2 fix, confirmation, Keep, and single-step Undo.
